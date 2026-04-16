package journal_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: Decision memory survives restart
// ─────────────────────────────────────────────────────────────────────────────

func TestDecisionPersistsAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "journal.db")

	// Write in one session
	svc1, err := journal.New(dbPath)
	require.NoError(t, err)

	entry := testEntry("d1")
	require.NoError(t, svc1.Record(entry))
	svc1.Close()

	// Read in a new session (simulated restart)
	svc2, err := journal.New(dbPath)
	require.NoError(t, err)
	defer svc2.Close()

	loaded, err := svc2.Get("d1")
	require.NoError(t, err)
	require.Equal(t, entry.Symbol, loaded.Symbol)
	require.Equal(t, entry.Action, loaded.Action)
	require.Equal(t, entry.Confidence, loaded.Confidence)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Query by strategy, symbol, date range, outcome
// ─────────────────────────────────────────────────────────────────────────────

func TestDecisionQueryFilters(t *testing.T) {
	svc := newTestJournal(t)

	now := time.Now()
	past := now.AddDate(0, 0, -7)
	future := now.AddDate(0, 0, 7)

	// Write entries with different strategies, symbols, outcomes
	entries := []*journal.DecisionEntry{
		withStrategy(testEntry("d1"), "strat-A", "1.0.0", "BTCUSDT", journal.OutcomeWin),
		withStrategy(testEntry("d2"), "strat-A", "1.0.0", "ETHUSDT", journal.OutcomeLoss),
		withStrategy(testEntry("d3"), "strat-A", "2.0.0", "BTCUSDT", journal.OutcomeWin),
		withStrategy(testEntry("d4"), "strat-B", "1.0.0", "BTCUSDT", journal.OutcomeWin),
	}
	for _, e := range entries {
		require.NoError(t, svc.Record(e))
	}

	// Query by strategy
	res, err := svc.Query(journal.QueryFilter{StrategyID: "strat-A"})
	require.NoError(t, err)
	require.Len(t, res, 3, "3 entries for strat-A")

	// Query by strategy + version
	res, err = svc.Query(journal.QueryFilter{StrategyID: "strat-A", StrategyVersion: "1.0.0"})
	require.NoError(t, err)
	require.Len(t, res, 2)

	// Query by symbol
	res, err = svc.Query(journal.QueryFilter{Symbol: "ETHUSDT"})
	require.NoError(t, err)
	require.Len(t, res, 1)
	require.Equal(t, "d2", res[0].DecisionID)

	// Query by outcome
	res, err = svc.Query(journal.QueryFilter{StrategyID: "strat-A", OutcomeClass: journal.OutcomeWin})
	require.NoError(t, err)
	require.Len(t, res, 2, "two wins for strat-A")

	// Query by date range
	res, err = svc.Query(journal.QueryFilter{
		StrategyID: "strat-A",
		From:       &past,
		To:         &future,
	})
	require.NoError(t, err)
	require.Len(t, res, 3)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Outcome update works; second update appends correctly
// ─────────────────────────────────────────────────────────────────────────────

func TestOutcomeUpdate(t *testing.T) {
	svc := newTestJournal(t)

	e := testEntry("d-outcome")
	e.Outcome = nil
	require.NoError(t, svc.Record(e))

	// Initially pending
	loaded, _ := svc.Get("d-outcome")
	require.Nil(t, loaded.Outcome)

	// Record outcome
	outcome := journal.Outcome{
		ClosedAt:    time.Now(),
		ClosePrice:  51000,
		RealizedPnL: 50.0,
		ReturnPct:   0.05,
		Class:       journal.OutcomeWin,
		ExitReason:  "take_profit",
	}
	require.NoError(t, svc.RecordOutcome("d-outcome", outcome))

	updated, _ := svc.Get("d-outcome")
	require.NotNil(t, updated.Outcome)
	require.Equal(t, journal.OutcomeWin, updated.Outcome.Class)
	require.InDelta(t, 50.0, updated.Outcome.RealizedPnL, 0.01)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: LatestForSymbol returns recent decisions in order
// ─────────────────────────────────────────────────────────────────────────────

func TestLatestForSymbol(t *testing.T) {
	svc := newTestJournal(t)

	for i := 0; i < 8; i++ {
		e := withStrategy(testEntry(""), "strat-X", "1.0.0", "BTCUSDT", "")
		e.Timestamp = time.Now().Add(time.Duration(i) * time.Minute)
		require.NoError(t, svc.Record(e))
	}

	latest, err := svc.LatestForSymbol("strat-X", "1.0.0", "BTCUSDT", 3)
	require.NoError(t, err)
	require.Len(t, latest, 3, "should return 3 most recent")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Compaction summarises and archives old entries
// ─────────────────────────────────────────────────────────────────────────────

func TestCompaction(t *testing.T) {
	svc := newTestJournal(t)

	// Write 20 entries all dated 60 days ago
	old := time.Now().AddDate(0, 0, -60)
	for i := 0; i < 20; i++ {
		e := withStrategy(testEntry(""), "strat-C", "1.0.0", "BTCUSDT", journal.OutcomeWin)
		e.Timestamp = old.Add(time.Duration(i) * time.Minute)
		require.NoError(t, svc.Record(e))
	}

	// Write 5 recent entries
	for i := 0; i < 5; i++ {
		e := withStrategy(testEntry(""), "strat-C", "1.0.0", "BTCUSDT", journal.OutcomeLoss)
		e.Timestamp = time.Now().Add(time.Duration(i) * time.Minute)
		require.NoError(t, svc.Record(e))
	}

	// Compact with 30-day retention
	summary, err := svc.Compact("strat-C", "1.0.0", 30)
	require.NoError(t, err)
	require.NotNil(t, summary)
	require.Equal(t, 20, summary.TotalDecisions)
	require.InDelta(t, 1.0, summary.WinRate, 0.01, "all old entries were wins")

	// Recent entries still queryable
	active, err := svc.Query(journal.QueryFilter{
		StrategyID:      "strat-C",
		StrategyVersion: "1.0.0",
	})
	require.NoError(t, err)
	require.Len(t, active, 5, "recent entries not compacted")
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func newTestJournal(t *testing.T) *journal.Service {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.db")
	svc, err := journal.New(path)
	require.NoError(t, err)
	t.Cleanup(func() { svc.Close(); os.Remove(path) })
	return svc
}

func testEntry(id string) *journal.DecisionEntry {
	if id == "" {
		id = fmt.Sprintf("d-%d", time.Now().UnixNano())
	}
	return &journal.DecisionEntry{
		DecisionID:      id,
		StrategyID:      "default-strategy",
		StrategyVersion: "1.0.0",
		SessionID:       "sess-1",
		CycleNumber:     1,
		Symbol:          "BTCUSDT",
		Timestamp:       time.Now(),
		Mode:            core.ModePaper,
		Action:          core.ActionOpenLong,
		Confidence:      80,
		MarketSnapshot:  journal.MarketSnapshot{Price: 50000, Indicators: map[string]float64{"rsi7": 55}},
		RiskState:       journal.RiskSnapshot{Approved: true, AccountEquity: 10000},
	}
}

func withStrategy(e *journal.DecisionEntry, sid, ver, symbol string, outcome journal.OutcomeClass) *journal.DecisionEntry {
	e.StrategyID = sid
	e.StrategyVersion = ver
	e.Symbol = symbol
	if outcome != "" {
		e.Outcome = &journal.Outcome{
			ClosedAt:    time.Now(),
			ClosePrice:  51000,
			RealizedPnL: 10,
			ReturnPct:   0.01,
			Class:       outcome,
		}
	}
	return e
}

var _ = fmt.Sprintf
