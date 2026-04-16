// Package optimizer – evaluator and promoter.
package optimizer

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/engine/pipeline"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// BacktestRunner  – provided by caller to decouple optimizer from pipeline
// ─────────────────────────────────────────────────────────────────────────────

// BacktestResult is what the pipeline returns at the end of a backtest run.
type BacktestResult struct {
	RunID        string
	TotalTrades  int
	NetReturn    float64
	MaxDrawdown  float64
	SharpeRatio  float64
	SortinoRatio float64
	WinRate      float64
	ProfitFactor float64
}

// BacktestRunner is a function that can run a backtest for given params and
// a time window, returning performance metrics.  The concrete implementation
// wires up the pipeline with the simulated adapter and a historical feed.
type BacktestRunner func(
	ctx context.Context,
	strategyID string,
	params registry.Parameters,
	from, to time.Time,
) (BacktestResult, error)

// ─────────────────────────────────────────────────────────────────────────────
// Evaluator
// ─────────────────────────────────────────────────────────────────────────────

// EvaluateCandidate runs the candidate through train and validation windows
// and computes a composite score.
func EvaluateCandidate(
	ctx context.Context,
	c *Candidate,
	runner BacktestRunner,
	trainFrom, trainTo, valFrom, valTo time.Time,
	thresholds PromotionThresholds,
) (*EvalResult, error) {
	runID := uuid.NewString()

	// Train split
	trainRes, err := runner(ctx, c.ParentID, c.Parameters, trainFrom, trainTo)
	if err != nil {
		return nil, fmt.Errorf("evaluate candidate %s: train run: %w", c.CandidateID, err)
	}

	// Validation split (out-of-sample)
	valRes, err := runner(ctx, c.ParentID, c.Parameters, valFrom, valTo)
	if err != nil {
		return nil, fmt.Errorf("evaluate candidate %s: val run: %w", c.CandidateID, err)
	}

	result := &EvalResult{
		RunID:           runID,
		StartTime:       trainFrom,
		EndTime:         valTo,
		TrainPeriod:     fmt.Sprintf("%s – %s", trainFrom.Format("2006-01-02"), trainTo.Format("2006-01-02")),
		ValidationPeriod: fmt.Sprintf("%s – %s", valFrom.Format("2006-01-02"), valTo.Format("2006-01-02")),

		TrainReturn:      trainRes.NetReturn,
		TrainDrawdown:    trainRes.MaxDrawdown,
		TrainSharpe:      trainRes.SharpeRatio,
		TrainSortino:     trainRes.SortinoRatio,
		TrainWinRate:     trainRes.WinRate,
		TrainProfitFactor: trainRes.ProfitFactor,
		TrainTrades:      trainRes.TotalTrades,

		ValReturn:       valRes.NetReturn,
		ValDrawdown:     valRes.MaxDrawdown,
		ValSharpe:       valRes.SharpeRatio,
		ValSortino:      valRes.SortinoRatio,
		ValWinRate:      valRes.WinRate,
		ValProfitFactor: valRes.ProfitFactor,
		ValTrades:       valRes.TotalTrades,
	}

	result.Score = computeScore(result)
	result.PassedPromotion, result.FailReasons = checkThresholds(result, thresholds)
	return result, nil
}

// computeScore produces a composite score from validation metrics.
// Higher is better.  Weights reflect risk-adjusted returns.
func computeScore(r *EvalResult) float64 {
	if r.ValTrades < 1 {
		return -math.MaxFloat64
	}
	// Weighted composite: Sharpe (40%) + return (30%) + win-rate (15%) + profit factor (15%)
	// Drawdown is handled via threshold filter, not score weighting
	score := 0.4*clamp(r.ValSharpe, -3, 5) +
		0.3*clamp(r.ValReturn*10, -3, 5) + // scale return to ~sharpe range
		0.15*clamp(r.ValWinRate*5, 0, 5) +
		0.15*clamp(r.ValProfitFactor-1, -2, 4)
	return score
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// checkThresholds returns (passed, reasons-for-failure).
func checkThresholds(r *EvalResult, t PromotionThresholds) (bool, []string) {
	var fails []string

	check := func(cond bool, msg string) {
		if !cond {
			fails = append(fails, msg)
		}
	}

	check(r.ValReturn >= t.MinValReturn,
		fmt.Sprintf("val_return %.2f%% < min %.2f%%", r.ValReturn*100, t.MinValReturn*100))
	check(math.Abs(r.ValDrawdown) <= t.MaxValDrawdown,
		fmt.Sprintf("|val_drawdown| %.2f%% > max %.2f%%", math.Abs(r.ValDrawdown)*100, t.MaxValDrawdown*100))
	check(r.ValSharpe >= t.MinValSharpe,
		fmt.Sprintf("val_sharpe %.2f < min %.2f", r.ValSharpe, t.MinValSharpe))
	check(r.ValWinRate >= t.MinValWinRate,
		fmt.Sprintf("val_win_rate %.2f < min %.2f", r.ValWinRate, t.MinValWinRate))
	check(r.ValProfitFactor >= t.MinValProfitFactor,
		fmt.Sprintf("val_profit_factor %.2f < min %.2f", r.ValProfitFactor, t.MinValProfitFactor))
	check(r.ValTrades >= t.MinValTrades,
		fmt.Sprintf("val_trades %d < min %d", r.ValTrades, t.MinValTrades))

	// Overfitting guard
	if r.TrainReturn > 0.001 {
		ratio := r.ValReturn / r.TrainReturn
		check(ratio >= t.MinValToTrainReturnRatio,
			fmt.Sprintf("val/train return ratio %.2f < min %.2f (possible overfit)", ratio, t.MinValToTrainReturnRatio))
	}

	return len(fails) == 0, fails
}

// ─────────────────────────────────────────────────────────────────────────────
// Promoter
// ─────────────────────────────────────────────────────────────────────────────

// Promoter writes passing candidates into the registry as new paper-status
// strategy versions.  Human approval is still required before live.
type Promoter struct {
	reg *registry.Service
}

// NewPromoter creates a Promoter backed by reg.
func NewPromoter(reg *registry.Service) *Promoter {
	return &Promoter{reg: reg}
}

// Promote creates a new strategy version for each passing candidate.
// It sets the version to StatusPaper and records lineage.
// It does NOT set StatusApproved — a human must call registry.SetStatus for that.
func (p *Promoter) Promote(parent *registry.StrategyRecord, candidates []Candidate, author string) ([]Candidate, error) {
	var promoted []Candidate
	for i := range candidates {
		c := &candidates[i]
		if c.EvalResult == nil || !c.EvalResult.PassedPromotion {
			continue
		}

		// Create new version in registry
		child, err := p.reg.NewVersion(
			parent.ID,
			parent.Version,
			"patch",
			author,
			c.Parameters,
			c.MutationDesc,
		)
		if err != nil {
			log.Printf("promoter: create version for candidate %s: %v", c.CandidateID, err)
			continue
		}

		// Move to paper status (not approved — human gate)
		if err := p.reg.SetStatus(child.ID, child.Version, registry.StatusPaper, author); err != nil {
			log.Printf("promoter: set paper status for %s/%s: %v", child.ID, child.Version, err)
			continue
		}

		// Record the performance in the registry
		perf := registry.PerformanceSummary{
			RunID:            c.EvalResult.RunID,
			RunType:          registry.RunBacktest,
			StartTime:        c.EvalResult.StartTime,
			EndTime:          c.EvalResult.EndTime,
			NetReturn:        c.EvalResult.ValReturn,
			MaxDrawdown:      c.EvalResult.ValDrawdown,
			SharpeRatio:      c.EvalResult.ValSharpe,
			SortinoRatio:     c.EvalResult.ValSortino,
			WinRate:          c.EvalResult.ValWinRate,
			ProfitFactor:     c.EvalResult.ValProfitFactor,
			TotalTrades:      c.EvalResult.ValTrades,
			TrainPeriod:      c.EvalResult.TrainPeriod,
			ValidationPeriod: c.EvalResult.ValidationPeriod,
		}
		p.reg.AddPerformance(child.ID, child.Version, perf)

		c.Promoted = true
		c.RegistryID = child.ID
		c.RegistryVer = child.Version
		promoted = append(promoted, *c)
	}
	return promoted, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Unused import guard – these are used in the real pipeline wiring
// ─────────────────────────────────────────────────────────────────────────────
var _ = pipeline.New
var _ = core.ModeBacktest
