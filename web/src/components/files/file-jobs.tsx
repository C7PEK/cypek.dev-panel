import { useQueryClient } from "@tanstack/react-query"
import { CheckCircle2Icon, CircleSlashIcon, DownloadCloudIcon, FolderArchiveIcon, FolderOpenIcon, Loader2Icon, XCircleIcon, XIcon } from "lucide-react"
import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"
import { filesKey, parentPath, useCancelFileJob, useFileJobs } from "@/lib/files"
import { formatBytes } from "@/lib/format"
import type { FileJob } from "@/lib/types"
import { cn } from "@/lib/utils"

// Finished jobs stay visible this long (the server keeps them ~2h).
const SHOW_FINISHED_MS = 10 * 60 * 1000

interface Props {
  serverId: string
  onOpenFolder: (dir: string) => void
}

/** Downloads from links and extractions running in the background. */
export function FileJobs({ serverId, onOpenFolder }: Props) {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const jobs = useFileJobs(serverId)
  const cancel = useCancelFileJob(serverId)
  const [hidden, setHidden] = useState<Set<string>>(new Set())
  const previous = useRef<Map<string, FileJob["state"]>>(new Map())

  // Announce jobs that finished while the tab is open and refresh listings.
  useEffect(() => {
    for (const j of jobs.data ?? []) {
      const before = previous.current.get(j.id)
      if (before === "running" && j.state !== "running") {
        const dirs = new Set([j.dir, j.file ? parentPath(j.file) : j.dir])
        for (const d of dirs) void qc.invalidateQueries({ queryKey: filesKey(serverId, d) })
        if (j.state === "done") {
          toast.success(t(j.kind === "download" ? "fileJobs.downloaded" : "fileJobs.extracted", { name: j.name }))
        } else if (j.state === "failed") {
          toast.error(t("fileJobs.failedToast", { name: j.name }))
        }
      }
      previous.current.set(j.id, j.state)
    }
  }, [jobs.data, qc, serverId, t])

  const now = jobs.dataUpdatedAt
  const visible = (jobs.data ?? []).filter((j) => {
    if (hidden.has(j.id)) return false
    if (j.state === "running" || !j.finishedAt) return true
    return now - Date.parse(j.finishedAt) < SHOW_FINISHED_MS
  })
  if (visible.length === 0) return null
  const finished = visible.filter((j) => j.state !== "running")

  return (
    <div className="flex flex-col gap-2 rounded-lg border bg-muted/20 p-3">
      <div className="flex items-center gap-2">
        <span className="label-caps text-[0.65rem] text-muted-foreground">{t("fileJobs.title")}</span>
        {finished.length > 0 && (
          <Button
            size="sm"
            variant="ghost"
            className="ml-auto h-6 px-2 text-xs"
            onClick={() => setHidden((h) => new Set([...h, ...finished.map((j) => j.id)]))}
          >
            {t("fileJobs.clear")}
          </Button>
        )}
      </div>
      {visible.map((j) => (
        <JobRow
          key={j.id}
          job={j}
          now={now}
          onCancel={() =>
            cancel.mutate(j.id, {
              onError: (e) => toast.error(describeError(t, e)),
            })
          }
          onOpenFolder={() => onOpenFolder(j.file ? parentPath(j.file) : j.dir)}
        />
      ))}
    </div>
  )
}

function JobRow({ job, now, onCancel, onOpenFolder }: { job: FileJob; now: number; onCancel: () => void; onOpenFolder: () => void }) {
  const { t } = useTranslation()
  const running = job.state === "running"
  const elapsed = Math.max(1, (now - Date.parse(job.startedAt)) / 1000)
  const speed = job.bytes / elapsed
  const known = job.total > 0
  const pct = known ? Math.min(100, (job.bytes / job.total) * 100) : 0
  const eta = known && speed > 0 ? Math.max(0, (job.total - job.bytes) / speed) : null
  const KindIcon = job.kind === "download" ? DownloadCloudIcon : FolderArchiveIcon

  let detail: string
  if (running) {
    const parts = [known ? `${formatBytes(job.bytes)} / ${formatBytes(job.total)}` : formatBytes(job.bytes)]
    if (job.bytes > 0) parts.push(`${formatBytes(Math.round(speed))}/s`)
    if (eta !== null && job.phase === "downloading") parts.push(t("fileJobs.eta", { time: formatEta(eta) }))
    detail = parts.join(" · ")
  } else if (job.state === "done") {
    detail = t("fileJobs.done")
  } else if (job.state === "cancelled") {
    detail = t("fileJobs.cancelled")
  } else {
    detail = job.error ?? t("fileJobs.failed")
  }

  return (
    <div className="flex flex-col gap-1.5 rounded-md bg-background/60 px-3 py-2">
      <div className="flex min-w-0 items-center gap-2 text-sm">
        <KindIcon className="size-4 shrink-0 text-primary" />
        <span className="min-w-0 truncate font-mono text-xs" title={job.name}>
          {job.name}
        </span>
        <span className="shrink-0 text-xs text-muted-foreground">
          · {t(job.phase === "downloading" ? "fileJobs.downloading" : "fileJobs.extracting")}
        </span>
        <span className="ml-auto flex shrink-0 items-center gap-1">
          {running && <Loader2Icon className="size-3.5 animate-spin text-muted-foreground" />}
          {job.state === "done" && <CheckCircle2Icon className="size-4 text-emerald-500" />}
          {job.state === "failed" && <XCircleIcon className="size-4 text-destructive" />}
          {job.state === "cancelled" && <CircleSlashIcon className="size-4 text-muted-foreground" />}
          {running ? (
            <Button size="icon-sm" variant="ghost" onClick={onCancel} aria-label={t("fileJobs.cancel")} title={t("fileJobs.cancel")}>
              <XIcon />
            </Button>
          ) : (
            job.state === "done" && (
              <Button size="sm" variant="ghost" className="h-7 px-2 text-xs" onClick={onOpenFolder}>
                <FolderOpenIcon />
                {t("fileJobs.openFolder")}
              </Button>
            )
          )}
        </span>
      </div>
      {running && (
        <div className="h-1.5 w-full overflow-hidden rounded bg-muted">
          {known ? (
            <div className="h-full bg-primary transition-[width]" style={{ width: `${pct}%` }} />
          ) : (
            <div className="h-full w-1/3 animate-pulse rounded bg-primary/70" />
          )}
        </div>
      )}
      <p
        className={cn(
          "text-xs",
          job.state === "failed" ? "text-destructive" : job.state === "done" ? "text-emerald-500" : "text-muted-foreground",
        )}
      >
        {detail}
      </p>
    </div>
  )
}

function formatEta(seconds: number): string {
  if (seconds < 60) return `${Math.ceil(seconds)} s`
  const m = Math.floor(seconds / 60)
  if (m < 60) return `${m} min`
  return `${Math.floor(m / 60)} h ${m % 60} min`
}
