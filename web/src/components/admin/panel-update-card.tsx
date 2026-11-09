import { useQueryClient } from "@tanstack/react-query"
import { ArrowUpCircleIcon, CheckCircle2Icon, Loader2Icon, RefreshCwIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import { panelUpdateKey, usePanelUpdate, useRefreshPanelUpdate, useStartPanelUpdate } from "@/lib/system"
import type { PanelUpdateInfo } from "@/lib/types"

const POLL_MS = 3000
const GIVE_UP_MS = 5 * 60_000

/** Owner-only: installed vs latest release and a one-click update. */
export function PanelUpdateCard() {
  const { t, i18n } = useTranslation()
  const qc = useQueryClient()
  const info = usePanelUpdate(true)
  const refresh = useRefreshPanelUpdate()
  const start = useStartPanelUpdate()
  const [updating, setUpdating] = useState(false)
  const fromVersion = useRef("")

  // While updating, the panel restarts: poll until it reports a new version
  // or a failed run. Network errors in between are expected.
  useEffect(() => {
    if (!updating) return
    const started = Date.now()
    let stop = false
    const tick = async () => {
      if (stop) return
      try {
        const d = await api<PanelUpdateInfo>("GET", "/system/update")
        qc.setQueryData(panelUpdateKey, d)
        if (d.current !== fromVersion.current) {
          toast.success(t("panelUpdate.done", { version: d.current }))
          setTimeout(() => location.reload(), 800)
          return
        }
        if (!d.running && d.last && (d.last.state === "failed" || d.last.state === "uptodate") && d.last.from === fromVersion.current) {
          setUpdating(false)
          if (d.last.state === "failed") toast.error(t("panelUpdate.failed", { message: d.last.error ?? "" }))
          return
        }
      } catch {
        // Restarting; try again.
      }
      if (Date.now() - started > GIVE_UP_MS) {
        setUpdating(false)
        toast.error(t("panelUpdate.timeout"))
        return
      }
      setTimeout(() => void tick(), POLL_MS)
    }
    const id = setTimeout(() => void tick(), POLL_MS)
    return () => {
      stop = true
      clearTimeout(id)
    }
  }, [updating, qc, t])

  const d = info.data
  const noReleases = d?.error && /no releases/i.test(d.error)

  function startUpdate() {
    if (!d) return
    fromVersion.current = d.current
    start.mutate(undefined, {
      onSuccess: () => setUpdating(true),
      onError: (e) => toast.error(describeError(t, e)),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("panelUpdate.title")}</CardTitle>
        <CardDescription>{t("panelUpdate.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {info.isPending ? (
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            <Loader2Icon className="size-4 animate-spin" />
            {t("app.loading")}
          </p>
        ) : info.error ? (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, info.error)}</AlertDescription>
          </Alert>
        ) : d ? (
          <>
            <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
              <dt className="text-muted-foreground">{t("panelUpdate.current")}</dt>
              <dd className="font-mono">{d.current}</dd>
              <dt className="text-muted-foreground">{t("panelUpdate.latest")}</dt>
              <dd className="font-mono">
                {d.latest ?? "—"}
                {d.publishedAt && <span className="ml-2 font-sans text-xs text-muted-foreground">{formatDateTime(d.publishedAt, i18n.language)}</span>}
              </dd>
            </dl>

            {noReleases ? (
              <p className="text-sm text-muted-foreground">{t("panelUpdate.noReleases", { repo: d.repo })}</p>
            ) : d.error ? (
              <p className="text-sm text-muted-foreground">{t("panelUpdate.checkFailed", { message: d.error })}</p>
            ) : d.available ? (
              <p className="flex items-center gap-2 text-sm text-brand-cyan">
                <ArrowUpCircleIcon className="size-4" />
                {t("panelUpdate.available", { version: d.latest })}
              </p>
            ) : (
              <p className="flex items-center gap-2 text-sm text-emerald-400">
                <CheckCircle2Icon className="size-4" />
                {t("panelUpdate.upToDate")}
              </p>
            )}

            {d.notes && (
              <details className="rounded-lg border bg-muted/20 px-3 py-2 text-sm">
                <summary className="cursor-pointer font-medium">{t("panelUpdate.notes")}</summary>
                <p className="mt-2 max-h-64 overflow-y-auto text-xs whitespace-pre-wrap text-muted-foreground">{d.notes}</p>
              </details>
            )}

            {d.last?.state === "failed" && !updating && (
              <Alert variant="destructive">
                <AlertDescription>
                  {t("panelUpdate.lastFailed", { when: formatDateTime(d.last.finishedAt ?? d.last.startedAt, i18n.language), message: d.last.error ?? "" })}
                </AlertDescription>
              </Alert>
            )}

            {updating || d.running ? (
              <Alert className="border-brand-cyan/40 bg-brand-cyan/5">
                <AlertDescription className="flex items-center gap-2">
                  <Loader2Icon className="size-4 animate-spin" />
                  {t("panelUpdate.updating")}
                </AlertDescription>
              </Alert>
            ) : (
              <div className="flex flex-wrap gap-2">
                <Button variant="outline" size="sm" disabled={refresh.isPending} onClick={() => refresh.mutate(undefined, { onError: (e) => toast.error(describeError(t, e)) })}>
                  {refresh.isPending ? <Loader2Icon className="animate-spin" /> : <RefreshCwIcon />}
                  {t("panelUpdate.checkNow")}
                </Button>
                {d.available && (
                  <Button size="sm" disabled={!d.canUpdate || start.isPending} onClick={startUpdate}>
                    {start.isPending ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
                    {t("panelUpdate.updateTo", { version: d.latest })}
                  </Button>
                )}
              </div>
            )}
            {d.available && !d.canUpdate && <p className="text-xs text-muted-foreground">{t("panelUpdate.terminalOnly")}</p>}
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
