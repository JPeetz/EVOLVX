// web/src/components/evolvx/LineageGraph.tsx
//
// SVG strategy lineage tree. Renders the parent→child evolution graph
// with nodes coloured by status, connected by bezier curves.
// Pure React/SVG — no d3 dependency.

import React, { useMemo } from 'react'
import type { StrategyRecord, LineageNode } from '../../lib/evolvx-api'

interface Props {
  records: StrategyRecord[]
  lineage: LineageNode[]
  selectedVersion?: string
  onSelect: (version: string) => void
}

interface TreeNode {
  version: string
  status: string
  x: number
  y: number
  children: string[]
  parentVersion?: string
  mutation: string
  score?: number
}

const STATUS_FILL: Record<string, string> = {
  draft:      '#3f3f46',
  paper:      '#0c4a6e',
  approved:   '#064e3b',
  deprecated: '#451a03',
  disabled:   '#1c1c22',
}

const STATUS_STROKE: Record<string, string> = {
  draft:      '#71717a',
  paper:      '#38bdf8',
  approved:   '#34d399',
  deprecated: '#fbbf24',
  disabled:   '#3f3f46',
}

const STATUS_TEXT: Record<string, string> = {
  draft:      '#a1a1aa',
  paper:      '#7dd3fc',
  approved:   '#6ee7b7',
  deprecated: '#fcd34d',
  disabled:   '#52525b',
}

const NODE_W = 120
const NODE_H = 44
const H_GAP = 48
const V_GAP = 72

function buildTree(records: StrategyRecord[], lineage: LineageNode[]): TreeNode[] {
  // Build adjacency
  const nodeMap = new Map<string, TreeNode>()
  const childrenOf = new Map<string, string[]>()

  records.forEach(r => {
    nodeMap.set(r.version, {
      version: r.version,
      status: r.status,
      x: 0, y: 0,
      children: [],
      parentVersion: r.parent_version,
      mutation: lineage.find(l => l.version === r.version)?.mutation_summary ?? 'initial',
      score: lineage.find(l => l.version === r.version)?.eval_score,
    })
  })

  // Build children
  records.forEach(r => {
    if (r.parent_version) {
      const arr = childrenOf.get(r.parent_version) ?? []
      arr.push(r.version)
      childrenOf.set(r.parent_version, arr)
    }
  })

  // Find roots (no parent or parent not in records)
  const versions = new Set(records.map(r => r.version))
  const roots = records.filter(r => !r.parent_version || !versions.has(r.parent_version))

  // BFS layout
  const placed: TreeNode[] = []
  let colCounters = new Map<number, number>()

  function place(ver: string, depth: number) {
    const node = nodeMap.get(ver)
    if (!node) return
    const col = colCounters.get(depth) ?? 0
    colCounters.set(depth, col + 1)
    node.x = col * (NODE_W + H_GAP)
    node.y = depth * (NODE_H + V_GAP)
    placed.push(node)
    const children = childrenOf.get(ver) ?? []
    children.forEach(c => place(c, depth + 1))
  }

  roots.forEach(r => place(r.version, 0))
  return placed
}

function Curve({ from, to }: { from: TreeNode; to: TreeNode }) {
  const x1 = from.x + NODE_W / 2
  const y1 = from.y + NODE_H
  const x2 = to.x + NODE_W / 2
  const y2 = to.y
  const midY = (y1 + y2) / 2
  return (
    <path
      d={`M${x1},${y1} C${x1},${midY} ${x2},${midY} ${x2},${y2}`}
      fill="none"
      stroke="#3f3f46"
      strokeWidth="1.5"
      strokeDasharray="4 3"
    />
  )
}

export default function LineageGraph({ records, lineage, selectedVersion, onSelect }: Props) {
  const nodes = useMemo(() => buildTree(records, lineage), [records, lineage])

  const nodeMap = useMemo(() => {
    const m = new Map<string, TreeNode>()
    nodes.forEach(n => m.set(n.version, n))
    return m
  }, [nodes])

  const edges = useMemo(() => {
    const result: Array<{ from: TreeNode; to: TreeNode }> = []
    nodes.forEach(n => {
      if (n.parentVersion) {
        const parent = nodeMap.get(n.parentVersion)
        if (parent) result.push({ from: parent, to: n })
      }
    })
    return result
  }, [nodes, nodeMap])

  const maxX = Math.max(...nodes.map(n => n.x + NODE_W), 200)
  const maxY = Math.max(...nodes.map(n => n.y + NODE_H), 100)
  const svgW = maxX + 40
  const svgH = maxY + 40

  if (nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-32 text-zinc-600 font-mono text-xs">
        No lineage data
      </div>
    )
  }

  return (
    <div className="overflow-auto rounded-lg bg-zinc-900/50 border border-zinc-800 p-4">
      <svg width={svgW} height={svgH} className="overflow-visible">
        {/* Edges */}
        {edges.map((e, i) => (
          <Curve key={i} from={e.from} to={e.to} />
        ))}

        {/* Nodes */}
        {nodes.map(n => {
          const selected = n.version === selectedVersion
          const fill = STATUS_FILL[n.status] ?? '#3f3f46'
          const stroke = STATUS_STROKE[n.status] ?? '#71717a'
          const textColor = STATUS_TEXT[n.status] ?? '#a1a1aa'

          return (
            <g
              key={n.version}
              transform={`translate(${n.x + 20}, ${n.y + 20})`}
              onClick={() => onSelect(n.version)}
              className="cursor-pointer"
            >
              {/* Glow for selected */}
              {selected && (
                <rect
                  x={-3} y={-3}
                  width={NODE_W + 6} height={NODE_H + 6}
                  rx={8}
                  fill="none"
                  stroke={stroke}
                  strokeWidth="2"
                  opacity="0.4"
                />
              )}

              {/* Node body */}
              <rect
                width={NODE_W} height={NODE_H}
                rx={6}
                fill={fill}
                stroke={selected ? stroke : '#27272a'}
                strokeWidth={selected ? 1.5 : 1}
              />

              {/* Version text */}
              <text
                x={NODE_W / 2} y={16}
                textAnchor="middle"
                fill={textColor}
                fontSize="11"
                fontFamily="'JetBrains Mono', 'Fira Mono', monospace"
                fontWeight="600"
              >
                v{n.version}
              </text>

              {/* Status dot + label */}
              <circle cx={10} cy={30} r={3} fill={stroke} opacity="0.8" />
              <text
                x={20} y={33}
                fill="#52525b"
                fontSize="9"
                fontFamily="'JetBrains Mono', 'Fira Mono', monospace"
              >
                {n.status}
              </text>

              {/* Score if available */}
              {n.score !== undefined && n.score > 0 && (
                <text
                  x={NODE_W - 8} y={33}
                  textAnchor="end"
                  fill="#fbbf24"
                  fontSize="9"
                  fontFamily="'JetBrains Mono', 'Fira Mono', monospace"
                >
                  {n.score.toFixed(2)}
                </text>
              )}
            </g>
          )
        })}
      </svg>

      {/* Legend */}
      <div className="flex items-center gap-4 mt-3 pt-3 border-t border-zinc-800">
        {Object.entries(STATUS_STROKE).map(([status, color]) => (
          <div key={status} className="flex items-center gap-1.5">
            <div className="w-2 h-2 rounded-sm" style={{ backgroundColor: STATUS_FILL[status], border: `1px solid ${color}` }} />
            <span className="text-[10px] font-mono text-zinc-500 uppercase">{status}</span>
          </div>
        ))}
      </div>
    </div>
  )
}
