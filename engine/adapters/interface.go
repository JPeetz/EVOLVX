// Package adapters defines the single interface that separates the engine
// pipeline from exchange-specific or simulation-specific code.
package adapters

import (
	"context"

	"github.com/NoFxAiOS/nofx/engine/core"
)

// ─────────────────────────────────────────────────────────────────────────────
// ExecutionAdapter
// ─────────────────────────────────────────────────────────────────────────────

// ExecutionAdapter is the only seam between the pipeline and the outside world.
// Every execution mode — backtest, paper, live — must implement this interface.
// Nothing in the pipeline touches an exchange SDK directly.
type ExecutionAdapter interface {
	// Mode returns which execution context this adapter provides.
	Mode() core.Mode

	// SubmitOrder sends an order for execution and returns its initial state.
	// The adapter must NOT block until fill; return immediately with status
	// OrderPending or OrderFilled (for synchronous simulated fills).
	SubmitOrder(ctx context.Context, order *core.Order) (*core.Order, error)

	// QueryOrder returns the current state of a previously submitted order.
	// Used to poll for fills in async adapters.
	QueryOrder(ctx context.Context, orderID string) (*core.Order, error)

	// CancelOrder cancels a pending order. Idempotent.
	CancelOrder(ctx context.Context, orderID string) error

	// GetPositions returns all currently open positions visible to this adapter.
	GetPositions(ctx context.Context) ([]*core.Position, error)

	// GetBalance returns the current account balance as equity/available.
	GetBalance(ctx context.Context) (equity, available float64, err error)

	// Close releases resources.  Called when the session ends.
	Close() error
}

// ─────────────────────────────────────────────────────────────────────────────
// MarketFeed
// ─────────────────────────────────────────────────────────────────────────────

// MarketFeed is the source of MarketEvents for one session.
// For backtest it replays historical bars; for paper/live it delivers real-time.
type MarketFeed interface {
	// Next blocks until the next event is available.
	// Returns (nil, io.EOF) when the feed is exhausted (backtest end).
	Next(ctx context.Context) (*core.MarketEvent, error)

	// Close releases resources.
	Close() error
}

// ─────────────────────────────────────────────────────────────────────────────
// EventLogger
// ─────────────────────────────────────────────────────────────────────────────

// EventLogger persists every pipeline log entry in order.
// All three modes use the same logger interface.
type EventLogger interface {
	// Log appends one entry.  Must be safe for concurrent calls.
	Log(entry *core.LogEntry) error

	// Close flushes and releases resources.
	Close() error
}
