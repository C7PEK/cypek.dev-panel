import { ActivityIcon, CpuIcon, MemoryStickIcon, UsersIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { TimeChart, type ChartPoint } from "@/components/stats/time-chart"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBytes, formatPercent } from "@/lib/format"
import { useServerStats } from "@/lib/server-history"
import { cn } from "@/lib/utils"

const RANGES = [5, 15, 60] as const

const COLORS = {
  ram: "var(--primary)",
  cpu: "#00d8e6",
  players: "#34d399",
}

export function ResourceCharts({ serverId }: { serverId: string }) {
  const { t, i18n } = useTranslation()
  const [minutes, setMinutes] = useState<(typeof RANGES)[number]>(15)
  const stats = useServerStats(serverId, minutes)
  const data = stats.data
  const windowMs = minutes * 60_000
  const timeFmt = new Intl.DateTimeFormat(i18n.language, { hour: "2-digit", minute: "2-digit" })
  const formatTime = (ms: number) => timeFmt.format(new Date(ms))

  const live = (s: { state: string }) => s.state === "running" || s.state === "starting" || s.state === "stopping"
  const history = data?.history ?? []
  const ram: ChartPoint[] = history.map((h) => ({ t: Date.parse(h.at), v: live(h) ? h.memoryBytes : null }))
  const cpu: ChartPoint[] = history.map((h) => ({ t: Date.parse(h.at), v: live(h) ? h.cpuPercent : null }))
  const hasPlayers = history.some((h) => (h.players ?? -1) >= 0)
  const players: ChartPoint[] = history.map((h) => ({ t: Date.parse(h.at), v: (h.players ?? -1) >= 0 ? h.players! : null }))
  const last = history.length ? history[history.length - 1] : undefined
  const cores = data?.cpuCount || 1
  const memLimit = data?.memoryLimit || 0
  const peakMem = data?.summary.peakMemoryBytes ?? 0
  const memMax = memLimit || Math.max(peakMem * 1.25, 512 * 1024 * 1024)

  const stat = (label: string, value: string, hint?: string) => (
    <div className="flex flex-col">
      <span className="label-caps text-[0.6rem] text-muted-foreground">{label}</span>
      <span className="font-mono text-sm">
        {value}
        {hint && <span className="ml-1 text-xs text-muted-foreground">{hint}</span>}
      </span>
    </div>
  )

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-3">
        <CardTitle className="flex items-center gap-2 font-heading text-base font-medium tracking-[0.06em] uppercase">
          <ActivityIcon className="size-4 text-muted-foreground" />
          {t("history.resourcesTitle")}
        </CardTitle>
        <div className="flex items-center gap-1 rounded-lg border p-0.5" role="group" aria-label={t("history.range")}>
          {RANGES.map((m) => (
            <Button key={m} size="xs" variant={m === minutes ? "secondary" : "ghost"} onClick={() => setMinutes(m)}>
              {t("history.minutes", { count: m })}
            </Button>
          ))}
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {!history.length ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {stats.isPending ? t("app.loading") : t("history.noData")}
          </p>
        ) : (
          <>
            <div className="grid gap-5 lg:grid-cols-2">
              <section className="min-w-0">
                <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
                  <h4 className="flex items-center gap-1.5 text-sm font-medium">
                    <MemoryStickIcon className="size-4" style={{ color: COLORS.ram }} />
                    {t("stats.ram")}
                  </h4>
                  <div className="flex gap-4">
                    {stat(t("history.now"), last && live(last) ? formatBytes(last.memoryBytes) : "-", memLimit ? `/ ${formatBytes(memLimit)}` : undefined)}
                    {stat(t("history.avg"), formatBytes(data?.summary.avgMemoryBytes ?? 0))}
                    {stat(t("history.peak"), formatBytes(peakMem))}
                  </div>
                </div>
                <TimeChart
                  points={ram}
                  windowMs={windowMs}
                  yMax={memMax}
                  limit={memLimit || undefined}
                  limitLabel={t("history.limit")}
                  formatY={(v) => formatBytes(v)}
                  formatTime={formatTime}
                  color={COLORS.ram}
                />
              </section>
              <section className="min-w-0">
                <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
                  <h4 className="flex items-center gap-1.5 text-sm font-medium">
                    <CpuIcon className="size-4" style={{ color: COLORS.cpu }} />
                    {t("stats.cpu")}
                    <span className="text-xs font-normal text-muted-foreground">{t("history.cpuHint", { cores })}</span>
                  </h4>
                  <div className="flex gap-4">
                    {stat(t("history.now"), last && live(last) ? formatPercent(last.cpuPercent) : "-")}
                    {stat(t("history.avg"), formatPercent(data?.summary.avgCpuPercent ?? 0))}
                    {stat(t("history.peak"), formatPercent(data?.summary.peakCpuPercent ?? 0))}
                  </div>
                </div>
                <TimeChart
                  points={cpu}
                  windowMs={windowMs}
                  yMax={100}
                  formatY={(v) => formatPercent(v)}
                  formatTime={formatTime}
                  color={COLORS.cpu}
                />
              </section>
            </div>
            {hasPlayers && (
              <section className={cn("min-w-0")}>
                <div className="mb-2 flex flex-wrap items-end justify-between gap-2">
                  <h4 className="flex items-center gap-1.5 text-sm font-medium">
                    <UsersIcon className="size-4" style={{ color: COLORS.players }} />
                    {t("history.players")}
                  </h4>
                  <div className="flex gap-4">
                    {stat(t("history.now"), last && (last.players ?? -1) >= 0 ? String(last.players) : "-")}
                    {stat(t("history.peak"), String(data?.summary.peakPlayers ?? 0))}
                  </div>
                </div>
                <TimeChart
                  points={players}
                  windowMs={windowMs}
                  yMax={4}
                  integer
                  height={110}
                  formatY={(v) => String(Math.round(v))}
                  formatTime={formatTime}
                  color={COLORS.players}
                />
              </section>
            )}
          </>
        )}
      </CardContent>
    </Card>
  )
}
