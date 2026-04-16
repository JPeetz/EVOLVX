# EvolvX Architecture

## Core Principle

> **Comparability first. Intelligence second.**

The AI (LLM) is one pluggable component inside `StrategyEvaluator`. It generates signals, and that's all. Every other layer — execution, versioning, memory, scoring — is deterministic Go code that does not depend on prompt content.

This means:
- You can swap the AI model without changing execution logic
- You can run without an AI at all (rule-based evaluator)
- Backtest results are mathematically equivalent to paper results
- Every decision is explainable, queryable, and recoverable

---

## Layer 1: The Unified Pipeline (`engine/`)

### Why one pipeline for three modes?

Before EvolvX, NOFX had three separate execution paths:

```
Mode         Entry point              Risk checks    Fill model
──────────   ──────────────────────   ────────────   ─────────────
Backtest     backtest/engine.go       separate       no fee model
Paper        (not implemented)        —              —
Live         trader/auto_trader.go    inline         exchange fills
```

This creates an insidious problem: **you cannot compare a backtest result to a live result** because they literally ran different code. A bug in one path doesn't affect the other. A risk rule change has to be made in two places.

EvolvX solves this with a single `pipeline.go` that all three modes flow through. The only things that differ per mode are three injected interfaces:

```go
type Config struct {
    Feed      MarketFeed        // historical bars OR live ticker
    Adapter   ExecutionAdapter  // sim fill model OR real exchange
    Logger    EventLogger       // same schema, different file
}
```

Everything else — strategy evaluation, risk checks, order creation, fill recording, metrics — is identical code running in all three modes.

### The CycleContext

Every pipeline iteration creates a `CycleContext` that flows through all stages:

```go
type CycleContext struct {
    SessionID       string       // ties all events in one run together
    CycleNumber     int64        // monotonically increasing
    Mode            Mode         // backtest | paper | live
    StrategyID      string       // stable across versions
    StrategyVersion string       // exact version evaluated
    Event           *MarketEvent // current bar
    Signal          *Signal      // what the AI decided
    Order           *Order       // post-risk-check intent
    RiskResult      *RiskCheckResult
    Fill            *Fill        // what actually executed
    Metrics         *Metrics     // running session stats
    AccountEquity   float64
    Positions       []*Position
    Errors          []error
}
```

This context is the audit record for one cycle. The `JournalRecorder` converts it directly into a `DecisionEntry` at the end of each cycle.

### The fill model

`SimulatedAdapter` applies a consistent fill model in both backtest and paper modes:

```
Fill price (buy)  = market_close × (1 + slippage_fraction)   // default: 0.05%
Fill price (sell) = market_close × (1 - slippage_fraction)
Fee               = notional × taker_fee_fraction             // default: 0.06%
Margin            = notional / leverage
Liquidation       ≈ entry × (1 - 0.9 / leverage)             // rough model
```

These are conservative, realistic defaults. They can be overridden per session via `FillModelParams`.

---

## Layer 2: Strategy Registry (`registry/`)

### Immutability invariant

The single most important rule in the registry: **a `StrategyRecord` row is never updated after it is inserted.**

```
strategies table:
  pk        │ id   │ version │ status   │ payload (JSON)
  ──────────┼──────┼─────────┼──────────┼──────────────────
  1         │ abc  │ 1.0.0   │ draft    │ { params... }
  2         │ abc  │ 1.0.1   │ paper    │ { params... }  ← status change
  3         │ abc  │ 1.1.0   │ paper    │ { params... }  ← param change
  4         │ abc  │ 1.1.1   │ approved │ { params... }  ← human approved
```

Row 1 never changes. Ever. If you need to see what the strategy looked like when it was first created, it's there. If a live trade was made against `abc@1.1.1`, you can retrieve the exact parameters it used at any point in the future.

### Semver convention

- **Patch bump** (1.0.0 → 1.0.1): status change or performance annotation
- **Minor bump** (1.0.0 → 1.1.0): parameter change (same general approach)
- **Major bump** (1.0.0 → 2.0.0): structural change (different indicators, different coin source)

`GetLatest()` sorts by semver, not by insertion order, so it always returns the semantically highest version regardless of the order rows were inserted.

### The human gate

`SetStatus()` to `StatusApproved` requires a non-empty `changedBy` string. This check happens in Go code, not as a database constraint, so it cannot be bypassed by a direct SQL write through the normal API. The only way to approve a strategy is to call the API with an authenticated identity.

```go
func (s *Service) SetStatus(id, version string, status StrategyStatus, changedBy string) error {
    if status == StatusApproved && changedBy == "" {
        return ErrApprovalRequired  // ← enforced here, always
    }
    // ...
}
```

---

## Layer 3: Decision Journal (`journal/`)

### Memory read-before-act

Before the AI evaluates a market event, the `AIStrategyEvaluator` fetches the 5 most recent decisions for the current symbol and injects them into the user prompt:

```json
[Prior decisions for BTCUSDT (most recent first)]:
[
  {"t":"2024-03-15 14:00","action":"open_long","conf":82,"outcome":"win","return":"4.8%"},
  {"t":"2024-03-14 09:30","action":"open_long","conf":78,"outcome":"loss","return":"-2.1%"},
  {"t":"2024-03-13 16:15","action":"wait","conf":60,"outcome":"pending"},
  ...
]
```

This gives the AI context about its own recent performance. It can avoid patterns that have been losing. This is the mechanism by which the system has "memory" — it reads before acting.

### Outcome recording

Outcomes are recorded separately from decisions because they're only known after the position closes. The journal uses a two-phase pattern:

```
Phase 1 (at decision time):  Record(DecisionEntry)
                              → outcome = nil, outcome_class = "pending"

Phase 2 (at position close): RecordOutcome(decisionID, Outcome)
                              → outcome = {...}, outcome_class = "win"/"loss"
```

The outcome recording can be triggered by:
- The pipeline detecting a close signal against an open position
- An explicit API call from external position monitoring
- The existing NOFX execution log (via migration or webhook)

### Compaction

Journal entries accumulate over time. Compaction summarises entries older than `retainDays` into a `StrategySummary` row and marks the individual rows as `archived=1`. Archived rows are not deleted — they can still be retrieved explicitly — but they are excluded from normal query results.

```
Before compaction (60 old entries + 5 recent):
  Query returns 65 entries

After Compact("strat-A", "1.0.0", 30):
  Query returns 5 entries (recent)
  GetSummary returns: { wins: 42, losses: 18, win_rate: 0.70, ... }
```

---

## Layer 4: Optimizer (`optimizer/`)

### Walk-forward validation

The optimizer uses a strict train/validation split. Training data is used to generate candidates. The validation split is held out completely — candidates are evaluated on it blind.

```
Time ──────────────────────────────────────────────────────────►

│◄────── train window ────────►│◄────── val window ──────────►│
  Jan 2023      Jan 2024          Jan 2024      Jun 2024
  
  candidate parameters are FIT on train data
  candidate scores are measured on val data ONLY
```

### Overfitting guard

A candidate that performs well on training data but poorly on validation data is a sign of overfitting — the parameters were tuned to historical noise rather than genuine signal.

The guard requires:

```
val_return / train_return ≥ 0.4
```

If a strategy earned 20% in training but only 4% in validation, that ratio is 0.2 — below the 0.4 threshold. It fails. The idea is that out-of-sample performance should be at least 40% as good as in-sample performance. If it's much worse, the strategy found patterns that don't generalise.

### Parallel evaluation

Candidates are evaluated concurrently using a worker pool. Each worker calls the `BacktestRunner` function independently. The default is 4 workers. The pool is controlled by a buffered semaphore channel:

```go
sem := make(chan struct{}, workers)
// each goroutine: sem <- struct{}{}; defer func() { <-sem }()
```

This prevents overwhelming the system while still parallelising the most expensive operation (running backtests).

### Promotion vs approval

The optimizer can **promote** candidates to `StatusPaper` automatically. It cannot promote to `StatusApproved` — that requires a human API call. This separation means:

- The optimizer can run unattended overnight and produce paper-ready candidates
- A human reviews the results in the morning and decides which ones go live
- No amount of good metrics can bypass the human review

---

## Integration with Existing NOFX Code

### What changes in `auto_trader.go`

The change to the existing file is minimal by design. `runCycle()` becomes:

```go
func (at *AutoTrader) runCycle() {
    event := at.buildMarketEvent() // UNCHANGED — existing market data assembly
    at.pipelineRunner.SendMarketEvent(event)
}
```

The `buildMarketEvent()` method assembles market data the same way it always did. The pipeline runner receives it and routes it through the new unified pipeline. The existing AI call, prompt builder, and response parser are all still used — they're wrapped behind the `LegacyDecisionEngine` interface.

### What doesn't change

- The UI (`web/`) — zero changes
- Exchange adapters (`trader/*.go`) — zero changes  
- AI model clients (`mcp/*.go`) — zero changes
- Market data service (`market/`) — zero changes
- Database schema for `nofx.db` — zero changes
- The existing `store/` package — zero changes
- Authentication (`auth/`) — zero changes

### Feature flag

During migration, a feature flag in the config enables the pipeline path:

```json
{
  "use_unified_pipeline": true
}
```

With this flag set to `false`, the existing `runCycle()` code path runs as before. This lets you verify that everything works before cutting over.

---

## Data Flow Summary

```
External World                  EvolvX Pipeline              Storage
──────────────                  ─────────────────            ───────
Exchange tick                   
  or Historical                 
  SQLite bar       ──────────►  MarketEvent
                                    │
                                    ▼
                                StrategyEvaluator            journal.db
                                  ← read prior decisions ◄── (prior decisions)
                                  → build prompts
                                  → call AI
                                  → parse response
                                    │
                                    ▼
                                []Signal
                                    │
                                    ▼
                                RiskChecker
                                  (same rules, all modes)
                                    │
                                    ▼ (if approved)
                                Order
                                    │
                                    ▼
Exchange API   ◄── live    ────  ExecutionAdapter
nofx.db fills              ────  (simulated, deterministic)
                                    │
                                    ▼
                                Fill                         event_log.db
                                    │──────────────────────► (append-only log)
                                    │
                                    ▼
                                JournalRecorder              journal.db
                                    │──────────────────────► (DecisionEntry)
                                    │
                                    ▼
                                MetricsCollector
                                (Sharpe, Sortino,
                                 drawdown, win rate)
                                                             
                  Optimizer reads journal + event log
                  to generate and score candidates           optimizer.db
                  Promoter writes to registry               registry.db
```
