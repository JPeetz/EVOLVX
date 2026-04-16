// Package pipeline – strategy evaluator.
//
// AIStrategyEvaluator wraps the existing decision.StrategyEngine so it
// satisfies the pipeline.StrategyEvaluator interface.  It:
//   1. Fetches prior decisions from the journal (memory read-before-act)
//   2. Calls the existing BuildSystemPrompt / BuildUserPrompt machinery
//   3. Calls the AI via the existing mcp.Client
//   4. Parses the response using the existing parseFullDecisionResponse
//   5. Returns []core.Signal — one per AI decision
//
// Nothing in the prompt-building or parsing logic changes.  We are WRAPPING,
// not rewriting.
package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────────────────────
// Dependencies injected from existing packages
// ─────────────────────────────────────────────────────────────────────────────

// LegacyDecisionEngine is the narrow interface over the existing
// decision.StrategyEngine that we need here.  The concrete *StrategyEngine
// already provides these; we just define the seam.
type LegacyDecisionEngine interface {
	// BuildSystemPrompt returns the full system prompt string.
	BuildSystemPrompt(accountEquity float64, variant string) string
	// BuildUserPrompt returns the full user prompt string.
	BuildUserPrompt(ctx LegacyTradingContext) string
	// CallAI sends the prompts and returns the raw AI response.
	CallAI(systemPrompt, userPrompt string) (string, error)
	// ParseResponse parses the raw AI response into a slice of legacy decisions.
	ParseResponse(raw string, equity float64) ([]LegacyDecision, string, error)
}

// LegacyTradingContext mirrors the subset of decision.TradingContext used.
type LegacyTradingContext struct {
	AccountEquity   float64
	Positions       []core.Position
	MarketData      map[string]*core.MarketEvent
	RecentTrades    []any // existing store.TradeRecord slice
	CycleNumber     int64
	PriorDecisions  string // summary injected from journal
}

// LegacyDecision mirrors decision.Decision so we avoid an import cycle.
type LegacyDecision struct {
	Symbol          string
	Action          string
	Leverage        int
	PositionSizeUSD float64
	StopLoss        float64
	TakeProfit      float64
	Confidence      int
	RiskUSD         float64
	Reasoning       string
}

// ─────────────────────────────────────────────────────────────────────────────
// AIStrategyEvaluator
// ─────────────────────────────────────────────────────────────────────────────

// AIStrategyEvaluator implements pipeline.StrategyEvaluator using the
// existing AI-call machinery plus the journal for memory injection.
type AIStrategyEvaluator struct {
	strategyID      string
	strategyVersion string
	params          registry.Parameters
	engine          LegacyDecisionEngine
	journal         *journal.Service
	// priorDecisionCount controls how many past decisions are injected into
	// the prompt per symbol.
	priorDecisionCount int
}

// NewAIStrategyEvaluator creates an evaluator.
func NewAIStrategyEvaluator(
	strategyID, strategyVersion string,
	params registry.Parameters,
	engine LegacyDecisionEngine,
	journal *journal.Service,
) *AIStrategyEvaluator {
	return &AIStrategyEvaluator{
		strategyID:         strategyID,
		strategyVersion:    strategyVersion,
		params:             params,
		engine:             engine,
		journal:            journal,
		priorDecisionCount: 5,
	}
}

// Evaluate implements StrategyEvaluator.
func (e *AIStrategyEvaluator) Evaluate(ctx context.Context, cc *core.CycleContext) ([]*core.Signal, error) {
	if e.engine == nil {
		return nil, fmt.Errorf("evaluator: no decision engine configured")
	}

	// ── 1. Build prior-decision summary for memory injection ─────────────────
	priorSummary := e.buildPriorSummary(cc.Event.Symbol)

	// ── 2. Build trading context for the legacy prompt builder ───────────────
	positions := make([]core.Position, 0, len(cc.Positions))
	for _, p := range cc.Positions {
		positions = append(positions, *p)
	}
	legacyCtx := LegacyTradingContext{
		AccountEquity:   cc.AccountEquity,
		Positions:       positions,
		MarketData:      map[string]*core.MarketEvent{cc.Event.Symbol: cc.Event},
		CycleNumber:     cc.CycleNumber,
		PriorDecisions:  priorSummary,
	}

	// ── 3. Build prompts via the existing engine ──────────────────────────────
	systemPrompt := e.engine.BuildSystemPrompt(cc.AccountEquity, e.params.TradingMode)
	userPrompt := e.engine.BuildUserPrompt(legacyCtx)

	// ── 4. Call AI ────────────────────────────────────────────────────────────
	rawResponse, err := e.engine.CallAI(systemPrompt, userPrompt)
	if err != nil {
		return nil, fmt.Errorf("evaluator: AI call: %w", err)
	}

	// ── 5. Parse response ─────────────────────────────────────────────────────
	decisions, cotTrace, err := e.engine.ParseResponse(rawResponse, cc.AccountEquity)
	if err != nil {
		return nil, fmt.Errorf("evaluator: parse response: %w", err)
	}

	// ── 6. Convert legacy decisions → core.Signal ────────────────────────────
	signals := make([]*core.Signal, 0, len(decisions))
	for _, d := range decisions {
		sig := &core.Signal{
			SignalID:         uuid.NewString(),
			StrategyID:       e.strategyID,
			StrategyVersion:  e.strategyVersion,
			Symbol:           d.Symbol,
			Timestamp:        time.Now(),
			Action:           core.SignalAction(d.Action),
			Leverage:         d.Leverage,
			PositionSizeUSD:  d.PositionSizeUSD,
			StopLoss:         d.StopLoss,
			TakeProfit:       d.TakeProfit,
			Confidence:       d.Confidence,
			RiskUSD:          d.RiskUSD,
			Reasoning:        d.Reasoning,
			RawAIResponse:    rawResponse,
			CoTTrace:         cotTrace,
		}
		signals = append(signals, sig)
	}

	return signals, nil
}

// buildPriorSummary fetches recent decisions from the journal and formats
// them as a compact string for injection into the user prompt.
func (e *AIStrategyEvaluator) buildPriorSummary(symbol string) string {
	if e.journal == nil {
		return ""
	}
	entries, err := e.journal.LatestForSymbol(
		e.strategyID, e.strategyVersion, symbol, e.priorDecisionCount,
	)
	if err != nil || len(entries) == 0 {
		return ""
	}

	type brief struct {
		Time       string `json:"t"`
		Action     string `json:"action"`
		Confidence int    `json:"conf"`
		Outcome    string `json:"outcome,omitempty"`
		ReturnPct  string `json:"return,omitempty"`
	}
	var items []brief
	for _, entry := range entries {
		b := brief{
			Time:       entry.Timestamp.Format("2006-01-02 15:04"),
			Action:     string(entry.Action),
			Confidence: entry.Confidence,
		}
		if entry.Outcome != nil {
			b.Outcome = string(entry.Outcome.Class)
			b.ReturnPct = fmt.Sprintf("%.2f%%", entry.Outcome.ReturnPct*100)
		} else {
			b.Outcome = "pending"
		}
		items = append(items, b)
	}

	data, _ := json.Marshal(items)
	return fmt.Sprintf("\n\n[Prior decisions for %s (most recent first)]: %s\n", symbol, string(data))
}

// ─────────────────────────────────────────────────────────────────────────────
// JournalRecorder  – pipeline stage that records decisions to the journal
// ─────────────────────────────────────────────────────────────────────────────

// JournalRecorder is called by the pipeline after a fill to persist the
// decision entry in the journal.  It is a thin helper, not a pipeline stage —
// the pipeline calls it explicitly after execute() succeeds.
type JournalRecorder struct {
	svc *journal.Service
}

// NewJournalRecorder creates a recorder backed by svc.
func NewJournalRecorder(svc *journal.Service) *JournalRecorder {
	return &JournalRecorder{svc: svc}
}

// RecordFromCycle writes a DecisionEntry from a completed CycleContext.
func (j *JournalRecorder) RecordFromCycle(cc *core.CycleContext) error {
	if cc.Signal == nil {
		return nil
	}

	marketSnap := journal.MarketSnapshot{}
	if cc.Event != nil {
		marketSnap = journal.MarketSnapshot{
			Price:       cc.Event.Close,
			Volume:      cc.Event.Volume,
			OI:          cc.Event.OI,
			FundingRate: cc.Event.FundingRate,
			Indicators:  cc.Event.Indicators,
		}
	}

	riskSnap := journal.RiskSnapshot{}
	if cc.RiskResult != nil {
		riskSnap.Approved = cc.RiskResult.Approved
		if len(cc.RiskResult.Reasons) > 0 {
			riskSnap.RejectionReason = cc.RiskResult.Reasons[0]
		}
	}
	riskSnap.AccountEquity = cc.AccountEquity
	riskSnap.OpenPositions = len(cc.Positions)

	posSnap := journal.PositionSnapshot{}
	for _, p := range cc.Positions {
		posSnap.OpenPositions = append(posSnap.OpenPositions, journal.PositionSummary{
			Symbol:        p.Symbol,
			Side:          p.Side,
			EntryPrice:    p.EntryPrice,
			MarkPrice:     p.MarkPrice,
			Quantity:      p.Quantity,
			UnrealizedPnL: p.UnrealizedPnL,
		})
		posSnap.TotalUnrealPnL += p.UnrealizedPnL
	}

	entry := &journal.DecisionEntry{
		StrategyID:      cc.StrategyID,
		StrategyVersion: cc.StrategyVersion,
		SessionID:       cc.SessionID,
		CycleNumber:     cc.CycleNumber,
		Symbol:          cc.Signal.Symbol,
		Timestamp:       cc.Signal.Timestamp,
		Mode:            cc.Mode,
		MarketSnapshot:  marketSnap,
		Action:          cc.Signal.Action,
		Confidence:      cc.Signal.Confidence,
		SignalInputs:    map[string]any{"indicators": cc.Event.Indicators},
		Reasoning:       cc.Signal.Reasoning,
		RawAIResponse:   cc.Signal.RawAIResponse,
		CoTTrace:        cc.Signal.CoTTrace,
		RiskState:       riskSnap,
		PositionState:   posSnap,
	}

	if cc.Fill != nil {
		t := cc.Fill.Timestamp
		entry.OrderID = cc.Fill.OrderID
		entry.FillPrice = cc.Fill.FilledPrice
		entry.FilledQty = cc.Fill.FilledQty
		entry.Fee = cc.Fill.Fee
		entry.ExecutedAt = &t
	}

	if cc.Failed() {
		entry.ErrorMessage = cc.Errors[0].Error()
	}

	return j.svc.Record(entry)
}
