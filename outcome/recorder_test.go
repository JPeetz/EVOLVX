package outcome_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/outcome"
	"github.com/stretchr/testify/require"
)

func TestOutcomeRecordedAutomatically(t *testing.T) {
	dir := t.TempDir()
	j, err := journal.New(filepath.Join(dir, "journal.db"))
	require.NoError(t, err)
	defer j.Close()

	rec, err := outcome.NewRecorder(j, filepath.Join(dir, "outcome.db"))
	require.NoError(t, err)
	defer rec.Close()

	// Write an open decision into the journal
	entry := &journal.DecisionEntry{
		DecisionID:      "d-open-1",
		StrategyID:      "s1",
		StrategyVersion: "1.0.0",
		SessionID:       "sess-1",
		Symbol:          "BTCUSDT",
		Timestamp:       time.Now(),
		Mode:            core.ModePaper,
		Action:          core.ActionOpenLong,
		Confidence:      80,
	}
	require.NoError(t, j.Record(entry))

	// Simulate the pipeline emitting an open fill
	openCC := &core.CycleContext{
		StrategyID:      "s1",
		StrategyVersion: "1.0.0",
		SessionID:       "sess-1",
		Mode:            core.ModePaper,
		Signal: &core.Signal{
			Symbol:          "BTCUSDT",
			Action:          core.ActionOpenLong,
			Leverage:        5,
			StopLoss:        48000,
			TakeProfit:      55000,
			PositionSizeUSD: 1000,
		},
		Fill: &core.Fill{
			Symbol:          "BTCUSDT",
			Side:            core.SideBuy,
			FilledPrice:     50000,
			FilledQty:       0.02,
			Fee:             0.60,
			Timestamp:       time.Now(),
			Mode:            core.ModePaper,
			StrategyID:      "s1",
			StrategyVersion: "1.0.0",
		},
	}
	rec.OnFill(context.Background(), openCC)

	// Verify position is tracked
	positions := rec.OpenPositions()
	require.Len(t, positions, 1)
	require.Equal(t, "BTCUSDT", positions[0].Symbol)
	require.Equal(t, "long", positions[0].Side)
	require.InDelta(t, 50000.0, positions[0].EntryPrice, 0.01)

	// Simulate a profitable close
	closeCC := &core.CycleContext{
		StrategyID:      "s1",
		StrategyVersion: "1.0.0",
		SessionID:       "sess-1",
		Mode:            core.ModePaper,
		Signal: &core.Signal{
			Symbol:  "BTCUSDT",
			Action:  core.ActionCloseLong,
			Reasoning: "take profit target reached",
		},
		Fill: &core.Fill{
			Symbol:      "BTCUSDT",
			Side:        core.SideSell,
			FilledPrice: 52000, // +2000 per BTC
			FilledQty:   0.02,
			Fee:         0.624,
			Timestamp:   time.Now().Add(2 * time.Hour),
			Mode:        core.ModePaper,
		},
	}
	rec.OnFill(context.Background(), closeCC)

	// Position should be removed from open set
	require.Len(t, rec.OpenPositions(), 0)

	// Journal entry should now have an outcome
	updated, err := j.Get("d-open-1")
	require.NoError(t, err)
	require.NotNil(t, updated.Outcome, "outcome must be auto-populated after close fill")
	require.Equal(t, journal.OutcomeWin, updated.Outcome.Class)
	require.Greater(t, updated.Outcome.RealizedPnL, 0.0)
	require.Greater(t, updated.Outcome.ReturnPct, 0.0)
	require.Equal(t, "take_profit", updated.Outcome.ExitReason)
	t.Logf("Auto-recorded outcome: class=%s pnl=%.2f return=%.2f%%",
		updated.Outcome.Class, updated.Outcome.RealizedPnL, updated.Outcome.ReturnPct*100)
}

func TestOutcomeRecordedForLoss(t *testing.T) {
	dir := t.TempDir()
	j, _ := journal.New(filepath.Join(dir, "journal.db"))
	defer j.Close()
	rec, _ := outcome.NewRecorder(j, filepath.Join(dir, "outcome.db"))
	defer rec.Close()

	j.Record(&journal.DecisionEntry{
		DecisionID: "d-loss", StrategyID: "s1", StrategyVersion: "1.0.0",
		Symbol: "ETHUSDT", Timestamp: time.Now(), Mode: core.ModePaper,
		Action: core.ActionOpenShort, Confidence: 70,
	})

	// Open short
	rec.OnFill(context.Background(), &core.CycleContext{
		StrategyID: "s1", StrategyVersion: "1.0.0", Mode: core.ModePaper,
		Signal: &core.Signal{Symbol: "ETHUSDT", Action: core.ActionOpenShort, Leverage: 3},
		Fill:   &core.Fill{Symbol: "ETHUSDT", Side: core.SideSell, FilledPrice: 3000, FilledQty: 0.5, Fee: 0.9, Timestamp: time.Now()},
	})

	// Close short at higher price (loss for short)
	rec.OnFill(context.Background(), &core.CycleContext{
		StrategyID: "s1", StrategyVersion: "1.0.0", Mode: core.ModePaper,
		Signal: &core.Signal{Symbol: "ETHUSDT", Action: core.ActionCloseShort, Reasoning: "stop loss triggered"},
		Fill:   &core.Fill{Symbol: "ETHUSDT", Side: core.SideBuy, FilledPrice: 3200, FilledQty: 0.5, Fee: 0.96, Timestamp: time.Now().Add(30 * time.Minute)},
	})

	updated, _ := j.Get("d-loss")
	require.NotNil(t, updated.Outcome)
	require.Equal(t, journal.OutcomeForcedExit, updated.Outcome.Class)
	require.Less(t, updated.Outcome.RealizedPnL, 0.0)
	require.Equal(t, "stop_loss", updated.Outcome.ExitReason)
}

func TestOpenPositionsSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "outcome.db")
	j, _ := journal.New(filepath.Join(dir, "journal.db"))
	defer j.Close()

	// Session 1: open a position
	rec1, _ := outcome.NewRecorder(j, dbPath)
	j.Record(&journal.DecisionEntry{
		DecisionID: "d-restart", StrategyID: "s1", StrategyVersion: "1.0.0",
		Symbol: "BTCUSDT", Timestamp: time.Now(), Mode: core.ModePaper,
		Action: core.ActionOpenLong,
	})
	rec1.OnFill(context.Background(), &core.CycleContext{
		StrategyID: "s1", StrategyVersion: "1.0.0", Mode: core.ModePaper,
		Signal: &core.Signal{Symbol: "BTCUSDT", Action: core.ActionOpenLong, Leverage: 5},
		Fill:   &core.Fill{Symbol: "BTCUSDT", Side: core.SideBuy, FilledPrice: 60000, FilledQty: 0.01, Timestamp: time.Now()},
	})
	require.Len(t, rec1.OpenPositions(), 1)
	rec1.Close()

	// Session 2: recorder should rehydrate the open position
	rec2, err := outcome.NewRecorder(j, dbPath)
	require.NoError(t, err)
	defer rec2.Close()
	require.Len(t, rec2.OpenPositions(), 1, "open position must survive recorder restart")
	require.Equal(t, "BTCUSDT", rec2.OpenPositions()[0].Symbol)
}

func TestComputeOutcomePnLArithmetic(t *testing.T) {
	open := outcome.OpenPosition{
		Symbol:     "BTCUSDT",
		Side:       "long",
		EntryPrice: 50000,
		EntryQty:   0.02,
		EntryFee:   0.60,
		Leverage:   5,
	}
	close := outcome.CloseEvent{
		ClosePrice: 52500,
		CloseQty:   0.02,
		CloseFee:   0.63,
		CloseTime:  time.Now(),
		ExitReason: "take_profit",
	}
	o := outcome.ComputeOutcome(open, close)
	// rawPnL = (52500 - 50000) * 0.02 = 50.00
	// fees   = 0.60 + 0.63 = 1.23
	// net    = 48.77
	require.InDelta(t, 48.77, o.RealizedPnL, 0.01)
	require.Equal(t, journal.OutcomeWin, o.Class)
	require.Equal(t, "take_profit", o.ExitReason)
	// margin = 50000 * 0.02 / 5 = 200
	// return = 48.77 / 200 = 24.38%
	require.InDelta(t, 0.2438, o.ReturnPct, 0.001)
}

var _ = os.Remove
