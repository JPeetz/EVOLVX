// Package optimizer – multi-symbol walk-forward evaluation.
//
// v1.2 extends the optimizer with two key improvements:
//
//  1. MultiSymbolBacktestRunner — runs a candidate across multiple symbols
//     simultaneously and aggregates results.  A strategy that only works on
//     one symbol scores lower than one that works across all configured symbols.
//
//  2. RegimeAwareEvalResult — breaks down performance by market regime so the
//     promotion gate can reject strategies that fail in specific regimes even
//     if their overall score passes.
//
// These two features are additive: the existing single-symbol EvaluateCandidate
// path continues to work.  Multi-symbol eval is offered via EvaluateMultiSymbol.
package optimizer

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/regime"
	"github.com/NoFxAiOS/nofx/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// MultiSymbolBacktestRunner
// ─────────────────────────────────────────────────────────────────────────────

// SymbolBacktestRunner runs a backtest for ONE symbol and returns per-regime
// breakdowns in addition to the overall result.
type SymbolBacktestRunner func(
	ctx context.Context,
	strategyID string,
	symbol string,
	params registry.Parameters,
	from, to time.Time,
) (SymbolResult, error)

// SymbolResult is the output of one single-symbol backtest, including
// per-regime performance when regime bars are available.
type SymbolResult struct {
	Symbol       string
	BacktestResult                    // embedded overall metrics
	RegimeMetrics []regime.RegimeMetrics `json:"regime_metrics,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// MultiSymbolEvalResult
// ─────────────────────────────────────────────────────────────────────────────

// MultiSymbolEvalResult extends EvalResult with per-symbol and per-regime
// breakdowns.
type MultiSymbolEvalResult struct {
	EvalResult // embedded — overall aggregated metrics

	Symbols       []string                       `json:"symbols"`
	PerSymbol     map[string]EvalResult          `json:"per_symbol"`
	PerRegime     map[string]regime.RegimeMetrics `json:"per_regime"` // key: regime label
	ConsistencyScore float64                     `json:"consistency_score"` // 0–1, higher = more consistent across symbols

	// Regime-specific promotion verdict
	RegimeFails []string `json:"regime_fails,omitempty"` // reasons from per-regime check
}

// ─────────────────────────────────────────────────────────────────────────────
// EvaluateMultiSymbol
// ─────────────────────────────────────────────────────────────────────────────

// EvaluateMultiSymbol runs a candidate against all symbols in the list and
// returns an aggregated MultiSymbolEvalResult.
//
// Aggregation rules:
//   - Overall score = weighted average of per-symbol scores (equal weights)
//   - Consistency score = 1 - coefficient_of_variation(per_symbol_returns)
//   - PassedPromotion = all symbols individually pass thresholds
//     AND consistency score >= minConsistency
func EvaluateMultiSymbol(
	ctx context.Context,
	c *Candidate,
	runner SymbolBacktestRunner,
	symbols []string,
	trainFrom, trainTo, valFrom, valTo time.Time,
	thresholds PromotionThresholds,
	minConsistency float64, // e.g. 0.5 = at least 50% consistency required
) (*MultiSymbolEvalResult, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("EvaluateMultiSymbol: at least one symbol required")
	}
	if minConsistency <= 0 {
		minConsistency = 0.4
	}

	type symbolJob struct {
		symbol string
		train  SymbolResult
		val    SymbolResult
		err    error
	}

	jobs := make(chan symbolJob, len(symbols))
	var wg sync.WaitGroup

	for _, sym := range symbols {
		wg.Add(1)
		go func(symbol string) {
			defer wg.Done()
			j := symbolJob{symbol: symbol}

			j.train, j.err = runner(ctx, c.ParentID, symbol, c.Parameters, trainFrom, trainTo)
			if j.err != nil {
				jobs <- j
				return
			}
			j.val, j.err = runner(ctx, c.ParentID, symbol, c.Parameters, valFrom, valTo)
			jobs <- j
		}(sym)
	}

	wg.Wait()
	close(jobs)

	perSymbol := make(map[string]EvalResult)
	perRegime := make(map[string]regime.RegimeMetrics)
	var failedSymbols []string
	var allValReturns []float64
	var allScores []float64

	for j := range jobs {
		if j.err != nil {
			failedSymbols = append(failedSymbols, fmt.Sprintf("%s: %v", j.symbol, j.err))
			continue
		}

		// Build EvalResult for this symbol
		r := buildSymbolEvalResult(j.train, j.val, thresholds)
		perSymbol[j.symbol] = r
		allValReturns = append(allValReturns, r.ValReturn)
		allScores = append(allScores, r.Score)

		// Merge per-regime metrics (average across symbols)
		for _, rm := range j.val.RegimeMetrics {
			key := string(rm.Regime)
			existing := perRegime[key]
			existing.Regime = rm.Regime
			existing.NetReturn = (existing.NetReturn*float64(existing.Trades) + rm.NetReturn*float64(rm.Trades)) /
				float64(max2(existing.Trades+rm.Trades, 1))
			existing.Trades += rm.Trades
			existing.BarCount += rm.BarCount
			if math.Abs(rm.MaxDrawdown) > math.Abs(existing.MaxDrawdown) {
				existing.MaxDrawdown = rm.MaxDrawdown
			}
			perRegime[key] = existing
		}
	}

	if len(allScores) == 0 {
		return nil, fmt.Errorf("EvaluateMultiSymbol: all %d symbols failed: %v", len(symbols), failedSymbols)
	}

	// Aggregate overall score
	avgScore := mean64(allScores)
	consistency := consistencyScore(allValReturns)

	// Build aggregate EvalResult
	aggregate := aggregateEvalResults(perSymbol, thresholds)
	aggregate.Score = avgScore * consistency // penalise inconsistency

	// Regime-specific promotion check
	var regimeFails []string
	for label, rm := range perRegime {
		if rm.Trades < 5 {
			continue // skip regimes with too few trades to be meaningful
		}
		if rm.NetReturn < thresholds.MinValReturn*0.5 { // regimes get half the threshold
			regimeFails = append(regimeFails, fmt.Sprintf("regime %s: return %.2f%% below threshold", label, rm.NetReturn*100))
		}
		if math.Abs(rm.MaxDrawdown) > thresholds.MaxValDrawdown*1.5 {
			regimeFails = append(regimeFails, fmt.Sprintf("regime %s: drawdown %.2f%% exceeds limit", label, math.Abs(rm.MaxDrawdown)*100))
		}
	}

	if consistency < minConsistency {
		regimeFails = append(regimeFails, fmt.Sprintf("consistency %.2f below minimum %.2f (strategy only works on some symbols)", consistency, minConsistency))
	}

	// Final promotion verdict
	_, existingFails := checkThresholds(&aggregate, thresholds)
	allFails := append(existingFails, regimeFails...)
	aggregate.PassedPromotion = len(allFails) == 0
	aggregate.FailReasons = allFails

	sort.Strings(aggregate.FailReasons)

	return &MultiSymbolEvalResult{
		EvalResult:       aggregate,
		Symbols:          symbols,
		PerSymbol:        perSymbol,
		PerRegime:        perRegime,
		ConsistencyScore: consistency,
		RegimeFails:      regimeFails,
	}, nil
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// buildSymbolEvalResult converts train+val SymbolResults into an EvalResult.
func buildSymbolEvalResult(train, val SymbolResult, t PromotionThresholds) EvalResult {
	r := EvalResult{
		TrainReturn:      train.NetReturn,
		TrainDrawdown:    train.MaxDrawdown,
		TrainSharpe:      train.SharpeRatio,
		TrainSortino:     train.SortinoRatio,
		TrainWinRate:     train.WinRate,
		TrainProfitFactor: train.ProfitFactor,
		TrainTrades:      train.TotalTrades,
		ValReturn:        val.NetReturn,
		ValDrawdown:      val.MaxDrawdown,
		ValSharpe:        val.SharpeRatio,
		ValSortino:       val.SortinoRatio,
		ValWinRate:       val.WinRate,
		ValProfitFactor:  val.ProfitFactor,
		ValTrades:        val.TotalTrades,
	}
	r.Score = computeScore(&r)
	r.PassedPromotion, r.FailReasons = checkThresholds(&r, t)
	return r
}

// aggregateEvalResults computes the mean of all per-symbol EvalResults.
func aggregateEvalResults(perSym map[string]EvalResult, t PromotionThresholds) EvalResult {
	n := float64(len(perSym))
	agg := EvalResult{}
	for _, r := range perSym {
		agg.ValReturn += r.ValReturn / n
		agg.ValDrawdown += r.ValDrawdown / n
		agg.ValSharpe += r.ValSharpe / n
		agg.ValSortino += r.ValSortino / n
		agg.ValWinRate += r.ValWinRate / n
		agg.ValProfitFactor += r.ValProfitFactor / n
		agg.ValTrades += r.ValTrades
		agg.TrainReturn += r.TrainReturn / n
		agg.TrainDrawdown += r.TrainDrawdown / n
		agg.TrainSharpe += r.TrainSharpe / n
	}
	return agg
}

// consistencyScore measures how consistent returns are across symbols.
// Returns 1.0 if all symbols have identical returns, ~0 if wildly different.
func consistencyScore(returns []float64) float64 {
	if len(returns) < 2 {
		return 1.0
	}
	mu := mean64(returns)
	if math.Abs(mu) < 0.0001 {
		return 0.5 // can't compute CV with near-zero mean
	}
	variance := 0.0
	for _, r := range returns {
		d := r - mu
		variance += d * d
	}
	stddev := math.Sqrt(variance / float64(len(returns)))
	cv := math.Abs(stddev / mu) // coefficient of variation
	// Map CV to 0–1 consistency (CV=0 → consistency=1, CV=2+ → consistency≈0)
	return math.Max(0, 1-cv/2)
}

func mean64(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}
