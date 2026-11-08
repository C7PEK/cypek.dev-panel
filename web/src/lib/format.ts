/** Formats bytes as "512 MB" / "1.2 GB" for tiles and headers. */
export function formatBytes(n: number | undefined): string {
  if (!n) return ""
  const mb = n / (1024 * 1024)
  if (mb >= 1024) return `${(mb / 1024).toFixed(1)} GB`
  return `${Math.round(mb)} MB`
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
