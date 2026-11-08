/** Formats bytes with a sensible unit: "512 B", "3.4 KB", "512 MB", "1.2 GB". */
export function formatBytes(n: number | undefined): string {
  if (n === undefined || n === null || Number.isNaN(n)) return ""
  if (n < 1024) return `${n} B`
  const kb = n / 1024
  if (kb < 1024) return `${kb < 10 ? kb.toFixed(1) : Math.round(kb)} KB`
  const mb = kb / 1024
  if (mb < 1024) return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
  const gb = mb / 1024
  if (gb < 1024) return `${gb.toFixed(1)} GB`
  return `${(gb / 1024).toFixed(2)} TB`
}

/** "3d 4h", "2h 05m" or "12m" from seconds. */
export function formatUptime(seconds: number | undefined): string {
  if (!seconds || seconds < 0) return ""
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${String(m).padStart(2, "0")}m`
  return `${m}m`
}

/** Whole percent with a cap, e.g. "37%". */
export function formatPercent(p: number | undefined): string {
  if (p === undefined || Number.isNaN(p)) return ""
  return `${Math.round(p)}%`
}

/** Short locale-aware date + time, e.g. "08.11.2026, 14:03". */
export function formatDateTime(iso: string | undefined, locale: string): string {
  if (!iso) return ""
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  try {
    return new Intl.DateTimeFormat(locale, { dateStyle: "short", timeStyle: "short" }).format(d)
  } catch {
    return d.toLocaleString()
  }
}

/** Relative time like "in 3 h" / "2 min ago" (Intl.RelativeTimeFormat). */
export function formatRelative(iso: string | undefined, locale: string): string {
  if (!iso) return ""
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const diff = (d.getTime() - Date.now()) / 1000
  const abs = Math.abs(diff)
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: "auto" })
  if (abs < 60) return rtf.format(Math.round(diff), "second")
  if (abs < 3600) return rtf.format(Math.round(diff / 60), "minute")
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), "hour")
  return rtf.format(Math.round(diff / 86400), "day")
}
