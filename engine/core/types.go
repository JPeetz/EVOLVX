// Package core defines the shared vocabulary for every execution mode.
// Backtest, paper, and live all speak these types — nothing else is
// allowed to cross the engine boundary.
package core

import (
	"fmt"
	"time"
)

// ─────────────────────────────────────────────────────────────────────────────
// Execution mode
// ─────────────────────────────────────────────────────────────────────────────

// Mode identifies which execution path is active.
type Mode string

const (
	ModeBacktest Mode = "backtest"
	ModePaper    Mode = "paper"
	ModeLive     Mode = "live"
)

// ─────────────────────────────────────────────────────────────────────────────
// Market event  (input side of the pipeline)
// ─────────────────────────────────────────────────────────────────────────────

// MarketEvent is the immutable snapshot of one market bar/tick delivered to
// the pipeline.  All three modes produce this identical struct so the strategy
// layer never needs to know where the data came from.
type MarketEvent struct {
	EventID     string    // uuid for this event
	Symbol      string    // e.g. "BTCUSDT"
	Timestamp   time.Time // candle close time (or tick time)
	Open        float64
	High        float64
	Low         float64
	Close       float64
	Volume      float64
	Timeframe   string            // "5m", "1h", etc.
	Indicators  map[string]float64 // pre-computed indicator values keyed by name
	OI          float64
	FundingRate float64
	Extra       map[string]any    // exchange-specific extras
}

// ─────────────────────────────────────────────────────────────────────────────
// Strategy signal  (output of strategy evaluation)
// ─────────────────────────────────────────────────────────────────────────────

// SignalAction enumerates all actions a strategy can emit.
type SignalAction string

const (
	ActionOpenLong   SignalAction = "open_long"
	ActionOpenShort  SignalAction = "open_short"
	ActionCloseLong  SignalAction = "close_long"
	ActionCloseShort SignalAction = "close_short"
	ActionHold       SignalAction = "hold"
	ActionWait       SignalAction = "wait"
)

// Signal is what a strategy version emits after evaluating a MarketEvent.
type Signal struct {
	SignalID         string
	StrategyID       string
	StrategyVersion  string
	Symbol           string
	Timestamp        time.Time
	Action           SignalAction
	Leverage         int
	PositionSizeUSD  float64
	StopLoss         float64
	TakeProfit       float64
	Confidence       int    // 0-100
	RiskUSD          float64
	Reasoning        string // AI chain-of-thought or rule explanation
	RawAIResponse    string // verbatim AI output, for auditability
	CoTTrace         string // extracted chain-of-thought
}

// ─────────────────────────────────────────────────────────────────────────────
// Order  (intent to trade, before execution)
// ─────────────────────────────────────────────────────────────────────────────

// OrderSide is "buy" or "sell".
type OrderSide string

const (
	SideBuy  OrderSide = "buy"
	SideSell OrderSide = "sell"
)

// OrderType controls fill model behaviour.
type OrderType string

const (
	OrderMarket OrderType = "market"
	OrderLimit  OrderType = "limit"
)

// OrderStatus tracks lifecycle.
type OrderStatus string

const (
	OrderPending   OrderStatus = "pending"
	OrderFilled    OrderStatus = "filled"
	OrderPartial   OrderStatus = "partial"
	OrderCancelled OrderStatus = "cancelled"
	OrderRejected  OrderStatus = "rejected"
)

// Order is the unified order object created after risk check passes.
type Order struct {
	OrderID         string
	ClientOrderID   string
	Mode            Mode
	Symbol          string
	Side            OrderSide
	Type            OrderType
	Quantity        float64
	Price           float64 // limit price; 0 for market
	Leverage        int
	StopLoss        float64
	TakeProfit      float64
	StrategyID      string
	StrategyVersion string
	SignalID        string
	Status          OrderStatus
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// ─────────────────────────────────────────────────────────────────────────────
// Fill  (confirmed execution result)
// ─────────────────────────────────────────────────────────────────────────────

// Fill is returned by every ExecutionAdapter regardless of mode.  It is the
// source-of-truth for what actually happened.
type Fill struct {
	FillID          string
	OrderID         string
	Symbol          string
	Side            OrderSide
	FilledQty       float64
	FilledPrice     float64
	Fee             float64
	FeeCurrency     string
	Slippage        float64 // abs(filled-requested) / requested
	LatencyMS       int64   // simulated or measured
	Timestamp       time.Time
	Mode            Mode
	StrategyID      string
	StrategyVersion string
}

// ─────────────────────────────────────────────────────────────────────────────
// Risk check
// ─────────────────────────────────────────────────────────────────────────────

// RiskCheckResult is the verdict from the risk layer.
type RiskCheckResult struct {
	Approved        bool
	AdjustedOrder   *Order // may be nil if rejected or unchanged
	Reasons         []string
}

// ─────────────────────────────────────────────────────────────────────────────
// Position state  (maintained inside the engine)
// ─────────────────────────────────────────────────────────────────────────────

// Position mirrors the current open position for a symbol.
type Position struct {
	Symbol           string
	Side             string  // "long" / "short"
	EntryPrice       float64
	MarkPrice        float64
	Quantity         float64
	Leverage         int
	UnrealizedPnL    float64
	LiquidationPrice float64
	StrategyID       string
	StrategyVersion  string
	OpenedAt         time.Time
}

// ─────────────────────────────────────────────────────────────────────────────
// Metrics snapshot
// ─────────────────────────────────────────────────────────────────────────────

// Metrics is emitted at the end of each cycle and written to the event log.
type Metrics struct {
	Timestamp       time.Time
	Mode            Mode
	StrategyID      string
	StrategyVersion string
	Equity          float64
	Available       float64
	UnrealizedPnL   float64
	RealizedPnL     float64
	TotalTrades     int
	WinCount        int
	LossCount       int
	WinRate         float64
	Drawdown        float64
	MaxDrawdown     float64
	SharpeRatio     float64
	SortinoRatio    float64
	ProfitFactor    float64
}

// ─────────────────────────────────────────────────────────────────────────────
// Event log entry  (append-only audit trail)
// ─────────────────────────────────────────────────────────────────────────────

// EventKind classifies a log entry.
type EventKind string

const (
	EventMarket   EventKind = "market"
	EventSignal   EventKind = "signal"
	EventOrder    EventKind = "order"
	EventFill     EventKind = "fill"
	EventRisk     EventKind = "risk"
	EventMetrics  EventKind = "metrics"
	EventError    EventKind = "error"
)

// LogEntry is one row in the append-only event log.
type LogEntry struct {
	EntryID   string
	Kind      EventKind
	Timestamp time.Time
	Mode      Mode
	SessionID string // ties all events in one run together
	Payload   any    // one of the concrete types above
}

// ─────────────────────────────────────────────────────────────────────────────
// Pipeline context  (flows through the whole pipeline per cycle)
// ─────────────────────────────────────────────────────────────────────────────

// CycleContext carries everything needed for one pipeline iteration.
// It is passed by pointer so each stage can annotate it without copies.
type CycleContext struct {
	SessionID       string
	CycleNumber     int64
	Mode            Mode
	StrategyID      string
	StrategyVersion string
	Event           *MarketEvent
	Signal          *Signal
	Order           *Order
	RiskResult      *RiskCheckResult
	Fill            *Fill
	Metrics         *Metrics
	AccountEquity   float64
	Positions       []*Position
	Errors          []error
}

// AddError appends to the error list without panicking.
func (c *CycleContext) AddError(err error) {
	c.Errors = append(c.Errors, err)
}

// Failed returns true if any non-nil error was recorded.
func (c *CycleContext) Failed() bool {
	return len(c.Errors) > 0
}

// ─────────────────────────────────────────────────────────────────────────────
// Fill model parameters  (used by the simulated adapter)
// ─────────────────────────────────────────────────────────────────────────────

// FillModelParams controls the simulated execution model.
type FillModelParams struct {
	// Slippage as a fraction of price, e.g. 0.0005 = 0.05 %
	SlippageFraction float64
	// Taker fee as a fraction, e.g. 0.0006 = 0.06 %
	TakerFeeFraction float64
	// Simulated fill latency in milliseconds
	LatencyMS int64
}

// DefaultFillModel returns conservative, realistic defaults.
func DefaultFillModel() FillModelParams {
	return FillModelParams{
		SlippageFraction: 0.0005,
		TakerFeeFraction: 0.0006,
		LatencyMS:        120,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

// IsOpenAction returns true for entry actions.
func IsOpenAction(a SignalAction) bool {
	return a == ActionOpenLong || a == ActionOpenShort
}

// IsCloseAction returns true for exit actions.
func IsCloseAction(a SignalAction) bool {
	return a == ActionCloseLong || a == ActionCloseShort
}

// ErrRejected is returned when an order is refused by risk control.
var ErrRejected = fmt.Errorf("order rejected by risk control")
