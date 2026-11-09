import { CpuIcon, MemoryStickIcon, UsersIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { CategoryIcon } from "@/components/servers/category-icon"
import { PowerButtons } from "@/components/servers/power-buttons"
import { InstallBadge } from "@/components/servers/install-badge"
import { StateBadge } from "@/components/servers/state-badge"
import { Sparkline } from "@/components/stats/sparkline"
import { Card, CardContent } from "@/components/ui/card"
import { formatBytes, formatPercent } from "@/lib/format"
import { mergeLive } from "@/lib/servers"
import type { LiveServer, Server } from "@/lib/types"

// Dashboard tile. The name is a "stretched link" covering the card so the
// buttons stay real <button>s instead of being nested inside an <a>.
export function ServerTile({ server: raw, live }: { server: Server; live?: LiveServer }) {
  const { t } = useTranslation()
  const server = mergeLive(raw, live)
  const { status } = server
  const running = status.state === "running" || status.state === "starting"
  const sample = live?.sample
  const memoryBytes = running ? (sample?.memoryBytes ?? status.memoryBytes) : undefined
  const cpu = running && sample ? sample.cpuPercent : undefined
  const history = live?.history ?? []
  const ramSeries = history.map((h) => h.memoryBytes)
  const cpuSeries = history.map((h) => h.cpuPercent)
  const ramMax = server.limits?.memoryMb > 0 ? server.limits.memoryMb * 1024 * 1024 : undefined

  return (
    <Card className="relative transition-colors hover:border-foreground/25 has-[a:focus-visible]:ring-3 has-[a:focus-visible]:ring-ring/50">
      <CardContent className="flex flex-col gap-3">
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
            <CategoryIcon category={server.category} templateId={server.templateId} className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <Link
              to={`/servers/${encodeURIComponent(server.id)}`}
              className="block truncate font-heading text-lg font-medium tracking-[0.03em] outline-none after:absolute after:inset-0 after:rounded-xl after:content-['']"
            >
              {server.name}
            </Link>
            <div className="label-caps mt-0.5 text-[0.62rem] text-muted-foreground">
              {t(`servers.category.${server.category}`)}
            </div>
          </div>
          {server.installState === "ready" ? (
            <StateBadge state={status.state} className="shrink-0" />
          ) : (
            <InstallBadge state={server.installState} className="shrink-0" />
          )}
        </div>

        {server.installState === "ready" && history.length > 1 && (
          <Sparkline
            values={ramSeries}
            secondary={cpuSeries}
            max={ramMax}
            title={t("stats.sparklineTitle")}
            className="h-9 rounded-md bg-muted/30"
          />
        )}

        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-3 font-mono text-xs text-muted-foreground">
            {server.players && (
              <span className="inline-flex items-center gap-1" title={t("console.players.title")}>
                <UsersIcon className="size-3.5" />
                {server.players.online}/{server.players.max}
              </span>
            )}
            {memoryBytes !== undefined ? (
              <span className="inline-flex items-center gap-1.5" title={t("stats.ram")}>
                <MemoryStickIcon className="size-3.5" />
                {formatBytes(memoryBytes)}
                {ramMax ? <span className="text-muted-foreground/60">/ {formatBytes(ramMax)}</span> : null}
              </span>
            ) : status.state === "crashed" && status.exitCode !== undefined ? (
              t("servers.exitCode", { code: status.exitCode })
            ) : (
              <span className="text-muted-foreground/50">—</span>
            )}
            {cpu !== undefined && (
              <span className="inline-flex items-center gap-1" title={t("stats.cpu")}>
                <CpuIcon className="size-3.5" />
                {formatPercent(cpu)}
              </span>
            )}
          </div>
          {server.installState === "ready" ? (
            <PowerButtons server={server} compact className="relative z-10" />
          ) : (
            <span className="text-xs text-muted-foreground">{t(`install.tile.${server.installState}`)}</span>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
