// Package pipeline implements the unified processing loop.
//
// Every mode — backtest, paper, live — passes through this exact sequence:
//
//   MarketEvent → StrategyEvaluator → RiskChecker → ExecutionAdapter → FillLogger → MetricsUpdater
//
// Nothing is duplicated across modes. Mode-specific behaviour lives only in
// the injected components (adapters, feeds, loggers).
package pipeline

import (
	"context"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/NoFxAiOS/nofx/engine/adapters"
	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Component interfaces
// ─────────────────────────────────────────────────────────────────────────────

// StrategyEvaluator evaluates a market event against a strategy version and
// returns a set of signals.  The AI call, prompt construction, and response
// parsing all live inside concrete implementations.
type StrategyEvaluator interface {
	Evaluate(ctx context.Context, cc *core.CycleContext) ([]*core.Signal, error)
}

// RiskChecker applies risk rules and returns an (optionally adjusted) order.
type RiskChecker interface {
	Check(ctx context.Context, order *core.Order, positions []*core.Position, equity float64) core.RiskCheckResult
}

// MetricsCollector updates running metrics after each fill (or hold).
type MetricsCollector interface {
	Update(ctx context.Context, cc *core.CycleContext) (*core.Metrics, error)
}

// ─────────────────────────────────────────────────────────────────────────────
// Pipeline configuration
// ─────────────────────────────────────────────────────────────────────────────

// Config bundles every injected dependency.
type Config struct {
	Mode            core.Mode
	SessionID       string
	StrategyID      string
	StrategyVersion string

	Feed      adapters.MarketFeed
	Adapter   adapters.ExecutionAdapter
	Evaluator StrategyEvaluator
	Risk      RiskChecker
	Metrics   MetricsCollector
	Logger    adapters.EventLogger

	// FillPollInterval controls how often QueryOrder is called in live mode.
	FillPollInterval time.Duration
	// FillPollMaxAttempts is the maximum number of QueryOrder calls before
	// the order is treated as lost.
	FillPollMaxAttempts int
}

func (c *Config) defaults() {
	if c.SessionID == "" {
		c.SessionID = uuid.NewString()
	}
	if c.FillPollInterval == 0 {
		c.FillPollInterval = 500 * time.Millisecond
	}
	if c.FillPollMaxAttempts == 0 {
		c.FillPollMaxAttempts = 10
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Pipeline
// ─────────────────────────────────────────────────────────────────────────────

// Pipeline is the unified execution loop.
type Pipeline struct {
	cfg     Config
	cycleNo int64
}

// New creates a Pipeline from cfg.
func New(cfg Config) (*Pipeline, error) {
	cfg.defaults()
	if cfg.Feed == nil {
		return nil, fmt.Errorf("pipeline: Feed is required")
	}
	if cfg.Adapter == nil {
		return nil, fmt.Errorf("pipeline: Adapter is required")
	}
	if cfg.Evaluator == nil {
		return nil, fmt.Errorf("pipeline: Evaluator is required")
	}
	if cfg.Risk == nil {
		return nil, fmt.Errorf("pipeline: Risk is required")
	}
	return &Pipeline{cfg: cfg}, nil
}

// Run blocks until the feed is exhausted or ctx is cancelled.
func (p *Pipeline) Run(ctx context.Context) error {
	defer p.cfg.Feed.Close()
	defer p.cfg.Adapter.Close()
	if p.cfg.Logger != nil {
		defer p.cfg.Logger.Close()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		event, err := p.cfg.Feed.Next(ctx)
		if err == io.EOF {
			return nil // backtest finished
		}
		if err != nil {
			return fmt.Errorf("pipeline: feed error: %w", err)
		}

		if err := p.processCycle(ctx, event); err != nil {
			// Log but don't abort — a single bad cycle should not kill the run
			p.logError(err)
		}
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Core cycle: market → signal → risk → order → fill → metrics
// ─────────────────────────────────────────────────────────────────────────────

func (p *Pipeline) processCycle(ctx context.Context, event *core.MarketEvent) error {
	p.cycleNo++

	equity, available, err := p.cfg.Adapter.GetBalance(ctx)
	if err != nil {
		return fmt.Errorf("cycle %d: get balance: %w", p.cycleNo, err)
	}
	positions, err := p.cfg.Adapter.GetPositions(ctx)
	if err != nil {
		return fmt.Errorf("cycle %d: get positions: %w", p.cycleNo, err)
	}

	cc := &core.CycleContext{
		SessionID:       p.cfg.SessionID,
		CycleNumber:     p.cycleNo,
		Mode:            p.cfg.Mode,
		StrategyID:      p.cfg.StrategyID,
		StrategyVersion: p.cfg.StrategyVersion,
		Event:           event,
		AccountEquity:   equity,
		Positions:       positions,
	}
	_ = available

	// 1. Log market event
	p.log(core.EventMarket, cc, event)

	// 2. Strategy evaluation
	signals, err := p.cfg.Evaluator.Evaluate(ctx, cc)
	if err != nil {
		cc.AddError(err)
		p.logError(err)
		return err
	}

	// Sort: close first, then open, then hold/wait  (same rule as existing code)
	sortSignals(signals)

	// 3–5. For each signal: risk check → order → fill
	for _, sig := range signals {
		cc.Signal = sig
		p.log(core.EventSignal, cc, sig)

		if sig.Action == core.ActionHold || sig.Action == core.ActionWait {
			continue
		}

		// Build order from signal
		order := signalToOrder(sig, p.cfg.Mode, equity)

		// Risk check
		rr := p.cfg.Risk.Check(ctx, order, positions, equity)
		cc.RiskResult = &rr
		p.log(core.EventRisk, cc, rr)

		if !rr.Approved {
			continue
		}
		if rr.AdjustedOrder != nil {
			order = rr.AdjustedOrder
		}
		cc.Order = order

		// Execute
		filled, fill, execErr := p.execute(ctx, order)
		if execErr != nil {
			cc.AddError(execErr)
			p.log(core.EventError, cc, execErr.Error())
			continue
		}
		cc.Order = filled
		cc.Fill = fill
		p.log(core.EventOrder, cc, filled)
		p.log(core.EventFill, cc, fill)
	}

	// 6. Metrics
	if p.cfg.Metrics != nil {
		m, mErr := p.cfg.Metrics.Update(ctx, cc)
		if mErr == nil && m != nil {
			cc.Metrics = m
			p.log(core.EventMetrics, cc, m)
		}
	}

	return nil
}

// execute submits an order and polls for fill.  Works identically for all modes.
func (p *Pipeline) execute(ctx context.Context, order *core.Order) (*core.Order, *core.Fill, error) {
	submitted, err := p.cfg.Adapter.SubmitOrder(ctx, order)
	if err != nil {
		return nil, nil, fmt.Errorf("submit order: %w", err)
	}

	// Simulated adapters fill synchronously
	if submitted.Status == core.OrderFilled {
		fill := &core.Fill{
			FillID:          uuid.NewString(),
			OrderID:         submitted.OrderID,
			Symbol:          submitted.Symbol,
			Side:            submitted.Side,
			FilledQty:       submitted.Quantity,
			FilledPrice:     submitted.Price,
			Timestamp:       time.Now(),
			Mode:            submitted.Mode,
			StrategyID:      submitted.StrategyID,
			StrategyVersion: submitted.StrategyVersion,
		}
		return submitted, fill, nil
	}

	// Live mode: poll until filled or max attempts
	for attempt := 0; attempt < p.cfg.FillPollMaxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return submitted, nil, ctx.Err()
		case <-time.After(p.cfg.FillPollInterval):
		}

		updated, err := p.cfg.Adapter.QueryOrder(ctx, submitted.ClientOrderID)
		if err != nil {
			continue
		}
		if updated.Status == core.OrderFilled {
			fill := &core.Fill{
				FillID:          uuid.NewString(),
				OrderID:         submitted.OrderID,
				Symbol:          submitted.Symbol,
				Side:            submitted.Side,
				FilledQty:       updated.Quantity,
				FilledPrice:     updated.Price,
				Timestamp:       time.Now(),
				Mode:            updated.Mode,
				StrategyID:      submitted.StrategyID,
				StrategyVersion: submitted.StrategyVersion,
			}
			return updated, fill, nil
		}
		if updated.Status == core.OrderCancelled || updated.Status == core.OrderRejected {
			return updated, nil, fmt.Errorf("order %s ended with status %s", submitted.OrderID, updated.Status)
		}
	}

	return submitted, nil, fmt.Errorf("order %s not filled after %d attempts", submitted.OrderID, p.cfg.FillPollMaxAttempts)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func signalToOrder(sig *core.Signal, mode core.Mode, equity float64) *core.Order {
	var side core.OrderSide
	switch sig.Action {
	case core.ActionOpenLong, core.ActionCloseLong:
		side = core.SideBuy
	case core.ActionOpenShort, core.ActionCloseShort:
		side = core.SideSell
	}

	qty := 0.0
	if sig.PositionSizeUSD > 0 && sig.Leverage > 0 {
		// qty = notional / (price * ... )  — price is resolved by adapter
		// Store in Quantity field as USD notional for now; adapter converts
		qty = sig.PositionSizeUSD
	}

	return &core.Order{
		OrderID:         uuid.NewString(),
		Mode:            mode,
		Symbol:          sig.Symbol,
		Side:            side,
		Type:            core.OrderMarket,
		Quantity:        qty,
		Leverage:        sig.Leverage,
		StopLoss:        sig.StopLoss,
		TakeProfit:      sig.TakeProfit,
		StrategyID:      sig.StrategyID,
		StrategyVersion: sig.StrategyVersion,
		SignalID:        sig.SignalID,
		Status:          core.OrderPending,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
}

var signalPriority = map[core.SignalAction]int{
	core.ActionCloseLong:  1,
	core.ActionCloseShort: 1,
	core.ActionOpenLong:   2,
	core.ActionOpenShort:  2,
	core.ActionHold:       3,
	core.ActionWait:       3,
}

func sortSignals(signals []*core.Signal) {
	sort.SliceStable(signals, func(i, j int) bool {
		return signalPriority[signals[i].Action] < signalPriority[signals[j].Action]
	})
}

func (p *Pipeline) log(kind core.EventKind, cc *core.CycleContext, payload any) {
	if p.cfg.Logger == nil {
		return
	}
	_ = p.cfg.Logger.Log(&core.LogEntry{
		EntryID:   uuid.NewString(),
		Kind:      kind,
		Timestamp: time.Now(),
		Mode:      cc.Mode,
		SessionID: cc.SessionID,
		Payload:   payload,
	})
}

func (p *Pipeline) logError(err error) {
	if p.cfg.Logger == nil || err == nil {
		return
	}
	_ = p.cfg.Logger.Log(&core.LogEntry{
		EntryID:   uuid.NewString(),
		Kind:      core.EventError,
		Timestamp: time.Now(),
		Mode:      p.cfg.Mode,
		SessionID: p.cfg.SessionID,
		Payload:   err.Error(),
	})
}
