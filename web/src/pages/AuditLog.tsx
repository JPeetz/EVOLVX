// web/src/pages/AuditLog.tsx
//
// Audit Log viewer — EvolvX v1.3
//
// Reads from the append-only event_log SQLite table populated by the
// SQLiteEventLogger in engine/pipeline/logger.go.
//
// Every pipeline event — market bars, signals, risk checks, fills, metrics,
// errors — is written here.  This page makes it inspectable.
//
// Layout:
//   Top row    : session selector + kind filter + date range
//   KPI row    : total events, fills, signals, errors
//   Main table : paginated event stream, expandable row payloads
//   Side panel : session summary stats

import React, { useState, useMemo } from 'react'
import useSWR from 'swr'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid,
  Tooltip as RTooltip, ResponsiveContainer, Cell
} from 'recharts'

import { fmt } from '../lib/evolvx-api'
import {
  PageShell, SectionHeader, MetricCard,
  ActionBadge, Spinner, EmptyState, ErrorBanner, Btn, Modal
} from '../components/evolvx/ui'

// ─────────────────────────────────────────────────────────────────────────────
// Types — mirror engine/core LogEntry
// ─────────────────────────────────────────────────────────────────────────────

type EventKind = 'market' | 'signal' | 'order' | 'fill' | 'risk' | 'metrics' | 'error'
type Mode = 'backtest' | 'paper' | 'live'

interface LogEntry {
  entry_id:   string
  kind:       EventKind
  timestamp:  string
  mode:       Mode
  session_id: string
  payload:    unknown
}

interface AuditQuery {
  session_id?: string
  kind?:       EventKind | ''
  mode?:       Mode | ''
  from?:       string
  to?:         string
  limit?:      number
  offset?:     number
}

interface SessionInfo {
  session_id:    string
  mode:          Mode
  strategy_id:   string
  first_event:   string
  last_event:    string
  event_count:   number
  fill_count:    number
  error_count:   number
}

// ─────────────────────────────────────────────────────────────────────────────
// API helpers
// ─────────────────────────────────────────────────────────────────────────────

async function fetchAuditLog(q: AuditQuery): Promise<LogEntry[]> {
  const params = new URLSearchParams()
  Object.entries(q).forEach(([k, v]) => v !== undefined && v !== '' && params.set(k, String(v)))
  const res = await fetch(`/api/v1/audit/events?${params}`, {
    headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
  })
  if (!res.ok) throw new Error(res.statusText)
  return res.json()
}

async function fetchSessions(): Promise<SessionInfo[]> {
  const res = await fetch('/api/v1/audit/sessions', {
    headers: { Authorization: `Bearer ${localStorage.getItem('token')}` }
  })
  if (!res.ok) throw new Error(res.statusText)
  return res.json()
}

// ─────────────────────────────────────────────────────────────────────────────
// Kind colours
// ─────────────────────────────────────────────────────────────────────────────

const KIND_STYLES: Record<EventKind, string> = {
  market:  'text-zinc-400 bg-zinc-800 border-zinc-700',
  signal:  'text-sky-300 bg-sky-950 border-sky-800',
  order:   'text-amber-300 bg-amber-950 border-amber-800',
  fill:    'text-emerald-300 bg-emerald-950 border-emerald-800',
  risk:    'text-orange-300 bg-orange-950 border-orange-800',
  metrics: 'text-violet-300 bg-violet-950 border-violet-800',
  error:   'text-red-300 bg-red-950 border-red-800',
}

const KIND_CHART_COLORS: Record<EventKind, string> = {
  market:  '#3f3f46',
  signal:  '#38bdf8',
  order:   '#fbbf24',
  fill:    '#34d399',
  risk:    '#fb923c',
  metrics: '#a78bfa',
  error:   '#f87171',
}

function KindBadge({ kind }: { kind: EventKind }) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-[10px] font-mono uppercase tracking-widest ${KIND_STYLES[kind]}`}>
      {kind}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Payload viewer
// ─────────────────────────────────────────────────────────────────────────────

function PayloadModal({ open, onClose, entry }: {
  open: boolean; onClose: () => void; entry: LogEntry | null
}) {
  if (!entry) return null
  const pretty = JSON.stringify(entry.payload, null, 2)

  return (
    <Modal open={open} onClose={onClose} title={`${entry.kind.toUpperCase()} — ${entry.entry_id.slice(0, 12)}…`} width="max-w-2xl">
      <div className="space-y-3">
        <div className="grid grid-cols-3 gap-3 text-xs font-mono">
          <div className="bg-zinc-800 rounded p-2">
            <div className="text-zinc-500 text-[10px] mb-0.5">Session</div>
            <div className="text-zinc-300 truncate">{entry.session_id.slice(0, 16)}…</div>
          </div>
          <div className="bg-zinc-800 rounded p-2">
            <div className="text-zinc-500 text-[10px] mb-0.5">Mode</div>
            <div className="text-zinc-300 capitalize">{entry.mode}</div>
          </div>
          <div className="bg-zinc-800 rounded p-2">
            <div className="text-zinc-500 text-[10px] mb-0.5">Time</div>
            <div className="text-zinc-300">{fmt.dateTime(entry.timestamp)}</div>
          </div>
        </div>
        <div>
          <div className="text-[10px] font-mono text-zinc-500 uppercase mb-2">Payload</div>
          <pre className="bg-zinc-900 border border-zinc-800 rounded-lg p-4 text-xs font-mono text-zinc-300 overflow-auto max-h-96 leading-relaxed">
            {pretty}
          </pre>
        </div>
      </div>
    </Modal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Session sidebar
// ─────────────────────────────────────────────────────────────────────────────

function SessionSidebar({ sessions, selected, onSelect }: {
  sessions: SessionInfo[]
  selected: string
  onSelect: (id: string) => void
}) {
  return (
    <div className="w-64 shrink-0">
      <SectionHeader title="Sessions" sub={`${sessions.length} total`} />
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden max-h-[600px] overflow-y-auto">
        {sessions.length === 0 ? (
          <EmptyState icon="📋" message="No sessions" />
        ) : (
          sessions.map(s => (
            <button
              key={s.session_id}
              onClick={() => onSelect(s.session_id)}
              className={`w-full text-left px-4 py-3 border-b border-zinc-800 last:border-0 transition-all hover:bg-zinc-800/60 ${
                selected === s.session_id
                  ? 'border-l-2 border-l-amber-400 bg-zinc-800/80'
                  : 'border-l-2 border-l-transparent'
              }`}
            >
              <div className="flex items-center justify-between mb-1">
                <span className="text-[10px] font-mono text-zinc-500 truncate">
                  {s.session_id.slice(0, 14)}…
                </span>
                <span className={`text-[9px] font-mono uppercase px-1 py-0.5 rounded ${
                  s.mode === 'live' ? 'text-emerald-400 bg-emerald-950' :
                  s.mode === 'paper' ? 'text-sky-400 bg-sky-950' :
                  'text-zinc-400 bg-zinc-800'
                }`}>{s.mode}</span>
              </div>
              <div className="text-[10px] font-mono text-zinc-600">{fmt.date(s.first_event)}</div>
              <div className="flex gap-3 mt-1 text-[10px] font-mono">
                <span className="text-zinc-500">{s.event_count} events</span>
                {s.fill_count > 0 && <span className="text-emerald-500">{s.fill_count} fills</span>}
                {s.error_count > 0 && <span className="text-red-400">{s.error_count} errors</span>}
              </div>
            </button>
          ))
        )}
      </div>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Event kind breakdown chart
// ─────────────────────────────────────────────────────────────────────────────

function KindBreakdown({ events }: { events: LogEntry[] }) {
  const counts = useMemo(() => {
    const c: Record<string, number> = {}
    events.forEach(e => { c[e.kind] = (c[e.kind] ?? 0) + 1 })
    return Object.entries(c).map(([kind, count]) => ({ kind, count }))
      .sort((a, b) => b.count - a.count)
  }, [events])

  if (counts.length === 0) return null

  return (
    <div className="h-28">
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={counts} margin={{ top: 4, right: 4, left: -24, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
          <XAxis dataKey="kind" tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
          <YAxis tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
          <RTooltip
            contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }}
          />
          <Bar dataKey="count" radius={[2,2,0,0]}>
            {counts.map(e => (
              <Cell key={e.kind} fill={KIND_CHART_COLORS[e.kind as EventKind] ?? '#3f3f46'} />
            ))}
          </Bar>
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main AuditLog page
// ─────────────────────────────────────────────────────────────────────────────

const ALL_KINDS: EventKind[] = ['market', 'signal', 'order', 'fill', 'risk', 'metrics', 'error']
const PAGE_SIZE = 50

export default function AuditLog() {
  const [selectedSession, setSelectedSession] = useState('')
  const [kindFilter, setKindFilter]   = useState<EventKind | ''>('')
  const [modeFilter, setModeFilter]   = useState<Mode | ''>('')
  const [page, setPage]               = useState(0)
  const [selected, setSelected]       = useState<LogEntry | null>(null)

  const query = useMemo((): AuditQuery => ({
    session_id: selectedSession || undefined,
    kind:       kindFilter || undefined,
    mode:       modeFilter || undefined,
    limit:      PAGE_SIZE,
    offset:     page * PAGE_SIZE,
  }), [selectedSession, kindFilter, modeFilter, page])

  const { data: events, error, isLoading } = useSWR(
    ['audit-events', JSON.stringify(query)],
    () => fetchAuditLog(query),
    { refreshInterval: 5000 } // live refresh every 5s
  )

  const { data: sessions } = useSWR('audit-sessions', fetchSessions, {
    refreshInterval: 10000
  })

  // KPIs from current page
  const kpis = useMemo(() => {
    const ev = events ?? []
    return {
      total:   ev.length,
      signals: ev.filter(e => e.kind === 'signal').length,
      fills:   ev.filter(e => e.kind === 'fill').length,
      errors:  ev.filter(e => e.kind === 'error').length,
    }
  }, [events])

  return (
    <PageShell title="Audit Log" icon="🔍" tag="v1.3">

      {/* Global filters */}
      <div className="flex items-center gap-3 mb-6 flex-wrap">
        {/* Kind filters — toggleable pills */}
        <div className="flex items-center gap-1">
          <span className="text-[10px] font-mono text-zinc-500 mr-1 uppercase">Kind</span>
          <button
            onClick={() => setKindFilter('')}
            className={`px-2 py-1 rounded text-[10px] font-mono border transition-colors ${
              kindFilter === '' ? 'bg-amber-400 text-zinc-950 border-amber-400 font-bold' : 'border-zinc-700 text-zinc-500 hover:text-zinc-300'
            }`}
          >
            all
          </button>
          {ALL_KINDS.map(k => (
            <button
              key={k}
              onClick={() => setKindFilter(kindFilter === k ? '' : k)}
              className={`px-2 py-1 rounded text-[10px] font-mono border transition-colors ${
                kindFilter === k
                  ? `${KIND_STYLES[k]} font-bold`
                  : 'border-zinc-700 text-zinc-500 hover:text-zinc-300'
              }`}
            >
              {k}
            </button>
          ))}
        </div>

        {/* Mode filter */}
        <select
          value={modeFilter}
          onChange={e => { setModeFilter(e.target.value as Mode | ''); setPage(0) }}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
        >
          <option value="">All modes</option>
          <option value="backtest">Backtest</option>
          <option value="paper">Paper</option>
          <option value="live">Live</option>
        </select>

        {(kindFilter || modeFilter || selectedSession) && (
          <Btn size="sm" variant="ghost" onClick={() => {
            setKindFilter(''); setModeFilter(''); setSelectedSession(''); setPage(0)
          }}>
            Clear
          </Btn>
        )}

        <span className="ml-auto text-[10px] font-mono text-zinc-600">
          Auto-refreshes every 5s
        </span>
      </div>

      <div className="flex gap-5 items-start">
        {/* Session sidebar */}
        <SessionSidebar
          sessions={sessions ?? []}
          selected={selectedSession}
          onSelect={id => { setSelectedSession(id === selectedSession ? '' : id); setPage(0) }}
        />

        {/* Main content */}
        <div className="flex-1 min-w-0 space-y-4">

          {/* KPI row */}
          <div className="grid grid-cols-4 gap-3">
            <MetricCard label="Events (page)" value={String(kpis.total)} accent="zinc" />
            <MetricCard label="Signals" value={String(kpis.signals)} accent="sky" />
            <MetricCard label="Fills" value={String(kpis.fills)} accent="emerald" />
            <MetricCard label="Errors" value={String(kpis.errors)}
              accent={kpis.errors > 0 ? 'red' : 'zinc'}
              trend={kpis.errors > 0 ? 'down' : undefined}
            />
          </div>

          {/* Kind breakdown chart */}
          {events && events.length > 0 && (
            <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
              <SectionHeader title="Event Kind Distribution" sub="current page" />
              <KindBreakdown events={events} />
            </div>
          )}

          {/* Event table */}
          <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
            <div className="px-4 py-3 border-b border-zinc-800 flex items-center justify-between">
              <SectionHeader title="Event Stream" sub="newest first · click to expand payload" />
              <div className="flex items-center gap-2">
                <Btn size="sm" variant="ghost" onClick={() => setPage(p => Math.max(0, p-1))} disabled={page === 0}>← Prev</Btn>
                <span className="text-xs font-mono text-zinc-500">p.{page+1}</span>
                <Btn size="sm" variant="ghost" onClick={() => setPage(p => p+1)} disabled={(events?.length ?? 0) < PAGE_SIZE}>Next →</Btn>
              </div>
            </div>

            {isLoading ? (
              <div className="flex justify-center py-12"><Spinner /></div>
            ) : error ? (
              <div className="p-4">
                <ErrorBanner message="Failed to load audit log — ensure v1.3 backend audit endpoints are deployed" />
              </div>
            ) : !events?.length ? (
              <EmptyState icon="📋" message="No events match your filters" />
            ) : (
              <table className="w-full text-xs font-mono">
                <thead>
                  <tr className="border-b border-zinc-800">
                    {['Time', 'Kind', 'Mode', 'Session', 'Preview'].map(h => (
                      <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal">
                        {h}
                      </th>
                    ))}
                  </tr>
                </thead>
                <tbody>
                  {events.map(e => (
                    <tr
                      key={e.entry_id}
                      onClick={() => setSelected(e)}
                      className={`border-b border-zinc-800/50 cursor-pointer transition-colors hover:bg-zinc-800/40 ${
                        e.kind === 'error' ? 'bg-red-950/10' : ''
                      }`}
                    >
                      <td className="px-3 py-2.5 text-zinc-500 whitespace-nowrap">
                        {fmt.dateTime(e.timestamp)}
                      </td>
                      <td className="px-3 py-2.5">
                        <KindBadge kind={e.kind} />
                      </td>
                      <td className="px-3 py-2.5">
                        <span className={`text-[10px] font-mono capitalize ${
                          e.mode === 'live' ? 'text-emerald-400' :
                          e.mode === 'paper' ? 'text-sky-400' : 'text-zinc-500'
                        }`}>{e.mode}</span>
                      </td>
                      <td className="px-3 py-2.5 text-zinc-600">
                        {e.session_id.slice(0, 10)}…
                      </td>
                      <td className="px-3 py-2.5 text-zinc-500 max-w-xs">
                        <span className="truncate block">
                          {payloadPreview(e)}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </div>
        </div>
      </div>

      <PayloadModal
        open={!!selected}
        onClose={() => setSelected(null)}
        entry={selected}
      />
    </PageShell>
  )
}

// ─── helpers ─────────────────────────────────────────────────────────────────

function payloadPreview(e: LogEntry): string {
  if (!e.payload) return '—'
  const p = e.payload as Record<string, unknown>
  switch (e.kind) {
    case 'signal':
      return `${p.symbol ?? ''} ${p.action ?? ''} conf:${p.confidence ?? '?'}`
    case 'fill':
      return `${p.symbol ?? ''} ${p.side ?? ''} @ ${p.filled_price ?? '?'} qty:${p.filled_qty ?? '?'}`
    case 'order':
      return `${p.symbol ?? ''} ${p.side ?? ''} ${p.status ?? ''}`
    case 'risk':
      return `${(p as any).approved ? '✓ approved' : '✗ rejected'}: ${
        Array.isArray((p as any).reasons) ? (p as any).reasons[0] ?? '' : ''
      }`
    case 'metrics':
      return `equity:${typeof p.equity === 'number' ? p.equity.toFixed(2) : '?'} ` +
             `wr:${typeof p.win_rate === 'number' ? (p.win_rate * 100).toFixed(0) + '%' : '?'}`
    case 'error':
      return String(p) || 'error'
    case 'market':
      return `${p.symbol ?? ''} close:${p.close ?? '?'}`
    default:
      return JSON.stringify(p).slice(0, 60)
  }
}
