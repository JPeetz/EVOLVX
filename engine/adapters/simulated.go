// Package adapters – simulated adapter.
// Used for BOTH backtest and paper modes.  The only difference is that paper
// mode also publishes orders to a paper-order log visible in the dashboard.
package adapters

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// SimulatedAdapter
// ─────────────────────────────────────────────────────────────────────────────

// SimulatedAdapter provides deterministic, fee-aware execution for backtest
// and paper modes.  It maintains an in-memory book of positions and equity so
// that the pipeline sees the same state transitions as live.
type SimulatedAdapter struct {
	mode      core.Mode
	fillModel core.FillModelParams
	mu        sync.Mutex

	equity    float64
	cash      float64 // available (un-margined) cash
	positions map[string]*core.Position // symbol → position
	orders    map[string]*core.Order    // orderID → order

	// currentPrice is set by the pipeline before each SubmitOrder call
	// so the fill model can use the latest close price.
	currentPrices map[string]float64
}

// NewSimulatedAdapter creates a fresh adapter.
//
//	initialEquity: starting capital in USDT
//	mode:          core.ModeBacktest or core.ModePaper
//	fillModel:     slippage/fee/latency params
func NewSimulatedAdapter(
	initialEquity float64,
	mode core.Mode,
	fillModel core.FillModelParams,
) *SimulatedAdapter {
	return &SimulatedAdapter{
		mode:          mode,
		fillModel:     fillModel,
		equity:        initialEquity,
		cash:          initialEquity,
		positions:     make(map[string]*core.Position),
		orders:        make(map[string]*core.Order),
		currentPrices: make(map[string]float64),
	}
}

// SetCurrentPrice is called by the pipeline before SubmitOrder so the fill
// model always uses the correct bar's close price.
func (s *SimulatedAdapter) SetCurrentPrice(symbol string, price float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentPrices[symbol] = price
}

// ─── ExecutionAdapter implementation ─────────────────────────────────────────

func (s *SimulatedAdapter) Mode() core.Mode { return s.mode }

// SubmitOrder performs an immediate synchronous simulated fill.
func (s *SimulatedAdapter) SubmitOrder(ctx context.Context, order *core.Order) (*core.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	price, ok := s.currentPrices[order.Symbol]
	if !ok || price <= 0 {
		order.Status = core.OrderRejected
		return order, fmt.Errorf("simulated adapter: no current price for %s", order.Symbol)
	}

	// Apply slippage: buys pay a little more, sells receive a little less
	slip := s.fillModel.SlippageFraction
	var fillPrice float64
	switch order.Side {
	case core.SideBuy:
		fillPrice = price * (1 + slip)
	case core.SideSell:
		fillPrice = price * (1 - slip)
	}

	// Fee on notional value
	notional := order.Quantity * fillPrice
	fee := notional * s.fillModel.TakerFeeFraction

	// Margin required
	margin := notional / float64(order.Leverage)

	// Check available cash
	if order.Side == core.SideBuy && margin+fee > s.cash {
		order.Status = core.OrderRejected
		return order, fmt.Errorf("simulated adapter: insufficient margin %.2f required, %.2f available",
			margin+fee, s.cash)
	}

	// Apply fill to book
	switch order.Side {
	case core.SideBuy:
		s.cash -= margin + fee
		side := "long"
		if order.Side == core.SideSell {
			side = "short"
		}
		pos := &core.Position{
			Symbol:          order.Symbol,
			Side:            side,
			EntryPrice:      fillPrice,
			MarkPrice:       fillPrice,
			Quantity:        order.Quantity,
			Leverage:        order.Leverage,
			UnrealizedPnL:   0,
			StrategyID:      order.StrategyID,
			StrategyVersion: order.StrategyVersion,
			OpenedAt:        time.Now(),
		}
		// Set liquidation price (rough model: entry - entry/leverage * 0.9)
		pos.LiquidationPrice = fillPrice * (1 - 0.9/float64(order.Leverage))
		s.positions[order.Symbol] = pos

	case core.SideSell:
		// Closing: realise PnL
		if pos, exists := s.positions[order.Symbol]; exists {
			var pnl float64
			if pos.Side == "long" {
				pnl = (fillPrice - pos.EntryPrice) * pos.Quantity
			} else {
				pnl = (pos.EntryPrice - fillPrice) * pos.Quantity
			}
			s.cash += (pos.Quantity*pos.EntryPrice/float64(pos.Leverage)) + pnl - fee
			s.equity += pnl - fee
			delete(s.positions, order.Symbol)
		}
	}

	order.Status = core.OrderFilled
	order.UpdatedAt = time.Now()
	s.orders[order.OrderID] = order
	return order, nil
}

func (s *SimulatedAdapter) QueryOrder(_ context.Context, orderID string) (*core.Order, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o, ok := s.orders[orderID]
	if !ok {
		return nil, fmt.Errorf("simulated adapter: order %s not found", orderID)
	}
	return o, nil
}

func (s *SimulatedAdapter) CancelOrder(_ context.Context, orderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if o, ok := s.orders[orderID]; ok {
		o.Status = core.OrderCancelled
	}
	return nil
}

func (s *SimulatedAdapter) GetPositions(_ context.Context) ([]*core.Position, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*core.Position, 0, len(s.positions))
	for _, p := range s.positions {
		cp := *p
		out = append(out, &cp)
	}
	return out, nil
}

func (s *SimulatedAdapter) GetBalance(_ context.Context) (equity, available float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.equity, s.cash, nil
}

func (s *SimulatedAdapter) Close() error { return nil }

// ─── Fill helper ─────────────────────────────────────────────────────────────

// BuildFill converts a filled order into a Fill record.  Called by the
// pipeline after SubmitOrder returns a filled order.
func BuildFill(order *core.Order, fillPrice, fee, slippage float64) *core.Fill {
	return &core.Fill{
		FillID:          uuid.NewString(),
		OrderID:         order.OrderID,
		Symbol:          order.Symbol,
		Side:            order.Side,
		FilledQty:       order.Quantity,
		FilledPrice:     fillPrice,
		Fee:             fee,
		FeeCurrency:     "USDT",
		Slippage:        slippage,
		LatencyMS:       0, // synchronous sim
		Timestamp:       time.Now(),
		Mode:            order.Mode,
		StrategyID:      order.StrategyID,
		StrategyVersion: order.StrategyVersion,
	}
}

// UpdatePositionMarkPrices sweeps all open positions with the latest price
// for mark-to-market unrealised PnL.  Called each bar by the pipeline.
func (s *SimulatedAdapter) UpdatePositionMarkPrices(prices map[string]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	unrealized := 0.0
	for sym, pos := range s.positions {
		if mp, ok := prices[sym]; ok {
			pos.MarkPrice = mp
			if pos.Side == "long" {
				pos.UnrealizedPnL = (mp - pos.EntryPrice) * pos.Quantity
			} else {
				pos.UnrealizedPnL = (pos.EntryPrice - mp) * pos.Quantity
			}
			// Rough liquidation check
			if pos.Side == "long" && mp <= pos.LiquidationPrice {
				// Force-liquidate: total margin loss
				s.equity -= math.Abs(pos.UnrealizedPnL)
				delete(s.positions, sym)
				continue
			}
		}
		unrealized += pos.UnrealizedPnL
	}
	// equity = cash (margin out) + unrealised
	s.equity = s.cash + unrealized
	for range prices { break } // prevent unused warning
}
