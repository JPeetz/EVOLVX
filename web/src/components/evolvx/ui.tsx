// web/src/components/evolvx/ui.tsx
// Shared primitive components used across all four EvolvX panels.

import React from 'react'
import type { StrategyStatus, OutcomeClass, SignalAction } from '../../lib/evolvx-api'

// ─── Status Badge ─────────────────────────────────────────────────────────────

const STATUS_STYLES: Record<StrategyStatus, string> = {
  draft:      'bg-zinc-800 text-zinc-300 border-zinc-600',
  paper:      'bg-sky-950 text-sky-300 border-sky-700',
  approved:   'bg-emerald-950 text-emerald-300 border-emerald-700',
  deprecated: 'bg-amber-950 text-amber-400 border-amber-700',
  disabled:   'bg-zinc-900 text-zinc-500 border-zinc-700',
}

const STATUS_DOT: Record<StrategyStatus, string> = {
  draft:      'bg-zinc-400',
  paper:      'bg-sky-400 animate-pulse',
  approved:   'bg-emerald-400 animate-pulse',
  deprecated: 'bg-amber-400',
  disabled:   'bg-zinc-600',
}

export function StatusBadge({ status }: { status: StrategyStatus }) {
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded border text-xs font-mono font-medium tracking-wider uppercase ${STATUS_STYLES[status]}`}>
      <span className={`w-1.5 h-1.5 rounded-full ${STATUS_DOT[status]}`} />
      {status}
    </span>
  )
}

// ─── Outcome Badge ────────────────────────────────────────────────────────────

const OUTCOME_STYLES: Record<OutcomeClass, string> = {
  win:         'text-emerald-400 bg-emerald-950 border-emerald-800',
  loss:        'text-red-400 bg-red-950 border-red-800',
  breakeven:   'text-amber-400 bg-amber-950 border-amber-800',
  forced_exit: 'text-orange-400 bg-orange-950 border-orange-800',
  pending:     'text-zinc-400 bg-zinc-800 border-zinc-700',
}

export function OutcomeBadge({ outcome }: { outcome: OutcomeClass }) {
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono uppercase tracking-wider ${OUTCOME_STYLES[outcome]}`}>
      {outcome === 'win' ? '▲' : outcome === 'loss' ? '▼' : '—'} {outcome.replace('_', ' ')}
    </span>
  )
}

// ─── Signal Action Badge ──────────────────────────────────────────────────────

const ACTION_STYLES: Record<SignalAction, string> = {
  open_long:   'text-emerald-300 bg-emerald-950 border-emerald-800',
  open_short:  'text-red-300 bg-red-950 border-red-800',
  close_long:  'text-sky-300 bg-sky-950 border-sky-800',
  close_short: 'text-sky-300 bg-sky-950 border-sky-800',
  hold:        'text-zinc-400 bg-zinc-800 border-zinc-700',
  wait:        'text-zinc-500 bg-zinc-900 border-zinc-700',
}

export function ActionBadge({ action }: { action: SignalAction }) {
  const label = action.replace('_', ' ').toUpperCase()
  return (
    <span className={`inline-flex items-center px-2 py-0.5 rounded border text-xs font-mono font-bold tracking-wider ${ACTION_STYLES[action]}`}>
      {label}
    </span>
  )
}

// ─── Metric Card ──────────────────────────────────────────────────────────────

interface MetricCardProps {
  label: string
  value: string
  sub?: string
  accent?: 'amber' | 'emerald' | 'sky' | 'red' | 'zinc'
  trend?: 'up' | 'down' | 'flat'
}

const ACCENT_VALUE: Record<string, string> = {
  amber:   'text-amber-400',
  emerald: 'text-emerald-400',
  sky:     'text-sky-400',
  red:     'text-red-400',
  zinc:    'text-zinc-200',
}

export function MetricCard({ label, value, sub, accent = 'zinc', trend }: MetricCardProps) {
  return (
    <div className="bg-zinc-900 border border-zinc-800 rounded-lg p-4 flex flex-col gap-1">
      <span className="text-xs text-zinc-500 font-mono uppercase tracking-widest">{label}</span>
      <div className="flex items-end gap-2">
        <span className={`text-2xl font-mono font-bold tabular-nums ${ACCENT_VALUE[accent]}`}>
          {value}
        </span>
        {trend && (
          <span className={`text-sm mb-0.5 ${trend === 'up' ? 'text-emerald-400' : trend === 'down' ? 'text-red-400' : 'text-zinc-500'}`}>
            {trend === 'up' ? '↑' : trend === 'down' ? '↓' : '→'}
          </span>
        )}
      </div>
      {sub && <span className="text-xs text-zinc-500 font-mono">{sub}</span>}
    </div>
  )
}

// ─── Section Header ───────────────────────────────────────────────────────────

export function SectionHeader({ title, sub, children }: { title: string; sub?: string; children?: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between mb-4">
      <div>
        <h2 className="text-sm font-mono font-semibold text-zinc-100 uppercase tracking-widest">{title}</h2>
        {sub && <p className="text-xs text-zinc-500 mt-0.5">{sub}</p>}
      </div>
      {children && <div className="flex items-center gap-2">{children}</div>}
    </div>
  )
}

// ─── Page Shell ───────────────────────────────────────────────────────────────

export function PageShell({ title, icon, tag, children }: {
  title: string
  icon: string
  tag: string
  children: React.ReactNode
}) {
  return (
    <div className="min-h-screen bg-zinc-950 text-zinc-100">
      <div className="border-b border-zinc-800 px-6 py-4">
        <div className="flex items-center gap-3">
          <span className="text-xl">{icon}</span>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-sm font-mono font-bold text-zinc-100 uppercase tracking-widest">{title}</h1>
              <span className="text-[10px] font-mono px-1.5 py-0.5 rounded bg-amber-400 text-zinc-950 font-bold">
                {tag}
              </span>
            </div>
            <p className="text-xs text-zinc-500 font-mono mt-0.5">EvolvX v1.1</p>
          </div>
        </div>
      </div>
      <div className="p-6">{children}</div>
    </div>
  )
}

// ─── Spinner ─────────────────────────────────────────────────────────────────

export function Spinner({ size = 'md' }: { size?: 'sm' | 'md' | 'lg' }) {
  const s = size === 'sm' ? 'w-4 h-4' : size === 'lg' ? 'w-8 h-8' : 'w-6 h-6'
  return (
    <div className={`${s} border-2 border-zinc-700 border-t-amber-400 rounded-full animate-spin`} />
  )
}

// ─── Empty State ──────────────────────────────────────────────────────────────

export function EmptyState({ icon, message, sub }: { icon: string; message: string; sub?: string }) {
  return (
    <div className="flex flex-col items-center justify-center py-16 gap-3">
      <span className="text-4xl opacity-30">{icon}</span>
      <p className="text-zinc-400 font-mono text-sm">{message}</p>
      {sub && <p className="text-zinc-600 font-mono text-xs">{sub}</p>}
    </div>
  )
}

// ─── Error Banner ─────────────────────────────────────────────────────────────

export function ErrorBanner({ message }: { message: string }) {
  return (
    <div className="flex items-center gap-3 bg-red-950 border border-red-800 rounded-lg px-4 py-3">
      <span className="text-red-400 text-sm">⚠</span>
      <p className="text-red-300 font-mono text-xs">{message}</p>
    </div>
  )
}

// ─── Button ───────────────────────────────────────────────────────────────────

type BtnVariant = 'primary' | 'secondary' | 'danger' | 'ghost'

const BTN_STYLES: Record<BtnVariant, string> = {
  primary:   'bg-amber-400 text-zinc-950 hover:bg-amber-300 font-bold',
  secondary: 'bg-zinc-800 text-zinc-200 hover:bg-zinc-700 border border-zinc-700',
  danger:    'bg-red-950 text-red-300 hover:bg-red-900 border border-red-800',
  ghost:     'text-zinc-400 hover:text-zinc-200 hover:bg-zinc-800',
}

export function Btn({
  children, variant = 'secondary', onClick, disabled, size = 'md', className = ''
}: {
  children: React.ReactNode
  variant?: BtnVariant
  onClick?: () => void
  disabled?: boolean
  size?: 'sm' | 'md' | 'lg'
  className?: string
}) {
  const sz = size === 'sm' ? 'px-3 py-1 text-xs' : size === 'lg' ? 'px-6 py-3 text-sm' : 'px-4 py-2 text-xs'
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`
        inline-flex items-center gap-2 rounded font-mono tracking-wide
        transition-colors duration-150 cursor-pointer
        disabled:opacity-40 disabled:cursor-not-allowed
        ${sz} ${BTN_STYLES[variant]} ${className}
      `}
    >
      {children}
    </button>
  )
}

// ─── Modal ────────────────────────────────────────────────────────────────────

export function Modal({ open, onClose, title, children, width = 'max-w-3xl' }: {
  open: boolean
  onClose: () => void
  title: string
  children: React.ReactNode
  width?: string
}) {
  if (!open) return null
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center">
      <div
        className="absolute inset-0 bg-zinc-950/80 backdrop-blur-sm"
        onClick={onClose}
      />
      <div className={`relative w-full ${width} mx-4 bg-zinc-900 border border-zinc-700 rounded-xl shadow-2xl max-h-[90vh] flex flex-col`}>
        <div className="flex items-center justify-between px-6 py-4 border-b border-zinc-800 shrink-0">
          <h3 className="font-mono font-semibold text-sm text-zinc-100 uppercase tracking-widest">{title}</h3>
          <button onClick={onClose} className="text-zinc-500 hover:text-zinc-200 font-mono text-lg leading-none">×</button>
        </div>
        <div className="overflow-y-auto flex-1 p-6">{children}</div>
      </div>
    </div>
  )
}

// ─── Numeric delta ────────────────────────────────────────────────────────────

export function Delta({ value, format = 'pct' }: { value: number; format?: 'pct' | 'abs' }) {
  const positive = value >= 0
  const str = format === 'pct'
    ? `${positive ? '+' : ''}${(value * 100).toFixed(2)}%`
    : `${positive ? '+' : ''}${value.toFixed(2)}`
  return (
    <span className={`font-mono tabular-nums ${positive ? 'text-emerald-400' : 'text-red-400'}`}>
      {str}
    </span>
  )
}

// ─── Score bar ────────────────────────────────────────────────────────────────

export function ScoreBar({ score, max = 5 }: { score: number; max?: number }) {
  const pct = Math.max(0, Math.min(100, (score / max) * 100))
  const color = pct > 60 ? 'bg-emerald-500' : pct > 30 ? 'bg-amber-400' : 'bg-red-500'
  return (
    <div className="flex items-center gap-2">
      <div className="flex-1 h-1.5 bg-zinc-800 rounded-full overflow-hidden">
        <div className={`h-full rounded-full transition-all ${color}`} style={{ width: `${pct}%` }} />
      </div>
      <span className="text-xs font-mono text-zinc-400 w-8 text-right">{score.toFixed(2)}</span>
    </div>
  )
}
