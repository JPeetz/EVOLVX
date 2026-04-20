// web/src/pages/Intelligence.tsx
//
// Intelligence dashboard — EvolvX v1.2
//
// Three panels on one page:
//   1. Regime Map        — shows which market regime the current symbol is in,
//                          with a timeline and coverage breakdown
//   2. Multi-Symbol View — outcome heatmap per symbol, consistency score
//                          across the ensemble of approved versions
//   3. Ensemble Status   — which versions are in the ensemble, their weights,
//                          last signal breakdown, quorum state

import React, { useState, useMemo } from 'react'
import useSWR from 'swr'
import {
  AreaChart, Area, BarChart, Bar, RadarChart, Radar,
  PolarGrid, PolarAngleAxis, XAxis, YAxis, CartesianGrid,
  Tooltip as RTooltip, ResponsiveContainer, Cell, Legend
} from 'recharts'

import {
  journalApi, registryApi, fmt,
  type DecisionEntry, type StrategyRecord,
} from '../lib/evolvx-api'

import {
  PageShell, SectionHeader, MetricCard,
  StatusBadge, ActionBadge, Spinner, EmptyState, ErrorBanner,
  Btn, ScoreBar, Delta
} from '../components/evolvx/ui'
import OutcomeHeatmap from '../components/evolvx/OutcomeHeatmap'

// ─────────────────────────────────────────────────────────────────────────────
// Types matching the Go regime package output
// ─────────────────────────────────────────────────────────────────────────────

type RegimeLabel = 'bull' | 'bear' | 'sideways' | 'volatile'

interface RegimeCoverage {
  bull: number
  bear: number
  sideways: number
  volatile: number
}

interface RegimeWindow {
  label: RegimeLabel
  from: string
  to: string
  bars: number
}

interface RegimeAnalysis {
  symbol: string
  coverage: RegimeCoverage
  windows: RegimeWindow[]
  current_regime: RegimeLabel
}

interface EnsembleVoter {
  strategy_id: string
  strategy_version: string
  weight: number
  last_action: string
  last_confidence: number
  win_rate: number
  agreed: boolean
}

interface EnsembleStatus {
  symbol: string
  agreed_action: string
  weighted_confidence: number
  quorum: number
  voters: EnsembleVoter[]
}

// ─────────────────────────────────────────────────────────────────────────────
// Colour maps
// ─────────────────────────────────────────────────────────────────────────────

const REGIME_COLORS: Record<RegimeLabel, string> = {
  bull:     '#10b981',
  bear:     '#ef4444',
  sideways: '#f59e0b',
  volatile: '#8b5cf6',
}

const REGIME_BG: Record<RegimeLabel, string> = {
  bull:     'bg-emerald-950 border-emerald-800 text-emerald-300',
  bear:     'bg-red-950 border-red-800 text-red-300',
  sideways: 'bg-amber-950 border-amber-800 text-amber-300',
  volatile: 'bg-violet-950 border-violet-800 text-violet-300',
}

// ─────────────────────────────────────────────────────────────────────────────
// Regime Map panel
// ─────────────────────────────────────────────────────────────────────────────

function RegimePanel({ symbol }: { symbol: string }) {
  // Fetch regime analysis from the /api/v1/regime/:symbol endpoint (v1.2 backend)
  const { data: analysis, error } = useSWR<RegimeAnalysis>(
    symbol ? `/api/v1/regime/${symbol}` : null,
    (url: string) => fetch(url, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )

  if (!symbol) return <EmptyState icon="📊" message="Select a symbol to view regime map" />
  if (error) return <ErrorBanner message="Regime analysis unavailable — ensure v1.2 backend is deployed" />
  if (!analysis) return <div className="flex justify-center py-8"><Spinner /></div>

  const coverageData = [
    { regime: 'Bull',     value: +(analysis.coverage.bull * 100).toFixed(1),    fill: REGIME_COLORS.bull },
    { regime: 'Bear',     value: +(analysis.coverage.bear * 100).toFixed(1),    fill: REGIME_COLORS.bear },
    { regime: 'Sideways', value: +(analysis.coverage.sideways * 100).toFixed(1), fill: REGIME_COLORS.sideways },
    { regime: 'Volatile', value: +(analysis.coverage.volatile * 100).toFixed(1), fill: REGIME_COLORS.volatile },
  ]

  // Timeline: last 20 windows
  const recentWindows = (analysis.windows ?? []).slice(-20)

  return (
    <div className="space-y-4">
      {/* Current regime badge */}
      <div className="flex items-center gap-4">
        <div>
          <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">Current Regime</div>
          <span className={`inline-flex items-center gap-2 px-3 py-1.5 rounded-lg border text-sm font-mono font-bold uppercase tracking-widest ${REGIME_BG[analysis.current_regime]}`}>
            <span className="w-2 h-2 rounded-full animate-pulse" style={{ backgroundColor: REGIME_COLORS[analysis.current_regime] }} />
            {analysis.current_regime}
          </span>
        </div>
        <div className="grid grid-cols-4 gap-3 flex-1">
          {coverageData.map(c => (
            <div key={c.regime} className="bg-zinc-900 border border-zinc-800 rounded-lg p-3 text-center">
              <div className="text-[10px] font-mono mb-1" style={{ color: c.fill }}>{c.regime}</div>
              <div className="text-lg font-mono font-bold tabular-nums" style={{ color: c.fill }}>{c.value}%</div>
            </div>
          ))}
        </div>
      </div>

      {/* Coverage radar */}
      <div className="grid grid-cols-2 gap-4">
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Coverage Distribution" />
          <div className="h-44">
            <ResponsiveContainer width="100%" height="100%">
              <RadarChart data={coverageData}>
                <PolarGrid stroke="#27272a" />
                <PolarAngleAxis dataKey="regime" tick={{ fontSize: 10, fill: '#71717a', fontFamily: 'monospace' }} />
                <Radar dataKey="value" stroke="#f59e0b" fill="#f59e0b" fillOpacity={0.2} strokeWidth={1.5} />
              </RadarChart>
            </ResponsiveContainer>
          </div>
        </div>

        {/* Recent regime timeline */}
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Recent Regime Windows" sub="newest at bottom" />
          <div className="space-y-1 max-h-44 overflow-y-auto">
            {recentWindows.map((w, i) => (
              <div key={i} className="flex items-center gap-3">
                <div className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: REGIME_COLORS[w.label] }} />
                <span className="text-[10px] font-mono uppercase tracking-widest w-16 flex-shrink-0" style={{ color: REGIME_COLORS[w.label] }}>
                  {w.label}
                </span>
                <span className="text-[10px] font-mono text-zinc-500 flex-1">
                  {fmt.date(w.from)} – {fmt.date(w.to)}
                </span>
                <span className="text-[10px] font-mono text-zinc-600">{w.bars}b</span>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Multi-Symbol view panel
// ─────────────────────────────────────────────────────────────────────────────

function MultiSymbolPanel({ strategyId }: { strategyId: string }) {
  const SYMBOLS = ['BTCUSDT', 'ETHUSDT', 'SOLUSDT', 'BNBUSDT', 'XRPUSDT']

  const { data: allDecisions } = useSWR(
    strategyId ? ['journal-multi', strategyId] : null,
    () => journalApi.query({ strategy_id: strategyId, limit: 1000 })
  )

  const bySymbol = useMemo(() => {
    const map = new Map<string, DecisionEntry[]>()
    ;(allDecisions ?? []).forEach(d => {
      const arr = map.get(d.symbol) ?? []
      arr.push(d)
      map.set(d.symbol, arr)
    })
    return map
  }, [allDecisions])

  const symbolStats = useMemo(() => {
    return SYMBOLS.map(sym => {
      const decisions = bySymbol.get(sym) ?? []
      const closed = decisions.filter(d => d.outcome)
      const wins = closed.filter(d => d.outcome!.class === 'win')
      const pnl = closed.reduce((s, d) => s + (d.outcome?.realized_pnl ?? 0), 0)
      return {
        symbol: sym,
        total: decisions.length,
        winRate: closed.length > 0 ? wins.length / closed.length : 0,
        pnl,
        decisions,
      }
    }).filter(s => s.total > 0)
  }, [bySymbol])

  if (!strategyId) return <EmptyState icon="📈" message="Select a strategy to view multi-symbol breakdown" />

  // Consistency score across symbols
  const winRates = symbolStats.map(s => s.winRate).filter(r => r > 0)
  const consistency = winRates.length > 1
    ? 1 - (Math.max(...winRates) - Math.min(...winRates))
    : winRates.length === 1 ? 1.0 : 0

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-4 gap-3">
        <MetricCard
          label="Symbols Traded"
          value={String(symbolStats.length)}
          accent="zinc"
        />
        <MetricCard
          label="Consistency"
          value={fmt.pct(consistency)}
          sub="win rate variance across symbols"
          accent={consistency > 0.7 ? 'emerald' : consistency > 0.4 ? 'amber' : 'red'}
        />
        <MetricCard
          label="Best Symbol"
          value={symbolStats.sort((a,b) => b.winRate - a.winRate)[0]?.symbol ?? '—'}
          sub={symbolStats[0] ? fmt.pct(symbolStats[0].winRate) + ' win rate' : ''}
          accent="emerald"
        />
        <MetricCard
          label="Total Multi-Symbol PnL"
          value={`${symbolStats.reduce((s, x) => s + x.pnl, 0) >= 0 ? '+' : ''}${symbolStats.reduce((s, x) => s + x.pnl, 0).toFixed(2)}`}
          accent={symbolStats.reduce((s, x) => s + x.pnl, 0) >= 0 ? 'emerald' : 'red'}
        />
      </div>

      {symbolStats.length === 0 ? (
        <EmptyState icon="📭" message="No multi-symbol data yet" sub="Run the strategy across multiple symbols to see comparisons" />
      ) : (
        <div className="grid grid-cols-1 gap-4">
          {symbolStats.map(s => (
            <div key={s.symbol} className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
              <div className="flex items-center justify-between mb-3">
                <div className="flex items-center gap-3">
                  <span className="font-mono font-bold text-zinc-100">{s.symbol}</span>
                  <span className="text-xs font-mono text-zinc-500">{s.total} decisions</span>
                </div>
                <div className="flex items-center gap-4 text-xs font-mono">
                  <span className={s.winRate >= 0.5 ? 'text-emerald-400' : 'text-red-400'}>
                    {fmt.pct(s.winRate)} WR
                  </span>
                  <span className={s.pnl >= 0 ? 'text-emerald-400' : 'text-red-400'}>
                    {s.pnl >= 0 ? '+' : ''}{s.pnl.toFixed(2)} USDT
                  </span>
                </div>
              </div>
              <OutcomeHeatmap decisions={s.decisions} weeks={13} />
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Ensemble Status panel
// ─────────────────────────────────────────────────────────────────────────────

function EnsemblePanel({ strategyId }: { strategyId: string }) {
  const [symbol, setSymbol] = useState('BTCUSDT')

  // Fetch ensemble status — v1.2 backend endpoint
  const { data: status, error } = useSWR<EnsembleStatus>(
    strategyId ? `/api/v1/ensemble/${strategyId}/${symbol}` : null,
    (url: string) => fetch(url, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )

  // Fetch approved versions to show ensemble membership
  const { data: versions } = useSWR(
    strategyId ? ['registry-versions', strategyId] : null,
    ([, id]) => registryApi.listVersions(id)
  )

  const approvedVersions = (versions ?? []).filter(v => v.status === 'approved')

  return (
    <div className="space-y-4">
      {/* Symbol selector */}
      <div className="flex items-center gap-3">
        <label className="text-xs font-mono text-zinc-500 uppercase">Symbol</label>
        <input
          value={symbol}
          onChange={e => setSymbol(e.target.value.toUpperCase())}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 w-36"
        />
      </div>

      {/* Ensemble membership */}
      <div>
        <SectionHeader title={`${approvedVersions.length} Approved Versions in Ensemble`} />
        {approvedVersions.length === 0 ? (
          <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
            <EmptyState icon="🗳" message="No approved versions yet" sub="Promote strategies to StatusApproved to add them to the ensemble" />
          </div>
        ) : (
          <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
            {approvedVersions.map(v => {
              const voter = status?.voters?.find(vr => vr.strategy_version === v.version)
              return (
                <div key={v.version} className="flex items-center gap-4 px-4 py-3 border-b border-zinc-800 last:border-0">
                  <span className="font-mono text-sm text-amber-400 font-bold w-16">v{v.version}</span>
                  <StatusBadge status={v.status} />
                  {voter && (
                    <>
                      <div className="flex-1">
                        <ScoreBar score={voter.weight} max={3} />
                      </div>
                      <span className="text-xs font-mono text-zinc-500 w-20 text-right">
                        wt {voter.weight.toFixed(2)}
                      </span>
                      <span className={`text-xs font-mono w-16 text-right ${voter.agreed ? 'text-emerald-400' : 'text-zinc-500'}`}>
                        {voter.agreed ? '✓ agreed' : 'dissent'}
                      </span>
                      {voter.last_action && (
                        <ActionBadge action={voter.last_action as any} />
                      )}
                    </>
                  )}
                  {!voter && (
                    <span className="text-xs font-mono text-zinc-600 ml-auto">awaiting signal</span>
                  )}
                </div>
              )
            })}
          </div>
        )}
      </div>

      {/* Last vote result */}
      {status && (
        <div className={`border rounded-lg p-4 ${
          status.agreed_action === 'hold' || status.agreed_action === 'wait'
            ? 'bg-zinc-900 border-zinc-700'
            : status.agreed_action.includes('long')
            ? 'bg-emerald-950/30 border-emerald-800'
            : 'bg-red-950/30 border-red-800'
        }`}>
          <div className="flex items-center gap-4">
            <div>
              <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">Last Ensemble Decision</div>
              <ActionBadge action={status.agreed_action as any} />
            </div>
            <div>
              <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">Weighted Confidence</div>
              <span className="text-lg font-mono font-bold text-amber-400">{status.weighted_confidence.toFixed(0)}%</span>
            </div>
            <div>
              <div className="text-[10px] font-mono text-zinc-500 uppercase mb-1">Quorum</div>
              <span className="text-lg font-mono font-bold text-zinc-200">
                {status.quorum}/{status.voters?.length ?? 0}
              </span>
            </div>
          </div>
        </div>
      )}

      {error && (
        <ErrorBanner message="Ensemble status unavailable — ensure v1.2 backend is deployed and at least 2 versions are approved" />
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main Intelligence page
// ─────────────────────────────────────────────────────────────────────────────

type Panel = 'regime' | 'multi-symbol' | 'ensemble'

export default function Intelligence() {
  const [activePanel, setActivePanel] = useState<Panel>('regime')
  const [strategyId, setStrategyId] = useState('')
  const [symbol, setSymbol] = useState('BTCUSDT')

  const { data: traders } = useSWR<Array<{ id: string; name: string }>>(
    '/api/traders',
    (url: string) => fetch(url, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )

  const panels: Array<{ id: Panel; label: string; icon: string; desc: string }> = [
    { id: 'regime',       label: 'Regime Map',     icon: '🌊', desc: 'Current market regime + timeline' },
    { id: 'multi-symbol', label: 'Multi-Symbol',   icon: '🔢', desc: 'Per-symbol outcome breakdown' },
    { id: 'ensemble',     label: 'Ensemble',       icon: '🗳',  desc: 'Weighted voting across versions' },
  ]

  return (
    <PageShell title="Intelligence" icon="🧠" tag="v1.2">

      {/* Panel selector + global filters */}
      <div className="flex items-center gap-4 mb-6 flex-wrap">
        {/* Panel tabs */}
        <div className="flex bg-zinc-900 border border-zinc-800 rounded-lg p-1 gap-1">
          {panels.map(p => (
            <button
              key={p.id}
              onClick={() => setActivePanel(p.id)}
              className={`px-3 py-1.5 rounded text-xs font-mono flex items-center gap-1.5 transition-colors ${
                activePanel === p.id
                  ? 'bg-amber-400 text-zinc-950 font-bold'
                  : 'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800'
              }`}
            >
              {p.icon} {p.label}
            </button>
          ))}
        </div>

        {/* Strategy selector */}
        <select
          value={strategyId}
          onChange={e => setStrategyId(e.target.value)}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 min-w-48"
        >
          <option value="">All strategies</option>
          {(traders ?? []).map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>

        {/* Symbol for regime panel */}
        {activePanel === 'regime' && (
          <input
            value={symbol}
            onChange={e => setSymbol(e.target.value.toUpperCase())}
            placeholder="Symbol"
            className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 w-32"
          />
        )}
      </div>

      {/* Active panel */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-xl p-5">
        <SectionHeader
          title={panels.find(p => p.id === activePanel)!.label}
          sub={panels.find(p => p.id === activePanel)!.desc}
        />
        {activePanel === 'regime' && <RegimePanel symbol={symbol} />}
        {activePanel === 'multi-symbol' && <MultiSymbolPanel strategyId={strategyId} />}
        {activePanel === 'ensemble' && <EnsemblePanel strategyId={strategyId} />}
      </div>
    </PageShell>
  )
}
