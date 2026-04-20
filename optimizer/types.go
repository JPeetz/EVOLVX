// Package optimizer defines the types for the learning / optimization loop.
//
// The optimizer works exclusively on simulation outcomes — it never touches
// live orders.  The lifecycle is:
//
//   Parent strategy version
//       → GenerateCandidates()   (mutate parameters)
//       → EvaluateCandidates()   (run backtest via pipeline)
//       → FilterCandidates()     (apply promotion thresholds)
//       → Promote()              (write to registry, status=paper, await human)
//       → HumanApproval()        (registry.SetStatus approved)
package optimizer

import (
	"time"

	"github.com/NoFxAiOS/nofx/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// Candidate
// ─────────────────────────────────────────────────────────────────────────────

// Candidate is a mutated child of a parent strategy being evaluated.
type Candidate struct {
	CandidateID   string                  `json:"candidate_id"`
	ParentID      string                  `json:"parent_id"`
	ParentVersion string                  `json:"parent_version"`
	Parameters    registry.Parameters     `json:"parameters"`
	MutationDesc  string                  `json:"mutation_desc"` // what changed
	CreatedAt     time.Time               `json:"created_at"`
	EvalResult    *EvalResult             `json:"eval_result,omitempty"`
	Promoted      bool                    `json:"promoted"`
	RegistryID    string                  `json:"registry_id,omitempty"`   // set after promotion
	RegistryVer   string                  `json:"registry_version,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// EvalResult
// ─────────────────────────────────────────────────────────────────────────────

// EvalResult holds the metrics produced by one backtest run.
type EvalResult struct {
	RunID           string    `json:"run_id"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	TrainPeriod     string    `json:"train_period"`
	ValidationPeriod string   `json:"validation_period"`

	// Core metrics (train split)
	TrainReturn     float64 `json:"train_return"`
	TrainDrawdown   float64 `json:"train_max_drawdown"`
	TrainSharpe     float64 `json:"train_sharpe"`
	TrainSortino    float64 `json:"train_sortino"`
	TrainWinRate    float64 `json:"train_win_rate"`
	TrainProfitFactor float64 `json:"train_profit_factor"`
	TrainTrades     int     `json:"train_trades"`

	// Out-of-sample metrics (validation split)
	ValReturn       float64 `json:"val_return"`
	ValDrawdown     float64 `json:"val_max_drawdown"`
	ValSharpe       float64 `json:"val_sharpe"`
	ValSortino      float64 `json:"val_sortino"`
	ValWinRate      float64 `json:"val_win_rate"`
	ValProfitFactor float64 `json:"val_profit_factor"`
	ValTrades       int     `json:"val_trades"`

	// Composite score (higher is better)
	Score float64 `json:"score"`

	// Why it passed or failed the promotion threshold
	PassedPromotion bool     `json:"passed_promotion"`
	FailReasons     []string `json:"fail_reasons,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Promotion thresholds
// ─────────────────────────────────────────────────────────────────────────────

// PromotionThresholds defines the minimum bar a candidate must clear before
// it can be promoted to paper mode.  All thresholds apply to the VALIDATION
// split, not the training split.
type PromotionThresholds struct {
	// Minimum net return on validation period (e.g. 0.05 = 5%)
	MinValReturn float64
	// Maximum allowed drawdown on validation period (e.g. 0.10 = 10%)
	MaxValDrawdown float64
	// Minimum Sharpe ratio on validation period
	MinValSharpe float64
	// Minimum win rate on validation period
	MinValWinRate float64
	// Minimum profit factor on validation period
	MinValProfitFactor float64
	// Minimum number of trades in validation period
	MinValTrades int
	// Overfitting guard: val_return must be ≥ this fraction of train_return
	// e.g. 0.5 means val must earn at least half of what train earned
	MinValToTrainReturnRatio float64
}

// DefaultThresholds returns conservative promotion thresholds.
func DefaultThresholds() PromotionThresholds {
	return PromotionThresholds{
		MinValReturn:             0.03,  // 3%
		MaxValDrawdown:           0.15,  // 15%
		MinValSharpe:             0.5,
		MinValWinRate:            0.45,
		MinValProfitFactor:       1.1,
		MinValTrades:             10,
		MinValToTrainReturnRatio: 0.4,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Mutation spec
// ─────────────────────────────────────────────────────────────────────────────

// MutationType names the kind of parameter change.
type MutationType string

const (
	MutateRSIPeriod      MutationType = "rsi_period"
	MutateEMAPeriod      MutationType = "ema_period"
	MutateLeverage       MutationType = "leverage"
	MutateRiskReward     MutationType = "risk_reward"
	MutateConfidence     MutationType = "min_confidence"
	MutateTradingMode    MutationType = "trading_mode"
	MutateMaxPositions   MutationType = "max_positions"
	MutateMarginUsage    MutationType = "max_margin_usage"
)

// MutationSpec describes one parameter change applied to create a candidate.
type MutationSpec struct {
	Type     MutationType `json:"type"`
	OldValue any          `json:"old_value"`
	NewValue any          `json:"new_value"`
}

// OptimizationJob is the full specification for one optimization run.
type OptimizationJob struct {
	JobID           string              `json:"job_id"`
	StrategyID      string              `json:"strategy_id"`
	StrategyVersion string              `json:"strategy_version"`
	CreatedAt       time.Time           `json:"created_at"`
	CreatedBy       string              `json:"created_by"`
	Thresholds      PromotionThresholds `json:"thresholds"`
	TrainFrom       time.Time           `json:"train_from"`
	TrainTo         time.Time           `json:"train_to"`
	ValFrom         time.Time           `json:"val_from"`
	ValTo           time.Time           `json:"val_to"`
	MaxCandidates   int                 `json:"max_candidates"`
	Status          string              `json:"status"` // "pending","running","done","failed"
	CompletedAt     *time.Time          `json:"completed_at,omitempty"`
	PromotedCount   int                 `json:"promoted_count"`
	Candidates      []Candidate         `json:"candidates,omitempty"`

	// v1.2: multi-symbol walk-forward
	Symbols            []string `json:"symbols,omitempty"`             // empty = single-symbol (legacy)
	MinConsistency     float64  `json:"min_consistency,omitempty"`     // e.g. 0.4
	RegimeAware        bool     `json:"regime_aware,omitempty"`        // enable per-regime scoring
}
