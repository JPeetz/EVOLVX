// web/src/components/evolvx/StrategyDiff.tsx
//
// Side-by-side parameter diff between two strategy versions.
// Changed values highlighted in amber. Performance delta shown at bottom.
// Used from the Registry page when the user clicks "Compare versions".

import React, { useMemo } from 'react'
import type { StrategyRecord } from '../../lib/evolvx-api'
import { Modal, Delta } from './ui'

interface Props {
  open: boolean
  onClose: () => void
  versionA: StrategyRecord | null
  versionB: StrategyRecord | null
}

type DiffValue = {
  key: string
  label: string
  a: string
  b: string
  changed: boolean
  group: string
}

const PARAM_LABELS: Record<string, [string, string]> = {
  // [display label, group]
  coin_source_type:                  ['Coin Source',         'Coins'],
  static_coins:                      ['Static Coins',        'Coins'],
  use_coin_pool:                     ['Use Coin Pool',       'Coins'],
  use_oi_top:                        ['Use OI Top',         'Coins'],
  coin_pool_limit:                   ['Pool Limit',         'Coins'],
  trading_mode:                      ['Trading Mode',       'Strategy'],
  max_positions:                     ['Max Positions',      'Risk'],
  min_position_size:                 ['Min Size (USDT)',    'Risk'],
  max_margin_usage:                  ['Max Margin',        'Risk'],
  min_confidence:                    ['Min Confidence',    'Risk'],
  min_risk_reward_ratio:             ['Min R:R Ratio',     'Risk'],
  altcoin_max_leverage:              ['Altcoin Max Lev',   'Risk'],
  btceth_max_leverage:               ['BTC/ETH Max Lev',   'Risk'],
  altcoin_max_position_value_ratio:  ['Altcoin Pos Ratio', 'Risk'],
  btceth_max_position_value_ratio:   ['BTC/ETH Pos Ratio', 'Risk'],
  enable_rsi:                        ['Enable RSI',        'Indicators'],
  rsi_periods:                       ['RSI Periods',       'Indicators'],
  enable_ema:                        ['Enable EMA',        'Indicators'],
  ema_periods:                       ['EMA Periods',       'Indicators'],
  enable_macd:                       ['Enable MACD',       'Indicators'],
  enable_atr:                        ['Enable ATR',        'Indicators'],
  atr_periods:                       ['ATR Periods',       'Indicators'],
  enable_oi:                         ['Enable OI',         'Indicators'],
  enable_funding_rate:               ['Funding Rate',      'Indicators'],
  enable_quant_data:                 ['Quant Data',        'Indicators'],
  primary_timeframe:                 ['Primary TF',        'Data'],
  selected_timeframes:               ['Timeframes',        'Data'],
  primary_count:                     ['Bar Count',         'Data'],
}

function formatValue(v: unknown): string {
  if (v === null || v === undefined) return '—'
  if (Array.isArray(v)) return v.join(', ') || '—'
  if (typeof v === 'boolean') return v ? 'Yes' : 'No'
  if (typeof v === 'number') {
    if (v > 0 && v <= 1 && String(v).includes('.')) return `${(v * 100).toFixed(0)}%`
    return String(v)
  }
  return String(v)
}

function buildDiff(a: StrategyRecord, b: StrategyRecord): DiffValue[] {
  const diffs: DiffValue[] = []
  const params = Object.keys(PARAM_LABELS)

  params.forEach(key => {
    const [label, group] = PARAM_LABELS[key]
    const va = (a.parameters as Record<string, unknown>)[key]
    const vb = (b.parameters as Record<string, unknown>)[key]
    const fa = formatValue(va)
    const fb = formatValue(vb)
    diffs.push({ key, label, a: fa, b: fb, changed: fa !== fb, group })
  })

  return diffs
}

function PerformanceDelta({ a, b }: { a: StrategyRecord; b: StrategyRecord }) {
  const perfA = a.performance?.[a.performance.length - 1]
  const perfB = b.performance?.[b.performance.length - 1]

  if (!perfA && !perfB) return null

  const metrics = [
    { label: 'Net Return', ka: perfA?.net_return, kb: perfB?.net_return, fmt: 'pct' },
    { label: 'Sharpe',     ka: perfA?.sharpe_ratio, kb: perfB?.sharpe_ratio, fmt: 'abs' },
    { label: 'Max DD',     ka: perfA?.max_drawdown, kb: perfB?.max_drawdown, fmt: 'pct' },
    { label: 'Win Rate',   ka: perfA?.win_rate, kb: perfB?.win_rate, fmt: 'pct' },
    { label: 'Trades',     ka: perfA?.total_trades, kb: perfB?.total_trades, fmt: 'abs' },
  ] as const

  return (
    <div>
      <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-3 mt-2">
        Latest Performance
      </div>
      <div className="grid grid-cols-5 gap-3">
        {metrics.map(m => (
          <div key={m.label} className="bg-zinc-800 rounded-lg p-3">
            <div className="text-[10px] font-mono text-zinc-500 mb-1">{m.label}</div>
            <div className="text-xs font-mono text-zinc-400">
              {m.ka !== undefined ? (
                m.fmt === 'pct'
                  ? `${((m.ka as number) * 100).toFixed(1)}%`
                  : String(m.ka)
              ) : '—'}
            </div>
            <div className="text-xs font-mono text-zinc-200 mt-0.5">
              {m.kb !== undefined ? (
                m.fmt === 'pct'
                  ? `${((m.kb as number) * 100).toFixed(1)}%`
                  : String(m.kb)
              ) : '—'}
            </div>
            {m.ka !== undefined && m.kb !== undefined && (
              <div className="mt-1">
                <Delta
                  value={(m.kb as number) - (m.ka as number)}
                  format={m.fmt === 'pct' ? 'pct' : 'abs'}
                />
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  )
}

export default function StrategyDiff({ open, onClose, versionA, versionB }: Props) {
  const diffs = useMemo(() => {
    if (!versionA || !versionB) return []
    return buildDiff(versionA, versionB)
  }, [versionA, versionB])

  const groups = useMemo(() => {
    const g = new Map<string, DiffValue[]>()
    diffs.forEach(d => {
      const arr = g.get(d.group) ?? []
      arr.push(d)
      g.set(d.group, arr)
    })
    return g
  }, [diffs])

  const changedCount = diffs.filter(d => d.changed).length

  if (!versionA || !versionB) return null

  return (
    <Modal
      open={open}
      onClose={onClose}
      title={`Strategy Diff — v${versionA.version} → v${versionB.version}`}
      width="max-w-4xl"
    >
      {/* Header row */}
      <div className="grid grid-cols-[1fr_1fr_1fr] gap-4 mb-4">
        <div />
        <div className="bg-zinc-800 rounded-lg p-3 text-center">
          <div className="text-xs font-mono text-zinc-400 mb-1">VERSION A (base)</div>
          <div className="text-lg font-mono font-bold text-zinc-100">v{versionA.version}</div>
          <div className="text-[10px] font-mono text-zinc-500 mt-1">
            {versionA.status} · {new Date(versionA.created_at).toLocaleDateString()}
          </div>
        </div>
        <div className="bg-zinc-800 rounded-lg p-3 text-center border border-amber-900/50">
          <div className="text-xs font-mono text-zinc-400 mb-1">VERSION B (new)</div>
          <div className="text-lg font-mono font-bold text-amber-400">v{versionB.version}</div>
          <div className="text-[10px] font-mono text-zinc-500 mt-1">
            {versionB.status} · {new Date(versionB.created_at).toLocaleDateString()}
          </div>
        </div>
      </div>

      {/* Changed count */}
      <div className="flex items-center gap-2 mb-4">
        <span className="text-xs font-mono text-zinc-500">
          {changedCount === 0 ? 'No parameter changes' : `${changedCount} parameter${changedCount === 1 ? '' : 's'} changed`}
        </span>
        {changedCount > 0 && (
          <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-amber-950 text-amber-400 border border-amber-800">
            {changedCount} Δ
          </span>
        )}
      </div>

      {/* Diff table */}
      <div className="space-y-4">
        {Array.from(groups.entries()).map(([group, rows]) => {
          const hasChanges = rows.some(r => r.changed)
          return (
            <div key={group}>
              <div className="flex items-center gap-2 mb-2">
                <span className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest">{group}</span>
                {hasChanges && (
                  <span className="text-[9px] font-mono text-amber-500">● changed</span>
                )}
              </div>
              <div className="space-y-px rounded-lg overflow-hidden border border-zinc-800">
                {rows.map(row => (
                  <div
                    key={row.key}
                    className={`grid grid-cols-[1fr_1fr_1fr] gap-0 ${
                      row.changed ? 'bg-amber-950/20' : 'bg-zinc-900'
                    }`}
                  >
                    {/* Label */}
                    <div className="px-3 py-2 border-r border-zinc-800">
                      <span className={`text-xs font-mono ${row.changed ? 'text-amber-400' : 'text-zinc-500'}`}>
                        {row.changed && '● '}{row.label}
                      </span>
                    </div>
                    {/* Value A */}
                    <div className="px-3 py-2 border-r border-zinc-800">
                      <span className={`text-xs font-mono tabular-nums ${row.changed ? 'text-zinc-400 line-through decoration-zinc-600' : 'text-zinc-300'}`}>
                        {row.a}
                      </span>
                    </div>
                    {/* Value B */}
                    <div className="px-3 py-2">
                      <span className={`text-xs font-mono tabular-nums ${row.changed ? 'text-amber-300 font-semibold' : 'text-zinc-300'}`}>
                        {row.b}
                        {row.changed && ' ←'}
                      </span>
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )
        })}
      </div>

      {/* Performance delta */}
      <PerformanceDelta a={versionA} b={versionB} />
    </Modal>
  )
}
