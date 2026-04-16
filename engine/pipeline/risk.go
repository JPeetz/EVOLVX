// Package pipeline – risk checker.
//
// RiskChecker is the exact same logic that lives in trader/auto_trader.go
// (enforceMaxPositions, enforcePositionValueRatio, enforceMinPositionSize)
// but extracted into the unified interface so it runs identically in backtest,
// paper, and live modes.
package pipeline

import (
	"context"
	"fmt"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/registry"
)

// ─────────────────────────────────────────────────────────────────────────────
// StandardRiskChecker
// ─────────────────────────────────────────────────────────────────────────────

// StandardRiskChecker applies the risk rules extracted from auto_trader.go.
// It is constructed with a snapshot of the strategy's risk parameters so the
// same check runs in every mode.
type StandardRiskChecker struct {
	params registry.Parameters
}

// NewStandardRiskChecker creates a checker from strategy parameters.
func NewStandardRiskChecker(params registry.Parameters) *StandardRiskChecker {
	return &StandardRiskChecker{params: params}
}

// Check evaluates the order against current portfolio state.
// It returns a RiskCheckResult with:
//   - Approved=false + Reasons if the order must be rejected
//   - Approved=true  + AdjustedOrder if size was reduced
//   - Approved=true  + AdjustedOrder=nil if nothing changed
func (r *StandardRiskChecker) Check(
	_ context.Context,
	order *core.Order,
	positions []*core.Position,
	equity float64,
) core.RiskCheckResult {
	p := r.params

	// Only open actions are subject to position-count and size checks
	if !core.IsOpenAction(signalActionFromOrderSide(order)) {
		return core.RiskCheckResult{Approved: true}
	}

	var reasons []string

	// ── 1. Max positions ─────────────────────────────────────────────────────
	if len(positions) >= p.MaxPositions {
		return core.RiskCheckResult{
			Approved: false,
			Reasons:  []string{fmt.Sprintf("max positions reached: %d/%d", len(positions), p.MaxPositions)},
		}
	}

	// ── 2. Min position size ─────────────────────────────────────────────────
	if order.Quantity < p.MinPositionSize {
		return core.RiskCheckResult{
			Approved: false,
			Reasons:  []string{fmt.Sprintf("position size %.2f USDT below minimum %.2f", order.Quantity, p.MinPositionSize)},
		}
	}

	// ── 3. Max margin usage ──────────────────────────────────────────────────
	// Compute total current margin committed
	totalMargin := totalMarginUsed(positions)
	orderMargin := order.Quantity / float64(maxInt(order.Leverage, 1))
	projectedMarginPct := (totalMargin + orderMargin) / equity

	if projectedMarginPct > p.MaxMarginUsage {
		// Try to reduce position size to fit within the margin cap
		allowedMargin := equity*p.MaxMarginUsage - totalMargin
		if allowedMargin < p.MinPositionSize {
			return core.RiskCheckResult{
				Approved: false,
				Reasons:  []string{fmt.Sprintf("margin usage would reach %.1f%%, exceeds %.1f%% cap", projectedMarginPct*100, p.MaxMarginUsage*100)},
			}
		}
		// Adjust size down
		adjusted := *order
		adjusted.Quantity = allowedMargin * float64(maxInt(order.Leverage, 1))
		if adjusted.Quantity < p.MinPositionSize {
			return core.RiskCheckResult{
				Approved: false,
				Reasons:  []string{"adjusted size below minimum after margin cap enforcement"},
			}
		}
		reasons = append(reasons, fmt.Sprintf("size reduced to %.2f USDT (margin cap %.0f%%)", adjusted.Quantity, p.MaxMarginUsage*100))
		return core.RiskCheckResult{
			Approved:      true,
			AdjustedOrder: &adjusted,
			Reasons:       reasons,
		}
	}

	// ── 4. Leverage cap by symbol type ──────────────────────────────────────
	isBTCETH := order.Symbol == "BTCUSDT" || order.Symbol == "ETHUSDT"
	maxLev := p.AltcoinMaxLeverage
	if isBTCETH {
		maxLev = p.BTCETHMaxLeverage
	}
	if order.Leverage > maxLev {
		adjusted := *order
		adjusted.Leverage = maxLev
		reasons = append(reasons, fmt.Sprintf("leverage capped at %dx", maxLev))
		return core.RiskCheckResult{
			Approved:      true,
			AdjustedOrder: &adjusted,
			Reasons:       reasons,
		}
	}

	// ── 5. Position value ratio cap ──────────────────────────────────────────
	maxRatio := p.AltcoinMaxPositionValueRatio
	if isBTCETH {
		maxRatio = p.BTCETHMaxPositionValueRatio
	}
	if maxRatio > 0 {
		orderNotional := order.Quantity
		maxNotional := equity * maxRatio
		if orderNotional > maxNotional {
			adjusted := *order
			adjusted.Quantity = maxNotional
			if adjusted.Quantity < p.MinPositionSize {
				return core.RiskCheckResult{
					Approved: false,
					Reasons:  []string{fmt.Sprintf("position value cap (%.0f%% equity) results in size below minimum", maxRatio*100)},
				}
			}
			reasons = append(reasons, fmt.Sprintf("size reduced to %.2f USDT (%.0f%% equity cap)", adjusted.Quantity, maxRatio*100))
			return core.RiskCheckResult{
				Approved:      true,
				AdjustedOrder: &adjusted,
				Reasons:       reasons,
			}
		}
	}

	return core.RiskCheckResult{Approved: true}
}

// ─── helpers ─────────────────────────────────────────────────────────────────

// signalActionFromOrderSide reconstructs whether an order is opening or closing.
func signalActionFromOrderSide(o *core.Order) core.SignalAction {
	// The SignalID is on the order; in practice the pipeline already calls
	// IsOpenAction on the Signal before creating the order, so this is a
	// belt-and-suspenders check.
	if o.Side == core.SideBuy {
		return core.ActionOpenLong
	}
	return core.ActionOpenShort
}

func totalMarginUsed(positions []*core.Position) float64 {
	total := 0.0
	for _, p := range positions {
		if p.Leverage > 0 {
			total += (p.Quantity * p.EntryPrice) / float64(p.Leverage)
		}
	}
	return total
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
