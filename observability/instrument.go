// Package observability – instrumentation helpers.
//
// Thin typed helpers called by pipeline/registry/journal/optimizer/outcome/ensemble.
// Keeps prometheus import out of domain packages.
package observability

import (
	"fmt"
	"time"

	prom "github.com/prometheus/client_golang/prometheus"

	"github.com/NoFxAiOS/nofx/engine/core"
	"github.com/NoFxAiOS/nofx/journal"
	"github.com/NoFxAiOS/nofx/registry"
)

// RecordPipelineMetrics updates all pipeline gauges from a core.Metrics snapshot.
func RecordPipelineMetrics(m *core.Metrics) {
	if m == nil {
		return
	}
	l := prom.Labels{"strategy_id": m.StrategyID, "strategy_version": m.StrategyVersion, "mode": string(m.Mode)}
	PipelineEquity.With(l).Set(m.Equity)
	PipelineUnrealizedPnL.With(l).Set(m.UnrealizedPnL)
	PipelineRealizedPnL.With(l).Set(m.RealizedPnL)
	PipelineDrawdown.With(l).Set(m.Drawdown)
	PipelineMaxDrawdown.With(l).Set(m.MaxDrawdown)
	PipelineWinRate.With(l).Set(m.WinRate)
	PipelineSharpeRatio.With(l).Set(m.SharpeRatio)
	PipelineProfitFactor.With(l).Set(m.ProfitFactor)
	PipelineCyclesTotal.With(prom.Labels{"strategy_id": m.StrategyID, "mode": string(m.Mode)}).Inc()
}

func RecordFill(strategyID string, mode core.Mode, side core.OrderSide) {
	PipelineFillsTotal.With(prom.Labels{"strategy_id": strategyID, "mode": string(mode), "side": string(side)}).Inc()
}

func RecordRiskRejection(strategyID string, mode core.Mode, reason string) {
	if len(reason) > 60 {
		reason = reason[:60]
	}
	PipelineRiskRejectedTotal.With(prom.Labels{"strategy_id": strategyID, "mode": string(mode), "reason": reason}).Inc()
}

func RecordCycleDuration(strategyID string, mode core.Mode, duration time.Duration) {
	PipelineCycleDurationSeconds.With(prom.Labels{"strategy_id": strategyID, "mode": string(mode)}).Observe(duration.Seconds())
}

func RecordStatusChange(from, to registry.StrategyStatus) {
	RegistryStatusChangesTotal.With(prom.Labels{"from_status": string(from), "to_status": string(to)}).Inc()
	if to == registry.StatusApproved {
		RegistryApprovedTotal.Inc()
	}
	RegistryVersionsTotal.With(prom.Labels{"status": string(from)}).Dec()
	RegistryVersionsTotal.With(prom.Labels{"status": string(to)}).Inc()
}

func RecordNewVersion() {
	RegistryVersionsTotal.With(prom.Labels{"status": "draft"}).Inc()
}

func RecordDecision(strategyID string, mode core.Mode, action core.SignalAction) {
	JournalDecisionsTotal.With(prom.Labels{"strategy_id": strategyID, "mode": string(mode), "action": string(action)}).Inc()
	JournalPendingOutcomes.With(prom.Labels{"strategy_id": strategyID}).Inc()
}

func RecordOutcome(strategyID string, class journal.OutcomeClass, pnl float64, symbol string, holdingSecs float64) {
	outClass := string(class)
	JournalOutcomesTotal.With(prom.Labels{"strategy_id": strategyID, "outcome_class": outClass}).Inc()
	JournalPendingOutcomes.With(prom.Labels{"strategy_id": strategyID}).Dec()
	if pnl != 0 {
		OutcomePnLTotal.With(prom.Labels{"strategy_id": strategyID, "symbol": symbol, "outcome_class": outClass}).Add(pnl)
	}
	if holdingSecs > 0 {
		OutcomeHoldingDurationSeconds.With(prom.Labels{"strategy_id": strategyID, "outcome_class": outClass}).Observe(holdingSecs)
	}
}

func RecordJobCompleted(status string, evaluated, promoted int, duration time.Duration) {
	OptimizerJobsTotal.With(prom.Labels{"status": status}).Inc()
	OptimizerJobDurationSeconds.Observe(duration.Seconds())
	for i := 0; i < promoted; i++ {
		OptimizerCandidatesTotal.With(prom.Labels{"passed_promotion": "true"}).Inc()
	}
	for i := 0; i < evaluated-promoted; i++ {
		OptimizerCandidatesTotal.With(prom.Labels{"passed_promotion": "false"}).Inc()
	}
	OptimizerPromotedTotal.Add(float64(promoted))
}

func RecordPositionOpened()  { OutcomeOpenPositions.Inc() }
func RecordPositionClosed()  { OutcomeOpenPositions.Dec() }

func RecordRegime(symbol, regime string) {
	for _, r := range []string{"bull", "bear", "sideways", "volatile"} {
		RegimeCurrentLabel.With(prom.Labels{"symbol": symbol, "regime": r}).Set(0)
	}
	RegimeCurrentLabel.With(prom.Labels{"symbol": symbol, "regime": regime}).Set(1)
}

func RecordEnsembleVote(strategyID, agreedAction string, reachedQuorum bool) {
	EnsembleQuorumTotal.With(prom.Labels{
		"strategy_id": strategyID, "reached_quorum": fmt.Sprintf("%t", reachedQuorum),
	}).Inc()
	if reachedQuorum && agreedAction != "" {
		EnsembleAgreedAction.With(prom.Labels{"strategy_id": strategyID, "action": agreedAction}).Inc()
	}
}
