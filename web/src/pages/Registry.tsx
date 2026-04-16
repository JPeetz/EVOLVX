// web/src/pages/Registry.tsx
//
// Strategy Registry page — v1.1 EvolvX UI.
//
// Layout:
//   Left  (380px) : scrollable version history timeline + performance chart
//   Right (flex)  : SVG lineage graph + selected version detail panel
//
// The page fetches the strategy list from the existing NOFX /api/traders
// endpoint to populate the strategy selector, then loads registry data.

import React, { useState, useEffect, useCallback } from 'react'
import useSWR, { mutate } from 'swr'
import {
  AreaChart, Area, XAxis, YAxis, Tooltip as RTooltip,
  ResponsiveContainer, CartesianGrid
} from 'recharts'

import {
  registryApi,
  fmt,
  type StrategyRecord,
  type StrategyStatus,
} from '../lib/evolvx-api'

import {
  PageShell, StatusBadge, MetricCard, SectionHeader,
  Btn, Modal, Spinner, EmptyState, ErrorBanner, Delta
} from '../components/evolvx/ui'
import LineageGraph from '../components/evolvx/LineageGraph'
import StrategyDiff from '../components/evolvx/StrategyDiff'

// ─────────────────────────────────────────────────────────────────────────────
// Strategy selector (uses existing NOFX traders list as source of IDs)
// ─────────────────────────────────────────────────────────────────────────────

function useStrategyList() {
  // The existing NOFX API returns traders; each has a strategy_id.
  // We deduplicate by strategy_id to get the unique registered strategies.
  const { data, error } = useSWR<Array<{ id: string; name: string; strategy_id?: string }>>(
    '/api/traders',
    (url: string) => fetch(url, {
      headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
    }).then(r => r.json())
  )

  const strategies = React.useMemo(() => {
    const seen = new Set<string>()
    const result: Array<{ id: string; name: string }> = []
    data?.forEach(t => {
      const sid = t.strategy_id ?? t.id
      if (!seen.has(sid)) {
        seen.add(sid)
        result.push({ id: sid, name: t.name })
      }
    })
    return result
  }, [data])

  return { strategies, loading: !data && !error, error }
}

// ─────────────────────────────────────────────────────────────────────────────
// Version timeline entry
// ─────────────────────────────────────────────────────────────────────────────

function VersionRow({
  record,
  isSelected,
  isLatest,
  onSelect,
}: {
  record: StrategyRecord
  isSelected: boolean
  isLatest: boolean
  onSelect: () => void
}) {
  const perf = record.performance?.[record.performance.length - 1]

  return (
    <button
      onClick={onSelect}
      className={`w-full text-left px-4 py-3 border-l-2 transition-all duration-150 hover:bg-zinc-800/60 ${
        isSelected
          ? 'border-amber-400 bg-zinc-800/80'
          : 'border-zinc-800 hover:border-zinc-600'
      }`}
    >
      <div className="flex items-center justify-between mb-1.5">
        <div className="flex items-center gap-2">
          <span className={`font-mono font-bold text-sm ${isSelected ? 'text-amber-400' : 'text-zinc-200'}`}>
            v{record.version}
          </span>
          {isLatest && (
            <span className="text-[9px] font-mono px-1.5 py-0.5 rounded bg-zinc-700 text-zinc-400 uppercase tracking-wider">
              latest
            </span>
          )}
        </div>
        <StatusBadge status={record.status} />
      </div>

      <div className="flex items-center gap-3 text-[10px] font-mono text-zinc-500">
        <span>{fmt.date(record.created_at)}</span>
        <span className="text-zinc-700">·</span>
        <span>{record.author}</span>
      </div>

      {/* Mutation summary */}
      {record.parent_version && (
        <div className="mt-1.5 text-[10px] font-mono text-zinc-600 truncate">
          ↳ {record.parent_version} — {
            record.parameters.trading_mode !== undefined
              ? `mode: ${record.parameters.trading_mode}`
              : 'parameter change'
          }
        </div>
      )}

      {/* Quick perf */}
      {perf && (
        <div className="mt-2 flex gap-3">
          <span className={`text-[10px] font-mono ${perf.net_return >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
            {fmt.pctSigned(perf.net_return)} return
          </span>
          <span className="text-[10px] font-mono text-zinc-600">
            SR {perf.sharpe_ratio.toFixed(2)}
          </span>
          <span className="text-[10px] font-mono text-zinc-600">
            {perf.total_trades} trades
          </span>
        </div>
      )}
    </button>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Selected version detail panel
// ─────────────────────────────────────────────────────────────────────────────

function VersionDetail({
  record,
  onApprove,
  onDeprecate,
  onDiff,
  approving,
}: {
  record: StrategyRecord
  onApprove: () => void
  onDeprecate: () => void
  onDiff: () => void
  approving: boolean
}) {
  const [showParams, setShowParams] = useState(false)
  const perf = record.performance ?? []

  const perfChartData = perf.map((p, i) => ({
    run: i + 1,
    return: +(p.net_return * 100).toFixed(2),
    sharpe: +p.sharpe_ratio.toFixed(2),
    drawdown: +(Math.abs(p.max_drawdown) * 100).toFixed(2),
  }))

  const canApprove  = record.status === 'paper'
  const canDeprecate = record.status === 'approved' || record.status === 'paper'

  return (
    <div className="space-y-4">
      {/* Identity */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-1">
              <span className="text-xl font-mono font-bold text-zinc-100">v{record.version}</span>
              <StatusBadge status={record.status} />
            </div>
            <div className="text-xs font-mono text-zinc-500">
              {record.name} · {record.author} · {fmt.dateTime(record.created_at)}
            </div>
            {record.parent_version && (
              <div className="text-xs font-mono text-zinc-600 mt-0.5">
                Forked from v{record.parent_version}
              </div>
            )}
          </div>
          <div className="flex gap-2">
            <Btn size="sm" variant="ghost" onClick={onDiff}>Diff</Btn>
            <a
              href={registryApi.exportVersion(record.id, record.version)}
              download={`strategy-${record.version}.json`}
              className="inline-flex items-center gap-1 px-3 py-1 rounded bg-zinc-800 text-zinc-300 hover:bg-zinc-700 text-xs font-mono border border-zinc-700 transition-colors"
            >
              ↓ Export
            </a>
          </div>
        </div>

        {/* Action buttons */}
        <div className="flex gap-2 mt-4 pt-3 border-t border-zinc-800">
          {canApprove && (
            <Btn variant="primary" size="sm" onClick={onApprove} disabled={approving}>
              {approving ? <><Spinner size="sm" /> Approving…</> : '✓ Approve for Live'}
            </Btn>
          )}
          {canDeprecate && (
            <Btn variant="danger" size="sm" onClick={onDeprecate}>
              Deprecate
            </Btn>
          )}
          {record.status === 'approved' && (
            <span className="inline-flex items-center gap-1 text-xs font-mono text-emerald-400 px-3 py-1 rounded bg-emerald-950 border border-emerald-900">
              ✓ Live-approved
            </span>
          )}
        </div>
      </div>

      {/* Performance metrics */}
      {perf.length > 0 && (
        <div>
          <SectionHeader title="Performance History" sub={`${perf.length} evaluated run${perf.length > 1 ? 's' : ''}`} />
          <div className="grid grid-cols-2 gap-3 mb-4">
            {[
              { label: 'Net Return', value: fmt.pctSigned(perf[perf.length-1].net_return), accent: perf[perf.length-1].net_return >= 0 ? 'emerald' : 'red' },
              { label: 'Max Drawdown', value: fmt.pct(Math.abs(perf[perf.length-1].max_drawdown)), accent: 'red' },
              { label: 'Sharpe Ratio', value: perf[perf.length-1].sharpe_ratio.toFixed(2), accent: 'sky' },
              { label: 'Win Rate', value: fmt.pct(perf[perf.length-1].win_rate), accent: 'amber' },
            ].map(m => (
              <MetricCard key={m.label} label={m.label} value={m.value} accent={m.accent as 'emerald' | 'red' | 'sky' | 'amber'} />
            ))}
          </div>

          {/* Return over runs chart */}
          {perfChartData.length > 1 && (
            <div className="h-36 bg-zinc-900 border border-zinc-800 rounded-lg p-3">
              <p className="text-[10px] font-mono text-zinc-500 uppercase mb-2">Return % across runs</p>
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={perfChartData} margin={{ top: 0, right: 8, left: -24, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                  <XAxis dataKey="run" tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
                  <YAxis tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
                  <RTooltip
                    contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 11, fontFamily: 'monospace' }}
                    formatter={(v: number) => [`${v}%`, 'Return']}
                  />
                  <Area type="monotone" dataKey="return" stroke="#f59e0b" strokeWidth={1.5} fill="#f59e0b" fillOpacity={0.1} />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </div>
      )}

      {/* Parameters (collapsible) */}
      <div>
        <button
          onClick={() => setShowParams(v => !v)}
          className="flex items-center gap-2 text-[10px] font-mono text-zinc-500 uppercase tracking-widest hover:text-zinc-300 transition-colors"
        >
          <span>{showParams ? '▼' : '▶'}</span> Parameters
        </button>

        {showParams && (
          <div className="mt-3 bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
            {Object.entries(record.parameters)
              .filter(([, v]) => v !== undefined && v !== null && v !== '' && !(Array.isArray(v) && v.length === 0))
              .map(([k, v]) => (
                <div key={k} className="grid grid-cols-2 border-b border-zinc-800 last:border-0">
                  <div className="px-3 py-2 text-[10px] font-mono text-zinc-500">{k}</div>
                  <div className="px-3 py-2 text-[10px] font-mono text-zinc-300 tabular-nums">
                    {Array.isArray(v) ? v.join(', ') : typeof v === 'boolean' ? (v ? 'true' : 'false') : String(v)}
                  </div>
                </div>
              ))}
          </div>
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Approve confirmation modal
// ─────────────────────────────────────────────────────────────────────────────

function ApproveModal({ open, onClose, onConfirm, version }: {
  open: boolean
  onClose: () => void
  onConfirm: (email: string) => void
  version: string
}) {
  const [email, setEmail] = useState('j.peetz69@gmail.com')

  return (
    <Modal open={open} onClose={onClose} title={`Approve v${version} for Live Trading`} width="max-w-md">
      <div className="space-y-4">
        <div className="bg-amber-950/30 border border-amber-900/50 rounded-lg px-4 py-3">
          <p className="text-amber-300 text-xs font-mono">
            ⚠ This version will be eligible for live trading after approval.
            Once approved, it cannot be moved back to draft.
          </p>
        </div>
        <div>
          <label className="text-xs font-mono text-zinc-400 block mb-1.5">Approver identity (your email)</label>
          <input
            type="email"
            value={email}
            onChange={e => setEmail(e.target.value)}
            className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
          />
        </div>
        <div className="flex gap-3 pt-2">
          <Btn variant="primary" onClick={() => onConfirm(email)} disabled={!email}>
            Confirm Approval
          </Btn>
          <Btn variant="ghost" onClick={onClose}>Cancel</Btn>
        </div>
      </div>
    </Modal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main Registry page
// ─────────────────────────────────────────────────────────────────────────────

export default function Registry() {
  const { strategies, loading: loadingStrategies } = useStrategyList()
  const [selectedStrategyId, setSelectedStrategyId] = useState<string>('')
  const [selectedVersion, setSelectedVersion] = useState<string>('')
  const [diffBase, setDiffBase] = useState<string>('')
  const [showApprove, setShowApprove] = useState(false)
  const [showDiff, setShowDiff] = useState(false)
  const [approving, setApproving] = useState(false)
  const [statusError, setStatusError] = useState<string | null>(null)

  // Auto-select first strategy
  useEffect(() => {
    if (strategies.length > 0 && !selectedStrategyId) {
      setSelectedStrategyId(strategies[0].id)
    }
  }, [strategies, selectedStrategyId])

  const { data: versions, error: versionsError } = useSWR(
    selectedStrategyId ? ['registry-versions', selectedStrategyId] : null,
    ([, id]) => registryApi.listVersions(id)
  )

  const { data: lineage } = useSWR(
    selectedStrategyId ? ['registry-lineage', selectedStrategyId] : null,
    ([, id]) => registryApi.getLineage(id)
  )

  // Auto-select latest version
  useEffect(() => {
    if (versions && versions.length > 0 && !selectedVersion) {
      setSelectedVersion(versions[versions.length - 1].version)
    }
  }, [versions, selectedVersion])

  const selectedRecord = versions?.find(v => v.version === selectedVersion) ?? null
  const diffBaseRecord = versions?.find(v => v.version === diffBase) ?? null

  const handleApprove = useCallback(async (email: string) => {
    if (!selectedRecord) return
    setApproving(true)
    setStatusError(null)
    try {
      await registryApi.setStatus(selectedRecord.id, selectedRecord.version, 'approved', email)
      await mutate(['registry-versions', selectedStrategyId])
    } catch (e: unknown) {
      setStatusError(e instanceof Error ? e.message : 'Approval failed')
    } finally {
      setApproving(false)
      setShowApprove(false)
    }
  }, [selectedRecord, selectedStrategyId])

  const handleDeprecate = useCallback(async () => {
    if (!selectedRecord) return
    if (!confirm(`Deprecate v${selectedRecord.version}?`)) return
    try {
      await registryApi.setStatus(selectedRecord.id, selectedRecord.version, 'deprecated', 'j.peetz69@gmail.com')
      await mutate(['registry-versions', selectedStrategyId])
    } catch (e: unknown) {
      setStatusError(e instanceof Error ? e.message : 'Failed')
    }
  }, [selectedRecord, selectedStrategyId])

  const handleDiff = useCallback(() => {
    // Default diff = latest vs previous
    if (versions && versions.length >= 2) {
      const sorted = [...versions].sort((a, b) => a.version.localeCompare(b.version, undefined, { numeric: true }))
      setDiffBase(sorted[sorted.length - 2].version)
      setSelectedVersion(sorted[sorted.length - 1].version)
    }
    setShowDiff(true)
  }, [versions])

  return (
    <PageShell title="Strategy Registry" icon="🗂" tag="v1.1">
      {/* Strategy selector */}
      <div className="flex items-center gap-3 mb-6">
        <label className="text-xs font-mono text-zinc-500 uppercase tracking-widest whitespace-nowrap">
          Strategy
        </label>
        {loadingStrategies ? (
          <Spinner size="sm" />
        ) : (
          <select
            value={selectedStrategyId}
            onChange={e => { setSelectedStrategyId(e.target.value); setSelectedVersion('') }}
            className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 min-w-64"
          >
            {strategies.map(s => (
              <option key={s.id} value={s.id}>{s.name} ({s.id.slice(0, 8)}…)</option>
            ))}
          </select>
        )}
        {versions && (
          <span className="text-xs font-mono text-zinc-600">
            {versions.length} version{versions.length !== 1 ? 's' : ''}
          </span>
        )}
      </div>

      {statusError && <div className="mb-4"><ErrorBanner message={statusError} /></div>}

      {!selectedStrategyId ? (
        <EmptyState icon="🗂" message="Select a strategy to view its registry" />
      ) : versionsError ? (
        <ErrorBanner message="Failed to load strategy versions" />
      ) : !versions ? (
        <div className="flex items-center justify-center h-48"><Spinner /></div>
      ) : versions.length === 0 ? (
        <EmptyState icon="📋" message="No versions registered yet" sub="Run the migration tool to import existing strategies" />
      ) : (
        <div className="flex gap-5 items-start">
          {/* ── Left: Version timeline ── */}
          <div className="w-80 shrink-0">
            <SectionHeader title="Version History" sub="newest at top" />
            <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
              {[...versions]
                .sort((a, b) => b.version.localeCompare(a.version, undefined, { numeric: true }))
                .map((r, i, arr) => (
                  <div key={r.version} className="border-b border-zinc-800 last:border-0">
                    <VersionRow
                      record={r}
                      isSelected={r.version === selectedVersion}
                      isLatest={i === 0}
                      onSelect={() => setSelectedVersion(r.version)}
                    />
                  </div>
                ))}
            </div>
          </div>

          {/* ── Right: Detail + lineage ── */}
          <div className="flex-1 min-w-0 space-y-5">
            {selectedRecord && (
              <VersionDetail
                record={selectedRecord}
                onApprove={() => setShowApprove(true)}
                onDeprecate={handleDeprecate}
                onDiff={handleDiff}
                approving={approving}
              />
            )}

            {/* Lineage graph */}
            <div>
              <SectionHeader title="Lineage Graph" sub="click a node to select that version" />
              <LineageGraph
                records={versions}
                lineage={lineage ?? []}
                selectedVersion={selectedVersion}
                onSelect={setSelectedVersion}
              />
            </div>
          </div>
        </div>
      )}

      {/* Modals */}
      <ApproveModal
        open={showApprove}
        onClose={() => setShowApprove(false)}
        onConfirm={handleApprove}
        version={selectedRecord?.version ?? ''}
      />

      <StrategyDiff
        open={showDiff}
        onClose={() => setShowDiff(false)}
        versionA={diffBaseRecord}
        versionB={selectedRecord}
      />
    </PageShell>
  )
}
