package pipeline_test

import (
	"context"
	"testing"
	"time"

	"github.com/NoFxAiOS/nofx/engine/adapters"
	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/engine/feeds"
	"github.com/NoFxAiOS/nofx/engine/pipeline"
	"github.com/stretchr/testify/require"
)

// ─────────────────────────────────────────────────────────────────────────────
// Test 1: Backtest and paper produce identical fills given identical events
// ─────────────────────────────────────────────────────────────────────────────

func TestModeParityBacktestVsPaper(t *testing.T) {
	events := makeTestEvents(50)

	backtestFills := collectFills(t, core.ModeBacktest, events)
	paperFills := collectFills(t, core.ModePaper, events)

	require.Equal(t, len(backtestFills), len(paperFills),
		"backtest and paper must produce the same number of fills")

	for i := range backtestFills {
		bt := backtestFills[i]
		pp := paperFills[i]
		require.Equal(t, bt.Symbol, pp.Symbol, "fill[%d] symbol mismatch", i)
		require.Equal(t, bt.Side, pp.Side, "fill[%d] side mismatch", i)
		require.InDelta(t, bt.FilledQty, pp.FilledQty, 0.0001,
			"fill[%d] qty mismatch: bt=%.4f paper=%.4f", i, bt.FilledQty, pp.FilledQty)
		require.InDelta(t, bt.FilledPrice, pp.FilledPrice, 0.01,
			"fill[%d] price mismatch (same slippage model must apply)", i)
		require.InDelta(t, bt.Fee, pp.Fee, 0.0001,
			"fill[%d] fee mismatch (same fee model must apply)", i)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 2: The pipeline is the only path — no direct order placement
// ─────────────────────────────────────────────────────────────────────────────

func TestAllFillsPassThroughPipeline(t *testing.T) {
	mock := &captureAdapter{mode: core.ModePaper, equity: 10000}
	events := makeTestEvents(10)

	p, err := pipeline.New(pipeline.Config{
		Mode:            core.ModePaper,
		StrategyID:      "test",
		StrategyVersion: "1.0.0",
		Feed:            feeds.NewSliceReplayFeed(events),
		Adapter:         mock,
		Evaluator:       deterministicEvaluator("BTCUSDT", core.ActionOpenLong),
		Risk:            permissiveRisk{},
	})
	require.NoError(t, err)
	require.NoError(t, p.Run(context.Background()))

	// Every order the adapter received must have been routed through pipeline.execute()
	for _, order := range mock.orders {
		require.Equal(t, core.ModePaper, order.Mode,
			"order must carry the pipeline mode, not be injected directly")
		require.NotEmpty(t, order.StrategyID, "every order must carry a strategy ID")
		require.NotEmpty(t, order.StrategyVersion, "every order must carry a strategy version")
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Test 3: Simulated adapter equity is consistent after fills
// ─────────────────────────────────────────────────────────────────────────────

func TestSimulatedAdapterEquityConsistency(t *testing.T) {
	const initialEquity = 10000.0
	sim := adapters.NewSimulatedAdapter(initialEquity, core.ModeBacktest, core.DefaultFillModel())
	ctx := context.Background()

	// Open a long
	sim.SetCurrentPrice("BTCUSDT", 50000.0)
	order := &core.Order{
		OrderID:         "o1",
		Mode:            core.ModeBacktest,
		Symbol:          "BTCUSDT",
		Side:            core.SideBuy,
		Type:            core.OrderMarket,
		Quantity:        1000, // 1000 USDT notional
		Leverage:        5,
		StrategyID:      "test",
		StrategyVersion: "1.0.0",
	}
	filled, err := sim.SubmitOrder(ctx, order)
	require.NoError(t, err)
	require.Equal(t, core.OrderFilled, filled.Status)

	equity1, avail1, err := sim.GetBalance(ctx)
	require.NoError(t, err)
	require.Less(t, avail1, initialEquity, "available must decrease after margin reserved")
	t.Logf("after open: equity=%.2f available=%.2f", equity1, avail1)

	// Close the long (prices moved up → profit)
	sim.SetCurrentPrice("BTCUSDT", 51000.0)
	sim.UpdatePositionMarkPrices(map[string]float64{"BTCUSDT": 51000.0})

	closeOrder := &core.Order{
		OrderID: "o2", Mode: core.ModeBacktest,
		Symbol: "BTCUSDT", Side: core.SideSell,
		Type: core.OrderMarket, Quantity: order.Quantity,
		Leverage: 5, StrategyID: "test", StrategyVersion: "1.0.0",
	}
	filled2, err := sim.SubmitOrder(ctx, closeOrder)
	require.NoError(t, err)
	require.Equal(t, core.OrderFilled, filled2.Status)

	equity2, avail2, err := sim.GetBalance(ctx)
	require.NoError(t, err)
	require.Greater(t, equity2, initialEquity, "equity must increase after profitable close")
	require.InDelta(t, equity2, avail2, 1.0, "all margin returned after close")
	t.Logf("after close: equity=%.2f available=%.2f (profit=%.2f)", equity2, avail2, equity2-initialEquity)
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func makeTestEvents(n int) []*core.MarketEvent {
	events := make([]*core.MarketEvent, n)
	base := 50000.0
	for i := 0; i < n; i++ {
		price := base + float64(i)*10
		events[i] = &core.MarketEvent{
			EventID:   fmt.Sprintf("evt-%d", i),
			Symbol:    "BTCUSDT",
			Timestamp: time.Now().Add(time.Duration(i) * 5 * time.Minute),
			Open:      price,
			High:      price * 1.001,
			Low:       price * 0.999,
			Close:     price,
			Volume:    1000,
			Timeframe: "5m",
			Indicators: map[string]float64{
				"ema20": price * 0.99,
				"rsi7":  55.0,
			},
		}
	}
	return events
}

func collectFills(t *testing.T, mode core.Mode, events []*core.MarketEvent) []*core.Fill {
	t.Helper()
	cap := &captureAdapter{mode: mode, equity: 10000}

	// Re-slice so both modes see identical events from separate copies
	eventsCopy := make([]*core.MarketEvent, len(events))
	copy(eventsCopy, events)

	p, err := pipeline.New(pipeline.Config{
		Mode:            mode,
		StrategyID:      "test-strategy",
		StrategyVersion: "1.0.0",
		Feed:            feeds.NewSliceReplayFeed(eventsCopy),
		Adapter:         cap,
		Evaluator:       deterministicEvaluator("BTCUSDT", core.ActionOpenLong),
		Risk:            permissiveRisk{},
	})
	require.NoError(t, err)
	require.NoError(t, p.Run(context.Background()))
	return cap.fills
}

// captureAdapter records all orders and fills without external side-effects.
type captureAdapter struct {
	mode   core.Mode
	equity float64
	orders []*core.Order
	fills  []*core.Fill
}

func (a *captureAdapter) Mode() core.Mode { return a.mode }
func (a *captureAdapter) SubmitOrder(_ context.Context, o *core.Order) (*core.Order, error) {
	// Apply a deterministic fill: market price from order price field
	price := 50000.0
	qty := o.Quantity / price // convert USD notional to coin qty
	fee := qty * price * 0.0006
	o.Status = core.OrderFilled
	o.Price = price
	a.orders = append(a.orders, o)
	f := &core.Fill{
		FillID:          fmt.Sprintf("fill-%d", len(a.fills)),
		OrderID:         o.OrderID,
		Symbol:          o.Symbol,
		Side:            o.Side,
		FilledQty:       qty,
		FilledPrice:     price * 1.0005, // slippage
		Fee:             fee,
		Timestamp:       time.Now(),
		Mode:            a.mode,
		StrategyID:      o.StrategyID,
		StrategyVersion: o.StrategyVersion,
	}
	a.fills = append(a.fills, f)
	return o, nil
}
func (a *captureAdapter) QueryOrder(_ context.Context, _ string) (*core.Order, error) {
	return nil, nil
}
func (a *captureAdapter) CancelOrder(_ context.Context, _ string) error { return nil }
func (a *captureAdapter) GetPositions(_ context.Context) ([]*core.Position, error) {
	return nil, nil
}
func (a *captureAdapter) GetBalance(_ context.Context) (float64, float64, error) {
	return a.equity, a.equity, nil
}
func (a *captureAdapter) Close() error { return nil }

// deterministicEvaluator always emits the same signal for every event.
func deterministicEvaluator(symbol string, action core.SignalAction) pipeline.StrategyEvaluator {
	return &fixedEvaluator{symbol: symbol, action: action}
}

type fixedEvaluator struct {
	symbol string
	action core.SignalAction
}

func (e *fixedEvaluator) Evaluate(_ context.Context, cc *core.CycleContext) ([]*core.Signal, error) {
	return []*core.Signal{{
		SignalID:        "sig-test",
		StrategyID:      cc.StrategyID,
		StrategyVersion: cc.StrategyVersion,
		Symbol:          e.symbol,
		Timestamp:       time.Now(),
		Action:          e.action,
		Leverage:        3,
		PositionSizeUSD: 300,
		Confidence:      80,
	}}, nil
}

// permissiveRisk approves everything.
type permissiveRisk struct{}

func (permissiveRisk) Check(_ context.Context, o *core.Order, _ []*core.Position, _ float64) core.RiskCheckResult {
	return core.RiskCheckResult{Approved: true}
}

// fmt import needed for sprintf in captureAdapter
var _ = fmt.Sprintf
