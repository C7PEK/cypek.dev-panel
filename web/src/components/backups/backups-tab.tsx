import { ArchiveIcon, DownloadIcon, Loader2Icon, PlusIcon, RotateCcwIcon, Trash2Icon } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { Textarea } from "@/components/ui/textarea"
import { isApiError } from "@/lib/api"
import {
  type Backup,
  type BackupSettings,
  backupDownloadUrl,
  useBackups,
  useCreateBackup,
  useDeleteBackup,
  useRestoreBackup,
  useUpdateBackupSettings,
} from "@/lib/backups"
import { describeError } from "@/lib/errors"
import { formatBytes, formatDateTime } from "@/lib/format"
import type { Server } from "@/lib/types"

export function BackupsTab({ server }: { server: Server }) {
  const { t, i18n } = useTranslation()
  const list = useBackups(server.id)
  const create = useCreateBackup(server.id)
  const [note, setNote] = useState("")
  const [restoring, setRestoring] = useState<Backup | null>(null)
  const [deleting, setDeleting] = useState<Backup | null>(null)
  const stopped = server.status.state === "stopped" || server.status.state === "crashed"
  const job = list.data?.job
  const busy = !!job && !job.done
  const onErr = (e: unknown) => toast.error(describeError(t, e))

  function download(b: Backup) {
    const a = document.createElement("a")
    a.href = backupDownloadUrl(server.id, b.id)
    a.download = b.fileName
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("backups.title")}</h2>
          <p className="text-xs text-muted-foreground">{t("backups.subtitle")}</p>
        </div>
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault()
            create.mutate(note.trim(), {
              onSuccess: () => {
                setNote("")
                toast.success(t("backups.started"))
              },
              onError: onErr,
            })
          }}
        >
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder={t("backups.notePlaceholder")} maxLength={120} className="w-56" />
          <Button type="submit" disabled={busy || create.isPending || list.data?.writable === false}>
            {busy && job?.kind === "backup" ? <Loader2Icon className="animate-spin" /> : <PlusIcon />}
            {t("backups.create")}
          </Button>
        </form>
      </div>

      {list.data && !list.data.writable && (
        <Alert variant="destructive">
          <AlertTitle>{t("backups.notWritableTitle")}</AlertTitle>
          <AlertDescription>{t("backups.notWritableBody", { dir: list.data.dir })}</AlertDescription>
        </Alert>
      )}
      {job && !job.done && (
        <div className="flex items-center gap-2 rounded-lg border bg-muted/30 p-3 text-sm">
          <Loader2Icon className="size-4 animate-spin text-primary" />
          {t(job.kind === "backup" ? "backups.progressBackup" : "backups.progressRestore", { files: job.files, bytes: formatBytes(job.bytes) })}
        </div>
      )}
      {job?.error && (
        <Alert variant="destructive">
          <AlertTitle>{t(job.kind === "backup" ? "backups.failedBackup" : "backups.failedRestore")}</AlertTitle>
          <AlertDescription>{job.error}</AlertDescription>
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
            <ArchiveIcon className="size-8 text-muted-foreground" />
            <p className="font-heading text-lg uppercase tracking-[0.04em]">{t("backups.emptyTitle")}</p>
            <p className="max-w-md text-sm text-muted-foreground">{t("backups.emptyBody")}</p>
          </CardContent>
        </Card>
      ) : (
        <ul className="flex flex-col gap-2">
          {list.data.items.map((b) => (
            <li key={b.id} className="flex flex-wrap items-center gap-3 rounded-xl border bg-card p-3">
              <ArchiveIcon className="size-5 shrink-0 text-muted-foreground" />
              <div className="min-w-0 flex-1">
                <div className="flex flex-wrap items-center gap-2 text-sm">
                  <span className="font-mono">{formatDateTime(b.createdAt, i18n.language)}</span>
                  <span className="text-muted-foreground">{formatBytes(b.size)}</span>
                  <Badge variant="outline" className="label-caps text-[0.6rem]">
                    {t(`backups.triggers.${b.trigger}`)}
                  </Badge>
                </div>
                <div className="truncate text-xs text-muted-foreground">{b.note || b.fileName}</div>
              </div>
              <div className="flex items-center gap-1">
                <Button size="sm" variant="outline" onClick={() => download(b)}>
                  <DownloadIcon />
                  <span className="hidden sm:inline">{t("backups.download")}</span>
                </Button>
                <Button size="sm" variant="outline" disabled={!stopped || busy} title={!stopped ? t("backups.restoreNeedsStopped") : undefined} onClick={() => setRestoring(b)}>
                  <RotateCcwIcon />
                  <span className="hidden sm:inline">{t("backups.restore")}</span>
                </Button>
                <Button size="sm" variant="ghost" aria-label={t("backups.delete")} onClick={() => setDeleting(b)}>
                  <Trash2Icon />
                </Button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {list.data && (
        <>
          <SettingsCard serverId={server.id} settings={list.data.settings} dir={list.data.dir} isMinecraft={server.category === "minecraft"} />
          <RestoreDialog serverId={server.id} backup={restoring} onClose={() => setRestoring(null)} />
          <DeleteDialog serverId={server.id} backup={deleting} onClose={() => setDeleting(null)} />
        </>
      )}
    </div>
  )
}

function SettingsCard({ serverId, settings, dir, isMinecraft }: { serverId: string; settings: BackupSettings; dir: string; isMinecraft: boolean }) {
  const { t } = useTranslation()
  const update = useUpdateBackupSettings(serverId)
  const [excludes, setExcludes] = useState(settings.excludes.join("\n"))
  const [keepLast, setKeepLast] = useState(String(settings.keepLast))
  const [keepDays, setKeepDays] = useState(String(settings.keepDays))
  const [flush, setFlush] = useState(settings.flushMinecraft)
  useEffect(() => {
    setExcludes(settings.excludes.join("\n"))
    setKeepLast(String(settings.keepLast))
    setKeepDays(String(settings.keepDays))
    setFlush(settings.flushMinecraft)
  }, [settings])
  const fieldError = isApiError(update.error) && update.error.field === "excludes" ? describeError(t, update.error) : undefined
  const generalError = update.error && !fieldError ? describeError(t, update.error) : null

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-base font-medium tracking-[0.04em] uppercase">{t("backups.settingsTitle")}</CardTitle>
        <CardDescription>
          {t("backups.dir")}: <code className="font-mono">{dir}</code>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <form
          onSubmit={(e) => {
            e.preventDefault()
            update.mutate(
              {
                excludes: excludes
                  .split("\n")
                  .map((l) => l.trim())
                  .filter(Boolean),
                keepLast: Math.max(0, Number.parseInt(keepLast, 10) || 0),
                keepDays: Math.max(0, Number.parseInt(keepDays, 10) || 0),
                flushMinecraft: flush,
              },
              { onSuccess: () => toast.success(t("servers.saved")) },
            )
          }}
        >
          <FieldGroup>
            <Field data-invalid={!!fieldError || undefined}>
              <FieldLabel htmlFor="bk-ex">{t("backups.excludes")}</FieldLabel>
              <Textarea id="bk-ex" value={excludes} onChange={(e) => setExcludes(e.target.value)} className="min-h-20 font-mono text-xs" placeholder={"logs/\ncache/\n*.log"} spellCheck={false} />
              <FieldDescription>{t("backups.excludesHelp")}</FieldDescription>
              <FieldError>{fieldError}</FieldError>
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field>
                <FieldLabel htmlFor="bk-last">{t("backups.keepLast")}</FieldLabel>
                <Input id="bk-last" type="number" inputMode="numeric" min={0} value={keepLast} onChange={(e) => setKeepLast(e.target.value)} />
                <FieldDescription>{t("backups.zeroUnlimited")}</FieldDescription>
              </Field>
              <Field>
                <FieldLabel htmlFor="bk-days">{t("backups.keepDays")}</FieldLabel>
                <Input id="bk-days" type="number" inputMode="numeric" min={0} value={keepDays} onChange={(e) => setKeepDays(e.target.value)} />
                <FieldDescription>{t("backups.zeroUnlimited")}</FieldDescription>
              </Field>
            </div>
            {isMinecraft && (
              <Field orientation="horizontal">
                <FieldContent>
                  <FieldLabel htmlFor="bk-flush">{t("backups.flushMinecraft")}</FieldLabel>
                  <FieldDescription>{t("backups.flushMinecraftHelp")}</FieldDescription>
                </FieldContent>
                <Switch id="bk-flush" checked={flush} onCheckedChange={setFlush} />
              </Field>
            )}
            {generalError && (
              <Alert variant="destructive">
                <AlertDescription>{generalError}</AlertDescription>
              </Alert>
            )}
            <div className="flex justify-end">
              <Button type="submit" disabled={update.isPending}>
                {update.isPending ? t("servers.saving") : t("servers.save")}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}

function RestoreDialog({ serverId, backup, onClose }: { serverId: string; backup: Backup | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const restore = useRestoreBackup(serverId)
  return (
    <Dialog open={!!backup} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="font-heading uppercase tracking-[0.04em]">{t("backups.restoreTitle")}</DialogTitle>
          <DialogDescription>{backup && t("backups.restoreBody", { date: formatDateTime(backup.createdAt, i18n.language) })}</DialogDescription>
        </DialogHeader>
        <Alert variant="destructive">
          <AlertDescription>{t("backups.restoreWarning")}</AlertDescription>
        </Alert>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t("power.cancel")}
          </Button>
          <Button
            variant="destructive"
            disabled={restore.isPending}
            onClick={() =>
              backup &&
              restore.mutate(backup.id, {
                onSuccess: () => {
                  toast.success(t("backups.restoreStarted"))
                  onClose()
                },
                onError: (e) => toast.error(describeError(t, e)),
              })
            }
          >
            {t("backups.restoreConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

function DeleteDialog({ serverId, backup, onClose }: { serverId: string; backup: Backup | null; onClose: () => void }) {
  const { t, i18n } = useTranslation()
  const del = useDeleteBackup(serverId)
  return (
    <Dialog open={!!backup} onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="font-heading uppercase tracking-[0.04em]">{t("backups.deleteTitle")}</DialogTitle>
          <DialogDescription>{backup && t("backups.deleteBody", { date: formatDateTime(backup.createdAt, i18n.language), size: formatBytes(backup.size) })}</DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onClose}>
            {t("power.cancel")}
          </Button>
          <Button variant="destructive" disabled={del.isPending} onClick={() => backup && del.mutate(backup.id, { onSuccess: onClose, onError: (e) => toast.error(describeError(t, e)) })}>
            {t("power.confirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
