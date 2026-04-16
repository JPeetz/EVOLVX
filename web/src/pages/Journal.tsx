// web/src/pages/Journal.tsx
//
// Decision Journal dashboard — v1.1 EvolvX UI.
//
// Layout:
//   Top row   : 4 KPI cards (total, win rate, avg return, total PnL)
//   Row 2     : Outcome heatmap (left 60%) + Win/loss area chart (right 40%)
//   Bottom    : Filterable decision table with expandable row detail

import React, { useState, useMemo } from 'react'
import useSWR from 'swr'
import {
  AreaChart, Area, BarChart, Bar,
  XAxis, YAxis, CartesianGrid, Tooltip as RTooltip,
  ResponsiveContainer, Legend, ReferenceLine
} from 'recharts'

import {
  journalApi, fmt,
  type DecisionEntry, type OutcomeClass, type SignalAction
} from '../lib/evolvx-api'

import {
  PageShell, MetricCard, SectionHeader,
  StatusBadge, OutcomeBadge, ActionBadge,
  Btn, Modal, Spinner, EmptyState, ErrorBanner, Delta
} from '../components/evolvx/ui'
import OutcomeHeatmap from '../components/evolvx/OutcomeHeatmap'

// ─────────────────────────────────────────────────────────────────────────────
// Strategy + version selectors (reuses existing NOFX traders API)
// ─────────────────────────────────────────────────────────────────────────────

function useTraders() {
  const { data } = useSWR<Array<{ id: string; name: string }>>(
    '/api/traders',
    (url: string) => fetch(url, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )
  return data ?? []
}

// ─────────────────────────────────────────────────────────────────────────────
// KPI computation
// ─────────────────────────────────────────────────────────────────────────────

function computeKPIs(decisions: DecisionEntry[]) {
  const closed = decisions.filter(d => d.outcome)
  const wins = closed.filter(d => d.outcome!.class === 'win')
  const losses = closed.filter(d => d.outcome!.class === 'loss')
  const totalPnL = closed.reduce((s, d) => s + (d.outcome?.realized_pnl ?? 0), 0)
  const totalReturn = closed.reduce((s, d) => s + (d.outcome?.return_pct ?? 0), 0)
  const winRate = closed.length > 0 ? wins.length / closed.length : 0
  const avgReturn = closed.length > 0 ? totalReturn / closed.length : 0
  const grossProfit = wins.reduce((s, d) => s + (d.outcome?.realized_pnl ?? 0), 0)
  const grossLoss = Math.abs(losses.reduce((s, d) => s + (d.outcome?.realized_pnl ?? 0), 0))
  const profitFactor = grossLoss > 0 ? grossProfit / grossLoss : grossProfit > 0 ? 99 : 0

  return { total: decisions.length, closed: closed.length, wins: wins.length, losses: losses.length, winRate, avgReturn, totalPnL, profitFactor }
}

// ─────────────────────────────────────────────────────────────────────────────
// Equity curve from decisions
// ─────────────────────────────────────────────────────────────────────────────

function buildEquityCurve(decisions: DecisionEntry[]) {
  const closed = decisions
    .filter(d => d.outcome)
    .sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())

  let equity = 0
  return closed.map(d => {
    equity += d.outcome!.realized_pnl
    return {
      date: fmt.date(d.outcome!.closed_at),
      pnl: +d.outcome!.realized_pnl.toFixed(2),
      equity: +equity.toFixed(2),
      symbol: d.symbol,
    }
  })
}

// ─────────────────────────────────────────────────────────────────────────────
// Daily bar chart (wins vs losses per day)
// ─────────────────────────────────────────────────────────────────────────────

function buildDailyBars(decisions: DecisionEntry[]) {
  const byDay = new Map<string, { date: string; wins: number; losses: number; pnl: number }>()
  decisions.filter(d => d.outcome).forEach(d => {
    const day = d.outcome!.closed_at.slice(0, 10)
    const e = byDay.get(day) ?? { date: day, wins: 0, losses: 0, pnl: 0 }
    if (d.outcome!.class === 'win') e.wins++
    else if (d.outcome!.class === 'loss') e.losses++
    e.pnl += d.outcome!.realized_pnl
    byDay.set(day, e)
  })
  return Array.from(byDay.values())
    .sort((a, b) => a.date.localeCompare(b.date))
    .slice(-30) // last 30 days
}

// ─────────────────────────────────────────────────────────────────────────────
// Decision detail modal
// ─────────────────────────────────────────────────────────────────────────────

function DecisionModal({ open, onClose, decision }: {
  open: boolean
  onClose: () => void
  decision: DecisionEntry | null
}) {
  if (!decision) return null

  return (
    <Modal open={open} onClose={onClose} title={`Decision — ${decision.symbol} ${decision.action.replace('_', ' ').toUpperCase()}`} width="max-w-2xl">
      <div className="space-y-4">
        {/* Identity row */}
        <div className="grid grid-cols-3 gap-3">
          <div className="bg-zinc-800 rounded-lg p-3">
            <div className="text-[10px] font-mono text-zinc-500 mb-1">Symbol</div>
            <div className="text-sm font-mono font-bold text-zinc-100">{decision.symbol}</div>
          </div>
          <div className="bg-zinc-800 rounded-lg p-3">
            <div className="text-[10px] font-mono text-zinc-500 mb-1">Confidence</div>
            <div className="text-sm font-mono font-bold text-amber-400">{decision.confidence}%</div>
          </div>
          <div className="bg-zinc-800 rounded-lg p-3">
            <div className="text-[10px] font-mono text-zinc-500 mb-1">Mode</div>
            <div className="text-sm font-mono font-bold text-sky-400 capitalize">{decision.mode}</div>
          </div>
        </div>

        {/* Market context */}
        <div>
          <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-2">Market at Decision</div>
          <div className="grid grid-cols-4 gap-2">
            {[
              { label: 'Price', value: fmt.usd(decision.market_snapshot.price) },
              { label: 'OI', value: decision.market_snapshot.oi > 0 ? `${(decision.market_snapshot.oi / 1e6).toFixed(1)}M` : '—' },
              { label: 'Funding', value: decision.market_snapshot.funding_rate > 0 ? fmt.pct(decision.market_snapshot.funding_rate) : '—' },
              { label: 'Volume', value: decision.market_snapshot.volume > 0 ? `${(decision.market_snapshot.volume / 1e6).toFixed(1)}M` : '—' },
            ].map(m => (
              <div key={m.label} className="bg-zinc-900 rounded p-2 border border-zinc-800">
                <div className="text-[9px] font-mono text-zinc-600">{m.label}</div>
                <div className="text-xs font-mono text-zinc-300 tabular-nums">{m.value}</div>
              </div>
            ))}
          </div>
          {/* Indicators */}
          {Object.keys(decision.market_snapshot.indicators ?? {}).length > 0 && (
            <div className="mt-2 grid grid-cols-4 gap-2">
              {Object.entries(decision.market_snapshot.indicators).slice(0, 8).map(([k, v]) => (
                <div key={k} className="bg-zinc-900 rounded p-2 border border-zinc-800">
                  <div className="text-[9px] font-mono text-zinc-600">{k}</div>
                  <div className="text-xs font-mono text-zinc-300 tabular-nums">{Number(v).toFixed(2)}</div>
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Risk state */}
        <div>
          <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-2">Risk State</div>
          <div className="grid grid-cols-4 gap-2">
            <div className="bg-zinc-900 rounded p-2 border border-zinc-800">
              <div className="text-[9px] font-mono text-zinc-600">Equity</div>
              <div className="text-xs font-mono text-zinc-300">{fmt.usd(decision.risk_state.account_equity)}</div>
            </div>
            <div className="bg-zinc-900 rounded p-2 border border-zinc-800">
              <div className="text-[9px] font-mono text-zinc-600">Margin %</div>
              <div className="text-xs font-mono text-zinc-300">{decision.risk_state.margin_usage_pct.toFixed(1)}%</div>
            </div>
            <div className="bg-zinc-900 rounded p-2 border border-zinc-800">
              <div className="text-[9px] font-mono text-zinc-600">Positions</div>
              <div className="text-xs font-mono text-zinc-300">{decision.risk_state.open_positions}/{decision.risk_state.max_positions}</div>
            </div>
            <div className="bg-zinc-900 rounded p-2 border border-zinc-800">
              <div className="text-[9px] font-mono text-zinc-600">Approved</div>
              <div className={`text-xs font-mono ${decision.risk_state.approved ? 'text-emerald-400' : 'text-red-400'}`}>
                {decision.risk_state.approved ? '✓ Yes' : '✗ No'}
              </div>
            </div>
          </div>
        </div>

        {/* Reasoning */}
        {decision.reasoning && (
          <div>
            <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-2">AI Reasoning</div>
            <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-3 max-h-32 overflow-y-auto">
              <p className="text-xs text-zinc-400 font-mono leading-relaxed whitespace-pre-wrap">{decision.reasoning}</p>
            </div>
          </div>
        )}

        {/* Outcome */}
        {decision.outcome ? (
          <div>
            <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-2">Outcome</div>
            <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
              <div className="flex items-center gap-4">
                <OutcomeBadge outcome={decision.outcome.class} />
                <Delta value={decision.outcome.return_pct} format="pct" />
                <span className={`text-sm font-mono font-bold tabular-nums ${decision.outcome.realized_pnl >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
                  {decision.outcome.realized_pnl >= 0 ? '+' : ''}{decision.outcome.realized_pnl.toFixed(2)} USDT
                </span>
                <span className="text-xs font-mono text-zinc-500 ml-auto">{decision.outcome.exit_reason}</span>
              </div>
              <div className="mt-2 text-[10px] font-mono text-zinc-600">
                Held {decision.outcome.holding_period} · Closed {fmt.dateTime(decision.outcome.closed_at)}
              </div>
            </div>
          </div>
        ) : (
          <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-3 text-xs font-mono text-zinc-500 text-center">
            Position still open or outcome not yet recorded
          </div>
        )}

        {/* Review notes */}
        {decision.review_notes && (
          <div className="bg-sky-950/30 border border-sky-900/50 rounded-lg p-3">
            <div className="text-[10px] font-mono text-sky-500 uppercase mb-1">Review Note</div>
            <p className="text-xs font-mono text-sky-300">{decision.review_notes}</p>
          </div>
        )}
      </div>
    </Modal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main Journal page
// ─────────────────────────────────────────────────────────────────────────────

export default function Journal() {
  const traders = useTraders()

  const [strategyId, setStrategyId] = useState('')
  const [symbol, setSymbol] = useState('')
  const [outcomeFilter, setOutcomeFilter] = useState<OutcomeClass | ''>('')
  const [page, setPage] = useState(0)
  const [selectedDecision, setSelectedDecision] = useState<DecisionEntry | null>(null)

  const PAGE_SIZE = 20

  const query = useMemo(() => ({
    strategy_id: strategyId || undefined,
    symbol: symbol || undefined,
    outcome: outcomeFilter || undefined,
    limit: PAGE_SIZE,
    offset: page * PAGE_SIZE,
  }), [strategyId, symbol, outcomeFilter, page])

  const { data: decisions, error, isLoading } = useSWR(
    ['journal-decisions', JSON.stringify(query)],
    () => journalApi.query(query)
  )

  // Load all for KPIs/charts (larger batch)
  const { data: allDecisions } = useSWR(
    strategyId ? ['journal-all', strategyId] : null,
    () => journalApi.query({ strategy_id: strategyId, limit: 500 })
  )

  const kpis = useMemo(() => computeKPIs(allDecisions ?? []), [allDecisions])
  const equityCurve = useMemo(() => buildEquityCurve(allDecisions ?? []), [allDecisions])
  const dailyBars = useMemo(() => buildDailyBars(allDecisions ?? []), [allDecisions])

  return (
    <PageShell title="Decision Journal" icon="📓" tag="v1.1">

      {/* Filters */}
      <div className="flex items-center gap-3 mb-6 flex-wrap">
        <select
          value={strategyId}
          onChange={e => { setStrategyId(e.target.value); setPage(0) }}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
        >
          <option value="">All strategies</option>
          {traders.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>

        <input
          placeholder="Symbol (e.g. BTCUSDT)"
          value={symbol}
          onChange={e => { setSymbol(e.target.value.toUpperCase()); setPage(0) }}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 w-44"
        />

        <select
          value={outcomeFilter}
          onChange={e => { setOutcomeFilter(e.target.value as OutcomeClass | ''); setPage(0) }}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
        >
          <option value="">All outcomes</option>
          <option value="win">Wins only</option>
          <option value="loss">Losses only</option>
          <option value="pending">Pending</option>
          <option value="breakeven">Breakeven</option>
        </select>

        {(strategyId || symbol || outcomeFilter) && (
          <Btn variant="ghost" size="sm" onClick={() => { setStrategyId(''); setSymbol(''); setOutcomeFilter(''); setPage(0) }}>
            Clear filters
          </Btn>
        )}
      </div>

      {/* KPI row */}
      <div className="grid grid-cols-4 gap-3 mb-6">
        <MetricCard
          label="Total Decisions"
          value={String(kpis.total)}
          sub={`${kpis.closed} closed`}
          accent="zinc"
        />
        <MetricCard
          label="Win Rate"
          value={fmt.pct(kpis.winRate)}
          sub={`${kpis.wins}W / ${kpis.losses}L`}
          accent={kpis.winRate >= 0.5 ? 'emerald' : 'red'}
          trend={kpis.winRate >= 0.5 ? 'up' : 'down'}
        />
        <MetricCard
          label="Avg Return"
          value={fmt.pctSigned(kpis.avgReturn)}
          accent={kpis.avgReturn >= 0 ? 'emerald' : 'red'}
          trend={kpis.avgReturn >= 0 ? 'up' : 'down'}
        />
        <MetricCard
          label="Total PnL"
          value={`${kpis.totalPnL >= 0 ? '+' : ''}${kpis.totalPnL.toFixed(2)}`}
          sub={`PF ${kpis.profitFactor > 99 ? '∞' : kpis.profitFactor.toFixed(2)}`}
          accent={kpis.totalPnL >= 0 ? 'emerald' : 'red'}
          trend={kpis.totalPnL >= 0 ? 'up' : 'down'}
        />
      </div>

      {/* Charts row */}
      <div className="grid grid-cols-5 gap-5 mb-6">
        {/* Heatmap */}
        <div className="col-span-3 bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Outcome Heatmap" sub="6 months · hover a day for detail" />
          <OutcomeHeatmap decisions={allDecisions ?? []} weeks={26} />
        </div>

        {/* Daily bars */}
        <div className="col-span-2 bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Daily Results" sub="last 30 days" />
          <div className="h-48">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={dailyBars} margin={{ top: 0, right: 4, left: -28, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis dataKey="date" tick={{ fontSize: 8, fill: '#52525b', fontFamily: 'monospace' }}
                  tickFormatter={d => d.slice(5)} interval="preserveStartEnd" />
                <YAxis tick={{ fontSize: 8, fill: '#52525b', fontFamily: 'monospace' }} />
                <RTooltip
                  contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }}
                />
                <ReferenceLine y={0} stroke="#3f3f46" />
                <Bar dataKey="wins" fill="#059669" stackId="a" radius={[0,0,0,0]} />
                <Bar dataKey="losses" fill="#dc2626" stackId="b" radius={[0,0,0,0]} />
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      </div>

      {/* Equity curve */}
      {equityCurve.length > 1 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4 mb-6">
          <SectionHeader title="Cumulative PnL" sub="realized gains/losses over time" />
          <div className="h-44">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={equityCurve} margin={{ top: 4, right: 8, left: -12, bottom: 0 }}>
                <defs>
                  <linearGradient id="equityGrad" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.3} />
                    <stop offset="95%" stopColor="#f59e0b" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis dataKey="date" tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }}
                  interval="preserveStartEnd" />
                <YAxis tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
                <RTooltip
                  contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 11, fontFamily: 'monospace' }}
                  formatter={(v: number) => [`${v >= 0 ? '+' : ''}${v.toFixed(2)} USDT`, 'PnL']}
                />
                <ReferenceLine y={0} stroke="#3f3f46" />
                <Area type="monotone" dataKey="equity" stroke="#f59e0b" strokeWidth={1.5}
                  fill="url(#equityGrad)" dot={false} />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Decision table */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
        <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
          <SectionHeader title="Decisions" sub={`showing ${decisions?.length ?? 0} results`} />
          <div className="flex items-center gap-2">
            <Btn size="sm" variant="ghost" onClick={() => setPage(p => Math.max(0, p - 1))} disabled={page === 0}>← Prev</Btn>
            <span className="text-xs font-mono text-zinc-500">p.{page + 1}</span>
            <Btn size="sm" variant="ghost" onClick={() => setPage(p => p + 1)} disabled={(decisions?.length ?? 0) < PAGE_SIZE}>Next →</Btn>
          </div>
        </div>

        {isLoading ? (
          <div className="flex justify-center py-12"><Spinner /></div>
        ) : error ? (
          <div className="p-4"><ErrorBanner message="Failed to load decisions" /></div>
        ) : !decisions?.length ? (
          <EmptyState icon="📭" message="No decisions match your filters" />
        ) : (
          <table className="w-full text-xs font-mono">
            <thead>
              <tr className="border-b border-zinc-800">
                {['Time', 'Symbol', 'Action', 'Conf', 'Fill Price', 'Outcome', 'Return', 'PnL'].map(h => (
                  <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal">
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {decisions.map(d => (
                <tr
                  key={d.decision_id}
                  onClick={() => setSelectedDecision(d)}
                  className="border-b border-zinc-800/50 hover:bg-zinc-800/40 cursor-pointer transition-colors"
                >
                  <td className="px-3 py-2.5 text-zinc-500 whitespace-nowrap">{fmt.dateTime(d.timestamp)}</td>
                  <td className="px-3 py-2.5 text-zinc-200 font-semibold">{d.symbol}</td>
                  <td className="px-3 py-2.5"><ActionBadge action={d.action} /></td>
                  <td className="px-3 py-2.5">
                    <span className={`tabular-nums ${d.confidence >= 80 ? 'text-emerald-400' : d.confidence >= 65 ? 'text-amber-400' : 'text-zinc-500'}`}>
                      {d.confidence}%
                    </span>
                  </td>
                  <td className="px-3 py-2.5 text-zinc-400 tabular-nums">
                    {d.fill_price ? fmt.usd(d.fill_price) : '—'}
                  </td>
                  <td className="px-3 py-2.5">
                    {d.outcome ? <OutcomeBadge outcome={d.outcome.class} /> : (
                      <span className="text-zinc-600">pending</span>
                    )}
                  </td>
                  <td className="px-3 py-2.5 tabular-nums">
                    {d.outcome ? <Delta value={d.outcome.return_pct} format="pct" /> : '—'}
                  </td>
                  <td className="px-3 py-2.5 tabular-nums">
                    {d.outcome ? (
                      <span className={d.outcome.realized_pnl >= 0 ? 'text-emerald-400' : 'text-red-400'}>
                        {d.outcome.realized_pnl >= 0 ? '+' : ''}{d.outcome.realized_pnl.toFixed(2)}
                      </span>
                    ) : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>

      <DecisionModal
        open={!!selectedDecision}
        onClose={() => setSelectedDecision(null)}
        decision={selectedDecision}
      />
    </PageShell>
  )
}
