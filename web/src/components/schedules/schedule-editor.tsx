import { ArrowDownIcon, ArrowUpIcon, PlusIcon, Trash2Icon, WandSparklesIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import {
  CRON_PRESETS,
  chainWaitSeconds,
  presetFor,
  restartWithWarnings,
  useCreateSchedule,
  useCronPreview,
  useUpdateSchedule,
} from "@/lib/schedules"
import type { Schedule, ScheduleAction, ScheduleActionType, ScheduleInput } from "@/lib/types"
import { cn } from "@/lib/utils"

const ACTION_TYPES: ScheduleActionType[] = ["warn", "command", "wait", "restart", "stop", "start", "kill", "backup"]

interface Props {
  serverId: string
  schedule?: Schedule
  open: boolean
  onOpenChange: (open: boolean) => void
}

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const t = setTimeout(() => setV(value), ms)
    return () => clearTimeout(t)
  }, [value, ms])
  return v
}

export function ScheduleEditor({ serverId, schedule, open, onOpenChange }: Props) {
  const { t, i18n } = useTranslation()
  const create = useCreateSchedule(serverId)
  const update = useUpdateSchedule(serverId)
  const mutation = schedule ? update : create

  const [name, setName] = useState(schedule?.name ?? "")
  const [cron, setCron] = useState(schedule?.cron ?? "0 4 * * *")
  const [preset, setPreset] = useState(presetFor(schedule?.cron ?? "0 4 * * *"))
  const [enabled, setEnabled] = useState(schedule?.enabled ?? true)
  const [onlyIfRunning, setOnlyIfRunning] = useState(schedule?.onlyIfRunning ?? true)
  const [skipIfPlayers, setSkipIfPlayers] = useState(schedule?.skipIfPlayers ?? false)
  const [actions, setActions] = useState<ScheduleAction[]>(schedule?.actions ?? [{ type: "restart" }])
  const [localError, setLocalError] = useState<string | null>(null)

  const debouncedCron = useDebounced(cron, 400)
  const preview = useCronPreview(debouncedCron, open)

  const error = mutation.error
  const apiField = isApiError(error) ? error.field : undefined
  const fieldError = (f: string) => (apiField === f ? describeError(t, error) : undefined)
  const generalError = error && !apiField ? describeError(t, error) : null

  function pickPreset(key: string) {
    setPreset(key)
    const p = CRON_PRESETS.find((x) => x.key === key)
    if (p) setCron(p.cron)
  }

  function updateAction(i: number, patch: Partial<ScheduleAction>) {
    setActions((list) => list.map((a, idx) => (idx === i ? { ...a, ...patch } : a)))
  }
  function move(i: number, dir: -1 | 1) {
    setActions((list) => {
      const j = i + dir
      if (j < 0 || j >= list.length) return list
      const next = [...list]
      ;[next[i], next[j]] = [next[j], next[i]]
      return next
    })
  }
  function addAction(type: ScheduleActionType) {
    const a: ScheduleAction = { type }
    if (type === "warn") a.message = t("schedules.presetMessages.generic")
    if (type === "wait") a.seconds = 60
    if (type === "command") a.text = ""
    setActions((list) => [...list, a])
  }
  function applyRestartPreset() {
    setActions(
      restartWithWarnings({
        ten: t("schedules.presetMessages.ten"),
        five: t("schedules.presetMessages.five"),
        one: t("schedules.presetMessages.one"),
      }),
    )
    if (!name.trim()) setName(t("schedules.presets.restartWarnName"))
  }

  function submit() {
    setLocalError(null)
    if (!name.trim()) {
      setLocalError(t("schedules.validation.name"))
      return
    }
    if (actions.length === 0) {
      setLocalError(t("schedules.validation.noActions"))
      return
    }
    for (const a of actions) {
      if (a.type === "warn" && !a.message?.trim()) return setLocalError(t("schedules.validation.warnMessage"))
      if (a.type === "command" && !a.text?.trim()) return setLocalError(t("schedules.validation.commandText"))
      if (a.type === "wait" && (!a.seconds || a.seconds < 1 || a.seconds > 3600)) return setLocalError(t("schedules.validation.wait"))
    }
    if (chainWaitSeconds(actions) > 7200) return setLocalError(t("schedules.validation.totalWait"))
    const input: ScheduleInput = { name: name.trim(), cron: cron.trim(), enabled, actions, onlyIfRunning, skipIfPlayers }
    const done = () => onOpenChange(false)
    if (schedule) update.mutate({ id: schedule.id, input }, { onSuccess: done })
    else create.mutate(input, { onSuccess: done })
  }

  const totalWait = chainWaitSeconds(actions)

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        if (!o && mutation.isPending) return
        if (!o) mutation.reset()
        onOpenChange(o)
      }}
    >
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">
            {schedule ? t("schedules.editTitle") : t("schedules.newTitle")}
          </DialogTitle>
          <DialogDescription>{t("schedules.editorDescription")}</DialogDescription>
        </DialogHeader>

        <FieldGroup>
          <Field data-invalid={!!fieldError("name") || undefined}>
            <FieldLabel htmlFor="sch-name">{t("schedules.name")}</FieldLabel>
            <Input id="sch-name" value={name} onChange={(e) => setName(e.target.value)} placeholder={t("schedules.namePlaceholder")} maxLength={80} />
            <FieldError>{fieldError("name")}</FieldError>
          </Field>

          <div className="grid gap-4 sm:grid-cols-2">
            <Field>
              <FieldLabel htmlFor="sch-preset">{t("schedules.when")}</FieldLabel>
              <select
                id="sch-preset"
                value={preset}
                onChange={(e) => pickPreset(e.target.value)}
                className="h-8 w-full rounded-lg border border-input bg-background px-2.5 text-sm text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30 [&>option]:bg-background [&>option]:text-foreground"
              >
                {CRON_PRESETS.map((p) => (
                  <option key={p.key} value={p.key}>
                    {t(`schedules.presets.${p.key}`)}
                  </option>
                ))}
                <option value="custom">{t("schedules.presets.custom")}</option>
              </select>
            </Field>
            <Field data-invalid={!!fieldError("cron") || undefined}>
              <FieldLabel htmlFor="sch-cron">{t("schedules.cron")}</FieldLabel>
              <Input
                id="sch-cron"
                value={cron}
                onChange={(e) => {
                  setCron(e.target.value)
                  setPreset(presetFor(e.target.value))
                }}
                className="font-mono"
                placeholder="0 4 * * *"
                spellCheck={false}
              />
              <FieldDescription>{t("schedules.cronHelp")}</FieldDescription>
              <FieldError>{fieldError("cron")}</FieldError>
            </Field>
          </div>

          <div className="rounded-lg border bg-muted/30 p-3 text-xs">
            <div className="label-caps mb-1 text-[0.62rem] text-muted-foreground">{t("schedules.nextRuns")}</div>
            {preview.isError ? (
              <span className="text-destructive">{t("errors.invalid_cron")}</span>
            ) : preview.data ? (
              <>
                <ul className="flex flex-wrap gap-x-4 gap-y-1 font-mono">
                  {preview.data.next.map((n) => (
                    <li key={n}>{formatDateTime(n, i18n.language)}</li>
                  ))}
                </ul>
                <div className="mt-1 text-muted-foreground">{t("schedules.timezone", { tz: preview.data.timezone })}</div>
              </>
            ) : (
              <span className="text-muted-foreground">{t("app.loading")}</span>
            )}
          </div>

          <div className="flex flex-col gap-2">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <span className="text-sm font-medium">{t("schedules.actions")}</span>
              <Button type="button" variant="outline" size="sm" onClick={applyRestartPreset}>
                <WandSparklesIcon />
                {t("schedules.presets.restartWarn")}
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">{t("schedules.warnNote")}</p>
            <ol className="flex flex-col gap-2">
              {actions.map((a, i) => (
                <li key={i} className="flex flex-wrap items-center gap-2 rounded-lg border bg-card p-2">
                  <span className="label-caps w-6 text-[0.6rem] text-muted-foreground">{i + 1}.</span>
                  <span className="min-w-24 text-sm font-medium">{t(`schedules.actionTypes.${a.type}`)}</span>
                  {a.type === "warn" && (
                    <Input
                      value={a.message ?? ""}
                      onChange={(e) => updateAction(i, { message: e.target.value })}
                      placeholder={t("schedules.warnPlaceholder")}
                      className="min-w-40 flex-1"
                      maxLength={256}
                    />
                  )}
                  {a.type === "command" && (
                    <Input
                      value={a.text ?? ""}
                      onChange={(e) => updateAction(i, { text: e.target.value })}
                      placeholder="say Hello"
                      className="min-w-40 flex-1 font-mono"
                      maxLength={1024}
                    />
                  )}
                  {a.type === "wait" && (
                    <span className="flex items-center gap-1.5 text-sm">
                      <Input
                        type="number"
                        inputMode="numeric"
                        min={1}
                        max={3600}
                        value={a.seconds ?? 60}
                        onChange={(e) => updateAction(i, { seconds: Number(e.target.value) })}
                        className="w-24"
                      />
                      {t("schedules.seconds")}
                    </span>
                  )}
                  <span className="ml-auto flex items-center gap-0.5">
                    <Button type="button" variant="ghost" size="icon-xs" onClick={() => move(i, -1)} disabled={i === 0} aria-label={t("schedules.moveUp")}>
                      <ArrowUpIcon />
                    </Button>
                    <Button type="button" variant="ghost" size="icon-xs" onClick={() => move(i, 1)} disabled={i === actions.length - 1} aria-label={t("schedules.moveDown")}>
                      <ArrowDownIcon />
                    </Button>
                    <Button type="button" variant="ghost" size="icon-xs" onClick={() => setActions((l) => l.filter((_, idx) => idx !== i))} aria-label={t("schedules.remove")}>
                      <Trash2Icon />
                    </Button>
                  </span>
                </li>
              ))}
            </ol>
            <div className="flex flex-wrap items-center gap-1.5">
              <span className="label-caps text-[0.6rem] text-muted-foreground">{t("schedules.addAction")}</span>
              {ACTION_TYPES.map((type) => (
                <Button key={type} type="button" variant="outline" size="xs" onClick={() => addAction(type)} disabled={actions.length >= 32}>
                  <PlusIcon />
                  {t(`schedules.actionTypes.${type}`)}
                </Button>
              ))}
            </div>
            {totalWait > 0 && (
              <p className={cn("text-xs text-muted-foreground", totalWait > 7200 && "text-destructive")}>
                {t("schedules.totalWait", { minutes: Math.round(totalWait / 60) })}
              </p>
            )}
            <FieldError>{fieldError("actions")}</FieldError>
          </div>

          <div className="grid gap-3 sm:grid-cols-3">
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="sch-enabled">{t("schedules.enabled")}</FieldLabel>
              </FieldContent>
              <Switch id="sch-enabled" checked={enabled} onCheckedChange={setEnabled} />
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="sch-oir">{t("schedules.onlyIfRunning")}</FieldLabel>
              </FieldContent>
              <Switch id="sch-oir" checked={onlyIfRunning} onCheckedChange={setOnlyIfRunning} />
            </Field>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="sch-sip">{t("schedules.skipIfPlayers")}</FieldLabel>
              </FieldContent>
              <Switch id="sch-sip" checked={skipIfPlayers} onCheckedChange={setSkipIfPlayers} />
            </Field>
          </div>

          {(localError || generalError) && (
            <Alert variant="destructive">
              <AlertDescription>{localError ?? generalError}</AlertDescription>
            </Alert>
          )}
        </FieldGroup>

        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={mutation.isPending}>
            {t("power.cancel")}
          </Button>
          <Button type="button" onClick={submit} disabled={mutation.isPending}>
            {mutation.isPending ? t("servers.saving") : t("servers.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
