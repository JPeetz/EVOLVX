# Changelog

All notable changes to EvolvX are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versioning follows [Semantic Versioning](https://semver.org/).

EvolvX is built on [NOFX](https://github.com/NoFxAiOS/nofx) by NoFxAiOS.
None of the original NOFX packages are modified in any release.

---

## [2.0.0] — Multi-Trader Memory

### Added
- **`multitrader/hub.go`** — `SharedJournalHub` routes decisions from N concurrent traders into a single shared journal. Supports event broadcasting via `DecisionSubscriber` and `OutcomeSubscriber` interfaces. Thread-safe with goroutine-isolated subscriber callbacks. `LatestForSymbolAcrossStrategies()` enables cross-strategy memory reads.
- **`memory/symbol_store.go`** — `SymbolStore` subscribes to the hub and maintains a live per-symbol aggregate (win rate, avg return, total PnL, contributing strategies) in memory and SQLite. `RebuildFromJournal()` performs a cold-start full scan. `FormatPromptContext()` produces a one-line summary ready for AI prompt injection.
- **`attribution/engine.go`** — `Engine.Compute(from, to)` queries the shared journal and produces a full `AttributionReport` breaking down PnL, win rate, and drawdown by strategy version, symbol, regime, and a strategy×symbol interaction matrix.
- **`compaction/worker.go`** — `Worker` runs `journal.Compact()` on a configurable schedule. `RetentionPolicy` supports three tiers: per-strategy override, per-status default (approved=180d, paper=60d, draft=14d, deprecated=7d), and global default (30d). `MinTradesToCompact` prevents premature compaction of strategies with insufficient data.
- **`web/src/pages/Attribution.tsx`** — Attribution dashboard with four panels: By Strategy (PnL bar chart + table with share bars, Sharpe, drawdown), By Symbol (pie chart + win rate bars), By Regime (per-regime cards + comparison chart), Symbol Memory (consolidated cross-strategy knowledge table).
- **`v2_test.go`** — 8 tests: hub routing from multiple traders, subscriber delivery, deregister, symbol store consolidation across strategies, prompt context formatting, attribution PnL arithmetic, compaction policy by status, minimum trades guard.

### Changed
- `web/src/router-additions.tsx` — Attribution route added at `/attribution` with `v2.0` badge. Route count: 6.
- `README.md` — Comparison table updated with v2.0 column. Roadmap v2.0 marked released. Project structure updated with four new packages.

---

## [1.3.0] — Observability

### Added
- **`observability/metrics.go`** — 25 Prometheus metrics across five subsystems: pipeline (equity, PnL, win rate, drawdown, Sharpe, profit factor, fills, risk rejections, cycle duration), registry (version counts, status changes, approvals), journal (decisions, outcomes, pending), optimizer (jobs, candidates, promotions, job duration), regime (current label per symbol), ensemble (votes, agreed actions).
- **`observability/server.go`** — HTTP server on port 9090 serving `/metrics` (Prometheus text format), `/health` (JSON liveness), and `/info` (build metadata).
- **`observability/instrument.go`** — Typed helper functions (`RecordPipelineMetrics`, `RecordFill`, `RecordStatusChange`, `RecordDecision`, `RecordOutcome`, `RecordJobCompleted`, `RecordRegime`, `RecordEnsembleVote`) called by domain packages to update metrics without importing prometheus directly.
- **`notifications/service.go`** — `Service` sends Slack (webhook attachments) and Telegram (MarkdownV2) alerts with per-event-kind rate limiting. Pre-built constructors: `StrategyPromotedToPaper`, `StrategyApproved`, `StrategyDeprecated`, `OptimizerJobDone`, `LargeWin`, `LargeLoss`.
- **`notifications/service_test.go`** — 4 tests: Slack payload shape, Telegram payload shape, rate limiting suppresses duplicates, no-channel config does not panic.
- **`dashboards/evolvx_overview.json`** — Grafana dashboard with five row groups: pipeline performance (equity, PnL, win rate, drawdown, Sharpe stat panels + time series), registry lifecycle, journal outcomes, optimizer activity, regime map. Variables: strategy (multi-select), mode, symbol. Includes Grafana alerting rule templates for high drawdown, stalled fills, and accumulating pending outcomes.
- **`dashboards/README.md`** — Setup guide: Prometheus scraping config, Grafana datasource, three import methods (UI, API, Docker provisioning), full metric reference table.
- **`api/audit_handlers.go`** — `AuditService` reads the `event_log` SQLite table. HTTP endpoints: `GET /audit/events` (filterable by session, kind, mode, date range), `GET /audit/sessions` (session summaries with event/fill/error counts), `GET /audit/events/:id` (single event detail).
- **`web/src/pages/AuditLog.tsx`** — Audit log viewer with session sidebar, kind filter pills, mode selector, KPI row, event kind distribution bar chart, paginated event table with expandable payload modal. Auto-refreshes every 5 seconds.

### Changed
- `web/src/router-additions.tsx` — AuditLog route added at `/audit`. Route count: 5.
- `README.md` — Comparison table updated. Roadmap v1.3 marked released. Project structure updated.

---

## [1.2.0] — Advanced Learning

### Added
- **`outcome/types.go`** — `OpenPosition`, `CloseEvent`, `ComputeOutcome()` pure function with correct long/short PnL arithmetic. Fee deduction, margin-based return percentage, holding period formatting, forced exit detection.
- **`outcome/recorder.go`** — `Recorder` subscribes to pipeline fills via `OnFill()`. Matches close fills to open positions by symbol+strategy, calls `journal.RecordOutcome()` automatically. Survives restarts by persisting open positions to SQLite. `UpdateMarkPrices()` tracks peak unrealised PnL for drawdown statistics.
- **`outcome/recorder_test.go`** — 4 tests: win recording, loss recording, restart persistence, PnL arithmetic.
- **`regime/detector.go`** — Rule-based classifier (no ML library). Classifies each bar as `bull | bear | sideways | volatile` using EMA trend, rolling momentum sum, and rolling volatility. `Split()` groups labeled bars by regime with short-run merging. `Coverage()` returns fraction per regime. `ExtractWindows()` produces contiguous time windows for the optimizer.
- **`regime/detector_test.go`** — 6 tests: all four regimes detected, coverage sums to 1.0, window contiguity.
- **`ensemble/voter.go`** — `Vote()` aggregates signals by weighted majority vote. `WeightFromPerformance()` derives voter weight from registry Sharpe×WinRate×ProfitFactor. `WeightFromJournal()` derives weight from empirical recent outcomes. Config: `MinQuorum`, `MinWeightedConfidence`, `ActionAgreement` (majority/strict). Hold emitted on quorum miss, low confidence, or all abstentions.
- **`ensemble/voter_test.go`** — 7 tests: majority quorum, no quorum → hold, low confidence → hold, weight dominance, abstentions don't count, breakdown recording, strict mode.
- **`optimizer/multisymbol.go`** — `EvaluateMultiSymbol()` runs candidates across N symbols in parallel, computes consistency score (1 − CV of returns), aggregates per-regime metrics, applies extended fail reasons for per-regime threshold violations and insufficient consistency.
- **`optimizer/multisymbol_test.go`** — 4 tests: inconsistent strategy blocked, consistent strategy passes, per-symbol results present, regime integration.
- **`web/src/pages/Intelligence.tsx`** — Three-panel dashboard: Regime Map (current regime badge, radar coverage chart, window timeline), Multi-Symbol (per-symbol heatmaps + consistency KPI), Ensemble Status (version weight bars, last vote breakdown, quorum result).

### Changed
- `optimizer/types.go` — `OptimizationJob` extended with `Symbols`, `MinConsistency`, `RegimeAware` fields.
- `web/src/lib/evolvx-api.ts` — `RegimeAnalysis`, `EnsembleStatus`, `MultiSymbolEvalResult`, `regimeApi`, `ensembleApi`, `outcomeApi` added.
- `web/src/router-additions.tsx` — Intelligence route added at `/intelligence`. Route count: 4.
- `README.md` — Comparison table updated. Roadmap v1.2 marked released. Project structure updated.

---

## [1.1.0] — UI Integration

### Added
- **`web/src/lib/evolvx-api.ts`** — Fully typed API client for all EvolvX v1 backend endpoints. Mirrors all Go types. SWR-ready fetch functions. Formatting helpers (`fmt.pct`, `fmt.usd`, `fmt.dateTime`, etc.).
- **`web/src/components/evolvx/ui.tsx`** — Shared primitive library: `StatusBadge`, `OutcomeBadge`, `ActionBadge`, `MetricCard`, `ScoreBar`, `Delta`, `Modal`, `Btn`, `Spinner`, `EmptyState`, `ErrorBanner`, `PageShell`.
- **`web/src/components/evolvx/LineageGraph.tsx`** — Pure SVG strategy evolution tree. BFS layout algorithm, status-coloured nodes, bezier edge curves, click-to-select, animated glow on selected node. No d3 dependency.
- **`web/src/components/evolvx/OutcomeHeatmap.tsx`** — GitHub contribution calendar: 26 weeks, intensity-scaled colour by win/loss/pending, hover tooltip with wins/losses/PnL, full legend.
- **`web/src/components/evolvx/StrategyDiff.tsx`** — Side-by-side parameter diff modal. Changed parameters highlighted amber with strikethrough on old value. Grouped by category. Performance delta cards at bottom.
- **`web/src/pages/Registry.tsx`** — Version timeline (newest→oldest, quick perf stats), lineage graph panel, version detail (AreaChart of returns, parameters collapsible), approve (human gate confirmation modal), deprecate, export button.
- **`web/src/pages/Journal.tsx`** — 4 KPI cards, outcome heatmap, daily wins/losses bar chart, cumulative equity curve, paginated decision table (filterable by strategy/symbol/outcome), decision detail modal with market context, risk state, reasoning, outcome.
- **`web/src/pages/Optimizer.tsx`** — Job list with live 3s polling for running jobs, new job form (date pickers, threshold preview, candidate count slider), candidate table with expandable train/val comparison and fail reason breakdown, score distribution bar chart, promotion tracker.
- **`web/src/router-additions.tsx`** — Route definitions and nav items for Registry, Journal, Optimizer. Copy-paste integration guide.
- **`web/package-additions.json`** — Documents that zero new npm packages are required. Optional JetBrains Mono font snippet.

### Changed
- `README.md` — Comparison table updated. Roadmap v1.1 marked released. Project structure updated with web/src subtree.

---

## [1.0.0] — Foundation

### Added
- **`engine/core/types.go`** — Shared vocabulary for all execution modes: `MarketEvent`, `Signal`, `Order`, `Fill`, `Position`, `Metrics`, `CycleContext`, `LogEntry`, `FillModelParams`. `SignalAction`, `OrderSide`, `OrderStatus`, `EventKind`, `Mode` enumerations.
- **`engine/adapters/interface.go`** — `ExecutionAdapter`, `MarketFeed`, `EventLogger` interfaces. The only seam between the pipeline and exchange/simulation code.
- **`engine/adapters/simulated.go`** — `SimulatedAdapter` with deterministic fill model (configurable slippage, taker fee, latency). Used for both backtest and paper modes. Maintains in-memory position book with mark-to-market PnL and liquidation detection.
- **`engine/adapters/live.go`** — `LiveAdapter` wraps the existing `trader.Trader` exchange client behind `ExecutionAdapter`. Fill polling loop for async exchange confirmation.
- **`engine/pipeline/pipeline.go`** — Unified processing loop. All three modes flow through `processCycle()`: market event → strategy evaluation → risk check → execution → fill logging → metrics. Signal priority sorting (close before open before hold).
- **`engine/pipeline/risk.go`** — `StandardRiskChecker` ports all existing auto_trader risk rules: max positions, min position size, margin usage cap, leverage cap by symbol type (BTC/ETH vs altcoin), position value ratio cap.
- **`engine/pipeline/evaluator.go`** — `AIStrategyEvaluator` wraps existing `decision.StrategyEngine` behind `StrategyEvaluator` interface. Fetches prior journal decisions before AI call. `JournalRecorder` converts `CycleContext` to `DecisionEntry`.
- **`engine/pipeline/metrics.go`** — `RunningMetrics` tracks equity, PnL, drawdown, win rate, profit factor, Sharpe, Sortino per session.
- **`engine/pipeline/logger.go`** — `SQLiteEventLogger` batches and flushes log entries to the `event_log` table using WAL mode.
- **`engine/feeds/feeds.go`** — `SQLiteHistoricalFeed` (backtest), `ChannelFeed` (paper/live), `SliceReplayFeed` (tests).
- **`registry/types.go`** — `StrategyRecord`, `Parameters`, `PerformanceSummary`, `LineageNode`, `StrategyStatus` lifecycle enum.
- **`registry/service.go`** — `Create`, `NewVersion` (semver bump), `SetStatus` (human gate on approved), `AddPerformance`, `GetVersion`, `GetLatest` (true semver sort), `ListVersions`, `ListByStatus`, `GetLineage`, `Export`, `Import`. Immutability invariant: no row is ever updated after insertion.
- **`registry/migrate.go`** — `MigrateFromLegacy` imports from existing `auto_traders` SQLite table. Idempotent.
- **`journal/types.go`** — `DecisionEntry`, `Outcome`, `OutcomeClass`, `MarketSnapshot`, `RiskSnapshot`, `PositionSnapshot`, `StrategySummary`, `BriefDecision`, `QueryFilter`.
- **`journal/service.go`** — `Record`, `RecordOutcome` (only mutation allowed after insert), `AddReviewNote`, `Get`, `Query` (filterable by strategy/symbol/date/outcome), `LatestForSymbol`, `Compact` (archives to summary, marks rows archived), `GetSummary`.
- **`optimizer/types.go`** — `Candidate`, `EvalResult`, `PromotionThresholds`, `OptimizationJob`, `MutationSpec`.
- **`optimizer/generator.go`** — `GenerateCandidates()` produces systematic mutations across RSI periods, EMA periods, leverage, confidence, margin usage, trading mode, max positions.
- **`optimizer/evaluator.go`** — `EvaluateCandidate()` runs train and validation splits. `computeScore()` weighted composite (Sharpe 40%, return 30%, win rate 15%, profit factor 15%). `checkThresholds()` with 7 rules including overfitting guard (val/train ratio ≥ 0.4). `Promoter.Promote()` writes passing candidates to registry at StatusPaper.
- **`optimizer/service.go`** — `Submit`, `Run` (parallel worker pool), `GetJob`, `ListJobs`. Job state persisted to SQLite.
- **`trader/auto_trader_pipeline.go`** — `PipelineRunner` shim. `NewPipelineRunnerForLive`, `NewPipelineRunnerForPaper`, `RunBacktest`. Minimal change to existing `auto_trader.go` — replaces body of `runCycle()`.
- **`api/registry_handlers.go`** — HTTP handlers for registry (CRUD, versioning, status, lineage, export/import), journal (query, outcome, review, summary, compact), optimizer (submit, run, get, list).
- **`api/services.go`** — `Services` struct, `NewServices()`, `Close()`, `RunMigrationIfNeeded()`.
- **`cmd/migrate/main.go`** — CLI migration tool. Reads `--legacy-db`, writes `--registry-db` and `--journal-db`. Imports decisions from `decision_logs` or `decision_records`.
- **Tests** — 12 test suites: pipeline mode parity, fills-bypass prevention, simulated adapter equity, adapter interface contract, risk checker (6 rules), strategy immutability, status transitions, export/import round-trip, version lineage, GetLatest semver, decision persistence, query filters, compaction, optimizer threshold enforcement, live strategy immutability, job lifecycle.

---

## [Unreleased]

### Known gaps (good first contributions)
- `go.mod` missing `github.com/prometheus/client_golang v1.19.0` dependency declaration
- `api/attribution_handlers.go` — HTTP handlers for `/api/v1/attribution/report` and `/api/v1/memory/symbols` (engines exist, handlers not yet wired)
- `v2_test.go` — missing `fmt` import (compilation fix)
- Grafana alerting rule templates for drawdown, stalled fills, pending outcome accumulation
