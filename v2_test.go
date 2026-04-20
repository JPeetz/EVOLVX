// v2.0 tests across multitrader, memory, attribution, and compaction packages.
package v2_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/attribution"
	"github.com/NoFxAiOS/nofx/compaction"
	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/memory"
	"github.com/NoFxAiOS/nofx/multitrader"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: SharedJournalHub routes from multiple traders to one journal
// ─────────────────────────────────────────────────────────────────────────────

func TestHubRoutesMultipleTraders(t *testing.T) {
	dir := t.TempDir()
	j, err := journal.New(filepath.Join(dir, "journal.db"))
	require.NoError(t, err)
	defer j.Close()

	hub := multitrader.NewSharedJournalHub(j)

	// Register 3 traders
	for i, id := range []string{"trader-a", "trader-b", "trader-c"} {
		err := hub.Register(multitrader.TraderRegistration{
			TraderID:        id,
			StrategyID:      fmt.Sprintf("strategy-%d", i),
			StrategyVersion: "1.0.0",
			Name:            fmt.Sprintf("Trader %s", id),
		})
		require.NoError(t, err)
	}

	require.Len(t, hub.ActiveTraders(), 3)

	// Each trader writes a decision
	for i, trader := range []string{"trader-a", "trader-b", "trader-c"} {
		err := hub.Record(&journal.DecisionEntry{
			DecisionID:      fmt.Sprintf("d-%d", i),
			StrategyID:      fmt.Sprintf("strategy-%d", i),
			StrategyVersion: "1.0.0",
			Symbol:          "BTCUSDT",
			Timestamp:       time.Now(),
			Mode:            core.ModePaper,
			Action:          core.ActionOpenLong,
			Confidence:      80,
		})
		require.NoError(t, err)
		_ = trader
	}

	// All 3 decisions must be queryable from one journal
	entries, err := hub.Query(journal.QueryFilter{Symbol: "BTCUSDT", Limit: 10})
	require.NoError(t, err)
	require.Len(t, entries, 3, "all 3 trader decisions must be in the shared journal")
}

func TestHubSubscribersReceiveEvents(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "journal.db"))
	defer j.Close()

	hub := multitrader.NewSharedJournalHub(j)

	received := make(chan *journal.DecisionEntry, 5)
	hub.SubscribeDecisions(&captureSubscriber{ch: received})

	hub.Record(&journal.DecisionEntry{
		DecisionID: "d-sub", StrategyID: "s1", StrategyVersion: "1.0.0",
		Symbol: "ETHUSDT", Timestamp: time.Now(), Mode: core.ModePaper,
		Action: core.ActionOpenShort,
	})

	select {
	case e := <-received:
		require.Equal(t, "ETHUSDT", e.Symbol)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("subscriber did not receive event within 500ms")
	}
}

func TestHubDeregisterRemovesTrader(t *testing.T) {
	j, _ := journal.New(filepath.Join(t.TempDir(), "j.db"))
	defer j.Close()
	hub := multitrader.NewSharedJournalHub(j)

	hub.Register(multitrader.TraderRegistration{TraderID: "t1", StrategyID: "s1"})
	hub.Register(multitrader.TraderRegistration{TraderID: "t2", StrategyID: "s2"})
	require.Len(t, hub.ActiveTraders(), 2)

	hub.Deregister("t1")
	require.Len(t, hub.ActiveTraders(), 1)
	require.Equal(t, "t2", hub.ActiveTraders()[0].TraderID)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: SymbolStore builds and serves consolidated memory
// ─────────────────────────────────────────────────────────────────────────────

func TestSymbolStoreConsolidatesAcrossStrategies(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "j.db"))
	defer j.Close()

	hub := multitrader.NewSharedJournalHub(j)
	store, err := memory.NewSymbolStore(filepath.Join(dir, "memory.db"), hub)
	require.NoError(t, err)
	defer store.Close()

	// Two strategies trade BTCUSDT with different outcomes
	for i := 0; i < 5; i++ {
		hub.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("win-%d", i), StrategyID: "s1",
			StrategyVersion: "1.0.0", Symbol: "BTCUSDT",
			Timestamp: time.Now(), Mode: core.ModeLive, Action: core.ActionOpenLong,
			Outcome: &journal.Outcome{Class: journal.OutcomeWin, RealizedPnL: 20, ReturnPct: 0.02},
		})
	}
	for i := 0; i < 3; i++ {
		hub.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("loss-%d", i), StrategyID: "s2",
			StrategyVersion: "1.0.0", Symbol: "BTCUSDT",
			Timestamp: time.Now(), Mode: core.ModeLive, Action: core.ActionOpenShort,
			Outcome: &journal.Outcome{Class: journal.OutcomeLoss, RealizedPnL: -15, ReturnPct: -0.015},
		})
	}

	// Rebuild from journal to pick up all decisions
	time.Sleep(100 * time.Millisecond) // let goroutine notifications settle
	err = store.RebuildFromJournal(j)
	require.NoError(t, err)

	mem := store.Get("BTCUSDT")
	require.NotNil(t, mem)
	require.Equal(t, 8, mem.TotalDecisions)
	require.Equal(t, 2, mem.ContributingStrategies, "both strategies should be counted")
	require.Greater(t, mem.TotalPnL, 0.0, "net PnL should be positive (5 wins of 20 - 3 losses of 15 = 55)")
}

func TestSymbolStoreFormatPromptContext(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "j.db"))
	defer j.Close()
	hub := multitrader.NewSharedJournalHub(j)
	store, _ := memory.NewSymbolStore(filepath.Join(dir, "m.db"), hub)
	defer store.Close()

	// Write 10 winning decisions to trigger prompt context
	for i := 0; i < 10; i++ {
		j.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("w%d", i), StrategyID: "s1",
			StrategyVersion: "1.0.0", Symbol: "SOLUSDT",
			Timestamp: time.Now(), Mode: core.ModeLive, Action: core.ActionOpenLong,
			Outcome: &journal.Outcome{Class: journal.OutcomeWin, RealizedPnL: 10, ReturnPct: 0.03},
		})
	}
	store.RebuildFromJournal(j)

	ctx := store.FormatPromptContext("SOLUSDT")
	require.NotEmpty(t, ctx, "should produce context when >= 5 closed decisions exist")
	require.Contains(t, ctx, "SOLUSDT")
	require.Contains(t, ctx, "100%", "10/10 wins = 100% win rate")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Attribution engine produces correct PnL breakdown
// ─────────────────────────────────────────────────────────────────────────────

func TestAttributionEngineByStrategy(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "j.db"))
	defer j.Close()
	hub := multitrader.NewSharedJournalHub(j)

	// Strategy 1: 10 trades, all wins, +100 PnL
	for i := 0; i < 10; i++ {
		hub.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("s1-d%d", i), StrategyID: "strategy-1",
			StrategyVersion: "1.0.0", Symbol: "BTCUSDT",
			Timestamp: time.Now(), Mode: core.ModeLive, Action: core.ActionOpenLong,
			Outcome: &journal.Outcome{Class: journal.OutcomeWin, RealizedPnL: 10, ReturnPct: 0.01},
		})
	}
	// Strategy 2: 10 trades, all losses, -50 PnL
	for i := 0; i < 10; i++ {
		hub.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("s2-d%d", i), StrategyID: "strategy-2",
			StrategyVersion: "1.0.0", Symbol: "ETHUSDT",
			Timestamp: time.Now(), Mode: core.ModeLive, Action: core.ActionOpenShort,
			Outcome: &journal.Outcome{Class: journal.OutcomeLoss, RealizedPnL: -5, ReturnPct: -0.005},
		})
	}
	time.Sleep(100 * time.Millisecond)

	engine := attribution.NewEngine(hub)
	from := time.Now().Add(-1 * time.Hour)
	to   := time.Now().Add(1 * time.Hour)
	report, err := engine.Compute(from, to)
	require.NoError(t, err)

	require.Equal(t, 20, report.TotalTrades)
	require.InDelta(t, 50.0, report.TotalPnL, 0.01, "100 wins - 50 losses = 50 total")
	require.Len(t, report.ByStrategy, 2)
	require.Len(t, report.BySymbol, 2)

	// strategy-1 should have positive PnL share
	var s1, s2 *attribution.StrategyAttribution
	for i := range report.ByStrategy {
		if report.ByStrategy[i].StrategyID == "strategy-1" {
			s1 = &report.ByStrategy[i]
		} else {
			s2 = &report.ByStrategy[i]
		}
	}
	require.NotNil(t, s1)
	require.NotNil(t, s2)
	require.InDelta(t, 100.0, s1.PnL, 0.01)
	require.InDelta(t, -50.0, s2.PnL, 0.01)
	require.InDelta(t, 2.0, s1.PnLShare, 0.01, "100/50 = 2.0 (strategy-1 earned 2x the total)")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Compaction policy selects correct retention per strategy status
// ─────────────────────────────────────────────────────────────────────────────

func TestCompactionPolicyRetentionByStatus(t *testing.T) {
	policy := compaction.DefaultPolicy()

	tests := []struct {
		status   registry.StrategyStatus
		expected int
	}{
		{registry.StatusApproved, 180},
		{registry.StatusPaper, 60},
		{registry.StatusDraft, 14},
		{registry.StatusDeprecated, 7},
	}

	for _, tc := range tests {
		days := policy.RetentionDays("any-strategy-id", tc.status)
		require.Equal(t, tc.expected, days, "status %s should retain %d days", tc.status, tc.expected)
	}
}

func TestCompactionWorkerSkipsStrategiesWithFewTrades(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "j.db"))
	defer j.Close()

	reg, _ := registry.New(filepath.Join(dir, "reg.db"))
	defer reg.Close()

	// Create a strategy with only 5 decisions (below MinTradesToCompact=20)
	r, _ := reg.Create(&registry.StrategyRecord{
		Name: "tiny-strategy", Author: "test",
		Parameters: registry.Parameters{MaxPositions: 1},
	})
	for i := 0; i < 5; i++ {
		ts := time.Now().AddDate(0, 0, -60) // old enough to compact
		j.Record(&journal.DecisionEntry{
			DecisionID: fmt.Sprintf("d%d", i), StrategyID: r.ID,
			StrategyVersion: r.Version, Symbol: "BTCUSDT",
			Timestamp: ts, Mode: core.ModeBacktest, Action: core.ActionHold,
		})
	}

	policy := compaction.DefaultPolicy()
	policy.MinTradesToCompact = 20
	worker := compaction.NewWorker(j, reg, policy, time.Hour)

	result := worker.RunOnce()
	require.Equal(t, 0, result.StrategiesCompacted,
		"strategy with only 5 decisions should not be compacted (min is 20)")
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

type captureSubscriber struct {
	ch chan *journal.DecisionEntry
}

func (c *captureSubscriber) OnDecision(e *journal.DecisionEntry) {
	c.ch <- e
}

var _ = fmt.Sprintf
var _ = os.Remove
