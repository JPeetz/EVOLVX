// web/src/pages/Optimizer.tsx
//
// Strategy Optimizer dashboard — v1.1 EvolvX UI.
//
// Layout:
//   Left  (320px) : Job list (sorted newest first) with status indicators
//   Right (flex)  : Job detail — candidate table + score chart + promote action

import React, { useState, useMemo } from 'react'
import useSWR, { mutate } from 'swr'
import {
  BarChart, Bar, XAxis, YAxis, CartesianGrid,
  Tooltip as RTooltip, ResponsiveContainer, Cell, ReferenceLine
} from 'recharts'

import {
  optimizerApi, registryApi, fmt,
  type OptimizationJob, type Candidate,
  type PromotionThresholds,
} from '../lib/evolvx-api'

import {
  PageShell, SectionHeader, MetricCard,
  Btn, Modal, Spinner, EmptyState, ErrorBanner, ScoreBar, Delta
} from '../components/evolvx/ui'

// ─────────────────────────────────────────────────────────────────────────────
// Job status indicator
// ─────────────────────────────────────────────────────────────────────────────

const JOB_STATUS_STYLES = {
  pending: 'text-zinc-400 bg-zinc-800 border-zinc-700',
  running: 'text-sky-300 bg-sky-950 border-sky-800 animate-pulse',
  done:    'text-emerald-300 bg-emerald-950 border-emerald-800',
  failed:  'text-red-300 bg-red-950 border-red-800',
}

function JobStatusBadge({ status }: { status: OptimizationJob['status'] }) {
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded border text-[10px] font-mono uppercase tracking-wider ${JOB_STATUS_STYLES[status]}`}>
      {status === 'running' && <span className="w-1.5 h-1.5 rounded-full bg-sky-400 animate-pulse" />}
      {status === 'done' && '✓ '}
      {status}
    </span>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// New job form
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

function toDate(daysAgo: number): string {
  const d = new Date()
  d.setDate(d.getDate() - daysAgo)
  return d.toISOString().slice(0, 10)
}

interface NewJobFormProps {
  open: boolean
  onClose: () => void
  onSubmit: (job: OptimizationJob) => void
}

function NewJobForm({ open, onClose, onSubmit }: NewJobFormProps) {
  const traders = useTraders()
  const [strategyId, setStrategyId] = useState('')
  const [maxCandidates, setMaxCandidates] = useState(20)
  const [trainDays, setTrainDays] = useState(120)
  const [valDays, setValDays] = useState(60)
  const [submitting, setSubmitting] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Default thresholds
  const thresholds: PromotionThresholds = {
    min_val_return: 0.03,
    max_val_drawdown: 0.15,
    min_val_sharpe: 0.5,
    min_val_win_rate: 0.45,
    min_val_profit_factor: 1.1,
    min_val_trades: 10,
    min_val_to_train_return_ratio: 0.4,
  }

  async function handleSubmit() {
    if (!strategyId) { setError('Select a strategy'); return }
    setSubmitting(true); setError(null)
    try {
      const now = new Date()
      const valStart = new Date(now); valStart.setDate(now.getDate() - valDays)
      const trainStart = new Date(valStart); trainStart.setDate(valStart.getDate() - trainDays)

      const job = await optimizerApi.submitJob({
        strategy_id: strategyId,
        strategy_version: 'latest',
        created_by: 'j.peetz69@gmail.com',
        train_from: trainStart.toISOString(),
        train_to:   valStart.toISOString(),
        val_from:   valStart.toISOString(),
        val_to:     now.toISOString(),
        thresholds,
        max_candidates: maxCandidates,
      })
      onSubmit(job)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to submit job')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Modal open={open} onClose={onClose} title="New Optimization Job" width="max-w-lg">
      <div className="space-y-4">
        {error && <ErrorBanner message={error} />}

        <div>
          <label className="text-xs font-mono text-zinc-400 block mb-1.5">Strategy</label>
          <select
            value={strategyId}
            onChange={e => setStrategyId(e.target.value)}
            className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
          >
            <option value="">Select a strategy…</option>
            {traders.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
          </select>
        </div>

        <div className="grid grid-cols-2 gap-4">
          <div>
            <label className="text-xs font-mono text-zinc-400 block mb-1.5">
              Train window (days)
            </label>
            <input
              type="number" min={30} max={365} value={trainDays}
              onChange={e => setTrainDays(Number(e.target.value))}
              className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
            />
            <p className="text-[10px] font-mono text-zinc-600 mt-0.5">{toDate(trainDays + valDays)} → {toDate(valDays)}</p>
          </div>
          <div>
            <label className="text-xs font-mono text-zinc-400 block mb-1.5">
              Validation window (days)
            </label>
            <input
              type="number" min={14} max={180} value={valDays}
              onChange={e => setValDays(Number(e.target.value))}
              className="w-full bg-zinc-800 border border-zinc-700 rounded px-3 py-2 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500"
            />
            <p className="text-[10px] font-mono text-zinc-600 mt-0.5">{toDate(valDays)} → today</p>
          </div>
        </div>

        <div>
          <label className="text-xs font-mono text-zinc-400 block mb-1.5">
            Max candidates ({maxCandidates})
          </label>
          <input
            type="range" min={5} max={40} value={maxCandidates}
            onChange={e => setMaxCandidates(Number(e.target.value))}
            className="w-full accent-amber-400"
          />
          <div className="flex justify-between text-[10px] font-mono text-zinc-600 mt-0.5">
            <span>5 (fast)</span><span>40 (thorough)</span>
          </div>
        </div>

        {/* Threshold summary */}
        <div className="bg-zinc-800/50 rounded-lg p-3 border border-zinc-700">
          <div className="text-[10px] font-mono text-zinc-500 uppercase tracking-widest mb-2">Promotion thresholds (defaults)</div>
          <div className="grid grid-cols-2 gap-x-4 gap-y-1">
            {[
              ['Min return', `${(thresholds.min_val_return * 100).toFixed(0)}%`],
              ['Max drawdown', `${(thresholds.max_val_drawdown * 100).toFixed(0)}%`],
              ['Min Sharpe', thresholds.min_val_sharpe.toFixed(1)],
              ['Min win rate', `${(thresholds.min_val_win_rate * 100).toFixed(0)}%`],
              ['Min trades', String(thresholds.min_val_trades)],
              ['Val/train ratio', thresholds.min_val_to_train_return_ratio.toFixed(1)],
            ].map(([k, v]) => (
              <div key={k} className="flex justify-between text-[10px] font-mono">
                <span className="text-zinc-500">{k}</span>
                <span className="text-zinc-300">{v}</span>
              </div>
            ))}
          </div>
        </div>

        <div className="flex gap-3 pt-1">
          <Btn variant="primary" onClick={handleSubmit} disabled={submitting}>
            {submitting ? <><Spinner size="sm" /> Submitting…</> : 'Submit Job'}
          </Btn>
          <Btn variant="ghost" onClick={onClose}>Cancel</Btn>
        </div>
      </div>
    </Modal>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Candidate row
// ─────────────────────────────────────────────────────────────────────────────

function CandidateRow({ candidate, rank }: { candidate: Candidate; rank: number }) {
  const r = candidate.eval_result
  const [expanded, setExpanded] = useState(false)

  return (
    <>
      <tr
        onClick={() => setExpanded(v => !v)}
        className={`border-b border-zinc-800/50 cursor-pointer transition-colors text-xs font-mono ${
          candidate.promoted
            ? 'bg-emerald-950/20 hover:bg-emerald-950/30'
            : r?.passed_promotion
            ? 'bg-amber-950/20 hover:bg-amber-950/30'
            : 'hover:bg-zinc-800/30'
        }`}
      >
        <td className="px-3 py-2.5 text-zinc-500 tabular-nums">{rank}</td>
        <td className="px-3 py-2.5 text-zinc-300 max-w-[200px]">
          <span className="truncate block" title={candidate.mutation_desc}>{candidate.mutation_desc}</span>
        </td>
        <td className="px-3 py-2.5 tabular-nums">
          {r ? <Delta value={r.val_return} format="pct" /> : '—'}
        </td>
        <td className="px-3 py-2.5 tabular-nums text-red-400">
          {r ? `${(Math.abs(r.val_max_drawdown) * 100).toFixed(1)}%` : '—'}
        </td>
        <td className="px-3 py-2.5 tabular-nums text-sky-400">
          {r ? r.val_sharpe.toFixed(2) : '—'}
        </td>
        <td className="px-3 py-2.5 tabular-nums text-amber-400">
          {r ? `${(r.val_win_rate * 100).toFixed(0)}%` : '—'}
        </td>
        <td className="px-3 py-2.5 tabular-nums text-zinc-400">
          {r ? r.val_trades : '—'}
        </td>
        <td className="px-3 py-2.5 w-32">
          {r ? <ScoreBar score={r.score} /> : '—'}
        </td>
        <td className="px-3 py-2.5">
          {candidate.promoted ? (
            <span className="text-[10px] text-emerald-400 font-mono">✓ promoted</span>
          ) : r?.passed_promotion ? (
            <span className="text-[10px] text-amber-400 font-mono">● passes</span>
          ) : r ? (
            <span className="text-[10px] text-red-500 font-mono">✗ fails</span>
          ) : (
            <span className="text-[10px] text-zinc-600 font-mono">—</span>
          )}
        </td>
      </tr>
      {expanded && r && (
        <tr className="bg-zinc-900/80 border-b border-zinc-800">
          <td colSpan={9} className="px-6 py-3">
            <div className="grid grid-cols-2 gap-6">
              {/* Train vs Val comparison */}
              <div>
                <div className="text-[10px] font-mono text-zinc-500 uppercase mb-2">Train vs Validation</div>
                <div className="grid grid-cols-2 gap-2 text-[10px] font-mono">
                  {[
                    ['Return',    `${(r.train_return * 100).toFixed(2)}%`, `${(r.val_return * 100).toFixed(2)}%`],
                    ['Drawdown',  `${(Math.abs(r.train_max_drawdown) * 100).toFixed(1)}%`, `${(Math.abs(r.val_max_drawdown) * 100).toFixed(1)}%`],
                    ['Sharpe',    r.train_sharpe.toFixed(2), r.val_sharpe.toFixed(2)],
                    ['Win Rate',  `${(r.train_win_rate * 100).toFixed(0)}%`, `${(r.val_win_rate * 100).toFixed(0)}%`],
                    ['Trades',    String(r.train_trades), String(r.val_trades)],
                  ].map(([label, train, val]) => (
                    <div key={label} className="grid grid-cols-3 items-center gap-2 py-0.5">
                      <span className="text-zinc-500">{label}</span>
                      <span className="text-zinc-400 text-right">{train}</span>
                      <span className="text-amber-300 text-right font-semibold">{val}</span>
                    </div>
                  ))}
                </div>
              </div>
              {/* Fail reasons */}
              {r.fail_reasons && r.fail_reasons.length > 0 && (
                <div>
                  <div className="text-[10px] font-mono text-red-500 uppercase mb-2">Why it failed</div>
                  <ul className="space-y-1">
                    {r.fail_reasons.map((reason, i) => (
                      <li key={i} className="text-[10px] font-mono text-red-400 flex gap-1.5">
                        <span>✗</span><span>{reason}</span>
                      </li>
                    ))}
                  </ul>
                </div>
              )}
              {candidate.promoted && candidate.registry_version && (
                <div className="text-[10px] font-mono text-emerald-400">
                  ✓ Promoted to registry v{candidate.registry_version} (StatusPaper — awaiting your approval)
                </div>
              )}
            </div>
          </td>
        </tr>
      )}
    </>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Job detail panel
// ─────────────────────────────────────────────────────────────────────────────

function JobDetail({ job, onRun }: { job: OptimizationJob; onRun: () => void }) {
  const candidates = job.candidates ?? []
  const sorted = [...candidates].sort((a, b) =>
    (b.eval_result?.score ?? -99) - (a.eval_result?.score ?? -99)
  )

  const scoreData = sorted
    .filter(c => c.eval_result)
    .map((c, i) => ({
      rank: i + 1,
      score: +(c.eval_result!.score).toFixed(3),
      passed: c.eval_result!.passed_promotion,
      label: c.mutation_desc.slice(0, 20),
    }))

  const passCount = candidates.filter(c => c.eval_result?.passed_promotion).length
  const promotedCount = candidates.filter(c => c.promoted).length

  return (
    <div className="space-y-5">
      {/* Job header */}
      <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
        <div className="flex items-start justify-between">
          <div>
            <div className="flex items-center gap-3 mb-1">
              <span className="text-xs font-mono text-zinc-500">Job</span>
              <span className="text-sm font-mono text-zinc-200 truncate max-w-48">{job.job_id.slice(0, 16)}…</span>
              <JobStatusBadge status={job.status} />
            </div>
            <div className="text-[10px] font-mono text-zinc-500">
              Created by {job.created_by} · {fmt.relTime(job.created_at)}
            </div>
            <div className="text-[10px] font-mono text-zinc-600 mt-0.5">
              Train: {fmt.date(job.train_from)} → {fmt.date(job.train_to)} ·
              Val: {fmt.date(job.val_from)} → {fmt.date(job.val_to)}
            </div>
          </div>
          {job.status === 'pending' && (
            <Btn variant="primary" size="sm" onClick={onRun}>▶ Run Job</Btn>
          )}
          {job.status === 'running' && (
            <div className="flex items-center gap-2">
              <Spinner size="sm" />
              <span className="text-xs font-mono text-sky-400">Running…</span>
            </div>
          )}
        </div>

        {job.status === 'done' && (
          <div className="mt-4 pt-3 border-t border-zinc-800 grid grid-cols-4 gap-3">
            <MetricCard label="Evaluated" value={String(candidates.length)} accent="zinc" />
            <MetricCard label="Passed threshold" value={String(passCount)} accent={passCount > 0 ? 'amber' : 'zinc'} />
            <MetricCard label="Promoted" value={String(promotedCount)} accent={promotedCount > 0 ? 'emerald' : 'zinc'} />
            <MetricCard label="Status" value={promotedCount > 0 ? '✓ Done' : '—'} accent={promotedCount > 0 ? 'emerald' : 'zinc'} />
          </div>
        )}
      </div>

      {/* Score distribution chart */}
      {scoreData.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
          <SectionHeader title="Candidate Score Distribution" sub="amber = passed threshold" />
          <div className="h-36">
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={scoreData} margin={{ top: 0, right: 8, left: -24, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#27272a" />
                <XAxis dataKey="rank" tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
                <YAxis tick={{ fontSize: 9, fill: '#52525b', fontFamily: 'monospace' }} />
                <RTooltip
                  contentStyle={{ backgroundColor: '#18181b', border: '1px solid #3f3f46', borderRadius: 6, fontSize: 10, fontFamily: 'monospace' }}
                  formatter={(v: number, _: string, props: { payload: { label: string } }) => [v.toFixed(3), props.payload.label]}
                />
                <ReferenceLine y={0} stroke="#3f3f46" />
                <Bar dataKey="score" radius={[2,2,0,0]}>
                  {scoreData.map((entry, i) => (
                    <Cell key={i} fill={entry.passed ? '#f59e0b' : '#3f3f46'} />
                  ))}
                </Bar>
              </BarChart>
            </ResponsiveContainer>
          </div>
        </div>
      )}

      {/* Candidate table */}
      {job.status === 'done' && candidates.length > 0 && (
        <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
          <div className="px-4 py-3 border-b border-zinc-800">
            <SectionHeader
              title={`${candidates.length} Candidates`}
              sub="sorted by score · click a row to expand · amber rows pass threshold"
            />
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-xs font-mono">
              <thead>
                <tr className="border-b border-zinc-800">
                  {['#', 'Mutation', 'Val Return', 'Drawdown', 'Sharpe', 'Win Rate', 'Trades', 'Score', 'Status'].map(h => (
                    <th key={h} className="px-3 py-2 text-left text-[10px] text-zinc-500 uppercase tracking-widest font-normal whitespace-nowrap">
                      {h}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sorted.map((c, i) => (
                  <CandidateRow key={c.candidate_id} candidate={c} rank={i + 1} />
                ))}
              </tbody>
            </table>
          </div>

          {promotedCount > 0 && (
            <div className="px-4 py-3 border-t border-zinc-800 bg-emerald-950/20">
              <p className="text-xs font-mono text-emerald-400">
                ✓ {promotedCount} candidate{promotedCount > 1 ? 's' : ''} promoted to registry with StatusPaper.
                Go to <strong>Strategy Registry</strong> to review and approve for live trading.
              </p>
            </div>
          )}
        </div>
      )}

      {job.status === 'pending' && (
        <EmptyState icon="⏳" message="Job not yet started" sub="Click 'Run Job' to begin evaluation" />
      )}
      {job.status === 'running' && (
        <EmptyState icon="⚙️" message="Evaluating candidates…" sub="This may take a few minutes. Refresh to check." />
      )}
      {job.status === 'failed' && (
        <ErrorBanner message="Job failed. Check server logs for details." />
      )}
    </div>
  )
}

// ─────────────────────────────────────────────────────────────────────────────
// Main Optimizer page
// ─────────────────────────────────────────────────────────────────────────────

export default function Optimizer() {
  const traders = useTraders()
  const [filterStrategyId, setFilterStrategyId] = useState('')
  const [selectedJobId, setSelectedJobId] = useState<string | null>(null)
  const [showNewJob, setShowNewJob] = useState(false)

  const { data: jobs, error, mutate: mutateJobs } = useSWR(
    filterStrategyId ? ['optimizer-jobs', filterStrategyId] : 'optimizer-jobs-all',
    () => filterStrategyId
      ? optimizerApi.listJobs(filterStrategyId)
      : Promise.resolve([])
  )

  // Auto-select first job
  React.useEffect(() => {
    if (jobs && jobs.length > 0 && !selectedJobId) {
      setSelectedJobId(jobs[0].job_id)
    }
  }, [jobs, selectedJobId])

  const { data: selectedJob, mutate: mutateJob } = useSWR(
    selectedJobId ? ['optimizer-job', selectedJobId] : null,
    ([, id]) => optimizerApi.getJob(id),
    { refreshInterval: (data) => data?.status === 'running' ? 3000 : 0 }
  )

  async function handleRun() {
    if (!selectedJobId) return
    try {
      await optimizerApi.runJob(selectedJobId)
      mutateJob()
    } catch (e) {
      console.error(e)
    }
  }

  async function handleJobCreated(job: OptimizationJob) {
    setShowNewJob(false)
    setSelectedJobId(job.job_id)
    await mutateJobs()
  }

  return (
    <PageShell title="Strategy Optimizer" icon="⚙️" tag="v1.1">
      {/* Header controls */}
      <div className="flex items-center gap-3 mb-6">
        <select
          value={filterStrategyId}
          onChange={e => { setFilterStrategyId(e.target.value); setSelectedJobId(null) }}
          className="bg-zinc-800 border border-zinc-700 rounded px-3 py-1.5 text-xs font-mono text-zinc-200 focus:outline-none focus:border-amber-500 min-w-56"
        >
          <option value="">Select a strategy to view jobs</option>
          {traders.map(t => <option key={t.id} value={t.id}>{t.name}</option>)}
        </select>

        <Btn variant="primary" size="sm" onClick={() => setShowNewJob(true)}>
          + New Optimization Job
        </Btn>
      </div>

      {!filterStrategyId ? (
        <EmptyState
          icon="⚙️"
          message="Select a strategy to view optimization jobs"
          sub="Or click 'New Optimization Job' to start one for any strategy"
        />
      ) : error ? (
        <ErrorBanner message="Failed to load jobs" />
      ) : !jobs ? (
        <div className="flex justify-center py-12"><Spinner /></div>
      ) : (
        <div className="flex gap-5 items-start">
          {/* ── Left: Job list ── */}
          <div className="w-72 shrink-0">
            <SectionHeader title="Jobs" sub={`${jobs.length} total`} />
            {jobs.length === 0 ? (
              <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4">
                <EmptyState icon="📋" message="No jobs yet" sub="Click 'New Optimization Job'" />
              </div>
            ) : (
              <div className="bg-zinc-900 border border-zinc-800 rounded-lg overflow-hidden">
                {[...jobs]
                  .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
                  .map(job => (
                    <button
                      key={job.job_id}
                      onClick={() => setSelectedJobId(job.job_id)}
                      className={`w-full text-left px-4 py-3 border-b border-zinc-800 last:border-0 transition-all hover:bg-zinc-800/60 ${
                        selectedJobId === job.job_id ? 'border-l-2 border-l-amber-400 bg-zinc-800/80' : 'border-l-2 border-l-transparent'
                      }`}
                    >
                      <div className="flex items-center justify-between mb-1">
                        <span className="text-[10px] font-mono text-zinc-500 truncate">{job.job_id.slice(0, 12)}…</span>
                        <JobStatusBadge status={job.status} />
                      </div>
                      <div className="text-[10px] font-mono text-zinc-600">{fmt.relTime(job.created_at)}</div>
                      {job.status === 'done' && (
                        <div className="text-[10px] font-mono text-emerald-500 mt-0.5">
                          ↑ {job.promoted_count} promoted
                        </div>
                      )}
                      <div className="text-[10px] font-mono text-zinc-700 mt-0.5 truncate">
                        {fmt.date(job.val_from)} – {fmt.date(job.val_to)}
                      </div>
                    </button>
                  ))}
              </div>
            )}
          </div>

          {/* ── Right: Job detail ── */}
          <div className="flex-1 min-w-0">
            {!selectedJobId || !selectedJob ? (
              <EmptyState icon="📊" message="Select a job to view results" />
            ) : (
              <JobDetail job={selectedJob} onRun={handleRun} />
            )}
          </div>
        </div>
      )}

      <NewJobForm
        open={showNewJob}
        onClose={() => setShowNewJob(false)}
        onSubmit={handleJobCreated}
      />
    </PageShell>
  )
}
