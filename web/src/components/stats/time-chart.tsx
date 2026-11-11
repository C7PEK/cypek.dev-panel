import { useEffect, useMemo, useRef, useState } from "react"

import { cn } from "@/lib/utils"

export interface ChartPoint {
  t: number
  v: number | null
}

interface Props {
  points: ChartPoint[]
  windowMs: number
  yMax: number
  limit?: number
  limitLabel?: string
  formatY: (v: number) => string
  formatTime: (t: number) => string
  color: string
  height?: number
  className?: string
  integer?: boolean
}

const PAD = { top: 10, right: 10, bottom: 20, left: 52 }

export function TimeChart({ points, windowMs, yMax, limit, limitLabel, formatY, formatTime, color, height = 150, className, integer }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const [hover, setHover] = useState<number | null>(null)

  useEffect(() => {
    const el = ref.current
    if (!el) return
    const ro = new ResizeObserver(([entry]) => setWidth(Math.floor(entry.contentRect.width)))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  const now = points.length ? points[points.length - 1].t : Date.now()
  const start = now - windowMs
  const visible = useMemo(() => points.filter((p) => p.t >= start), [points, start])
  const dataMax = visible.reduce((m, p) => (p.v !== null && p.v > m ? p.v : m), 0)
  let top = Math.max(yMax, dataMax * 1.1, limit ?? 0)
  if (integer) top = Math.max(1, Math.ceil(top))
  if (top <= 0) top = 1

  const w = Math.max(width - PAD.left - PAD.right, 10)
  const h = height - PAD.top - PAD.bottom
  const x = (t: number) => PAD.left + ((t - start) / windowMs) * w
  const y = (v: number) => PAD.top + h - (v / top) * h

  const segments: ChartPoint[][] = []
  let cur: ChartPoint[] = []
  for (const p of visible) {
    if (p.v === null) {
      if (cur.length) segments.push(cur)
      cur = []
    } else cur.push(p)
  }
  if (cur.length) segments.push(cur)

  const line = (seg: ChartPoint[]) => seg.map((p, i) => `${i ? "L" : "M"}${x(p.t).toFixed(1)},${y(p.v!).toFixed(1)}`).join("")
  const area = (seg: ChartPoint[]) =>
    `${line(seg)}L${x(seg[seg.length - 1].t).toFixed(1)},${y(0)}L${x(seg[0].t).toFixed(1)},${y(0)}Z`

  const grid = [0, 0.5, 1].map((f) => (integer ? Math.round(top * f) : top * f))
  const hovered = hover !== null ? visible[hover] : null
  const gradientId = useMemo(() => `g-${Math.random().toString(36).slice(2)}`, [])

  function onMove(e: React.PointerEvent<SVGSVGElement>) {
    if (!visible.length) return
    const rect = e.currentTarget.getBoundingClientRect()
    const t = start + ((e.clientX - rect.left - PAD.left) / w) * windowMs
    let best = 0
    for (let i = 1; i < visible.length; i++) {
      if (Math.abs(visible[i].t - t) < Math.abs(visible[best].t - t)) best = i
    }
    setHover(best)
  }

  return (
    <div ref={ref} className={cn("relative w-full select-none", className)} style={{ height }}>
      {width > 0 && (
        <svg width={width} height={height} onPointerMove={onMove} onPointerLeave={() => setHover(null)} className="touch-none">
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" style={{ stopColor: color }} stopOpacity="0.35" />
              <stop offset="100%" style={{ stopColor: color }} stopOpacity="0.02" />
            </linearGradient>
          </defs>
          {grid.map((g) => (
            <g key={g}>
              <line x1={PAD.left} x2={PAD.left + w} y1={y(g)} y2={y(g)} className="stroke-border" strokeDasharray={g === 0 ? undefined : "3 4"} />
              <text x={PAD.left - 6} y={y(g)} dy="0.32em" textAnchor="end" className="fill-muted-foreground font-mono text-[10px]">
                {formatY(g)}
              </text>
            </g>
          ))}
          {limit !== undefined && limit > 0 && (
            <g>
              <line x1={PAD.left} x2={PAD.left + w} y1={y(limit)} y2={y(limit)} stroke="var(--destructive)" strokeOpacity="0.6" strokeDasharray="6 4" />
              {limitLabel && (
                <text x={PAD.left + w} y={y(limit) - 4} textAnchor="end" className="fill-destructive font-mono text-[10px]">
                  {limitLabel}
                </text>
              )}
            </g>
          )}
          {segments.map((seg, i) => (
            <g key={i}>
              {seg.length > 1 && <path d={area(seg)} fill={`url(#${gradientId})`} />}
              <path d={line(seg)} fill="none" style={{ stroke: color }} strokeWidth="1.75" strokeLinejoin="round" />
              {seg.length === 1 && <circle cx={x(seg[0].t)} cy={y(seg[0].v!)} r="2" style={{ fill: color }} />}
            </g>
          ))}
          {[start, start + windowMs / 2, now].map((t, i) => (
            <text key={i} x={x(t)} y={height - 4} textAnchor={i === 0 ? "start" : i === 2 ? "end" : "middle"} className="fill-muted-foreground font-mono text-[10px]">
              {formatTime(t)}
            </text>
          ))}
          {hovered && (
            <g>
              <line x1={x(hovered.t)} x2={x(hovered.t)} y1={PAD.top} y2={PAD.top + h} className="stroke-foreground/30" />
              {hovered.v !== null && <circle cx={x(hovered.t)} cy={y(hovered.v)} r="3.5" style={{ fill: color }} className="stroke-background" strokeWidth="2" />}
            </g>
          )}
        </svg>
      )}
      {hovered && (
        <div
          className="pointer-events-none absolute top-1 z-10 rounded-md border bg-popover/95 px-2 py-1 font-mono text-[11px] shadow-md backdrop-blur"
          style={{ left: Math.min(Math.max(x(hovered.t) - 50, 0), Math.max(width - 110, 0)) }}
        >
          <div className="text-muted-foreground">{formatTime(hovered.t)}</div>
          <div className="font-medium">{hovered.v === null ? "-" : formatY(hovered.v)}</div>
        </div>
      )}
    </div>
  )
}
