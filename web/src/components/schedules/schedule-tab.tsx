import { CalendarClockIcon, ChevronDownIcon, PencilIcon, PlayIcon, PlusIcon, SquareIcon, Trash2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ScheduleEditor } from "@/components/schedules/schedule-editor"
import { ResultBadge, RunsPanel } from "@/components/schedules/runs-panel"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Switch } from "@/components/ui/switch"
import { describeError } from "@/lib/errors"
import { formatDateTime, formatRelative } from "@/lib/format"
import { describeCron, useCancelSchedule, useDeleteSchedule, useRunSchedule, useSchedules, useUpdateSchedule } from "@/lib/schedules"
import type { Schedule } from "@/lib/types"

interface Props {
  serverId: string
  isCustom: boolean
  templateId?: string
}

export function ScheduleTab({ serverId, isCustom, templateId }: Props) {
  const { t } = useTranslation()
  const list = useSchedules(serverId)
  const [editing, setEditing] = useState<Schedule | null | "new">(null)

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("schedules.title")}</h2>
          <p className="text-xs text-muted-foreground">
            {t("schedules.subtitle")}
            {list.data?.timezone ? ` ${t("schedules.timezone", { tz: list.data.timezone })}` : ""}
          </p>
        </div>
        <Button onClick={() => setEditing("new")}>
          <PlusIcon />
          {t("schedules.new")}
        </Button>
      </div>
      {isCustom && (
        <Alert>
          <AlertDescription>{t("schedules.customNote")}</AlertDescription>
        </Alert>
      )}
      {templateId === "fivem-txadmin" && (
        <Alert className="border-amber-500/40 bg-amber-500/10">
          <AlertDescription className="text-amber-200">{t("schedules.txAdminNote")}</AlertDescription>
        </Alert>
      )}
      {list.error ? (
        <Alert variant="destructive">
          <AlertDescription>{describeError(t, list.error)}</AlertDescription>
        </Alert>
      ) : list.isPending ? (
        <div className="h-24 animate-pulse rounded-xl bg-muted/50" />
      ) : list.data.items.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center gap-2 py-10 text-center">
            <CalendarClockIcon className="size-8 text-muted-foreground" />
            <p className="font-heading text-lg uppercase tracking-[0.04em]">{t("schedules.emptyTitle")}</p>
            <p className="max-w-md text-sm text-muted-foreground">{t("schedules.emptyBody")}</p>
            <Button className="mt-2" onClick={() => setEditing("new")}>
              <PlusIcon />
              {t("schedules.new")}
            </Button>
          </CardContent>
        </Card>
      ) : (
        <ul className="flex flex-col gap-3">
          {list.data.items.map((s) => (
            <ScheduleRow key={s.id} serverId={serverId} schedule={s} onEdit={() => setEditing(s)} />
          ))}
        </ul>
      )}
      {editing !== null && (
        <ScheduleEditor
          key={editing === "new" ? "new" : editing.id}
          serverId={serverId}
          schedule={editing === "new" ? undefined : editing}
          open
          onOpenChange={(o) => {
            if (!o) setEditing(null)
          }}
        />
      )}
    </div>
  )
}

function ScheduleRow({ serverId, schedule: s, onEdit }: { serverId: string; schedule: Schedule; onEdit: () => void }) {
  const { t, i18n } = useTranslation()
  const update = useUpdateSchedule(serverId)
  const del = useDeleteSchedule(serverId)
  const run = useRunSchedule(serverId)
  const cancel = useCancelSchedule(serverId)
  const [showRuns, setShowRuns] = useState(false)
  const [confirmDelete, setConfirmDelete] = useState(false)
  const summary = describeCron(s.cron)
  const summaryText = summary
    ? t(`schedules.cronSummary.${summary.key}`, {
        ...summary.params,
        dayName: typeof summary.params?.day === "number" ? t(`schedules.days.${summary.params.day}`) : "",
      })
    : null
  const onErr = (e: unknown) => toast.error(describeError(t, e))

  return (
    <li className="rounded-xl border bg-card p-3 sm:p-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-heading text-base font-medium tracking-[0.03em]">{s.name}</span>
            {s.running && <ResultBadge result="running" />}
            {!s.enabled && <span className="label-caps text-[0.6rem] text-muted-foreground">{t("schedules.disabled")}</span>}
          </div>
          <div className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
            <span>
              {summaryText ?? t("schedules.cronSummary.custom")} <code className="rounded bg-muted px-1 font-mono">{s.cron}</code>
            </span>
            {s.enabled && s.nextRunAt && (
              <span title={formatDateTime(s.nextRunAt, i18n.language)}>
                {t("schedules.next")}: {formatRelative(s.nextRunAt, i18n.language)}
              </span>
            )}
            {s.lastRunAt && (
              <span title={formatDateTime(s.lastRunAt, i18n.language)}>
                {t("schedules.last")}: {formatRelative(s.lastRunAt, i18n.language)}
              </span>
            )}
          </div>
          <div className="mt-2 flex flex-wrap gap-1">
            {s.actions.map((a, i) => (
              <span key={i} className="rounded-md border bg-muted/40 px-1.5 py-0.5 text-[0.7rem]">
                {t(`schedules.actionTypes.${a.type}`)}
                {a.type === "wait" && ` ${a.seconds}s`}
                {a.type === "warn" && a.message ? `: ${a.message}` : ""}
                {a.type === "command" && a.text ? `: ${a.text}` : ""}
              </span>
            ))}
          </div>
          <div className="mt-1 flex flex-wrap gap-x-3 text-[0.7rem] text-muted-foreground">
            {s.onlyIfRunning && <span>{t("schedules.onlyIfRunning")}</span>}
            {s.skipIfPlayers && <span>{t("schedules.skipIfPlayers")}</span>}
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-1.5">
          <label className="mr-1 flex items-center gap-1.5 text-xs text-muted-foreground">
            <Switch
              checked={s.enabled}
              onCheckedChange={(v) => update.mutate({ id: s.id, input: { enabled: v } }, { onError: onErr })}
              aria-label={t("schedules.enabled")}
            />
          </label>
          {s.running ? (
            <Button size="sm" variant="outline" onClick={() => cancel.mutate(s.id, { onError: onErr })}>
              <SquareIcon />
              {t("schedules.cancelRun")}
            </Button>
          ) : (
            <Button size="sm" variant="outline" onClick={() => run.mutate(s.id, { onSuccess: () => toast.success(t("schedules.started")), onError: onErr })}>
              <PlayIcon />
              {t("schedules.runNow")}
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={onEdit} aria-label={t("schedules.edit")}>
            <PencilIcon />
          </Button>
          <Button size="sm" variant="ghost" onClick={() => setConfirmDelete(true)} aria-label={t("schedules.delete")}>
            <Trash2Icon />
          </Button>
        </div>
      </div>
      <button
        type="button"
        className="mt-3 inline-flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground"
        onClick={() => setShowRuns((v) => !v)}
      >
        <ChevronDownIcon className={showRuns ? "size-3.5 rotate-180 transition-transform" : "size-3.5 transition-transform"} />
        {t("schedules.history")}
      </button>
      {showRuns && (
        <div className="mt-2">
          <RunsPanel serverId={serverId} scheduleId={s.id} />
        </div>
      )}
      <Dialog open={confirmDelete} onOpenChange={setConfirmDelete}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="font-heading uppercase tracking-[0.04em]">{t("schedules.deleteTitle")}</DialogTitle>
            <DialogDescription>{t("schedules.deleteBody", { name: s.name })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmDelete(false)}>
              {t("power.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={del.isPending}
              onClick={() => del.mutate(s.id, { onSuccess: () => setConfirmDelete(false), onError: onErr })}
            >
              {t("power.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </li>
  )
}
