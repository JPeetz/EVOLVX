// web/src/components/evolvx/OutcomeHeatmap.tsx
//
// GitHub-style contribution calendar where each cell = one day,
// coloured by aggregated outcome for that day.
// Green = net wins, Red = net losses, Amber = mixed/breakeven, Grey = no activity.

import React, { useMemo, useState } from 'react'
import type { DecisionEntry, OutcomeClass } from '../../lib/evolvx-api'

interface Props {
  decisions: DecisionEntry[]
  weeks?: number // how many weeks to show (default 26 = 6 months)
}

interface DayData {
  date: string
  wins: number
  losses: number
  pending: number
  total: number
  pnl: number
  netClass: OutcomeClass | 'none'
}

function isoDate(d: Date) {
  return d.toISOString().slice(0, 10)
}

function buildCalendar(decisions: DecisionEntry[], weeks: number): DayData[][] {
  // Aggregate decisions by day
  const byDay = new Map<string, DayData>()

  decisions.forEach(d => {
    const day = d.timestamp.slice(0, 10)
    const existing = byDay.get(day) ?? {
      date: day, wins: 0, losses: 0, pending: 0, total: 0, pnl: 0, netClass: 'none' as const,
    }
    existing.total++
    if (d.outcome) {
      if (d.outcome.class === 'win') { existing.wins++; existing.pnl += d.outcome.realized_pnl }
      else if (d.outcome.class === 'loss') { existing.losses++; existing.pnl += d.outcome.realized_pnl }
      else { existing.pending++ }
    } else {
      existing.pending++
    }
    byDay.set(day, existing)
  })

  // Determine net class per day
  byDay.forEach(day => {
    if (day.total === 0) { day.netClass = 'none'; return }
    if (day.pending === day.total) { day.netClass = 'pending'; return }
    if (day.wins > day.losses) day.netClass = 'win'
    else if (day.losses > day.wins) day.netClass = 'loss'
    else day.netClass = 'breakeven'
  })

  // Build week grid (Sun→Sat)
  const today = new Date()
  // Roll back to the start of the oldest week
  const startDate = new Date(today)
  startDate.setDate(today.getDate() - (weeks * 7) + 1)
  // Align to Sunday
  startDate.setDate(startDate.getDate() - startDate.getDay())

  const grid: DayData[][] = []
  let current = new Date(startDate)

  for (let w = 0; w < weeks; w++) {
    const week: DayData[] = []
    for (let d = 0; d < 7; d++) {
      const iso = isoDate(current)
      week.push(byDay.get(iso) ?? {
        date: iso, wins: 0, losses: 0, pending: 0, total: 0, pnl: 0, netClass: 'none',
      })
      current.setDate(current.getDate() + 1)
    }
    grid.push(week)
  }

  return grid
}

const CELL_COLOR: Record<DayData['netClass'], string> = {
  none:      'bg-zinc-800 hover:bg-zinc-700',
  pending:   'bg-zinc-700 hover:bg-zinc-600',
  win:       'bg-emerald-800 hover:bg-emerald-700',
  loss:      'bg-red-900 hover:bg-red-800',
  breakeven: 'bg-amber-900 hover:bg-amber-800',
  forced_exit: 'bg-orange-900 hover:bg-orange-800',
}

const CELL_INTENSITY: Record<DayData['netClass'], (ratio: number) => string> = {
  none:       () => 'bg-zinc-800',
  pending:    () => 'bg-zinc-700',
  win:        (r) => r > 0.7 ? 'bg-emerald-500' : r > 0.4 ? 'bg-emerald-700' : 'bg-emerald-900',
  loss:       (r) => r > 0.7 ? 'bg-red-500' : r > 0.4 ? 'bg-red-700' : 'bg-red-900',
  breakeven:  () => 'bg-amber-800',
  forced_exit: () => 'bg-orange-900',
}

const MONTHS = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec']
const DAYS = ['S','M','T','W','T','F','S']

export default function OutcomeHeatmap({ decisions, weeks = 26 }: Props) {
  const [tooltip, setTooltip] = useState<DayData | null>(null)
  const grid = useMemo(() => buildCalendar(decisions, weeks), [decisions, weeks])

  // Month labels: find first col of each month
  const monthLabels: Array<{ col: number; month: string }> = []
  grid.forEach((week, w) => {
    const firstDay = week[0]
    const d = new Date(firstDay.date)
    if (d.getDate() <= 7) {
      monthLabels.push({ col: w, month: MONTHS[d.getMonth()] })
    }
  })

  return (
    <div className="relative">
      {/* Month labels */}
      <div className="flex gap-1 ml-7 mb-1 h-4">
        {grid.map((_, w) => {
          const label = monthLabels.find(m => m.col === w)
          return (
            <div key={w} className="w-3 flex-shrink-0">
              {label && (
                <span className="text-[9px] font-mono text-zinc-500 whitespace-nowrap">{label.month}</span>
              )}
            </div>
          )
        })}
      </div>

      <div className="flex gap-1">
        {/* Day labels */}
        <div className="flex flex-col gap-1 mr-1">
          {DAYS.map((d, i) => (
            <div key={i} className="w-3 h-3 flex items-center justify-center">
              {i % 2 === 1 && <span className="text-[9px] font-mono text-zinc-600">{d}</span>}
            </div>
          ))}
        </div>

        {/* Grid */}
        {grid.map((week, w) => (
          <div key={w} className="flex flex-col gap-1">
            {week.map((day, d) => {
              const maxInWeek = Math.max(...week.map(x => x.total), 1)
              const intensity = day.total / maxInWeek
              const colorClass = day.total === 0
                ? CELL_COLOR[day.netClass]
                : CELL_INTENSITY[day.netClass]?.(intensity) ?? CELL_COLOR[day.netClass]

              return (
                <div
                  key={d}
                  className={`w-3 h-3 rounded-sm cursor-pointer transition-colors ${colorClass}`}
                  onMouseEnter={() => setTooltip(day)}
                  onMouseLeave={() => setTooltip(null)}
                />
              )
            })}
          </div>
        ))}
      </div>

      {/* Tooltip */}
      {tooltip && tooltip.total > 0 && (
        <div className="absolute bottom-full left-1/2 -translate-x-1/2 mb-2 z-10 pointer-events-none">
          <div className="bg-zinc-800 border border-zinc-700 rounded-lg px-3 py-2 shadow-xl text-xs font-mono whitespace-nowrap">
            <div className="text-zinc-300 font-semibold mb-1">{tooltip.date}</div>
            <div className="flex gap-3">
              <span className="text-emerald-400">▲ {tooltip.wins} wins</span>
              <span className="text-red-400">▼ {tooltip.losses} losses</span>
              {tooltip.pending > 0 && <span className="text-zinc-500">◌ {tooltip.pending} pending</span>}
            </div>
            <div className={`mt-1 ${tooltip.pnl >= 0 ? 'text-emerald-400' : 'text-red-400'}`}>
              PnL {tooltip.pnl >= 0 ? '+' : ''}{tooltip.pnl.toFixed(2)} USDT
            </div>
          </div>
        </div>
      )}

      {/* Legend */}
      <div className="flex items-center gap-3 mt-3 pt-3 border-t border-zinc-800">
        <span className="text-[10px] font-mono text-zinc-600">Less</span>
        {['bg-zinc-800', 'bg-emerald-900', 'bg-emerald-700', 'bg-emerald-500'].map((c, i) => (
          <div key={i} className={`w-3 h-3 rounded-sm ${c}`} />
        ))}
        <span className="text-[10px] font-mono text-zinc-600">More wins</span>
        <div className="w-px h-3 bg-zinc-700 mx-1" />
        {['bg-red-900', 'bg-red-700', 'bg-red-500'].map((c, i) => (
          <div key={i} className={`w-3 h-3 rounded-sm ${c}`} />
        ))}
        <span className="text-[10px] font-mono text-zinc-600">More losses</span>
        <div className="w-px h-3 bg-zinc-700 mx-1" />
        <div className="w-3 h-3 rounded-sm bg-amber-800" />
        <span className="text-[10px] font-mono text-zinc-600">Mixed</span>
      </div>
    </div>
  )
}
