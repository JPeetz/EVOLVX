// Package adapters – live adapter.
// LiveAdapter wraps the existing trader.Trader interface so the pipeline can
// call it through the same ExecutionAdapter contract used by the simulated
// adapter.  No exchange SDK is imported here; the existing trader package
// handles that.
package adapters

import (
	"context"
	"fmt"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// ExchangeClient  — narrow interface over the existing trader.Trader
// ─────────────────────────────────────────────────────────────────────────────

// ExchangeClient is the subset of the existing Trader behaviour that the live
// adapter needs.  The concrete *trader.Trader already satisfies this; we keep
// the interface narrow to avoid coupling.
type ExchangeClient interface {
	PlaceOrder(symbol, side, orderType string, qty, price float64, leverage int) (string, error)
	GetOrderStatus(orderID string) (status string, filledQty, avgPrice, fee float64, err error)
	CancelOrder(orderID string) error
	GetPositions() ([]ExchangePosition, error)
	GetBalance() (totalEquity, available float64, err error)
}

// ExchangePosition mirrors the position struct from the trader package.
type ExchangePosition struct {
	Symbol           string
	Side             string
	EntryPrice       float64
	MarkPrice        float64
	Quantity         float64
	Leverage         int
	UnrealizedPnL    float64
	LiquidationPrice float64
}

// ─────────────────────────────────────────────────────────────────────────────
// LiveAdapter
// ─────────────────────────────────────────────────────────────────────────────

// LiveAdapter implements ExecutionAdapter for real exchange connectivity.
// It delegates every operation to the injected ExchangeClient.
type LiveAdapter struct {
	client ExchangeClient
}

// NewLiveAdapter wraps an existing exchange client.
func NewLiveAdapter(client ExchangeClient) *LiveAdapter {
	return &LiveAdapter{client: client}
}

func (l *LiveAdapter) Mode() core.Mode { return core.ModeLive }

// SubmitOrder sends the order to the real exchange and returns immediately
// with a pending-status order.  The pipeline's fill-polling loop calls
// QueryOrder until the order is filled.
func (l *LiveAdapter) SubmitOrder(_ context.Context, order *core.Order) (*core.Order, error) {
	side := string(order.Side)
	orderType := string(order.Type)

	exchangeOrderID, err := l.client.PlaceOrder(
		order.Symbol,
		side,
		orderType,
		order.Quantity,
		order.Price,
		order.Leverage,
	)
	if err != nil {
		order.Status = core.OrderRejected
		return order, fmt.Errorf("live adapter: place order: %w", err)
	}

	// Store the exchange order ID in ClientOrderID for tracking
	order.ClientOrderID = exchangeOrderID
	order.Status = core.OrderPending
	order.UpdatedAt = time.Now()
	return order, nil
}

// QueryOrder polls the exchange for the current order state.
func (l *LiveAdapter) QueryOrder(_ context.Context, orderID string) (*core.Order, error) {
	status, filledQty, avgPrice, fee, err := l.client.GetOrderStatus(orderID)
	if err != nil {
		return nil, fmt.Errorf("live adapter: query order %s: %w", orderID, err)
	}

	order := &core.Order{
		OrderID:   uuid.NewString(),
		ClientOrderID: orderID,
		UpdatedAt: time.Now(),
	}

	switch status {
	case "FILLED":
		order.Status = core.OrderFilled
		order.Quantity = filledQty
		order.Price = avgPrice
		_ = fee
	case "PARTIAL":
		order.Status = core.OrderPartial
		order.Quantity = filledQty
	case "CANCELLED":
		order.Status = core.OrderCancelled
	default:
		order.Status = core.OrderPending
	}

	return order, nil
}

func (l *LiveAdapter) CancelOrder(_ context.Context, orderID string) error {
	return l.client.CancelOrder(orderID)
}

func (l *LiveAdapter) GetPositions(_ context.Context) ([]*core.Position, error) {
	eps, err := l.client.GetPositions()
	if err != nil {
		return nil, fmt.Errorf("live adapter: get positions: %w", err)
	}
	out := make([]*core.Position, 0, len(eps))
	for _, ep := range eps {
		out = append(out, &core.Position{
			Symbol:           ep.Symbol,
			Side:             ep.Side,
			EntryPrice:       ep.EntryPrice,
			MarkPrice:        ep.MarkPrice,
			Quantity:         ep.Quantity,
			Leverage:         ep.Leverage,
			UnrealizedPnL:    ep.UnrealizedPnL,
			LiquidationPrice: ep.LiquidationPrice,
		})
	}
	return out, nil
}

func (l *LiveAdapter) GetBalance(_ context.Context) (float64, float64, error) {
	return l.client.GetBalance()
}

func (l *LiveAdapter) Close() error { return nil }
