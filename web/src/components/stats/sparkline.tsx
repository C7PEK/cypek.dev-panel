import { cn } from "@/lib/utils"

interface Props {
  /** Primary series (RAM); drawn filled. */
  values: number[]
  /** Optional secondary series (CPU); thin line. */
  secondary?: number[]
  /** Upper bound of the primary series; defaults to its max. */
  max?: number
  /** Upper bound of the secondary series; defaults to max(100, its max). */
  secondaryMax?: number
  /** Number of slots the chart is sized for (keeps the x scale stable while history fills). */
  slots?: number
  className?: string
  title?: string
}

function path(values: number[], max: number, slots: number, w: number, h: number): string {
  if (values.length === 0 || max <= 0) return ""
  const step = w / Math.max(slots - 1, 1)
  const offset = (slots - values.length) * step
  return values
    .map((v, i) => {
      const x = offset + i * step
      const y = h - Math.min(v / max, 1) * (h - 2) - 1
      return `${i === 0 ? "M" : "L"}${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(" ")
}

/** Tiny inline-SVG chart for tiles and headers; no library, theme aware. */
export function Sparkline({ values, secondary, max, secondaryMax, slots = 60, className, title }: Props) {
  const w = 120
  const h = 32
  const primaryMax = max && max > 0 ? max : Math.max(...values, 1)
  const secMax = secondaryMax && secondaryMax > 0 ? secondaryMax : Math.max(100, ...(secondary ?? [0]))
  const p = path(values, primaryMax, slots, w, h)
  const s = secondary ? path(secondary, secMax, slots, w, h) : ""
  const step = w / Math.max(slots - 1, 1)
  const lastX = w
  const firstX = (slots - values.length) * step
  return (
    <svg
      viewBox={`0 0 ${w} ${h}`}
      preserveAspectRatio="none"
      className={cn("h-8 w-full overflow-visible", className)}
      role="img"
      aria-label={title}
    >
      {title && <title>{title}</title>}
      {p && (
        <>
          <path d={`${p} L${lastX},${h} L${firstX.toFixed(1)},${h} Z`} fill="var(--primary)" opacity="0.18" />
          <path d={p} fill="none" stroke="var(--primary)" strokeWidth="1.5" vectorEffect="non-scaling-stroke" />
        </>
      )}
      {s && <path d={s} fill="none" stroke="var(--brand-cyan)" strokeWidth="1" opacity="0.8" vectorEffect="non-scaling-stroke" />}
    </svg>
  )
}
