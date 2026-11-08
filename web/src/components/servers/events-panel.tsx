import {
  AlertTriangleIcon,
  ChevronDownIcon,
  CircleCheckIcon,
  CircleStopIcon,
  HistoryIcon,
  PlayIcon,
  SkullIcon,
  UserIcon,
} from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatDateTime, formatRelative } from "@/lib/format"
import { useServerEvents } from "@/lib/server-history"
import type { ServerEvent } from "@/lib/types"
import { cn } from "@/lib/utils"

type Filter = "all" | "problems"

const ICONS: Record<ServerEvent["kind"], { icon: typeof PlayIcon; className: string }> = {
  start: { icon: PlayIcon, className: "text-emerald-400 bg-emerald-500/10" },
  ready: { icon: CircleCheckIcon, className: "text-emerald-400 bg-emerald-500/10" },
  stop: { icon: CircleStopIcon, className: "text-muted-foreground bg-muted" },
  exit: { icon: CircleStopIcon, className: "text-amber-300 bg-amber-500/10" },
  crash: { icon: SkullIcon, className: "text-red-400 bg-red-500/15" },
  error: { icon: AlertTriangleIcon, className: "text-amber-300 bg-amber-500/10" },
  action: { icon: UserIcon, className: "text-[#8b95ff] bg-[#5865f2]/10" },
}

// Server history: starts, crashes, error bursts from the console and who
// pressed which button, newest first.
export function EventsPanel({ serverId }: { serverId: string }) {
  const { t, i18n } = useTranslation()
  const events = useServerEvents(serverId)
  const [filter, setFilter] = useState<Filter>("all")
  const items = (events.data?.items ?? []).filter((e) => filter === "all" || e.kind === "crash" || e.kind === "error" || e.kind === "exit")
  const counts = events.data?.last24h

  const title = (e: ServerEvent): string => {
    switch (e.kind) {
      case "ready": {
        const d = e.message?.match(/(\d+h)?(\d+m)?(\d+s)/)?.[0]
        return d ? t("history.events.readyAfter", { duration: d }) : t("history.events.ready")
      }
      case "crash":
        return e.exitCode === 78 ? t("history.events.cannotStart") : t("history.events.crash", { code: e.exitCode ?? "?" })
      case "action":
        return t(`history.actions.${(e.message ?? "").replace(".", "_")}`, { defaultValue: e.message ?? "" })
      default:
        return t(`history.events.${e.kind}`)
    }
  }

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-3">
        <CardTitle className="flex items-center gap-2 font-heading text-base font-medium tracking-[0.06em] uppercase">
          <HistoryIcon className="size-4 text-muted-foreground" />
          {t("history.eventsTitle")}
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {counts && (
            <div className="flex items-center gap-1.5 text-xs" title={t("history.last24h")}>
              <span className={cn("rounded-md border px-2 py-0.5 font-mono", counts.crash > 0 ? "border-red-500/40 bg-red-500/10 text-red-300" : "text-muted-foreground")}>
                {t("history.countCrashes", { count: counts.crash })}
              </span>
              <span className={cn("rounded-md border px-2 py-0.5 font-mono", counts.error > 0 ? "border-amber-500/40 bg-amber-500/10 text-amber-300" : "text-muted-foreground")}>
                {t("history.countErrors", { count: counts.error })}
              </span>
              <span className="rounded-md border px-2 py-0.5 font-mono text-muted-foreground">{t("history.countStarts", { count: counts.start })}</span>
            </div>
          )}
          <div className="flex items-center gap-1 rounded-lg border p-0.5">
            {(["all", "problems"] as Filter[]).map((f) => (
              <Button key={f} size="xs" variant={f === filter ? "secondary" : "ghost"} onClick={() => setFilter(f)}>
                {t(`history.filter.${f}`)}
              </Button>
            ))}
          </div>
        </div>
      </CardHeader>
      <CardContent>
        {events.isPending ? (
          <p className="py-6 text-center text-sm text-muted-foreground">{t("app.loading")}</p>
        ) : items.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {filter === "problems" ? t("history.noProblems") : t("history.noEvents")}
          </p>
        ) : (
          <ol className="flex max-h-[28rem] flex-col gap-1.5 overflow-y-auto pr-1">
            {items.map((e, i) => (
              <EventRow key={`${e.at}-${i}`} event={e} title={title(e)} locale={i18n.language} />
            ))}
          </ol>
        )}
      </CardContent>
    </Card>
  )
}

function EventRow({ event: e, title, locale }: { event: ServerEvent; title: string; locale: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const { icon: Icon, className } = ICONS[e.kind] ?? ICONS.stop
  const expandable = (e.lines?.length ?? 0) > 1
  const showMessage = e.kind === "error" || (e.kind === "crash" && e.exitCode === 78)
  return (
    <li
      className={cn(
        "rounded-lg border px-3 py-2",
        e.kind === "crash" && "border-red-500/40 bg-red-500/[0.06]",
        e.kind === "error" && "border-amber-500/25",
      )}
    >
      <div className="flex items-start gap-3">
        <span className={cn("mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md", className)}>
          <Icon className="size-4" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline justify-between gap-x-3">
            <span className="text-sm font-medium">
              {title}
              {e.username && <span className="ml-1.5 font-normal text-muted-foreground">· {e.username}</span>}
              {(e.count ?? 0) > 0 && (
                <span className="ml-1.5 text-xs font-normal text-muted-foreground">{t("history.suppressed", { count: e.count })}</span>
              )}
            </span>
            <time className="shrink-0 font-mono text-xs text-muted-foreground" dateTime={e.at} title={formatDateTime(e.at, locale)}>
              {formatRelative(e.at, locale)}
            </time>
          </div>
          {showMessage && e.message && <p className="mt-0.5 truncate font-mono text-xs text-muted-foreground">{e.message}</p>}
          {expandable && (
            <>
              <button
                type="button"
                className="mt-1 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
                onClick={() => setOpen((o) => !o)}
                aria-expanded={open}
              >
                <ChevronDownIcon className={cn("size-3.5 transition-transform", open && "rotate-180")} />
                {open ? t("history.hideDetails") : t("history.showDetails", { count: e.lines!.length })}
              </button>
              {open && (
                <pre className="mt-2 max-h-72 overflow-auto rounded-md border bg-[#050506] p-2 font-mono text-[11px] leading-relaxed whitespace-pre text-zinc-300">
                  {e.lines!.join("\n")}
                </pre>
              )}
            </>
          )}
        </div>
      </div>
    </li>
  )
}
