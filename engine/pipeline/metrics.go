// Package pipeline – metrics collector.
//
// RunningMetrics maintains cumulative statistics across the entire session.
// It is updated after every cycle and emits a Metrics snapshot to the event log.
// The same collector runs in backtest, paper, and live modes.
package pipeline

import (
	"context"
	"math"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
)

// RunningMetrics implements MetricsCollector and tracks full session statistics.
type RunningMetrics struct {
	strategyID      string
	strategyVersion string
	mode            core.Mode

	startEquity  float64
	peakEquity   float64
	totalTrades  int
	wins         int
	losses       int
	realizedPnL  float64
	maxDrawdown  float64
	returns      []float64 // per-fill returns for Sharpe/Sortino
	grossProfit  float64
	grossLoss    float64

	lastEquity float64
}

// NewRunningMetrics creates a collector for a session.
func NewRunningMetrics(startEquity float64, strategyID, version string, mode core.Mode) *RunningMetrics {
	return &RunningMetrics{
		strategyID:      strategyID,
		strategyVersion: version,
		mode:            mode,
		startEquity:     startEquity,
		peakEquity:      startEquity,
		lastEquity:      startEquity,
	}
}

// Update is called each cycle by the pipeline.  It inspects the Fill (if any)
// to update running stats, then emits a Metrics snapshot.
func (m *RunningMetrics) Update(_ context.Context, cc *core.CycleContext) (*core.Metrics, error) {
	equity := cc.AccountEquity

	// Process fill
	if cc.Fill != nil {
		m.totalTrades++
		// Determine win/loss from the fill vs entry price.
		// The simulated adapter updates equity before the fill is returned,
		// so we track equity delta as a proxy for PnL per trade.
		delta := equity - m.lastEquity
		m.realizedPnL += delta
		if delta >= 0 {
			m.wins++
			m.grossProfit += delta
		} else {
			m.losses++
			m.grossLoss += math.Abs(delta)
		}
		if m.startEquity > 0 {
			m.returns = append(m.returns, delta/m.startEquity)
		}
	}

	m.lastEquity = equity

	// Drawdown
	if equity > m.peakEquity {
		m.peakEquity = equity
	}
	if m.peakEquity > 0 {
		dd := (m.peakEquity - equity) / m.peakEquity
		if dd > m.maxDrawdown {
			m.maxDrawdown = dd
		}
	}

	// Win rate
	winRate := 0.0
	if m.totalTrades > 0 {
		winRate = float64(m.wins) / float64(m.totalTrades)
	}

	// Profit factor
	pf := 0.0
	if m.grossLoss > 0 {
		pf = m.grossProfit / m.grossLoss
	} else if m.grossProfit > 0 {
		pf = math.MaxFloat32
	}

	// Net return
	netReturn := 0.0
	if m.startEquity > 0 {
		netReturn = (equity - m.startEquity) / m.startEquity
	}

	// Unrealised PnL
	unrealised := 0.0
	for _, p := range cc.Positions {
		unrealised += p.UnrealizedPnL
	}

	met := &core.Metrics{
		Timestamp:       time.Now(),
		Mode:            m.mode,
		StrategyID:      m.strategyID,
		StrategyVersion: m.strategyVersion,
		Equity:          equity,
		Available:       equity - unrealised,
		UnrealizedPnL:   unrealised,
		RealizedPnL:     m.realizedPnL,
		TotalTrades:     m.totalTrades,
		WinCount:        m.wins,
		LossCount:       m.losses,
		WinRate:         winRate,
		Drawdown:        (m.peakEquity - equity) / math.Max(m.peakEquity, 1),
		MaxDrawdown:     m.maxDrawdown,
		ProfitFactor:    pf,
	}

	// Sharpe and Sortino (annualised, assuming ~5-min bars)
	if len(m.returns) > 2 {
		met.SharpeRatio = sharpe(m.returns)
		met.SortinoRatio = sortino(m.returns)
	}

	_ = netReturn // available for caller via equity/startEquity
	return met, nil
}

// ─── Statistical helpers ──────────────────────────────────────────────────────

func mean(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func stdDev(v []float64, mu float64) float64 {
	s := 0.0
	for _, x := range v {
		d := x - mu
		s += d * d
	}
	return math.Sqrt(s / float64(len(v)))
}

func downDev(v []float64, mu float64) float64 {
	s := 0.0
	n := 0
	for _, x := range v {
		if x < mu {
			d := x - mu
			s += d * d
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return math.Sqrt(s / float64(n))
}

// barsPerYear for 5-minute bars
const barsPerYear = 365 * 24 * 12

func sharpe(returns []float64) float64 {
	mu := mean(returns)
	sd := stdDev(returns, mu)
	if sd == 0 {
		return 0
	}
	return mu / sd * math.Sqrt(barsPerYear)
}

func sortino(returns []float64) float64 {
	mu := mean(returns)
	dd := downDev(returns, mu)
	if dd == 0 {
		return 0
	}
	return mu / dd * math.Sqrt(barsPerYear)
}
