// web/src/lib/evolvx-api.ts
//
// Typed API client for all EvolvX v1.1 endpoints.
// Mirrors the Go types exactly so TypeScript gives us full type safety
// against the registry, journal, and optimizer services.

const BASE = '/api/v1'

async function req<T>(path: string, opts?: RequestInit): Promise<T> {
  const token = localStorage.getItem('token')
  const res = await fetch(`${BASE}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    ...opts,
  })
  if (!res.ok) {
    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error || res.statusText)
  }
  return res.json()
}

// ─────────────────────────────────────────────────────────────────────────────
// Registry types
// ─────────────────────────────────────────────────────────────────────────────

export type StrategyStatus = 'draft' | 'paper' | 'approved' | 'deprecated' | 'disabled'

export interface Parameters {
  coin_source_type: string
  static_coins: string[]
  use_coin_pool: boolean
  use_oi_top: boolean
  coin_pool_limit: number
  enable_ema: boolean
  ema_periods: number[]
  enable_macd: boolean
  enable_rsi: boolean
  rsi_periods: number[]
  enable_atr: boolean
  atr_periods: number[]
  enable_volume: boolean
  enable_oi: boolean
  enable_funding_rate: boolean
  enable_quant_data: boolean
  primary_timeframe: string
  selected_timeframes: string[]
  primary_count: number
  max_positions: number
  btceth_max_leverage: number
  altcoin_max_leverage: number
  btceth_max_position_value_ratio: number
  altcoin_max_position_value_ratio: number
  max_margin_usage: number
  min_position_size: number
  min_risk_reward_ratio: number
  min_confidence: number
  trading_mode: string
  role_definition?: string
  trading_frequency?: string
  entry_standards?: string
  decision_process?: string
  custom_prompt?: string
}

export interface PerformanceSummary {
  run_id: string
  run_type: 'backtest' | 'paper' | 'live'
  start_time: string
  end_time: string
  net_return: number
  max_drawdown: number
  sharpe_ratio: number
  sortino_ratio: number
  win_rate: number
  profit_factor: number
  total_trades: number
  train_period?: string
  validation_period?: string
}

export interface StrategyRecord {
  id: string
  name: string
  version: string
  created_at: string
  author: string
  parent_id?: string
  parent_version?: string
  status: StrategyStatus
  status_changed_at: string
  status_changed_by: string
  parameters: Parameters
  compatible_markets: string[]
  compatible_timeframes: string[]
  performance?: PerformanceSummary[]
}

export interface LineageNode {
  strategy_id: string
  version: string
  parent_id: string
  parent_version: string
  mutation_summary: string
  eval_score: number
  promoted: boolean
  promoted_at?: string
  created_at: string
}

// ─────────────────────────────────────────────────────────────────────────────
// Journal types
// ─────────────────────────────────────────────────────────────────────────────

export type OutcomeClass = 'win' | 'loss' | 'breakeven' | 'forced_exit' | 'pending'
export type SignalAction = 'open_long' | 'open_short' | 'close_long' | 'close_short' | 'hold' | 'wait'

export interface Outcome {
  closed_at: string
  close_price: number
  realized_pnl: number
  return_pct: number
  holding_period: string
  class: OutcomeClass
  exit_reason: string
}

export interface DecisionEntry {
  decision_id: string
  strategy_id: string
  strategy_version: string
  session_id: string
  cycle_number: number
  symbol: string
  timestamp: string
  mode: 'backtest' | 'paper' | 'live'
  action: SignalAction
  confidence: number
  reasoning: string
  cot_trace?: string
  risk_state: {
    account_equity: number
    margin_usage_pct: number
    open_positions: number
    max_positions: number
    approved: boolean
    rejection_reason?: string
  }
  market_snapshot: {
    price: number
    volume: number
    oi: number
    funding_rate: number
    indicators: Record<string, number>
  }
  fill_price?: number
  filled_qty?: number
  fee?: number
  outcome?: Outcome
  review_notes?: string
  error_message?: string
}

export interface BriefDecision {
  ts: string
  symbol: string
  action: SignalAction
  conf: number
  outcome: OutcomeClass
  return: string
}

export interface StrategySummary {
  strategy_id: string
  strategy_version: string
  symbol?: string
  from: string
  to: string
  total_decisions: number
  wins: number
  losses: number
  win_rate: number
  total_pnl: number
  avg_return_pct: number
  max_drawdown: number
  recent_decisions: BriefDecision[]
}

// ─────────────────────────────────────────────────────────────────────────────
// Optimizer types
// ─────────────────────────────────────────────────────────────────────────────

export interface PromotionThresholds {
  min_val_return: number
  max_val_drawdown: number
  min_val_sharpe: number
  min_val_win_rate: number
  min_val_profit_factor: number
  min_val_trades: number
  min_val_to_train_return_ratio: number
}

export interface EvalResult {
  run_id: string
  train_period: string
  validation_period: string
  train_return: number
  train_max_drawdown: number
  train_sharpe: number
  train_win_rate: number
  train_profit_factor: number
  train_trades: number
  val_return: number
  val_max_drawdown: number
  val_sharpe: number
  val_sortino: number
  val_win_rate: number
  val_profit_factor: number
  val_trades: number
  score: number
  passed_promotion: boolean
  fail_reasons?: string[]
}

export interface Candidate {
  candidate_id: string
  parent_id: string
  parent_version: string
  parameters: Parameters
  mutation_desc: string
  created_at: string
  eval_result?: EvalResult
  promoted: boolean
  registry_id?: string
  registry_version?: string
}

export interface OptimizationJob {
  job_id: string
  strategy_id: string
  strategy_version: string
  created_at: string
  created_by: string
  thresholds: PromotionThresholds
  train_from: string
  train_to: string
  val_from: string
  val_to: string
  max_candidates: number
  status: 'pending' | 'running' | 'done' | 'failed'
  completed_at?: string
  promoted_count: number
  candidates?: Candidate[]
}

// ─────────────────────────────────────────────────────────────────────────────
// Registry API
// ─────────────────────────────────────────────────────────────────────────────

export const registryApi = {
  listVersions: (id: string) =>
    req<StrategyRecord[]>(`/registry/strategies/${id}/versions`),

  getVersion: (id: string, version: string) =>
    req<StrategyRecord>(`/registry/strategies/${id}/versions/${version}`),

  getLatest: (id: string) =>
    req<StrategyRecord>(`/registry/strategies/${id}/versions/latest`),

  getLineage: (id: string) =>
    req<LineageNode[]>(`/registry/strategies/${id}/lineage`),

  listByStatus: (status: StrategyStatus) =>
    req<StrategyRecord[]>(`/registry/strategies?status=${status}`),

  create: (record: Partial<StrategyRecord>) =>
    req<StrategyRecord>('/registry/strategies', {
      method: 'POST',
      body: JSON.stringify(record),
    }),

  newVersion: (id: string, body: {
    parent_version: string
    bump_type: 'major' | 'minor' | 'patch'
    author: string
    parameters: Parameters
    mutation_summary: string
  }) =>
    req<StrategyRecord>(`/registry/strategies/${id}/versions`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  setStatus: (id: string, version: string, status: StrategyStatus, changedBy: string) =>
    req<{ ok: boolean }>(`/registry/strategies/${id}/versions/${version}/status`, {
      method: 'PUT',
      body: JSON.stringify({ status, changed_by: changedBy }),
    }),

  exportVersion: (id: string, version: string) =>
    `${BASE}/registry/strategies/${id}/export/${version}`,
}

// ─────────────────────────────────────────────────────────────────────────────
// Journal API
// ─────────────────────────────────────────────────────────────────────────────

export interface JournalQuery {
  strategy_id?: string
  strategy_version?: string
  symbol?: string
  outcome?: OutcomeClass
  from?: string
  to?: string
  limit?: number
  offset?: number
}

export const journalApi = {
  query: (q: JournalQuery) => {
    const params = new URLSearchParams()
    Object.entries(q).forEach(([k, v]) => v !== undefined && params.set(k, String(v)))
    return req<DecisionEntry[]>(`/journal/decisions?${params}`)
  },

  get: (id: string) => req<DecisionEntry>(`/journal/decisions/${id}`),

  recordOutcome: (id: string, outcome: Partial<Outcome>) =>
    req<{ ok: boolean }>(`/journal/decisions/${id}/outcome`, {
      method: 'POST',
      body: JSON.stringify(outcome),
    }),

  addReview: (id: string, note: string, reviewer: string) =>
    req<{ ok: boolean }>(`/journal/decisions/${id}/review`, {
      method: 'POST',
      body: JSON.stringify({ note, reviewer }),
    }),

  getSummary: (strategyId: string, version: string) =>
    req<StrategySummary | null>(`/journal/summaries/${strategyId}/${version}`),

  compact: (strategyId: string, version: string, retainDays = 30) =>
    req<StrategySummary>(`/journal/compact/${strategyId}/${version}?retain_days=${retainDays}`, {
      method: 'POST',
    }),
}

// ─────────────────────────────────────────────────────────────────────────────
// Optimizer API
// ─────────────────────────────────────────────────────────────────────────────

export const optimizerApi = {
  submitJob: (body: {
    strategy_id: string
    strategy_version: string
    created_by: string
    train_from: string
    train_to: string
    val_from: string
    val_to: string
    thresholds?: Partial<PromotionThresholds>
    max_candidates?: number
  }) =>
    req<OptimizationJob>('/optimizer/jobs', {
      method: 'POST',
      body: JSON.stringify(body),
    }),

  runJob: (jobId: string) =>
    req<{ job_id: string; status: string }>(`/optimizer/jobs/${jobId}/run`, {
      method: 'POST',
    }),

  getJob: (jobId: string) => req<OptimizationJob>(`/optimizer/jobs/${jobId}`),

  listJobs: (strategyId: string) =>
    req<OptimizationJob[]>(`/optimizer/jobs?strategy_id=${strategyId}`),
}

// ─────────────────────────────────────────────────────────────────────────────
// Formatting helpers shared across all pages
// ─────────────────────────────────────────────────────────────────────────────

export const fmt = {
  pct: (v: number) => `${(v * 100).toFixed(2)}%`,
  pctSigned: (v: number) => `${v >= 0 ? '+' : ''}${(v * 100).toFixed(2)}%`,
  num: (v: number, dp = 2) => v.toFixed(dp),
  usd: (v: number) => `$${v.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}`,
  date: (s: string) => new Date(s).toLocaleDateString('en-IE', { day: '2-digit', month: 'short', year: 'numeric' }),
  dateTime: (s: string) => new Date(s).toLocaleString('en-IE', { day: '2-digit', month: 'short', hour: '2-digit', minute: '2-digit' }),
  relTime: (s: string) => {
    const diff = Date.now() - new Date(s).getTime()
    const m = Math.floor(diff / 60000)
    if (m < 60) return `${m}m ago`
    const h = Math.floor(m / 60)
    if (h < 24) return `${h}h ago`
    return `${Math.floor(h / 24)}d ago`
  },
}
