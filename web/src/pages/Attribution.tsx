// web/src/pages/Attribution.tsx
//
// Cross-Strategy Attribution Dashboard — EvolvX v2.0
//
// Four panels:
//   1. By Strategy   — which version earned/lost what, PnL share bar
//   2. By Symbol     — which symbols work best, consistency across strategies
//   3. By Regime     — performance breakdown per market regime
//   4. Symbol Memory — consolidated cross-strategy knowledge per symbol

import React, { useState, useMemo } from 'react'
import useSWR from 'swr'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid, Cell,
  Tooltip as RTooltip, ResponsiveContainer, PieChart, Pie, Legend
} from 'recharts'

import { fmt } from '../lib/evolvx-api'
import {
  PageShell, SectionHeader, MetricCard,
  StatusBadge, Spinner, EmptyState, ErrorBanner, Delta, ScoreBar
} from '../components/evolvx/ui'

// ─────────────────────────────────────────────────────────────────────────────
// Types — mirror attribution/engine.go
// ─────────────────────────────────────────────────────────────────────────────

interface StrategyAttribution {
  strategy_id:      string
  strategy_version: string
  trades:           number
  wins:             number
  win_rate:         number
  pnl:              number
  pnl_share:        number
  avg_return:       number
  max_drawdown:     number
  sharpe_proxy:     number
  best_symbol:      string
  worst_symbol:     string
}

interface SymbolAttribution {
  symbol:        string
  trades:        number
  wins:          number
  win_rate:      number
  pnl:           number
  pnl_share:     number
  avg_return:    number
  best_strategy: string
}

interface RegimeAttribution {
  regime:     string
  trades:     number
  wins:       number
  win_rate:   number
  pnl:        number
  pnl_share:  number
  avg_return: number
}

interface AttributionReport {
  from:             string
  to:               string
  total_pnl:        number
  total_trades:     number
  total_wins:       number
  overall_win_rate: number
  by_strategy:      StrategyAttribution[]
  by_symbol:        SymbolAttribution[]
  by_regime:        RegimeAttribution[]
}

interface SymbolMemory {
  symbol:                  string
  total_decisions:         number
  total_closed:            number
  wins:                    number
  win_rate:                number
  avg_return:              number
  total_pnl:               number
  contributing_strategies: number
  best_strategy_id:        string
  best_strategy_version:   string
}

// ─────────────────────────────────────────────────────────────────────────────
// Regime colours
// ─────────────────────────────────────────────────────────────────────────────

const REGIME_COLORS: Record<string, string> = {
  bull:     '#10b981',
  bear:     '#ef4444',
  sideways: '#f59e0b',
  volatile: '#8b5cf6',
}

// ─────────────────────────────────────────────────────────────────────────────
// By-Strategy panel
// ─────────────────────────────────────────────────────────────────────────────

function ByStrategyPanel({ data }: { data: StrategyAttribution[] }) {
  if (!data.length) return <EmptyState icon="📊" message="No strategy attribution data" />

  const chartData = data.map(s => ({
    name: `v${s.strategy_version}`,
    pnl: +s.pnl.toFixed(2),
    trades: s.trades,
  }))

  return (
    <div className="space-y-4">
      {/* Bar chart */}
      <div className="h-44 bg-zinc-900 border border-zinc-800 rounded-lg p-4">
        <SectionHeader title="PnL by Strategy Version" />
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={chartData} margin={{ top: 0, right: 8, left: -16, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
            <XAxis dataKey="name" tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
            <YAxis tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
            <RTooltip contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }} />
            <Bar dataKey="pnl" radius={[2,2,0,0]}>
              {chartData.map((entry, i) => (
                <Cell key={i} fill={entry.pnl >= 0 ? '#10b981' : '#ef4444'} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>

      {/* Table */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
        <table className="w-full text-xs font-mono">
          <thead>
            <tr className="border-b border-zinc-800">
              {['Version', 'PnL', 'Share', 'Trades', 'Win Rate', 'Avg Return', 'Sharpe', 'Max DD', 'Best Symbol'].map(h => (
                <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal whitespace-nowrap">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((s, i) => (
              <tr key={i} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                <td className="px-3 py-2.5 font-bold text-amber-400">v{s.strategy_version}</td>
                <td className="px-3 py-2.5 tabular-nums"><Delta value={s.pnl} format="abs" /></td>
                <td className="px-3 py-2.5">
                  <div className="flex items-center gap-2 w-24">
                    <div className="flex-1 h-1.5 bg-zinc-800 rounded-full overflow-hidden">
                      <div
                        className={`h-full rounded-full ${s.pnl >= 0 ? 'bg-emerald-500' : 'bg-red-500'}`}
                        style={{ width: `${Math.min(100, Math.abs(s.pnl_share) * 100)}%` }}
                      />
                    </div>
                    <span className="text-zinc-500 w-10 text-right">{(s.pnl_share * 100).toFixed(0)}%</span>
                  </div>
                </td>
                <td className="px-3 py-2.5 text-zinc-400 tabular-nums">{s.trades}</td>
                <td className="px-3 py-2.5">
                  <span className={s.win_rate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}>
                    {fmt.pct(s.win_rate)}
                  </span>
                </td>
                <td className="px-3 py-2.5 tabular-nums"><Delta value={s.avg_return} format="pct" /></td>
                <td className="px-3 py-2.5 tabular-nums text-sky-400">{s.sharpe_proxy.toFixed(2)}</td>
                <td className="px-3 py-2.5 tabular-nums text-red-400">{fmt.pct(Math.abs(s.max_drawdown))}</td>
                <td className="px-3 py-2.5 text-zinc-400">{s.best_symbol || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// By-Symbol panel
// ─────────────────────────────────────────────────────────────────────────────

function BySymbolPanel({ data }: { data: SymbolAttribution[] }) {
  if (!data.length) return <EmptyState icon="📈" message="No symbol attribution data" />

  const pieData = data.slice(0, 8).map(s => ({
    name: s.symbol,
    value: Math.abs(s.pnl),
    positive: s.pnl >= 0,
  }))

  const COLORS = ['#10b981', '#3b82f6', '#f59e0b', '#8b5cf6', '#ec4899', '#14b8a6', '#f97316', '#84cc16']

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-4">
        {/* Pie */}
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="PnL Distribution by Symbol" />
          <div className="h-44">
            <ResponsiveContainer width="100%" height="100%">
              <PieChart>
                <Pie data={pieData} dataKey="value" nameKey="name" cx="50%" cy="50%" outerRadius={70}
                  label={({ name, percent }: { name: string; percent: number }) => `${name} ${(percent*100).toFixed(0)}%`}
                  labelLine={false}
                >
                  {pieData.map((_, i) => (
                    <Cell key={i} fill={COLORS[i % COLORS.length]} />
                  ))}
                </Pie>
                <RTooltip contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }} />
              </PieChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Win rate bars */}
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Win Rate by Symbol" />
          <div className="space-y-2 mt-2">
            {data.slice(0, 7).map(s => (
              <div key={s.symbol} className="flex items-center gap-3">
                <span className="text-xs font-mono text-zinc-300 w-20">{s.symbol}</span>
                <div className="flex-1 h-2 bg-zinc-800 rounded-full overflow-hidden">
                  <div
                    className={`h-full rounded-full ${s.win_rate >= 0.5 ? 'bg-emerald-500' : 'bg-red-500'}`}
                    style={{ width: `${s.win_rate * 100}%` }}
                  />
                </div>
                <span className={`text-[10px] font-mono w-10 text-right ${s.win_rate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}`}>
                  {fmt.pct(s.win_rate)}
                </span>
              </div>
            ))}
          </div>
        </div>
      </div>

      {/* Table */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
        <table className="w-full text-xs font-mono">
          <thead>
            <tr className="border-b border-zinc-800">
              {['Symbol', 'PnL', 'Trades', 'Win Rate', 'Avg Return', 'Best Strategy'].map(h => (
                <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {data.map((s, i) => (
              <tr key={i} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                <td className="px-3 py-2.5 font-bold text-zinc-100">{s.symbol}</td>
                <td className="px-3 py-2.5"><Delta value={s.pnl} format="abs" /></td>
                <td className="px-3 py-2.5 text-zinc-500 tabular-nums">{s.trades}</td>
                <td className="px-3 py-2.5">
                  <span className={s.win_rate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}>{fmt.pct(s.win_rate)}</span>
                </td>
                <td className="px-3 py-2.5"><Delta value={s.avg_return} format="pct" /></td>
                <td className="px-3 py-2.5 text-zinc-500 truncate max-w-28">{s.best_strategy || '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// By-Regime panel
// ─────────────────────────────────────────────────────────────────────────────

function ByRegimePanel({ data }: { data: RegimeAttribution[] }) {
  if (!data.length) return <EmptyState icon="🌊" message="No regime attribution data — requires v1.2 regime detection" />

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-4 gap-3">
        {data.map(r => (
          <div
            key={r.regime}
            className="bg-zinc-900 border rounded-lg p-4"
            style={{ borderColor: REGIME_COLORS[r.regime] ?? '#3f3f46' }}
          >
            <div className="text-sm font-mono font-bold uppercase mb-2" style={{ color: REGIME_COLORS[r.regime] }}>
              {r.regime}
            </div>
            <div className="space-y-1.5 text-xs font-mono">
              <div className="flex justify-between">
                <span className="text-zinc-500">PnL</span>
                <Delta value={r.pnl} format="abs" />
              </div>
              <div className="flex justify-between">
                <span className="text-zinc-500">Win Rate</span>
                <span className={r.win_rate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}>{fmt.pct(r.win_rate)}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-zinc-500">Trades</span>
                <span className="text-zinc-400">{r.trades}</span>
              </div>
              <div className="flex justify-between">
                <span className="text-zinc-500">Avg Ret</span>
                <Delta value={r.avg_return} format="pct" />
              </div>
            </div>
          </div>
        ))}
      </div>

      {/* Regime bar comparison */}
      <div className="h-44 bg-zinc-900 border border-zinc-800 rounded-lg p-4">
        <SectionHeader title="Win Rate by Regime" />
        <ResponsiveContainer width="100%" height="100%">
          <BarChart data={data} margin={{ top: 0, right: 8, left: -16, bottom: 0 }}>
            <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
            <XAxis dataKey="regime" tick={{ fontSize: 10, fill: '#52525b', fontFamily: 'monospace' }} />
            <YAxis tickFormatter={v => `${(v*100).toFixed(0)}%`} tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
            <RTooltip contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }}
              formatter={(v: number) => [`${(v*100).toFixed(1)}%`, 'Win Rate']}
            />
            <Bar dataKey="win_rate" radius={[2,2,0,0]}>
              {data.map((r) => (
                <Cell key={r.regime} fill={REGIME_COLORS[r.regime] ?? '#3f3f46'} />
              ))}
            </Bar>
          </BarChart>
        </ResponsiveContainer>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Symbol Memory panel
// ─────────────────────────────────────────────────────────────────────────────

function SymbolMemoryPanel() {
  const { data: memories, error } = useSWR<SymbolMemory[]>(
    '/api/v1/memory/symbols',
    (url: string) => fetch(url, { headers: { Authorization: `Bearer ${localStorage.getItem('token')}` } }).then(r => r.json())
  )

  if (error) return <ErrorBanner message="Symbol memory unavailable — ensure v2.0 backend is deployed" />
  if (!memories) return <div className="flex justify-center py-8"><Spinner /></div>
  if (!memories.length) return <EmptyState icon="🧠" message="No symbol memory yet" sub="Symbol memory builds automatically as traders run" />

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-3 gap-3">
        <MetricCard
          label="Symbols Tracked"
          value={String(memories.length)}
          accent="zinc"
        />
        <MetricCard
          label="Best Symbol"
          value={memories.sort((a,b) => b.win_rate - a.win_rate)[0]?.symbol ?? '—'}
          sub={`${fmt.pct(memories[0]?.win_rate ?? 0)} WR`}
          accent="emerald"
        />
        <MetricCard
          label="Total Cross-Strategy PnL"
          value={`${memories.reduce((s,m) => s+m.total_pnl, 0) >= 0 ? '+' : ''}${memories.reduce((s,m) => s+m.total_pnl, 0).toFixed(2)}`}
          accent={memories.reduce((s,m) => s+m.total_pnl, 0) >= 0 ? 'emerald' : 'red'}
        />
      </div>

      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
        <table className="w-full text-xs font-mono">
          <thead>
            <tr className="border-b border-zinc-800">
              {['Symbol', 'Decisions', 'Win Rate', 'Avg Return', 'Total PnL', 'Strategies', 'Best Strategy'].map(h => (
                <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal whitespace-nowrap">{h}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {memories.map((m, i) => (
              <tr key={i} className="border-b border-zinc-800/50 hover:bg-zinc-800/30">
                <td className="px-3 py-2.5 font-bold text-zinc-100">{m.symbol}</td>
                <td className="px-3 py-2.5 text-zinc-500 tabular-nums">{m.total_closed}</td>
                <td className="px-3 py-2.5">
                  <span className={m.win_rate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}>{fmt.pct(m.win_rate)}</span>
                </td>
                <td className="px-3 py-2.5"><Delta value={m.avg_return} format="pct" /></td>
                <td className="px-3 py-2.5"><Delta value={m.total_pnl} format="abs" /></td>
                <td className="px-3 py-2.5 text-zinc-500 tabular-nums">{m.contributing_strategies}</td>
                <td className="px-3 py-2.5 text-zinc-500 truncate max-w-32">
                  {m.best_strategy_id ? `${m.best_strategy_id.slice(0,8)}… v${m.best_strategy_version}` : '—'}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main page
// ─────────────────────────────────────────────────────────────────────────────

type Panel = 'strategy' | 'symbol' | 'regime' | 'memory'

export default function Attribution() {
  const [panel, setPanel] = useState<Panel>('strategy')
  const [days, setDays]   = useState(30)

  const from = useMemo(() => {
    const d = new Date(); d.setDate(d.getDate() - days); return d.toISOString()
  }, [days])
  const to = new Date().toISOString()

  const { data: report, error, isLoading } = useSWR<AttributionReport>(
    ['attribution', from, to],
    () => fetch(`/api/v1/attribution/report?from=${from}&to=${to}`, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )

  const panels: Array<{ id: Panel; label: string; icon: string }> = [
    { id: 'strategy', label: 'By Strategy', icon: '🗂' },
    { id: 'symbol',   label: 'By Symbol',   icon: '📈' },
    { id: 'regime',   label: 'By Regime',   icon: '🌊' },
    { id: 'memory',   label: 'Symbol Memory', icon: '🧠' },
  ]

  return (
    <PageShell title="Attribution" icon="🏅" tag="v2.0">

      {/* Controls */}
      <div className="flex items-center gap-4 mb-6 flex-wrap">
        {/* Panel selector */}
        <div className="flex bg-zinc-900 border border-zinc-800 rounded-lg p-1 gap-1">
          {panels.map(p => (
            <button
              key={p.id}
              onClick={() => setPanel(p.id)}
              className={`px-3 py-1.5 rounded text-xs font-mono flex items-center gap-1.5 transition-colors ${
                panel === p.id
                  ? 'bg-amber-400 text-zinc-950 font-bold'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800'
              }`}
            >
              {p.icon} {p.label}
            </button>
          ))}
        </div>

        {/* Time range */}
        {panel !== 'memory' && (
          <div className="flex items-center gap-2">
            <span className="text-[10px] font-mono text-zinc-500">Period</span>
            {[7, 30, 90, 180].map(d => (
              <button
                key={d}
                onClick={() => setDays(d)}
                className={`px-2.5 py-1 rounded text-[10px] font-mono border transition-colors ${
                  days === d
                    ? 'bg-zinc-700 text-zinc-100 border-zinc-600'
                    : 'border-zinc-800 text-zinc-500 hover:text-zinc-300'
                }`}
              >
                {d}d
              </button>
            ))}
          </div>
        )}
      </div>

      {/* KPI row */}
      {report && panel !== 'memory' && (
        <div className="grid grid-cols-4 gap-3 mb-5">
          <MetricCard label="Total Trades"    value={String(report.total_trades)}   accent="zinc" />
          <MetricCard label="Total PnL"       value={`${report.total_pnl >= 0 ? '+' : ''}${report.total_pnl.toFixed(2)}`}
            accent={report.total_pnl >= 0 ? 'emerald' : 'red'} />
          <MetricCard label="Overall Win Rate" value={fmt.pct(report.overall_win_rate)}
            accent={report.overall_win_rate >= 0.5 ? 'emerald' : 'red'} />
          <MetricCard label="Active Strategies" value={String(report.by_strategy?.length ?? 0)} accent="sky" />
        </div>
      )}

      {/* Panel content */}
      <div className="bg-zinc-900/50 border border-zinc-800 rounded-xl p-5">
        {panel !== 'memory' && (
          <>
            {isLoading && <div className="flex justify-center py-12"><Spinner /></div>}
            {error && <ErrorBanner message="Attribution unavailable — ensure v2.0 backend is deployed" />}
            {!isLoading && !error && report && (
              <>
                {panel === 'strategy' && <ByStrategyPanel data={report.by_strategy ?? []} />}
                {panel === 'symbol'   && <BySymbolPanel   data={report.by_symbol ?? []} />}
                {panel === 'regime'   && <ByRegimePanel   data={report.by_regime ?? []} />}
              </>
            )}
          </>
        )}
        {panel === 'memory' && <SymbolMemoryPanel />}
      </div>
    </PageShell>
  )
}
