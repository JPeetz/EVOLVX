// Package observability provides Prometheus metrics for EvolvX.
//
// Metric naming convention: evolvx_<subsystem>_<name>_<unit>
//
// Subsystems:
//   pipeline   — per-session execution metrics (equity, fills, drawdown)
//   registry   — strategy version and status change counters
//   journal    — decision and outcome counters
//   optimizer  — job, candidate, and promotion counters
//   outcome    — real-time position and PnL tracking
//
// All metrics are labelled with strategy_id and mode where applicable so
// Grafana dashboards can filter by strategy or execution mode.
package observability

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// ─────────────────────────────────────────────────────────────────────────────
// Metrics — all exported so pipeline/registry/etc can call them directly
// ─────────────────────────────────────────────────────────────────────────────

var (
	// ── Pipeline ──────────────────────────────────────────────────────────────

	// PipelineEquity tracks the current account equity per strategy+mode.
	PipelineEquity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_equity_usdt",
		Help: "Current account equity in USDT",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineUnrealizedPnL tracks unrealised PnL across open positions.
	PipelineUnrealizedPnL = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_unrealized_pnl_usdt",
		Help: "Total unrealised PnL across open positions",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineRealizedPnL tracks total realised PnL for the session.
	PipelineRealizedPnL = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_realized_pnl_usdt",
		Help: "Total realised PnL for the current session",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineFillsTotal counts fills by side and mode.
	PipelineFillsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_pipeline_fills_total",
		Help: "Total number of confirmed fills",
	}, []string{"strategy_id", "mode", "side"})

	// PipelineDrawdown tracks the current drawdown fraction (0–1).
	PipelineDrawdown = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_drawdown_ratio",
		Help: "Current drawdown as a fraction of peak equity",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineMaxDrawdown tracks the session maximum drawdown.
	PipelineMaxDrawdown = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_max_drawdown_ratio",
		Help: "Maximum drawdown seen in the current session",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineWinRate tracks the rolling win rate (0–1).
	PipelineWinRate = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_win_rate_ratio",
		Help: "Win rate as a fraction of closed trades",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineSharpeRatio tracks the annualised Sharpe ratio.
	PipelineSharpeRatio = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_sharpe_ratio",
		Help: "Annualised Sharpe ratio for the current session",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineProfitFactor tracks gross profit / gross loss.
	PipelineProfitFactor = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_pipeline_profit_factor",
		Help: "Profit factor (gross profit / gross loss)",
	}, []string{"strategy_id", "strategy_version", "mode"})

	// PipelineCyclesTotal counts pipeline cycles processed.
	PipelineCyclesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_pipeline_cycles_total",
		Help: "Total pipeline cycles processed",
	}, []string{"strategy_id", "mode"})

	// PipelineCycleDurationSeconds tracks pipeline cycle processing time.
	PipelineCycleDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evolvx_pipeline_cycle_duration_seconds",
		Help:    "Pipeline cycle processing duration in seconds",
		Buckets: prometheus.DefBuckets,
	}, []string{"strategy_id", "mode"})

	// PipelineRiskRejectedTotal counts orders rejected by the risk checker.
	PipelineRiskRejectedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_pipeline_risk_rejected_total",
		Help: "Total orders rejected by the risk checker",
	}, []string{"strategy_id", "mode", "reason"})

	// ── Registry ──────────────────────────────────────────────────────────────

	// RegistryVersionsTotal counts total strategy versions by status.
	RegistryVersionsTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_registry_versions_total",
		Help: "Total strategy versions currently in the registry",
	}, []string{"status"})

	// RegistryStatusChangesTotal counts status transitions.
	RegistryStatusChangesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_registry_status_changes_total",
		Help: "Total strategy status transitions",
	}, []string{"from_status", "to_status"})

	// RegistryApprovedTotal counts human approvals (the most important gate).
	RegistryApprovedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "evolvx_registry_approved_total",
		Help: "Total strategies approved for live trading by a human",
	})

	// ── Journal ──────────────────────────────────────────────────────────────

	// JournalDecisionsTotal counts decisions recorded by action type.
	JournalDecisionsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_journal_decisions_total",
		Help: "Total decisions recorded in the journal",
	}, []string{"strategy_id", "mode", "action"})

	// JournalOutcomesTotal counts outcome recordings by class.
	JournalOutcomesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_journal_outcomes_total",
		Help: "Total outcomes recorded by class",
	}, []string{"strategy_id", "outcome_class"})

	// JournalPendingOutcomes tracks how many decisions still have no outcome.
	JournalPendingOutcomes = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_journal_pending_outcomes_total",
		Help: "Number of decisions with outcome_class=pending",
	}, []string{"strategy_id"})

	// ── Optimizer ─────────────────────────────────────────────────────────────

	// OptimizerJobsTotal counts optimization jobs by final status.
	OptimizerJobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_optimizer_jobs_total",
		Help: "Total optimization jobs by final status",
	}, []string{"status"})

	// OptimizerCandidatesTotal counts candidates evaluated per job.
	OptimizerCandidatesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_optimizer_candidates_total",
		Help: "Total candidates evaluated",
	}, []string{"passed_promotion"})

	// OptimizerPromotedTotal counts candidates promoted to paper.
	OptimizerPromotedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "evolvx_optimizer_promoted_total",
		Help: "Total candidates promoted to registry StatusPaper",
	})

	// OptimizerJobDurationSeconds tracks total job evaluation time.
	OptimizerJobDurationSeconds = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "evolvx_optimizer_job_duration_seconds",
		Help:    "Total time to complete an optimization job",
		Buckets: []float64{30, 60, 120, 300, 600, 1200, 3600},
	})

	// ── Outcome recorder ─────────────────────────────────────────────────────

	// OutcomeOpenPositions tracks the number of currently open tracked positions.
	OutcomeOpenPositions = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "evolvx_outcome_open_positions_total",
		Help: "Number of currently tracked open positions",
	})

	// OutcomePnLTotal tracks cumulative realised PnL across all strategies.
	OutcomePnLTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_outcome_realized_pnl_usdt_total",
		Help: "Cumulative realised PnL in USDT",
	}, []string{"strategy_id", "symbol", "outcome_class"})

	// OutcomeHoldingDurationSeconds tracks position holding time.
	OutcomeHoldingDurationSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "evolvx_outcome_holding_duration_seconds",
		Help:    "Position holding duration in seconds",
		Buckets: []float64{60, 300, 900, 1800, 3600, 14400, 86400},
	}, []string{"strategy_id", "outcome_class"})

	// ── Regime ────────────────────────────────────────────────────────────────

	// RegimeCurrentLabel tracks the current regime per symbol (as a labelled gauge, 1 = active).
	RegimeCurrentLabel = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "evolvx_regime_current",
		Help: "Current market regime per symbol (1 = active regime)",
	}, []string{"symbol", "regime"})

	// ── Ensemble ──────────────────────────────────────────────────────────────

	// EnsembleQuorumTotal counts ensemble votes that reached/missed quorum.
	EnsembleQuorumTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_ensemble_votes_total",
		Help: "Total ensemble vote attempts",
	}, []string{"strategy_id", "reached_quorum"})

	// EnsembleAgreedAction counts the action agreed by the ensemble.
	EnsembleAgreedAction = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "evolvx_ensemble_agreed_action_total",
		Help: "Actions agreed by the ensemble",
	}, []string{"strategy_id", "action"})
)
