package optimizer_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/optimizer"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test: Only candidates passing ALL thresholds are promoted
// ─────────────────────────────────────────────────────────────────────────────

func TestPromotionRequiresAllThresholds(t *testing.T) {
	reg := newTestRegistry(t)
	parent, _ := reg.Create(baseOptimizerRecord())
	promoter := optimizer.NewPromoter(reg)
	thresholds := optimizer.DefaultThresholds()

	tests := []struct {
		name      string
		result    optimizer.EvalResult
		wantPass  bool
		failReason string
	}{
		{
			name: "all criteria met",
			result: optimizer.EvalResult{
				ValReturn:       0.10,
				ValDrawdown:     -0.05,
				ValSharpe:       1.2,
				ValWinRate:      0.55,
				ValProfitFactor: 1.5,
				ValTrades:       25,
				TrainReturn:     0.15,
			},
			wantPass: true,
		},
		{
			name: "insufficient return",
			result: optimizer.EvalResult{
				ValReturn:       0.01, // below 3% min
				ValDrawdown:     -0.05,
				ValSharpe:       1.2,
				ValWinRate:      0.55,
				ValProfitFactor: 1.5,
				ValTrades:       25,
				TrainReturn:     0.10,
			},
			wantPass:   false,
			failReason: "val_return",
		},
		{
			name: "excessive drawdown",
			result: optimizer.EvalResult{
				ValReturn:       0.08,
				ValDrawdown:     -0.20, // above 15% max
				ValSharpe:       1.2,
				ValWinRate:      0.55,
				ValProfitFactor: 1.5,
				ValTrades:       25,
				TrainReturn:     0.12,
			},
			wantPass:   false,
			failReason: "val_drawdown",
		},
		{
			name: "too few trades (not statistically significant)",
			result: optimizer.EvalResult{
				ValReturn:       0.10,
				ValDrawdown:     -0.05,
				ValSharpe:       1.2,
				ValWinRate:      0.55,
				ValProfitFactor: 1.5,
				ValTrades:       3, // below min 10
				TrainReturn:     0.15,
			},
			wantPass:   false,
			failReason: "val_trades",
		},
		{
			name: "overfitting: val/train ratio too low",
			result: optimizer.EvalResult{
				ValReturn:       0.04, // only 20% of train return
				ValDrawdown:     -0.05,
				ValSharpe:       1.0,
				ValWinRate:      0.50,
				ValProfitFactor: 1.2,
				ValTrades:       20,
				TrainReturn:     0.20, // very high train return
			},
			wantPass:   false,
			failReason: "val/train",
		},
		{
			name: "sub-threshold Sharpe",
			result: optimizer.EvalResult{
				ValReturn:       0.06,
				ValDrawdown:     -0.08,
				ValSharpe:       0.3, // below 0.5 min
				ValWinRate:      0.50,
				ValProfitFactor: 1.2,
				ValTrades:       15,
				TrainReturn:     0.08,
			},
			wantPass:   false,
			failReason: "val_sharpe",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.result // copy
			passed, failReasons := optimizer.CheckThresholds(&result, thresholds)
			require.Equal(t, tc.wantPass, passed,
				"threshold check: expected passed=%v, got passed=%v (reasons: %v)",
				tc.wantPass, passed, failReasons)

			if !tc.wantPass {
				found := false
				for _, r := range failReasons {
					if containsStr(r, tc.failReason) {
						found = true
						break
					}
				}
				require.True(t, found,
					"expected fail reason containing %q in %v", tc.failReason, failReasons)
			}
		})
	}

	// Run the full promoter: only the "all criteria met" candidate should be promoted
	var candidates []optimizer.Candidate
	for _, tc := range tests {
		result := tc.result
		result.PassedPromotion, result.FailReasons = optimizer.CheckThresholds(&result, thresholds)
		candidates = append(candidates, optimizer.Candidate{
			CandidateID:   tc.name,
			ParentID:      parent.ID,
			ParentVersion: parent.Version,
			EvalResult:    &result,
		})
	}

	promoted, err := promoter.Promote(parent, candidates, "test-author")
	require.NoError(t, err)
	require.Len(t, promoted, 1, "exactly one candidate should be promoted")
	require.Equal(t, "all criteria met", promoted[0].CandidateID)

	// Promoted version must be in the registry at StatusPaper, not StatusApproved
	if len(promoted) > 0 {
		regEntry, err := reg.GetVersion(promoted[0].RegistryID, promoted[0].RegistryVer)
		require.NoError(t, err)
		require.Equal(t, registry.StatusPaper, regEntry.Status,
			"promoted candidate must land at StatusPaper, not StatusApproved (human gate required)")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Candidate generator produces distinct mutations
// ─────────────────────────────────────────────────────────────────────────────

func TestCandidateGeneratorDistinctMutations(t *testing.T) {
	parent := baseOptimizerRecord()
	candidates := optimizer.GenerateCandidates(parent, 50)

	require.NotEmpty(t, candidates)

	// All candidates must differ from the parent in at least one parameter
	for _, c := range candidates {
		require.NotEqual(t, parent.Parameters, c.Parameters,
			"candidate %s must differ from parent in at least one parameter", c.CandidateID)
		require.NotEmpty(t, c.MutationDesc)
		require.Equal(t, parent.ID, c.ParentID)
		require.Equal(t, parent.Version, c.ParentVersion)
	}

	// No two candidates should be identical
	seen := map[string]bool{}
	for _, c := range candidates {
		require.False(t, seen[c.MutationDesc],
			"duplicate mutation %q", c.MutationDesc)
		seen[c.MutationDesc] = true
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Optimizer job lifecycle (submit → run → done)
// ─────────────────────────────────────────────────────────────────────────────

func TestOptimizerJobLifecycle(t *testing.T) {
	reg := newTestRegistry(t)
	parent, _ := reg.Create(baseOptimizerRecord())

	optSvc, err := optimizer.New(
		filepath.Join(t.TempDir(), "opt.db"),
		reg,
		mockBacktestRunner(t),
		2, // workers
	)
	require.NoError(t, err)
	defer optSvc.Close()

	now := time.Now()
	job, err := optSvc.Submit(
		parent.ID, parent.Version, "tester",
		now.AddDate(0, -2, 0), now.AddDate(0, -1, 0), // train: 2→1 month ago
		now.AddDate(0, -1, 0), now,                    // val: 1 month ago → now
		optimizer.DefaultThresholds(),
		5,
	)
	require.NoError(t, err)
	require.Equal(t, "pending", job.Status)

	// Run
	require.NoError(t, optSvc.Run(context.Background(), job.JobID))

	// Check completion
	completed, err := optSvc.GetJob(job.JobID)
	require.NoError(t, err)
	require.Equal(t, "done", completed.Status)
	require.NotNil(t, completed.CompletedAt)
	require.NotEmpty(t, completed.Candidates)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Live strategy is not mutated by optimizer
// ─────────────────────────────────────────────────────────────────────────────

func TestOptimizerNeverMutatesLiveStrategy(t *testing.T) {
	reg := newTestRegistry(t)
	parent, _ := reg.Create(baseOptimizerRecord())

	// Approve parent for live
	reg.SetStatus(parent.ID, parent.Version, registry.StatusPaper, "tester")
	latest, _ := reg.GetLatest(parent.ID)
	reg.SetStatus(latest.ID, latest.Version, registry.StatusApproved, "human-approver")

	promoter := optimizer.NewPromoter(reg)
	thresholds := optimizer.DefaultThresholds()

	// Create a passing candidate
	result := optimizer.EvalResult{
		ValReturn: 0.10, ValDrawdown: -0.05, ValSharpe: 1.2,
		ValWinRate: 0.55, ValProfitFactor: 1.5, ValTrades: 25,
		TrainReturn: 0.15,
	}
	result.PassedPromotion, _ = optimizer.CheckThresholds(&result, thresholds)

	approvedLatest, _ := reg.GetLatest(parent.ID)
	candidates := []optimizer.Candidate{{
		CandidateID:   "c1",
		ParentID:      approvedLatest.ID,
		ParentVersion: approvedLatest.Version,
		Parameters:    approvedLatest.Parameters,
		EvalResult:    &result,
	}}

	promoted, err := promoter.Promote(approvedLatest, candidates, "optimizer")
	require.NoError(t, err)
	require.Len(t, promoted, 1)

	// The promoted version must be a NEW version at StatusPaper
	newVer, err := reg.GetVersion(promoted[0].RegistryID, promoted[0].RegistryVer)
	require.NoError(t, err)
	require.Equal(t, registry.StatusPaper, newVer.Status,
		"optimizer must never create an approved/live strategy directly")

	// The original approved version must still be approved and unchanged
	originalApproved, err := reg.GetVersion(approvedLatest.ID, approvedLatest.Version)
	require.NoError(t, err)
	require.Equal(t, registry.StatusApproved, originalApproved.Status,
		"original approved version must not be touched by the optimizer")
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func newTestRegistry(t *testing.T) *registry.Service {
	t.Helper()
	svc, err := registry.New(filepath.Join(t.TempDir(), "reg.db"))
	require.NoError(t, err)
	t.Cleanup(func() { svc.Close() })
	return svc
}

func baseOptimizerRecord() *registry.StrategyRecord {
	return &registry.StrategyRecord{
		Name:   "opt-test-strategy",
		Author: "tester",
		Parameters: registry.Parameters{
			CoinSourceType: "static", StaticCoins: []string{"BTCUSDT"},
			PrimaryTimeframe: "5m", SelectedTimeframes: []string{"5m", "15m"},
			PrimaryCount: 30, MaxPositions: 3, MinPositionSize: 12,
			MinConfidence: 75, TradingMode: "conservative",
			AltcoinMaxLeverage: 5, BTCETHMaxLeverage: 5, MaxMarginUsage: 0.8,
			EnableRSI: true, RSIPeriods: []int{7},
			EnableEMA: true, EMAPeriods: []int{20},
		},
	}
}

// mockBacktestRunner returns a BacktestRunner that returns plausible but
// deterministic results based on the parameters, for testing job lifecycle.
func mockBacktestRunner(t *testing.T) optimizer.BacktestRunner {
	t.Helper()
	return func(_ context.Context, _ string, params registry.Parameters, _, _ time.Time) (optimizer.BacktestResult, error) {
		// Simulate different results for different confidence thresholds
		ret := 0.08
		if params.MinConfidence > 80 {
			ret = 0.04 // stricter confidence = fewer trades = lower return
		}
		return optimizer.BacktestResult{
			TotalTrades:  15,
			NetReturn:    ret,
			MaxDrawdown:  -0.06,
			SharpeRatio:  0.9,
			SortinoRatio: 1.1,
			WinRate:      0.52,
			ProfitFactor: 1.3,
		}, nil
	}
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

var _ = os.Remove
