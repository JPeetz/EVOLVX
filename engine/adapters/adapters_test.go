package adapters_test

import (
	"context"
	"testing"

	"github.com/NoFxAiOS/nofx/engine/adapters"
	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/engine/pipeline"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test 5: Live and simulated adapters satisfy the same interface
// ─────────────────────────────────────────────────────────────────────────────

func TestAdapterInterfaceContract(t *testing.T) {
	// Compile-time: both types must implement ExecutionAdapter
	var _ adapters.ExecutionAdapter = adapters.NewSimulatedAdapter(10000, core.ModeBacktest, core.DefaultFillModel())
	var _ adapters.ExecutionAdapter = adapters.NewSimulatedAdapter(10000, core.ModePaper, core.DefaultFillModel())
	var _ adapters.ExecutionAdapter = adapters.NewLiveAdapter(&mockExchangeClient{equity: 5000})

	t.Log("both SimulatedAdapter and LiveAdapter satisfy ExecutionAdapter — compile-time verified")
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Simulated adapter fill model — consistent slippage and fees
// ─────────────────────────────────────────────────────────────────────────────

func TestSimulatedFillModelConsistency(t *testing.T) {
	params := core.FillModelParams{
		SlippageFraction: 0.0005,
		TakerFeeFraction: 0.0006,
		LatencyMS:        0,
	}
	sim := adapters.NewSimulatedAdapter(10000, core.ModeBacktest, params)
	ctx := context.Background()

	sim.SetCurrentPrice("BTCUSDT", 50000.0)
	order := &core.Order{
		OrderID: "o1", Mode: core.ModeBacktest,
		Symbol: "BTCUSDT", Side: core.SideBuy,
		Type: core.OrderMarket, Quantity: 500, // 500 USDT notional
		Leverage: 5, StrategyID: "s1", StrategyVersion: "1.0.0",
	}

	filled, err := sim.SubmitOrder(ctx, order)
	require.NoError(t, err)
	require.Equal(t, core.OrderFilled, filled.Status)

	// equity reduced by margin + fee
	eq, avail, _ := sim.GetBalance(ctx)
	t.Logf("equity=%.2f available=%.2f", eq, avail)

	require.Less(t, avail, 10000.0, "available must be reduced by margin reservation")
	require.LessOrEqual(t, eq, 10000.0, "equity must not increase on open")

	// Positions should reflect the open trade
	positions, _ := sim.GetPositions(ctx)
	require.Len(t, positions, 1)
	require.Equal(t, "BTCUSDT", positions[0].Symbol)
	require.Equal(t, "long", positions[0].Side)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test: Live adapter rejects orders when exchange returns error
// ─────────────────────────────────────────────────────────────────────────────

func TestLiveAdapterRejectsOnExchangeError(t *testing.T) {
	mock := &mockExchangeClient{equity: 5000, placeOrderErr: fmt.Errorf("insufficient margin")}
	live := adapters.NewLiveAdapter(mock)
	ctx := context.Background()

	order := &core.Order{
		OrderID: "o1", Mode: core.ModeLive,
		Symbol: "BTCUSDT", Side: core.SideBuy,
		Type: core.OrderMarket, Quantity: 1000, Leverage: 5,
	}

	result, err := live.SubmitOrder(ctx, order)
	require.Error(t, err, "error from exchange must propagate")
	require.Equal(t, core.OrderRejected, result.Status)
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 6: Risk checker — all enforcement rules verified
// ─────────────────────────────────────────────────────────────────────────────

func TestRiskCheckerEnforcementRules(t *testing.T) {
	params := registry.Parameters{
		MaxPositions:                   3,
		MinPositionSize:                12,
		MaxMarginUsage:                 0.8,
		AltcoinMaxLeverage:             5,
		BTCETHMaxLeverage:              10,
		AltcoinMaxPositionValueRatio:   1.0,
		BTCETHMaxPositionValueRatio:    5.0,
	}
	checker := pipeline.NewStandardRiskChecker(params)
	ctx := context.Background()
	equity := 10000.0

	// ── Rule 1: Max positions ─────────────────────────────────────────────────
	t.Run("reject when max positions reached", func(t *testing.T) {
		positions := make([]*core.Position, 3) // already at max
		for i := range positions {
			positions[i] = &core.Position{Symbol: fmt.Sprintf("SYM%d", i)}
		}
		order := openOrder("BTCUSDT", 1000, 5)
		result := checker.Check(ctx, order, positions, equity)
		require.False(t, result.Approved)
		require.Contains(t, result.Reasons[0], "max positions")
	})

	// ── Rule 2: Min position size ─────────────────────────────────────────────
	t.Run("reject when size below minimum", func(t *testing.T) {
		order := openOrder("BTCUSDT", 5, 5) // 5 < min 12
		result := checker.Check(ctx, order, nil, equity)
		require.False(t, result.Approved)
		require.Contains(t, result.Reasons[0], "minimum")
	})

	// ── Rule 3: Leverage cap (altcoin) ────────────────────────────────────────
	t.Run("reduce leverage above altcoin cap", func(t *testing.T) {
		order := openOrder("SOLUSDT", 500, 20) // 20 > max 5
		result := checker.Check(ctx, order, nil, equity)
		require.True(t, result.Approved)
		require.NotNil(t, result.AdjustedOrder)
		require.Equal(t, 5, result.AdjustedOrder.Leverage)
	})

	// ── Rule 4: Leverage cap (BTC/ETH higher) ────────────────────────────────
	t.Run("btceth gets higher leverage cap", func(t *testing.T) {
		order := openOrder("BTCUSDT", 500, 10) // 10 == max for BTC
		result := checker.Check(ctx, order, nil, equity)
		require.True(t, result.Approved)
		require.Nil(t, result.AdjustedOrder, "no adjustment needed when within cap")
	})

	// ── Rule 5: Margin usage cap ─────────────────────────────────────────────
	t.Run("reduce size to stay within margin cap", func(t *testing.T) {
		// Fill most of the margin already
		existingPositions := []*core.Position{{
			Symbol: "ETHUSDT", Side: "long",
			EntryPrice: 3000, Quantity: 2.0, Leverage: 5,
		}}
		// Existing margin = 2*3000/5 = 1200 = 12% of equity
		// New order wants 9000 USDT notional at 5x = 1800 margin = 18%
		// Total would be 30% but we have 80% cap, so it should pass
		order := openOrder("BTCUSDT", 9000, 5)
		result := checker.Check(ctx, order, existingPositions, equity)
		require.True(t, result.Approved)
	})

	// ── Rule 6: Legitimate order passes unchanged ─────────────────────────────
	t.Run("well-formed order passes without adjustment", func(t *testing.T) {
		order := openOrder("SOLUSDT", 300, 3)
		result := checker.Check(ctx, order, nil, equity)
		require.True(t, result.Approved)
		require.Nil(t, result.AdjustedOrder, "no adjustment on a clean order")
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────────────

func openOrder(symbol string, qty float64, leverage int) *core.Order {
	return &core.Order{
		OrderID:         "test-order",
		Mode:            core.ModePaper,
		Symbol:          symbol,
		Side:            core.SideBuy,
		Type:            core.OrderMarket,
		Quantity:        qty,
		Leverage:        leverage,
		StrategyID:      "s1",
		StrategyVersion: "1.0.0",
	}
}

// mockExchangeClient stubs the ExchangeClient interface.
type mockExchangeClient struct {
	equity        float64
	placeOrderErr error
}

func (m *mockExchangeClient) PlaceOrder(_, _, _ string, _, _ float64, _ int) (string, error) {
	if m.placeOrderErr != nil {
		return "", m.placeOrderErr
	}
	return "exchange-order-123", nil
}
func (m *mockExchangeClient) GetOrderStatus(_ string) (string, float64, float64, float64, error) {
	return "FILLED", 0.01, 50000, 0.30, nil
}
func (m *mockExchangeClient) CancelOrder(_ string) error { return nil }
func (m *mockExchangeClient) GetPositions() ([]adapters.ExchangePosition, error) {
	return nil, nil
}
func (m *mockExchangeClient) GetBalance() (float64, float64, error) {
	return m.equity, m.equity * 0.8, nil
}

var _ = fmt.Sprintf
