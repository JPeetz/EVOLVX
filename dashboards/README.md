# EvolvX Grafana Dashboards

## Available dashboards

| File | Title | Description |
|---|---|---|
| `evolvx_overview.json` | EvolvX — Trading OS Overview | Pipeline equity/PnL, registry lifecycle, journal outcomes, optimizer activity, regime map |

## Prerequisites

- Grafana 10.0+ running (local or cloud)
- Prometheus scraping EvolvX metrics on port 9090
- EvolvX v1.3+ running

## Setup

### 1. Start the EvolvX metrics server

The metrics server starts automatically when `USE_OBSERVABILITY=true` is set in `.env`.

```bash
# .env
USE_OBSERVABILITY=true
METRICS_PORT=9090      # default
```

Or start it manually in your main.go:

```go
import "github.com/NoFxAiOS/nofx/observability"

srv := observability.NewServer(observability.DefaultServerConfig())
go srv.Start(ctx)
```

Verify it's running:

```bash
curl http://localhost:9090/metrics | grep evolvx_pipeline
curl http://localhost:9090/health
```

### 2. Add Prometheus as a Grafana datasource

In Grafana: **Configuration → Data Sources → Add data source → Prometheus**

- URL: `http://localhost:9090`  (or wherever your Prometheus is)
- Name: `Prometheus` (must match the `DS_PROMETHEUS` input in the dashboard JSON)

### 3. Configure Prometheus to scrape EvolvX

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: evolvx
    static_configs:
      - targets: ['localhost:9090']
    scrape_interval: 15s
    metrics_path: /metrics
```

### 4. Import the dashboard

**Option A — Grafana UI:**

1. Grafana sidebar → **Dashboards → Import**
2. Upload `evolvx_overview.json`
3. Select your Prometheus datasource when prompted

**Option B — Grafana API:**

```bash
curl -X POST http://admin:admin@localhost:3001/api/dashboards/import \
  -H "Content-Type: application/json" \
  -d @evolvx_overview.json
```

**Option C — Docker provisioning:**

Mount the dashboards directory as a Grafana provisioning volume:

```yaml
# docker-compose.yml addition
grafana:
  image: grafana/grafana:10.0.0
  volumes:
    - ./dashboards:/etc/grafana/provisioning/dashboards
  environment:
    - GF_AUTH_ANONYMOUS_ENABLED=true
    - GF_AUTH_ANONYMOUS_ORG_ROLE=Viewer
```

## Metric reference

All EvolvX metrics follow the naming convention `evolvx_<subsystem>_<name>_<unit>`.

| Metric | Type | Description |
|---|---|---|
| `evolvx_pipeline_equity_usdt` | Gauge | Account equity per strategy/mode |
| `evolvx_pipeline_realized_pnl_usdt` | Gauge | Realized PnL for the session |
| `evolvx_pipeline_unrealized_pnl_usdt` | Gauge | Unrealized PnL across open positions |
| `evolvx_pipeline_win_rate_ratio` | Gauge | Win rate (0–1) |
| `evolvx_pipeline_sharpe_ratio` | Gauge | Annualised Sharpe ratio |
| `evolvx_pipeline_max_drawdown_ratio` | Gauge | Maximum session drawdown (0–1) |
| `evolvx_pipeline_profit_factor` | Gauge | Gross profit / gross loss |
| `evolvx_pipeline_fills_total` | Counter | Fills by strategy/mode/side |
| `evolvx_pipeline_risk_rejected_total` | Counter | Orders rejected by risk checker |
| `evolvx_pipeline_cycle_duration_seconds` | Histogram | Pipeline cycle processing time |
| `evolvx_registry_versions_total` | Gauge | Versions by status |
| `evolvx_registry_status_changes_total` | Counter | Status transitions |
| `evolvx_registry_approved_total` | Counter | Human approvals (the gate metric) |
| `evolvx_journal_decisions_total` | Counter | Decisions recorded by action |
| `evolvx_journal_outcomes_total` | Counter | Outcomes by class (win/loss/etc) |
| `evolvx_journal_pending_outcomes_total` | Gauge | Open decisions without outcome |
| `evolvx_optimizer_jobs_total` | Counter | Jobs by final status |
| `evolvx_optimizer_candidates_total` | Counter | Candidates by pass/fail |
| `evolvx_optimizer_promoted_total` | Counter | Candidates promoted to paper |
| `evolvx_optimizer_job_duration_seconds` | Histogram | Job completion time |
| `evolvx_outcome_open_positions_total` | Gauge | Currently tracked open positions |
| `evolvx_outcome_realized_pnl_usdt_total` | Counter | Cumulative PnL by strategy/symbol/class |
| `evolvx_outcome_holding_duration_seconds` | Histogram | Position holding time |
| `evolvx_regime_current` | Gauge | Active regime per symbol (1 = active) |
| `evolvx_ensemble_votes_total` | Counter | Ensemble votes by quorum result |
| `evolvx_ensemble_agreed_action_total` | Counter | Actions agreed by ensemble |

## Alerting

Add these Grafana alert rules for production monitoring:

```yaml
# High drawdown alert
- alert: HighDrawdown
  expr: evolvx_pipeline_max_drawdown_ratio > 0.15
  for: 5m
  labels:
    severity: warning
  annotations:
    summary: "Drawdown exceeds 15% for {{ $labels.strategy_id }}"

# No fills in 2 hours (strategy might be stuck)
- alert: NoRecentFills
  expr: increase(evolvx_pipeline_fills_total[2h]) == 0
  for: 2h
  labels:
    severity: warning

# Pending outcomes accumulating (outcome recorder might be down)  
- alert: PendingOutcomesAccumulating
  expr: evolvx_journal_pending_outcomes_total > 50
  for: 30m
  labels:
    severity: warning
```
