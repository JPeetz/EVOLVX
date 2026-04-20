// Package outcome implements automatic outcome recording.
//
// When the pipeline processes a close signal (close_long / close_short),
// this package matches it to the original open decision in the journal
// and writes the Outcome — realized PnL, return %, holding period, exit reason.
//
// This eliminates the manual API calls that v1.1 required for outcome tracking.
// With this package running, journal outcomes are always populated automatically,
// which means AI memory context is always accurate and optimizer scores are real.
package outcome

import (
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
)

// ─────────────────────────────────────────────────────────────────────────────
// OpenPosition – tracked from the moment of the open fill
// ─────────────────────────────────────────────────────────────────────────────

// OpenPosition is held in-memory (and optionally persisted) while a position
// is live.  It carries enough context to compute outcome when the close arrives.
type OpenPosition struct {
	// Journal decision ID that opened this position
	DecisionID string `json:"decision_id"`

	Symbol          string           `json:"symbol"`
	Side            string           `json:"side"` // "long" / "short"
	EntryPrice      float64          `json:"entry_price"`
	EntryQty        float64          `json:"entry_qty"`
	EntryFee        float64          `json:"entry_fee"`
	EntryTime       time.Time        `json:"entry_time"`
	Leverage        int              `json:"leverage"`
	StopLoss        float64          `json:"stop_loss"`
	TakeProfit      float64          `json:"take_profit"`
	StrategyID      string           `json:"strategy_id"`
	StrategyVersion string           `json:"strategy_version"`
	SessionID       string           `json:"session_id"`
	Mode            core.Mode        `json:"mode"`

	// Peak unrealised PnL seen during the position — for drawdown tracking
	PeakUnrealPnL float64 `json:"peak_unreal_pnl"`
}

// ─────────────────────────────────────────────────────────────────────────────
// CloseEvent – describes the close fill
// ─────────────────────────────────────────────────────────────────────────────

// CloseEvent is produced by the pipeline when a close fill is confirmed.
type CloseEvent struct {
	Symbol     string        `json:"symbol"`
	Side       string        `json:"side"` // side of the close order (opposite of open)
	ClosePrice float64       `json:"close_price"`
	CloseQty   float64       `json:"close_qty"`
	CloseFee   float64       `json:"close_fee"`
	CloseTime  time.Time     `json:"close_time"`
	ExitReason string        `json:"exit_reason"` // "take_profit", "stop_loss", "signal", "liquidation"
	Mode       core.Mode     `json:"mode"`
}

// ─────────────────────────────────────────────────────────────────────────────
// ComputeOutcome  – pure function, no side effects
// ─────────────────────────────────────────────────────────────────────────────

// ComputeOutcome derives a journal.Outcome from an open position and its close event.
// All PnL arithmetic is in the close currency (USDT).
func ComputeOutcome(open OpenPosition, close CloseEvent) journal.Outcome {
	var rawPnL float64
	if open.Side == "long" {
		rawPnL = (close.ClosePrice - open.EntryPrice) * open.EntryQty
	} else {
		rawPnL = (open.EntryPrice - close.ClosePrice) * open.EntryQty
	}
	totalFees := open.EntryFee + close.CloseFee
	realizedPnL := rawPnL - totalFees

	// Return as a fraction of the margin committed
	margin := (open.EntryPrice * open.EntryQty) / float64(max1(open.Leverage))
	returnPct := 0.0
	if margin > 0 {
		returnPct = realizedPnL / margin
	}

	// Holding period
	held := close.CloseTime.Sub(open.EntryTime)
	holdingStr := formatDuration(held)

	// Classify
	class := journal.OutcomePending
	switch {
	case realizedPnL > 0.01:
		class = journal.OutcomeWin
	case realizedPnL < -0.01:
		class = journal.OutcomeLoss
	default:
		class = journal.OutcomeBreakEven
	}

	// Detect forced exit
	if close.ExitReason == "liquidation" || close.ExitReason == "stop_loss" {
		if realizedPnL < 0 {
			class = journal.OutcomeForcedExit
		}
	}

	return journal.Outcome{
		ClosedAt:      close.CloseTime,
		ClosePrice:    close.ClosePrice,
		RealizedPnL:   realizedPnL,
		ReturnPct:     returnPct,
		HoldingPeriod: holdingStr,
		Class:         class,
		ExitReason:    close.ExitReason,
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Helpers
// ─────────────────────────────────────────────────────────────────────────────

func max1(n int) int {
	if n < 1 {
		return 1
	}
	return n
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return "< 1 min"
	}
	if d < time.Hour {
		return fmt.Sprintf("%d min", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, hours)
}

// missing import guard
var _ = fmt.Sprintf
