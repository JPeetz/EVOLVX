# EvolvX Engineering Devlog

A narrative record of the architectural decisions made building EvolvX.
Written as a reference for contributors and future maintainers.

---

## The Problem Statement

NOFX is a genuinely excellent AI trading system — 10,000+ stars, multi-exchange, multi-model, polished UI. But after running it in production, a persistent problem emerged: **you can't tell if your strategy got better or if you just got lucky.**

Concretely:
- Backtest and live trade used different code paths, so results weren't comparable
- Strategies were mutable config blobs with no version history
- Decision memory was raw AI text — not queryable, not recoverable across restarts
- "Learning" meant editing prompts, not measuring outcomes

The goal of EvolvX: take NOFX's AI trading machinery and put it on top of four layers that make the system *provably* comparable, reproducible, and self-improving — without breaking a single existing feature.

**Principle: comparability first, intelligence second.**

---

## Session 1 — v1.0 Foundation

### The pipeline decision

The most important early decision: **one pipeline for all three modes**.

The temptation is to write a `BacktestRunner` that's separate from the live path because they feel different — one reads from SQLite, one reads from an exchange. But the *execution* logic is identical. Risk checks, signal evaluation, fill recording, metrics — these don't care whether the price came from a 2023 CSV or a live WebSocket.

The `CycleContext` struct became the load-bearing design. Every stage reads from it and writes to it. The logger converts it to a `LogEntry`. The `JournalRecorder` converts it to a `DecisionEntry`. Because context flows through the whole pipeline, we get a complete audit record for free.

### Simulated adapter for both backtest AND paper

Paper mode wasn't in the original NOFX. The decision to implement it as `SimulatedAdapter` with `ModePaper` rather than a separate adapter was deliberate: it means a strategy that shows 15% drawdown in backtest will show the same 15% in paper, because they're running identical code. The only difference is the `Mode` field on the `Fill`.

### Immutability in the registry

The invariant "no row is ever updated after insertion" sounds restrictive but it's the whole point. If you can update a strategy record, you lose the ability to say "what parameters was this trade made against?" Semver bumps are cheap. The audit trail they provide is invaluable.

The human gate (`SetStatus` to `StatusApproved` requires non-empty `changedBy`) is enforced in Go code, not as a database constraint — so it can't be bypassed by a direct SQL write through the normal API path.

### Overfitting guard: the 0.4 ratio

The walk-forward optimizer uses a validation-split-only score. But a strategy can still overfit: train on Jan–Jun, test on Jul–Sep, and if Jul–Sep happen to share the Jan–Jun patterns, you get a falsely confident result.

The guard: `val_return / train_return ≥ 0.4`. If a strategy earned 20% in training but only 4% in validation, ratio is 0.2 — it fails. This is a deliberately simple heuristic. It won't catch all overfitting but it catches the most obvious case: a strategy that memorised one specific market phase.

---

## Session 2 — v1.1 UI Integration

### Zero new npm dependencies

NOFX already uses React, recharts, SWR, zustand, and Tailwind. All four new pages (Registry, Journal, Optimizer, Intelligence) use only these existing dependencies. The `LineageGraph` is pure SVG with a hand-written BFS layout — no d3. This was a deliberate choice: adding d3 would double the npm dependency footprint for one component.

### The LineageGraph BFS layout

Strategy version trees tend to be shallow and wide (many patch versions from one parent) rather than deep. A simple BFS column-by-row layout works well for this shape. The tricky part is that semver version strings don't sort lexicographically — "1.10.0" would sort before "1.2.0". The layout algorithm uses the `parentVersion` field from the registry record, not string comparison.

### OutcomeHeatmap: why GitHub's model

The heatmap design choice — 26 weeks of 7×N grid, colour by day aggregate — was directly borrowed from GitHub's contribution graph. It's the right model because: (a) users already understand it, (b) it shows temporal density at a glance, (c) it degrades gracefully when data is sparse (grey cells = no activity, not missing data).

The intensity scaling within each colour band (darker green = more wins that day) uses the column-max as the normaliser rather than a global max. This means sparse weeks still show visible colour variation rather than all-pale cells.

---

## Session 3 — v1.2 Advanced Learning

### Automatic outcome recording: the feedback loop closure

In v1.1, the journal had outcome fields but they required manual API calls to populate. This meant the AI memory context (`LatestForSymbol` injected into prompts) was mostly empty in practice. v1.2's `Recorder` closes this loop by watching `OnFill()` calls.

The key design: `Recorder` maintains a `map[string]*OpenPosition` keyed by `symbol+"_"+strategyID`. When a close fill arrives, it looks up the open position, computes outcome with `ComputeOutcome()`, and calls `journal.RecordOutcome()` automatically. Open positions survive restarts via their own SQLite table — the recorder rehydrates on startup.

### Regime detection: why rule-based, not ML

Using an ML model for regime detection would introduce a training dependency, a model artifact that needs versioning, and an inference cost. The rule-based approach (EMA trend + rolling momentum + rolling volatility) is transparent, reproducible, and fast enough to run on every bar. Any trader can read the code and understand exactly what "bull" means. The thresholds are configurable.

The four-regime taxonomy (bull/bear/sideways/volatile) was chosen because it maps to how traders actually think about markets, and because each regime has a distinct risk profile: bull = long bias works; bear = short bias works; sideways = mean reversion works; volatile = reduce size.

### Ensemble voting: the quorum design

The ensemble could have used a simple average of confidence scores. Instead it uses weighted majority vote with a quorum requirement. The distinction matters: if two strategies vote long (high confidence) and one votes short (low confidence), a weighted average might still produce a long signal. The quorum approach makes the agreement requirement explicit and configurable.

The `MinWeightedConfidence` threshold exists because a quorum of three low-confidence voters shouldn't trigger a trade. Three 51% confidence voters agreeing on long is not the same signal as three 82% confidence voters.

---

## Session 4 — v1.3 Observability

### Prometheus over custom metrics

The 25 metrics in `observability/metrics.go` use the standard `prometheus/client_golang` library. The alternative was a simple JSON endpoint. Prometheus was chosen because: (a) Grafana integration is first-class, (b) the alerting infrastructure already exists in most deployments, (c) the `promauto` registration pattern means metrics self-describe.

The metric naming convention `evolvx_<subsystem>_<n>_<unit>` follows Prometheus best practices. Units in the metric name (`_usdt`, `_ratio`, `_seconds`, `_total`) prevent ambiguity.

### Separating the metrics server from the API server

The metrics server runs on port 9090, the trading API on port 3000. This separation matters in production: you may want to expose metrics to Prometheus without exposing the trading API, or vice versa. Network security rules become straightforward: allow port 9090 from your Prometheus host, keep port 3000 internal.

### Notification rate limiting

The `RateLimitSeconds` config exists because trading systems can generate bursts of events. A strategy deprecation triggering 10 simultaneous Slack messages is worse than useful. The per-event-kind rate limit means each alert type has its own bucket — a large win alert doesn't block a job completion alert.

### Audit log as a first-class feature

The `event_log` SQLite table was created in v1.0 by `SQLiteEventLogger`. In v1.3 we built an API and UI to read it. The design decision: rather than adding a new audit table, we made the existing event log queryable. This means the audit log captures everything the pipeline produces — market bars, risk rejections, fills, errors — with no additional instrumentation.

---

## Session 5 — v2.0 Multi-Trader Memory

### SharedJournalHub: the routing decision

NOFX runs multiple auto-traders in one process. The naive v1.x approach: each trader gets its own `journal.Service` pointing to its own SQLite file. The problem: cross-trader queries are impossible. You can't ask "what has any strategy learned about BTCUSDT?" without opening every journal file.

The hub is a write multiplexer that routes all traders to one shared journal. The subscription pattern (observers receive events asynchronously) keeps writer latency near-zero — subscribers run in goroutines and never block the trading loop.

The `LatestForSymbolAcrossStrategies()` method is the key new capability: it queries the shared journal without filtering by strategy, giving the AI prompt builder genuine cross-strategy context.

### Symbol memory: why not just query the journal?

`SymbolStore` could have been a thin wrapper that queries `journal.Query(symbol=...)` on every prompt build. The problem: at high frequency, that's an SQLite query per cycle per symbol. The symbol store maintains an in-memory aggregate that's updated incrementally via hub subscriptions and only written to SQLite on change. The `RebuildFromJournal()` method handles cold start and data recovery.

The `FormatPromptContext()` output is intentionally terse: one line, structured, with concrete numbers. The AI doesn't need a paragraph — it needs a signal.

### Attribution: pure computation

`attribution.Engine.Compute()` has no state and no side effects. It queries the hub, runs accumulators, and returns a report. This makes it trivially testable and easy to call from both the HTTP handler and background processes. The accumulators are simple structs with `add()` methods — no locks needed because `Compute()` operates on a snapshot of query results, not live data.

The strategy×symbol interaction matrix was added late in the design because it answers the most actionable question: not "which strategy is best overall?" but "which strategy is best on BTCUSDT specifically?" These answers are often different.

### Compaction: policy tiers

The three-tier retention policy (per-strategy override > per-status default > global default) reflects real operational needs. An approved strategy running live is precious — you want 180 days of history. A deprecated strategy from a failed experiment should be trimmed to 7 days to keep the database lean. The `MinTradesToCompact` guard prevents a newly created strategy from being compacted before it has enough data to produce a meaningful summary.

The worker deliberately calls `journal.Compact()` which was already tested in v1.0. No new compaction logic was written — just the scheduling wrapper.

---

## Architectural invariants (never violate these)

1. **The pipeline is the only execution path.** No code outside `engine/pipeline/` calls `ExecutionAdapter.SubmitOrder()` directly.

2. **Strategy records are immutable.** `registry.Service` has no `Update()` method and never will.

3. **Outcomes are the only post-insert mutation.** A `DecisionEntry` can have its `Outcome` field written exactly once. All other fields are set at creation.

4. **Promotion requires human approval.** `SetStatus(_, _, StatusApproved, "")` returns `ErrApprovalRequired`. The optimizer can only reach `StatusPaper`.

5. **Learning depends on outcomes, not prompts.** The optimizer scores candidates on validation-split `BacktestResult` metrics. Changing a prompt does not change a score.

6. **Existing NOFX packages are never modified.** `trader/`, `decision/`, `market/`, `mcp/`, `store/`, `backtest/`, `web/` are inherited as-is.

---

## Known technical debt

1. `go.mod` needs `github.com/prometheus/client_golang v1.19.0` declared explicitly.
2. `api/attribution_handlers.go` is missing — the `attribution.Engine` and `memory.SymbolStore` exist but aren't wired to HTTP handlers yet.
3. `v2_test.go` has a missing `fmt` import.
4. `engine/feeds/feeds.go` has a stub `import_stub_parse` variable that should be replaced with `json.Unmarshal` in production.
5. The `BacktestRunner` in `optimizer/evaluator.go` is still a function type that callers must implement. A concrete implementation backed by `trader.RunBacktest` needs wiring in `api/services.go`.
