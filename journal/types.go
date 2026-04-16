// Package journal implements durable per-strategy decision memory.
//
// Every signal the pipeline produces is recorded as a DecisionEntry.
// After the position closes, the entry is updated with the Outcome.
// A later evaluation can read prior decisions before acting — this is how
// the strategy gains "memory" across restarts.
package journal

import (
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// DecisionEntry – one recorded decision
// ─────────────────────────────────────────────────────────────────────────────

// DecisionEntry captures everything about one trading decision and its outcome.
type DecisionEntry struct {
	// Identity
	DecisionID      string    `json:"decision_id"`
	StrategyID      string    `json:"strategy_id"`
	StrategyVersion string    `json:"strategy_version"`
	SessionID       string    `json:"session_id"`
	CycleNumber     int64     `json:"cycle_number"`
	Symbol          string    `json:"symbol"`
	Timestamp       time.Time `json:"timestamp"`
	Mode            core.Mode `json:"mode"`

	// Market context at decision time
	MarketSnapshot MarketSnapshot `json:"market_snapshot"`

	// Signal
	Action          core.SignalAction `json:"action"`
	Confidence      int               `json:"confidence"`
	SignalInputs    map[string]any    `json:"signal_inputs"`   // indicators used
	Reasoning       string            `json:"reasoning"`       // AI chain-of-thought summary
	RawAIResponse   string            `json:"raw_ai_response,omitempty"`
	CoTTrace        string            `json:"cot_trace,omitempty"`

	// Risk state at decision time
	RiskState RiskSnapshot `json:"risk_state"`

	// Position state at decision time
	PositionState PositionSnapshot `json:"position_state"`

	// Execution result
	OrderID     string     `json:"order_id,omitempty"`
	FillPrice   float64    `json:"fill_price,omitempty"`
	FilledQty   float64    `json:"filled_qty,omitempty"`
	Fee         float64    `json:"fee,omitempty"`
	ExecutedAt  *time.Time `json:"executed_at,omitempty"`

	// Outcome – populated when the position is closed
	Outcome *Outcome `json:"outcome,omitempty"`

	// Error details if the decision or execution failed
	ErrorMessage string `json:"error_message,omitempty"`

	// Human review notes (added post-hoc)
	ReviewNotes string `json:"review_notes,omitempty"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy  string `json:"reviewed_by,omitempty"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Supporting snapshots
// ─────────────────────────────────────────────────────────────────────────────

// MarketSnapshot records the key market values at decision time.
type MarketSnapshot struct {
	Price       float64            `json:"price"`
	Volume      float64            `json:"volume"`
	OI          float64            `json:"oi"`
	FundingRate float64            `json:"funding_rate"`
	Indicators  map[string]float64 `json:"indicators"`
}

// RiskSnapshot records the risk parameters checked against this decision.
type RiskSnapshot struct {
	AccountEquity    float64 `json:"account_equity"`
	MarginUsage      float64 `json:"margin_usage_pct"`
	OpenPositions    int     `json:"open_positions"`
	MaxPositions     int     `json:"max_positions"`
	RejectionReason  string  `json:"rejection_reason,omitempty"`
	Approved         bool    `json:"approved"`
}

// PositionSnapshot records the portfolio state at decision time.
type PositionSnapshot struct {
	OpenPositions []PositionSummary `json:"open_positions"`
	TotalUnrealPnL float64          `json:"total_unrealized_pnl"`
}

// PositionSummary is a brief record of one open position.
type PositionSummary struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	EntryPrice    float64 `json:"entry_price"`
	MarkPrice     float64 `json:"mark_price"`
	Quantity      float64 `json:"quantity"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
}

// ─────────────────────────────────────────────────────────────────────────────
// Outcome – filled after the position closes
// ─────────────────────────────────────────────────────────────────────────────

// OutcomeClass categorises the result.
type OutcomeClass string

const (
	OutcomeWin        OutcomeClass = "win"
	OutcomeLoss       OutcomeClass = "loss"
	OutcomeBreakEven  OutcomeClass = "breakeven"
	OutcomeForcedExit OutcomeClass = "forced_exit" // liquidation or stop
	OutcomePending    OutcomeClass = "pending"
)

// Outcome records what actually happened after the decision was acted on.
type Outcome struct {
	ClosedAt      time.Time    `json:"closed_at"`
	ClosePrice    float64      `json:"close_price"`
	RealizedPnL   float64      `json:"realized_pnl"`   // absolute USDT
	ReturnPct     float64      `json:"return_pct"`      // fraction e.g. 0.02
	HoldingPeriod string       `json:"holding_period"`  // human-readable
	Class         OutcomeClass `json:"class"`
	ExitReason    string       `json:"exit_reason"`     // "take_profit", "stop_loss", "signal", etc.
}

// ─────────────────────────────────────────────────────────────────────────────
// Query filters
// ─────────────────────────────────────────────────────────────────────────────

// QueryFilter defines criteria for fetching decisions.
type QueryFilter struct {
	StrategyID      string
	StrategyVersion string
	Symbol          string
	Mode            core.Mode
	From            *time.Time
	To              *time.Time
	OutcomeClass    OutcomeClass
	Limit           int
	Offset          int
}

// ─────────────────────────────────────────────────────────────────────────────
// Summary  (compacted representation for large history)
// ─────────────────────────────────────────────────────────────────────────────

// StrategySummary is a compacted aggregate over many decisions.
// Used when the full history would be too large to fit in a prompt.
type StrategySummary struct {
	StrategyID      string    `json:"strategy_id"`
	StrategyVersion string    `json:"strategy_version"`
	Symbol          string    `json:"symbol,omitempty"`
	From            time.Time `json:"from"`
	To              time.Time `json:"to"`
	TotalDecisions  int       `json:"total_decisions"`
	Wins            int       `json:"wins"`
	Losses          int       `json:"losses"`
	WinRate         float64   `json:"win_rate"`
	TotalPnL        float64   `json:"total_pnl"`
	AvgReturnPct    float64   `json:"avg_return_pct"`
	MaxDrawdown     float64   `json:"max_drawdown"`
	// Most recent N decisions (brief)
	RecentDecisions []BriefDecision `json:"recent_decisions,omitempty"`
}

// BriefDecision is a one-line summary of a past decision.
type BriefDecision struct {
	Timestamp  time.Time        `json:"ts"`
	Symbol     string           `json:"symbol"`
	Action     core.SignalAction `json:"action"`
	Confidence int              `json:"confidence"`
	Result     OutcomeClass     `json:"result"`
	ReturnPct  float64          `json:"return_pct"`
}
