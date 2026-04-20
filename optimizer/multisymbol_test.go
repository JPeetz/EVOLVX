package optimizer_test

import (
	"context"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/optimizer"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/NoFxAiOS/nofx/regime"
	"github.com/stretchr/testify/require"
)

func TestMultiSymbolPromotionRequiresConsistency(t *testing.T) {
	parent := baseOptimizerRecord()
	candidate := optimizer.Candidate{
		CandidateID:   "c-multi",
		ParentID:      parent.ID,
		ParentVersion: parent.Version,
		Parameters:    parent.Parameters,
		MutationDesc:  "rsi_period=14",
	}

	// Runner that returns different results per symbol
	runner := func(_ context.Context, _ string, symbol string, _ registry.Parameters, _, _ time.Time) (optimizer.SymbolResult, error) {
		switch symbol {
		case "BTCUSDT":
			return optimizer.SymbolResult{Symbol: symbol, BacktestResult: optimizer.BacktestResult{
				TotalTrades: 20, NetReturn: 0.10, MaxDrawdown: -0.05,
				SharpeRatio: 1.2, WinRate: 0.55, ProfitFactor: 1.4,
			}}, nil
		case "ETHUSDT":
			return optimizer.SymbolResult{Symbol: symbol, BacktestResult: optimizer.BacktestResult{
				TotalTrades: 15, NetReturn: 0.09, MaxDrawdown: -0.06,
				SharpeRatio: 1.0, WinRate: 0.52, ProfitFactor: 1.3,
			}}, nil
		case "SOLUSDT":
			// Deliberately inconsistent — this symbol loses money
			return optimizer.SymbolResult{Symbol: symbol, BacktestResult: optimizer.BacktestResult{
				TotalTrades: 18, NetReturn: -0.03, MaxDrawdown: -0.12,
				SharpeRatio: 0.2, WinRate: 0.38, ProfitFactor: 0.8,
			}}, nil
		}
		return optimizer.SymbolResult{}, nil
	}

	thresholds := optimizer.DefaultThresholds()
	now := time.Now()

	result, err := optimizer.EvaluateMultiSymbol(
		context.Background(),
		&candidate,
		runner,
		[]string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
		now.AddDate(0, -4, 0), now.AddDate(0, -2, 0),
		now.AddDate(0, -2, 0), now,
		thresholds,
		0.5, // min consistency
	)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Should fail: SOLUSDT loses money and drags down consistency
	require.False(t, result.PassedPromotion,
		"strategy that fails on one symbol must not be promoted")
	require.NotEmpty(t, result.FailReasons)
	t.Logf("Fail reasons: %v", result.FailReasons)
	t.Logf("Consistency score: %.2f", result.ConsistencyScore)
}

func TestMultiSymbolPromotionPassesConsistentStrategy(t *testing.T) {
	parent := baseOptimizerRecord()
	candidate := optimizer.Candidate{
		CandidateID:   "c-consistent",
		ParentID:      parent.ID,
		ParentVersion: parent.Version,
		Parameters:    parent.Parameters,
	}

	// Consistently profitable runner across all symbols
	runner := func(_ context.Context, _ string, _ string, _ registry.Parameters, _, _ time.Time) (optimizer.SymbolResult, error) {
		return optimizer.SymbolResult{BacktestResult: optimizer.BacktestResult{
			TotalTrades: 20, NetReturn: 0.09, MaxDrawdown: -0.06,
			SharpeRatio: 1.1, WinRate: 0.53, ProfitFactor: 1.35,
		}}, nil
	}

	now := time.Now()
	result, err := optimizer.EvaluateMultiSymbol(
		context.Background(), &candidate, runner,
		[]string{"BTCUSDT", "ETHUSDT"},
		now.AddDate(0, -4, 0), now.AddDate(0, -2, 0),
		now.AddDate(0, -2, 0), now,
		optimizer.DefaultThresholds(), 0.4,
	)
	require.NoError(t, err)
	require.True(t, result.PassedPromotion, "consistent profitable strategy must pass: %v", result.FailReasons)
	require.Greater(t, result.ConsistencyScore, 0.4)
	require.Len(t, result.PerSymbol, 2)
}

func TestMultiSymbolPerSymbolResultsPresent(t *testing.T) {
	symbols := []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"}
	parent := baseOptimizerRecord()
	c := optimizer.Candidate{ParentID: parent.ID, ParentVersion: parent.Version, Parameters: parent.Parameters}

	runner := func(_ context.Context, _ string, sym string, _ registry.Parameters, _, _ time.Time) (optimizer.SymbolResult, error) {
		return optimizer.SymbolResult{Symbol: sym, BacktestResult: optimizer.BacktestResult{
			TotalTrades: 10, NetReturn: 0.05, MaxDrawdown: -0.04, SharpeRatio: 0.8,
		}}, nil
	}

	now := time.Now()
	result, err := optimizer.EvaluateMultiSymbol(
		context.Background(), &c, runner, symbols,
		now.AddDate(0, -3, 0), now.AddDate(0, -1, 0),
		now.AddDate(0, -1, 0), now,
		optimizer.DefaultThresholds(), 0.3,
	)
	require.NoError(t, err)
	for _, sym := range symbols {
		_, ok := result.PerSymbol[sym]
		require.True(t, ok, "per-symbol result must exist for %s", sym)
	}
}

func TestRegimeDetectorIntegration(t *testing.T) {
	// Generate a mixed regime series and verify we get all four regimes
	bars := make([]regime.Bar, 400)
	for i := range bars {
		var price float64
		switch {
		case i < 100:
			price = 50000 * (1 + float64(i)*0.002)      // bull
		case i < 200:
			price = 50000 * (1 - float64(i-100)*0.002)  // bear
		case i < 300:
			price = 40000 * (1 + 0.001*float64((i-200)%3-1)) // sideways
		default:
			price = 40000 * (1 + 0.02*float64((i-300)%2*2-1)) // volatile
		}
		bars[i] = regime.Bar{Time: time.Now().Add(time.Duration(i) * 5 * time.Minute), Close: price}
	}

	d := regime.New(regime.DefaultConfig())
	labeled := d.Classify(bars)
	coverage := regime.Coverage(labeled)

	t.Logf("Regime coverage: bull=%.1f%% bear=%.1f%% sideways=%.1f%% volatile=%.1f%%",
		coverage[regime.Bull]*100, coverage[regime.Bear]*100,
		coverage[regime.Sideways]*100, coverage[regime.Volatile]*100)

	// All four regimes should be represented
	for _, label := range []regime.Label{regime.Bull, regime.Bear, regime.Sideways, regime.Volatile} {
		require.Greater(t, coverage[label], 0.0, "regime %s should appear in mixed series", label)
	}
}
