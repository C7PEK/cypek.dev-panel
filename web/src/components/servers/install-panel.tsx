import { Loader2Icon, RotateCcwIcon } from "lucide-react"
import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"
import { isInstalling, useInstallJob, useReinstall } from "@/lib/servers"
import type { Server } from "@/lib/types"
import { cn } from "@/lib/utils"

/** Shown instead of the console while a template server is being installed (or failed to). */
export function InstallPanel({ server }: { server: Server }) {
  const { t } = useTranslation()
  const running = isInstalling(server)
  const job = useInstallJob(server.id, running || server.installState === "failed")
  const reinstall = useReinstall(server.id)
  const logRef = useRef<HTMLDivElement>(null)
  const lines = job.data?.lines ?? []

  useEffect(() => {
    const el = logRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lines.length])

  const failed = server.installState === "failed" || job.data?.state === "failed"
  const percent = job.data?.percent ?? -1
  const stepText =
    job.data && job.data.steps > 0 && job.data.state === "running"
      ? t("install.step", { step: job.data.step, steps: job.data.steps, title: job.data.stepTitle })
      : running
        ? t("install.starting")
        : ""

  return (
    <div className="flex flex-col gap-3">
      <div className="rounded-lg border bg-card p-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-2">
            {running && <Loader2Icon className="size-4 animate-spin text-primary" />}
            <span className="font-heading text-lg font-medium tracking-[0.04em] uppercase">
              {failed ? t("install.failedTitle") : t("install.title")}
            </span>
          </div>
          {percent >= 0 && running && <span className="font-mono text-xs text-muted-foreground">{percent}%</span>}
        </div>
        {stepText && <p className="mt-1 text-sm text-muted-foreground">{stepText}</p>}
        {running && (
          <div className="mt-3 h-1.5 w-full overflow-hidden rounded-full bg-muted" role="progressbar" aria-valuenow={percent >= 0 ? percent : undefined}>
            <div
              className={cn("h-full rounded-full bg-primary transition-[width]", percent < 0 && "w-1/3 animate-pulse")}
              style={percent >= 0 ? { width: `${percent}%` } : undefined}
            />
          </div>
        )}
      </div>

      {failed && (
        <Alert variant="destructive">
          <AlertTitle>{t("install.failedTitle")}</AlertTitle>
          <AlertDescription>
            <p>{job.data?.error || server.installError || t("install.failedGeneric")}</p>
            <Button
              variant="outline"
              size="sm"
              className="mt-3"
              disabled={reinstall.isPending || running}
              onClick={() =>
                reinstall.mutate(undefined, {
                  onError: (e) => toast.error(describeError(t, e)),
                })
              }
            >
              <RotateCcwIcon />
              {t("install.retry")}
            </Button>
          </AlertDescription>
        </Alert>
      )}

      <div
        ref={logRef}
        className="h-[40svh] min-h-48 overflow-y-auto rounded-lg border border-white/10 bg-[#050506] p-3 font-mono text-xs leading-relaxed text-zinc-300"
        aria-live="polite"
      >
        {lines.length === 0 ? (
          <span className="text-zinc-500">{running ? t("install.waitingLog") : t("install.noLog")}</span>
        ) : (
          lines.map((l, i) => (
            <div key={i} className={cn("whitespace-pre-wrap break-all", l.includes("ERROR") && "text-red-400")}>
              {l}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
