<div align="center">

```
███████╗██╗   ██╗ ██████╗ ██╗     ██╗   ██╗██╗  ██╗
██╔════╝██║   ██║██╔═══██╗██║     ██║   ██║╚██╗██╔╝
█████╗  ██║   ██║██║   ██║██║     ██║   ██║ ╚███╔╝ 
██╔══╝  ╚██╗ ██╔╝██║   ██║██║     ╚██╗ ██╔╝ ██╔██╗ 
███████╗ ╚████╔╝ ╚██████╔╝███████╗ ╚████╔╝ ██╔╝ ██╗
╚══════╝  ╚═══╝   ╚═════╝ ╚══════╝  ╚═══╝  ╚═╝  ╚═╝
```

### **The AI Trading OS with Memory, Discipline, and Controlled Evolution**

*Built on the shoulders of [NOFX](https://github.com/NoFxAiOS/nofx) · Forged into a production trading system*

<br/>

[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org)
[![License: AGPL v3](https://img.shields.io/badge/License-AGPL_v3-blueviolet?style=for-the-badge)](https://www.gnu.org/licenses/agpl-3.0)
[![Built On NOFX](https://img.shields.io/badge/Built%20On-NOFX%20by%20NoFxAiOS-ff6b35?style=for-the-badge)](https://github.com/NoFxAiOS/nofx)
[![Architecture](https://img.shields.io/badge/Architecture-Unified%20Pipeline-success?style=for-the-badge)](#architecture)
[![Tests](https://img.shields.io/badge/Tests-6%20Test%20Suites-brightgreen?style=for-the-badge)](#testing)
[![Author](https://img.shields.io/badge/Author-@joerg__peetz-1DA1F2?style=for-the-badge&logo=twitter)](https://x.com/joerg_peetz)

<br/>

> **EvolvX** transforms NOFX from a brilliant prompt-driven trading app into a disciplined,  
> testable, self-improving trading platform — without breaking a single existing feature.

<br/>

[**What's New**](#whats-new) · [**Architecture**](#architecture) · [**Getting Started**](#getting-started) · [**API Reference**](#api-reference) · [**Credits**](#credits-and-acknowledgements)

</div>

---

## The Problem with Prompt-First Trading

NOFX is an extraordinary piece of software. A 10,000+ star open-source AI trading system that lets multiple AI models compete to make trading decisions in real-time. If you haven't seen it — [go look at it now](https://github.com/NoFxAiOS/nofx). It's genuinely impressive.

But after running it in production, a pattern emerges:

```
❓  "Did my strategy get better, or did I just get lucky?"
❓  "Which parameter change improved returns?"  
❓  "What did the AI decide last Tuesday at 2am and why?"
❓  "If I backtest version 3 of my config, will it match what ran live?"
❓  "How do I prove the AI isn't overfitting?"
```

The core issue: **NOFX is a prompt-first system.** The AI is the foundation, and everything else is wired around it. Backtest, paper trade, and live trade use different code paths. Strategies are mutable config blobs. Decision history is raw AI text. There's no learning loop grounded in measured outcomes.

**EvolvX is the answer to those questions.** It takes NOFX's excellent AI trading machinery and places it on top of four new layers that make the system provably comparable, reproducible, and self-improving.

---

## What's New

EvolvX adds **four architectural layers** on top of NOFX. Every existing feature continues to work. Nothing is removed.

### Layer 1 — Unified Simulation Pipeline

```
BEFORE (NOFX)                          AFTER (EvolvX)
─────────────────────────────          ──────────────────────────────────────
Backtest: backtest/engine.go     ┐     
Paper:    (not implemented)      ├──►  ALL THREE → engine/pipeline/pipeline.go
Live:     trader/auto_trader.go  ┘     
                                        MarketEvent
Three divergent code paths              → StrategyEvaluator (AI call)
No shared fill model                    → RiskChecker (same rules)
No shared risk logic                    → ExecutionAdapter (sim or live)
No replay capability                    → Fill + EventLogger
                                        → MetricsCollector
                                        Same schema. Always.
```

Every market event, signal, order, fill, and metric passes through the same `processCycle()` function regardless of mode. Backtest results are **mathematically comparable** to paper results because they use the same fill model, same fee assumptions, same slippage model, same risk checks.

### Layer 2 — Strategy Registry and Versioning

```
BEFORE (NOFX)                          AFTER (EvolvX)
─────────────────────────────          ──────────────────────────────────────
StrategyConfig { ... }                 StrategyRecord {
 ↕ mutable                               ID: "uuid-stable-across-versions"
 ↕ no history                            Version: "1.3.0"    ← semver
 ↕ no lineage                            Status: "paper"      ← lifecycle
 ↕ no performance tracking               Parameters: { ... }  ← immutable
}                                        Performance: [...]   ← accumulated
                                         ParentID: "..."      ← lineage
                                       }
                                       
                                       NewVersion() → new row, old row unchanged
                                       SetStatus("approved", "j.peetz69@gmail.com") → human gate
```

Strategies are **versioned artifacts**, not mutable config. A change to any parameter creates a new version. The original version is preserved forever. Backtests and live runs always reference a specific `ID + Version` tuple — you can reproduce any historical run exactly.

### Layer 3 — Decision Memory (Journal)

```
BEFORE (NOFX)                          AFTER (EvolvX)
─────────────────────────────          ──────────────────────────────────────
decision_records table:                journal.DecisionEntry {
  system_prompt: "You are a..."          timestamp, symbol, strategy_version
  input_prompt:  "Market data..."        market_snapshot  ← price, OI, indicators
  raw_response:  "<reasoning>..."        signal_inputs    ← what the AI saw
  cot_trace:     "Step 1..."             reasoning        ← why it decided
  execution_log: "Filled at..."          risk_state       ← margin, positions
  ✗ no market snapshot                   position_state   ← what was open
  ✗ no risk state                        outcome          ← what actually happened
  ✗ no outcome                           review_notes     ← human annotation
  ✗ not queryable by outcome           }
  ✗ lost on restart                    
                                       Queryable by: strategy, symbol,
                                       date range, outcome class.
                                       Survives restarts. Compactable.
                                       Read before next decision (memory).
```

The AI now **reads its own history** before acting. The last 5 decisions for each symbol are injected into the user prompt as structured context. The system knows what it did, what the outcome was, and can avoid repeating mistakes.

### Layer 4 — Learning / Optimization Loop

```
BEFORE (NOFX)                          AFTER (EvolvX)
─────────────────────────────          ──────────────────────────────────────
"Learning" = editing prompts           Learning = measured outcome → candidate
                                                  generation → walk-forward
Ad hoc parameter tweaks                validation → threshold filter → human
                                                  approval → new version
No overfitting protection              
No version lineage                     Parent v1.0.0
No promotion rules                       ↓ GenerateCandidates() [~20 mutations]
No human gate                           ↓ EvaluateCandidate() [train split]
                                        ↓ EvaluateCandidate() [val split]  
                                        ↓ checkThresholds()
                                        ↓ Promote() → StatusPaper
                                        ↓ Human: SetStatus("approved", "you")
                                        ↓ Live
                                       Child v1.0.1 (lineage tracked)
```

Candidates are scored on **out-of-sample validation data only**. An overfitting guard rejects candidates where the validation return is less than 40% of the training return. A human approval gate prevents any strategy from going live without explicit confirmation.

---

## Architecture

### The Unified Pipeline

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                            EvolvX Pipeline                                  │
│                    (identical for all three modes)                          │
└──────────────────────────────────┬──────────────────────────────────────────┘
                                   │
              ┌────────────────────▼────────────────────┐
              │              MarketFeed                  │
              │  ┌──────────────┐  ┌───────────────────┐│
              │  │ Historical   │  │   ChannelFeed     ││
              │  │ (SQLite bars)│  │  (live/paper)     ││
              │  └──────────────┘  └───────────────────┘│
              └────────────────────┬────────────────────┘
                                   │ MarketEvent
                                   ▼
              ┌────────────────────────────────────────┐
              │         StrategyEvaluator              │
              │                                        │
              │  1. Read prior decisions (journal)     │
              │  2. BuildSystemPrompt(params)          │
              │  3. BuildUserPrompt(context)           │
              │  4. CallAI() → raw response            │
              │  5. ParseResponse() → []Signal         │
              └────────────────────┬───────────────────┘
                                   │ []Signal
                                   ▼
              ┌────────────────────────────────────────┐
              │           RiskChecker                  │
              │                                        │
              │  • Max positions                       │
              │  • Leverage caps (BTC/ETH vs altcoin)  │
              │  • Margin usage enforcement            │
              │  • Position value ratio caps           │
              │  • Min position size                   │
              └────────────────────┬───────────────────┘
                                   │ Order (approved / adjusted)
                                   ▼
              ┌────────────────────────────────────────┐
              │         ExecutionAdapter               │
              │                                        │
              │  ┌─────────────────┐  ┌─────────────┐ │
              │  │  SimulatedAdapter│  │ LiveAdapter │ │
              │  │  (backtest+paper)│  │  (live)     │ │
              │  │  • Slippage 0.05%│  │  • Real     │ │
              │  │  • Fee 0.06%     │  │    exchange │ │
              │  │  • Deterministic │  │  • Poll for │ │
              │  │    fill          │  │    fills    │ │
              │  └─────────────────┘  └─────────────┘ │
              └────────────────────┬───────────────────┘
                                   │ Fill
                     ┌─────────────┴──────────────┐
                     ▼                             ▼
       ┌─────────────────────┐     ┌──────────────────────┐
       │    EventLogger      │     │   JournalRecorder    │
       │  (append-only log)  │     │  (decision memory)   │
       └─────────────────────┘     └──────────────────────┘
                                              │
                                              ▼
                                   ┌──────────────────────┐
                                   │   MetricsCollector   │
                                   │  Sharpe, Sortino,    │
                                   │  Drawdown, Win Rate  │
                                   └──────────────────────┘
```

### Strategy Lifecycle

```
  DRAFT ──────► PAPER ──────► APPROVED ──────► DEPRECATED
    │              │               │                │
    │              │               │                ▼
    │              │               │           DISABLED
    │              ▼               │
    │       (optimizer can         │
    │        promote here          │
    │        automatically)        │
    │                              │
    └──────────────────────────────┘
              Human required
         (SetStatus needs changedBy)
         
  Every transition creates a NEW VERSION.
  No row is ever overwritten.
```

### Optimizer Flow

```
  Parent Strategy v1.0.0
         │
         ▼
  GenerateCandidates()
  ┌──────────────────────────────────────────────────────┐
  │  RSI period variants    [7, 9, 14, 21]               │
  │  EMA period variants    [10, 20, 50, 100]            │
  │  Leverage variants      [2, 3, 5, 7, 10]             │
  │  Confidence variants    [65, 70, 75, 80, 85]         │
  │  Margin usage variants  [50%, 60%, 70%, 80%, 90%]    │
  │  Trading mode variants  [aggressive, conservative,   │
  │                          scalping]                   │
  │  Max position variants  [2, 3, 4, 5]                 │
  └──────────────────────────────────────────────────────┘
         │ ~20 candidates
         ▼
  EvaluateCandidate() × N  (parallel workers)
  ┌──────────────────────────────────────────────────────┐
  │  Train split  [months -4 to -2]  ← fit              │
  │  Val split    [months -2 to now] ← score            │
  │                                                      │
  │  Metrics on VAL split only:                         │
  │    net_return, max_drawdown, sharpe, sortino         │
  │    win_rate, profit_factor, trade_count              │
  │    val/train ratio (overfitting guard)               │
  └──────────────────────────────────────────────────────┘
         │
         ▼
  checkThresholds()    ← conservative defaults
  ┌──────────────────────────────────────────────────────┐
  │  val_return      ≥ 3%                                │
  │  |val_drawdown|  ≤ 15%                               │
  │  val_sharpe      ≥ 0.5                               │
  │  val_win_rate    ≥ 45%                               │
  │  val_profit_factor ≥ 1.1                             │
  │  val_trades      ≥ 10                                │
  │  val/train ratio ≥ 0.4  ← no overfit               │
  └──────────────────────────────────────────────────────┘
         │ passing candidates
         ▼
  Promote() → registry v1.0.1, StatusPaper
         │
         ▼
  ✋ Human gate: SetStatus("approved", "j.peetz69@gmail.com")
         │
         ▼
  Live trading
```

### Database Layout

```
nofx.db          (existing — untouched)
  auto_traders   ← existing strategy configs
  decision_logs  ← existing AI decision logs
  klines         ← existing market data

registry.db      (new)
  strategies     ← versioned, immutable strategy records
  lineage        ← parent → child evolution graph

journal.db       (new)
  decisions      ← per-decision memory with outcomes
  summaries      ← compacted history for large datasets

optimizer.db     (new)
  opt_jobs       ← optimization job state

event_log.db     (new, per session)
  event_log      ← append-only pipeline audit trail
```

---

## Getting Started

### Prerequisites

Same as NOFX:

```bash
# macOS
brew install go ta-lib node

# Ubuntu/Debian  
sudo apt-get install golang libta-lib0-dev nodejs npm
```

### Installation

```bash
# Clone EvolvX
git clone https://github.com/JPeetz/EvolvX.git
cd EvolvX

# Install dependencies
go mod download

# Build
go build -o evolvx ./...
```

### Migrating from NOFX

If you already have NOFX running, migration is a single command. It is **100% safe** — it reads your existing database and writes to new files. Nothing is modified.

```bash
# One-time migration (idempotent — safe to run multiple times)
go run ./cmd/migrate/main.go \
  --legacy-db    ./nofx.db        \
  --registry-db  ./registry.db    \
  --journal-db   ./journal.db     \
  --import-decisions              # also imports your existing decision logs

# Output:
# migrate: imported 4 strategies, skipped 0
# migrate: imported 1,247 legacy decision records
# migration complete.
```

After migration, start the server normally:

```bash
./evolvx
# or with Docker:
docker compose up -d
```

Access the UI at `http://localhost:3000` — **all existing features continue to work identically.**

### Fresh Installation

```bash
# Start fresh (same as NOFX)
curl -fsSL https://raw.githubusercontent.com/JPeetz/EvolvX/main/install.sh | bash
open http://localhost:3000
```

---

## New Features (Quick Tour)

### Strategy Registry API

```bash
# List all versions of a strategy
curl http://localhost:3000/api/v1/registry/strategies/{id}/versions

# Create a new version (old version preserved)
curl -X POST http://localhost:3000/api/v1/registry/strategies/{id}/versions \
  -H "Content-Type: application/json" \
  -d '{
    "parent_version": "1.0.0",
    "bump_type": "patch",
    "author": "j.peetz69@gmail.com",
    "mutation_summary": "Raised min_confidence from 75 to 80",
    "parameters": { "min_confidence": 80, ... }
  }'

# Approve a strategy for live trading (human gate)
curl -X PUT http://localhost:3000/api/v1/registry/strategies/{id}/versions/{ver}/status \
  -d '{ "status": "approved", "changed_by": "j.peetz69@gmail.com" }'
# Returns 400 if changed_by is empty — the human gate is enforced server-side.

# Export a strategy for sharing / backup
curl http://localhost:3000/api/v1/registry/strategies/{id}/export/{version} > strategy-v1.0.0.json

# View strategy lineage
curl http://localhost:3000/api/v1/registry/strategies/{id}/lineage
```

### Decision Journal API

```bash
# Query decisions by strategy + outcome
curl "http://localhost:3000/api/v1/journal/decisions?strategy_id={id}&outcome=win&limit=20"

# Query a specific symbol's history
curl "http://localhost:3000/api/v1/journal/decisions?symbol=BTCUSDT&from=2024-01-01T00:00:00Z"

# Record an outcome after a position closes
curl -X POST http://localhost:3000/api/v1/journal/decisions/{decision_id}/outcome \
  -d '{
    "closed_at": "2024-03-15T14:30:00Z",
    "close_price": 72400.0,
    "realized_pnl": 48.50,
    "return_pct": 0.048,
    "class": "win",
    "exit_reason": "take_profit"
  }'

# Add a human review note
curl -X POST http://localhost:3000/api/v1/journal/decisions/{id}/review \
  -d '{ "note": "Good entry — RSI was oversold and OI was rising. Keep this pattern.", "reviewer": "joerg" }'

# Get compacted summary (for large history)
curl http://localhost:3000/api/v1/journal/summaries/{strategy_id}/{version}
```

### Optimizer API

```bash
# Submit an optimization job
curl -X POST http://localhost:3000/api/v1/optimizer/jobs \
  -d '{
    "strategy_id": "{id}",
    "strategy_version": "1.0.0",
    "created_by": "j.peetz69@gmail.com",
    "train_from": "2023-01-01T00:00:00Z",
    "train_to":   "2024-01-01T00:00:00Z",
    "val_from":   "2024-01-01T00:00:00Z",
    "val_to":     "2024-06-01T00:00:00Z",
    "max_candidates": 20
  }'
# Returns: { "job_id": "uuid", "status": "pending" }

# Run the job (async)
curl -X POST http://localhost:3000/api/v1/optimizer/jobs/{job_id}/run
# Returns: { "status": "running" }

# Check results
curl http://localhost:3000/api/v1/optimizer/jobs/{job_id}
# Returns full job with all candidates, scores, and promotion decisions

# List all jobs for a strategy
curl "http://localhost:3000/api/v1/optimizer/jobs?strategy_id={id}"
```

---

## Testing

Six test suites verify the core invariants:

```bash
go test ./... -v
```

```
✅ engine/pipeline  TestModeParityBacktestVsPaper
                    Same strategy + same events → identical fills in backtest and paper.
                    Proves the unified pipeline works.

✅ engine/pipeline  TestAllFillsPassThroughPipeline  
                    Every order carries StrategyID + Mode, proving no bypass exists.

✅ engine/adapters  TestAdapterInterfaceContract
                    Compile-time: both SimulatedAdapter and LiveAdapter satisfy
                    ExecutionAdapter. Swapping modes requires zero business logic changes.

✅ engine/adapters  TestRiskCheckerEnforcementRules
                    All 6 risk rules tested: max positions, min size, leverage caps,
                    margin usage, position value ratio, clean order passthrough.

✅ registry         TestStrategyVersionImmutability
                    NewVersion() preserves original. Creates new row. Never overwrites.

✅ registry         TestStatusTransitions
                    Human gate enforced. Invalid transitions rejected. Semver correct.

✅ journal          TestDecisionPersistsAcrossRestart
                    Decisions survive db close/reopen. Memory is durable.

✅ journal          TestDecisionQueryFilters
                    Query by strategy, symbol, date range, outcome class all verified.

✅ journal          TestCompaction
                    Old entries summarised and archived. Recent entries unaffected.

✅ optimizer        TestPromotionRequiresAllThresholds
                    5 failure modes tested individually. Only perfect candidate promoted.

✅ optimizer        TestOptimizerNeverMutatesLiveStrategy
                    Promoted version is StatusPaper. Original approved version untouched.

✅ optimizer        TestOptimizerJobLifecycle
                    Full submit → run → done lifecycle verified with mock runner.
```

---

## Key Design Decisions

### Why wrap, not rewrite?

NOFX has 10,000+ stars for good reason. The AI prompt machinery, exchange adapters, market data assembly, and UI are all excellent. EvolvX wraps the existing `decision.StrategyEngine` behind the `StrategyEvaluator` interface — the AI call and prompt builder are **unchanged**. Only the lifecycle around them is new.

### Why SQLite for everything?

NOFX already uses SQLite. Same deployment story — single binary, no external database. The new databases (`registry.db`, `journal.db`, `optimizer.db`) are separate files so they can be backed up independently and never corrupt the existing `nofx.db`.

### Why semver for strategy versions?

Semantic versioning gives you a clear signal: a `patch` bump (1.0.0 → 1.0.1) is a status change or performance update. A `minor` bump (1.0.0 → 1.1.0) is a parameter change. A `major` bump (1.0.0 → 2.0.0) is a structural change. This convention makes `GetLatest()` unambiguous and lineage graphs readable.

### Why a human approval gate?

The optimizer promotes candidates to `StatusPaper` automatically. Transitioning to `StatusApproved` (live) requires a non-empty `changed_by` field. This is not just a config flag — it's enforced server-side in the registry. No amount of automation can skip the human sign-off.

### Why separate the fill model from the adapter?

`SimulatedAdapter` is used for **both** backtest and paper modes. The only thing that differs is the `Mode` field on the fills. This means a strategy that shows drawdown in backtest will show **the same** drawdown in paper — because it's the same code. No more "it worked in backtest but not in paper" surprises.

---

## Project Structure

```
EvolvX/
├── engine/
│   ├── core/
│   │   └── types.go              ← shared vocabulary (events, orders, fills)
│   ├── adapters/
│   │   ├── interface.go          ← ExecutionAdapter, MarketFeed, EventLogger
│   │   ├── simulated.go          ← deterministic fill model (backtest + paper)
│   │   └── live.go               ← thin wrapper over existing exchange clients
│   ├── pipeline/
│   │   ├── pipeline.go           ← unified processing loop
│   │   ├── evaluator.go          ← AI call wrapper + journal injection
│   │   ├── risk.go               ← ported risk enforcement rules
│   │   ├── metrics.go            ← running Sharpe, Sortino, drawdown
│   │   └── logger.go             ← append-only SQLite event log
│   └── feeds/
│       └── feeds.go              ← SQLiteHistoricalFeed, ChannelFeed, SliceReplayFeed
│
├── registry/
│   ├── types.go                  ← StrategyRecord, Parameters, PerformanceSummary
│   ├── service.go                ← CRUD, versioning, immutability, human gate
│   └── migrate.go                ← one-time import from existing auto_traders table
│
├── journal/
│   ├── types.go                  ← DecisionEntry, Outcome, StrategySummary
│   └── service.go                ← Record, RecordOutcome, Query, Compact
│
├── optimizer/
│   ├── types.go                  ← Candidate, EvalResult, PromotionThresholds
│   ├── generator.go              ← systematic parameter mutation grid
│   ├── evaluator.go              ← walk-forward scoring + promoter
│   └── service.go                ← job orchestration, parallel workers
│
├── trader/
│   └── auto_trader_pipeline.go   ← PipelineRunner shim (minimal change to existing)
│
├── api/
│   ├── registry_handlers.go      ← HTTP handlers for registry, journal, optimizer
│   └── services.go               ← service construction + migration entry point
│
├── cmd/
│   └── migrate/
│       └── main.go               ← one-time CLI migration tool
│
├── [existing NOFX packages]      ← UNCHANGED
│   ├── trader/                   
│   ├── decision/                 
│   ├── market/                   
│   ├── mcp/                      
│   ├── store/                    
│   ├── backtest/                 
│   └── web/                      
│
└── docs/
    ├── ARCHITECTURE.md
    ├── MIGRATION.md
    └── API.md
```

---

## Comparison: NOFX vs EvolvX

| Capability | NOFX | EvolvX |
|---|---|---|
| AI trading (multi-model) | ✅ | ✅ unchanged |
| Multi-exchange support | ✅ | ✅ unchanged |
| Strategy Studio UI | ✅ | ✅ unchanged |
| AI Debate Arena | ✅ | ✅ unchanged |
| Real-time dashboard | ✅ | ✅ unchanged |
| Backtest engine | ✅ | ✅ unified pipeline |
| Paper trading | ❌ | ✅ new |
| Unified execution layer | ❌ | ✅ new |
| Strategy versioning | ❌ | ✅ new |
| Immutable strategy records | ❌ | ✅ new |
| Decision memory (durable) | partial | ✅ new |
| Query decisions by outcome | ❌ | ✅ new |
| Memory injected into AI prompt | ❌ | ✅ new |
| Human approval gate | ❌ | ✅ new |
| Walk-forward optimization | ❌ | ✅ new |
| Overfitting protection | ❌ | ✅ new |
| Strategy lineage graph | ❌ | ✅ new |
| Reproducible backtests | ❌ | ✅ new |
| Export/import strategies | ❌ | ✅ new |

---

## Roadmap

```
v1.0  ─── Foundation (this release)
      ✅  Unified pipeline (backtest + paper + live)
      ✅  Strategy registry with semver + immutability
      ✅  Decision journal with outcome tracking
      ✅  Walk-forward optimizer with human gate
      ✅  Migration tool from NOFX

v1.1  ─── UI Integration
      ◻   Registry UI panel (version history, lineage graph)
      ◻   Journal dashboard (outcome heatmaps, win/loss timeline)
      ◻   Optimizer UI (job submission, candidate comparison)
      ◻   Strategy diff viewer (compare versions side by side)

v1.2  ─── Advanced Learning
      ◻   Multi-symbol walk-forward evaluation
      ◻   Regime-aware backtesting (bull/bear/sideways splits)
      ◻   Ensemble strategy support (vote across versions)
      ◻   Automatic outcome recording from exchange fills

v1.3  ─── Observability
      ◻   Prometheus metrics export
      ◻   Grafana dashboard templates
      ◻   Slack/Telegram alerts for promotion events
      ◻   Audit log viewer in UI

v2.0  ─── Multi-Trader Memory
      ◻   Shared journal across trader instances
      ◻   Cross-strategy performance attribution
      ◻   Symbol-level memory consolidation
      ◻   Auto-compaction with configurable retention policy
```

---

## Credits and Acknowledgements

**EvolvX would not exist without NOFX.**

The entire AI trading machinery — multi-model AI clients, exchange adapters for Binance/Bybit/OKX/Hyperliquid/Aster/Lighter, the strategy prompt builder, the indicator pipeline, the Debate Arena, the AI500 integration, the React dashboard — is the work of the [NoFxAiOS team](https://github.com/NoFxAiOS) and the NOFX community.

```
Original Repository:  https://github.com/NoFxAiOS/nofx
Original Stars:       10,000+
Original Forks:       2,700+
Original License:     AGPL-3.0

EvolvX contributes:   Unified pipeline, strategy registry, decision journal,
                      optimization loop, and all associated tests.
EvolvX modifies:      Nothing in the original packages.
EvolvX license:       AGPL-3.0 (same as original, as required)
```

If you find EvolvX useful, please **star the original NOFX repository** at [github.com/NoFxAiOS/nofx](https://github.com/NoFxAiOS/nofx). Every star helps the original authors continue their work.

### EvolvX Author

```
Joerg Peetz
Senior Technical Support Engineer, Trend AI (formerly Trend Micro)
Based in Ireland

GitHub:   https://github.com/JPeetz
X/Twitter: https://x.com/joerg_peetz
LinkedIn: https://linkedin.com/in/joerg-peetz
Medium:   https://medium.com/@jpeetz
Email:    j.peetz69@gmail.com
```

Also the author of [NeuralLog](https://github.com/JPeetz/NeuralLog) — AI-powered log analysis for technical support operations, and various projects in the OpenClaw/AutoNovelClaw ecosystem.

---

## Contributing

EvolvX follows NOFX's contribution model. All contributions are tracked and rewarded.

```bash
# Fork, branch, change, test, PR
git checkout -b feature/my-improvement
go test ./...  # all tests must pass
git push origin feature/my-improvement
# Open PR against main
```

**Good first issues:** Look for the `good-first-issue` label. The UI integration for the registry and journal panels (v1.1) are well-scoped starting points.

**Architecture questions:** Open a Discussion rather than an Issue.

**Bug reports:** Include the strategy version, the mode (backtest/paper/live), and the relevant section of the event log.

---

## License

EvolvX is licensed under the **GNU Affero General Public License v3.0 (AGPL-3.0)**, the same license as NOFX. See [LICENSE](LICENSE) for the full text.

---

<div align="center">

*EvolvX: because "did the AI get better or did I get lucky?" deserves an answer.*

**[⭐ Star the original NOFX](https://github.com/NoFxAiOS/nofx)** · **[📖 Read the docs](docs/)** · **[🐛 Report a bug](https://github.com/JPeetz/EvolvX/issues)**

</div>
