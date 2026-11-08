import { ActivityIcon, ClockIcon, CpuIcon, HardDriveIcon, MemoryStickIcon, WifiOffIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { formatBytes, formatPercent, formatUptime } from "@/lib/format"
import type { HostStats } from "@/lib/types"
import { cn } from "@/lib/utils"

function Meter({ value, max, className }: { value: number; max: number; className?: string }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0
  const tone = pct > 90 ? "bg-red-400" : pct > 75 ? "bg-amber-400" : "bg-primary"
  return (
    <div className={cn("h-1.5 w-full overflow-hidden rounded-full bg-muted", className)} aria-hidden="true">
      <div className={cn("h-full rounded-full transition-[width] duration-700", tone)} style={{ width: `${pct}%` }} />
    </div>
  )
}

interface Props {
  host?: HostStats
  connected: boolean
}

/** Top-of-dashboard strip with CPU, RAM, disk, uptime and load of the host. */
export function HostBar({ host, connected }: Props) {
  const { t } = useTranslation()
  if (!host) {
    return <div className="h-20 animate-pulse rounded-xl bg-muted/50" aria-busy />
  }
  const items = [
    {
      key: "cpu",
      icon: CpuIcon,
      label: t("stats.cpu"),
      value: formatPercent(host.cpuPercent),
      sub: t("stats.cores", { count: host.cpuCount }),
      meter: { value: host.cpuPercent, max: 100 },
    },
    {
      key: "ram",
      icon: MemoryStickIcon,
      label: t("stats.ram"),
      value: formatBytes(host.memUsed),
      sub: t("stats.of", { total: formatBytes(host.memTotal) }),
      meter: { value: host.memUsed, max: host.memTotal },
    },
    {
      key: "disk",
      icon: HardDriveIcon,
      label: t("stats.disk"),
      value: formatBytes(host.diskUsed),
      sub: t("stats.of", { total: formatBytes(host.diskTotal) }),
      meter: { value: host.diskUsed, max: host.diskTotal },
      title: host.diskPath,
    },
    {
      key: "uptime",
      icon: ClockIcon,
      label: t("stats.uptime"),
      value: formatUptime(host.uptimeSeconds),
      sub: host.hostname,
    },
    {
      key: "load",
      icon: ActivityIcon,
      label: t("stats.load"),
      value: host.load1.toFixed(2),
      sub: host.os,
    },
  ]
  return (
    <section aria-label={t("stats.hostTitle")} className="rounded-xl border bg-card p-3 sm:p-4">
      <div className="mb-2 flex items-center justify-between">
        <span className="label-caps text-[0.66rem] text-muted-foreground">{t("stats.hostTitle")}</span>
        <span
          className={cn("label-caps inline-flex items-center gap-1.5 text-[0.6rem]", connected ? "text-muted-foreground" : "text-amber-400")}
          title={connected ? t("stats.live") : t("stats.polling")}
        >
          {connected ? (
            <span className="brand-pulse size-1.5 rounded-full bg-brand-cyan shadow-[0_0_8px_var(--brand-cyan)]" />
          ) : (
            <WifiOffIcon className="size-3" />
          )}
          {connected ? t("stats.live") : t("stats.polling")}
        </span>
      </div>
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        {items.map(({ key, icon: Icon, label, value, sub, meter, title }) => (
          <div key={key} className="flex min-w-0 flex-col gap-1" title={title}>
            <div className="flex items-center gap-1.5 text-[0.66rem] text-muted-foreground label-caps">
              <Icon className="size-3.5" />
              {label}
            </div>
            <div className="flex items-baseline gap-1.5">
              <span className="font-heading text-xl leading-none">{value || "—"}</span>
              <span className="truncate text-[0.7rem] text-muted-foreground">{sub}</span>
            </div>
            {meter && <Meter value={meter.value} max={meter.max} />}
          </div>
        ))}
      </div>
    </section>
  )
}
