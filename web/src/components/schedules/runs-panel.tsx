import { useTranslation } from "react-i18next"

import { Badge } from "@/components/ui/badge"
import { formatDateTime } from "@/lib/format"
import { useScheduleRuns } from "@/lib/schedules"
import type { ScheduleRunResult } from "@/lib/types"
import { cn } from "@/lib/utils"

const resultStyle: Record<ScheduleRunResult, string> = {
  ok: "border-emerald-500/40 bg-emerald-500/15 text-emerald-300",
  skipped: "border-border bg-muted text-muted-foreground",
  failed: "border-red-500/40 bg-red-500/15 text-red-300",
  running: "border-amber-500/40 bg-amber-500/15 text-amber-300",
}

export function ResultBadge({ result, className }: { result: ScheduleRunResult; className?: string }) {
  const { t } = useTranslation()
  return (
    <Badge variant="outline" className={cn("label-caps text-[0.6rem]", resultStyle[result], className)}>
      {t(`schedules.results.${result}`)}
    </Badge>
  )
}

export function RunsPanel({ serverId, scheduleId }: { serverId: string; scheduleId: number }) {
  const { t, i18n } = useTranslation()
  const runs = useScheduleRuns(serverId, scheduleId, true)
  if (runs.isPending) return <p className="text-xs text-muted-foreground">{t("app.loading")}</p>
  if (!runs.data || runs.data.length === 0) return <p className="text-xs text-muted-foreground">{t("schedules.noRuns")}</p>
  return (
    <ul className="flex flex-col gap-2">
      {runs.data.map((r) => (
        <li key={r.id} className="rounded-md border bg-muted/20 p-2">
          <div className="flex flex-wrap items-center gap-2 text-xs">
            <ResultBadge result={r.result} />
            <span className="font-mono">{formatDateTime(r.startedAt, i18n.language)}</span>
            <span className="text-muted-foreground">{t(`schedules.triggers.${r.trigger}`)}</span>
          </div>
          {r.log && (
            <pre className="mt-2 max-h-48 overflow-auto rounded bg-[#050506] p-2 font-mono text-[0.7rem] leading-relaxed whitespace-pre-wrap text-zinc-200">
              {r.log}
            </pre>
          )}
        </li>
      ))}
    </ul>
  )
}
