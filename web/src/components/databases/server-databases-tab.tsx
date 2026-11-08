import { DatabaseIcon, KeyRoundIcon, Loader2Icon, PlusIcon, RefreshCwIcon, Trash2Icon } from "lucide-react"
import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { CredentialsDialog } from "@/components/databases/credentials-dialog"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isApiError } from "@/lib/api"
import {
  type DatabaseCredentials,
  type ServerDatabase,
  SUFFIX_RE,
  fetchCredentials,
  previewName,
  useCreateDatabase,
  useDeleteDatabase,
  useRotatePassword,
  useServerDatabases,
} from "@/lib/databases"
import { describeError } from "@/lib/errors"
import { formatBytes, formatDateTime } from "@/lib/format"
import type { Server, User } from "@/lib/types"

export function ServerDatabasesTab({ server, user }: { server: Server; user: User }) {
  const { t, i18n } = useTranslation()
  const list = useServerDatabases(server.id)
  const rotate = useRotatePassword(server.id)
  const del = useDeleteDatabase(server.id)
  const [createOpen, setCreateOpen] = useState(false)
  const [creds, setCreds] = useState<DatabaseCredentials | null>(null)
  const [rotated, setRotated] = useState(false)
  const [loadingId, setLoadingId] = useState<number | null>(null)
  const [confirmRotate, setConfirmRotate] = useState<ServerDatabase | null>(null)
  const [deleting, setDeleting] = useState<ServerDatabase | null>(null)
  const [deleteText, setDeleteText] = useState("")
  const onErr = (e: unknown) => toast.error(describeError(t, e))

  if (list.error && isApiError(list.error) && (list.error.status === 403 || list.error.status === 404)) {
    return (
      <Alert>
        <AlertDescription>{t("database.noPermission")}</AlertDescription>
      </Alert>
    )
  }
  if (list.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>
          {describeError(t, list.error)}
          <Button variant="outline" size="sm" className="ml-3" onClick={() => void list.refetch()}>
            {t("app.retry")}
          </Button>
        </AlertDescription>
      </Alert>
    )
  }
  if (list.isPending) return <p className="text-sm text-muted-foreground">{t("app.loading")}</p>

  const status = list.data.status
  const items = list.data.items

  async function show(db: ServerDatabase) {
    setLoadingId(db.id)
    try {
      const c = await fetchCredentials(server.id, db.id)
      setRotated(false)
      setCreds(c)
    } catch (e) {
      onErr(e)
    } finally {
      setLoadingId(null)
    }
  }

  const header = (
    <div className="flex flex-wrap items-end justify-between gap-3">
      <div>
        <h2 className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("database.tabTitle")}</h2>
        <p className="text-xs text-muted-foreground">{t("database.tabSubtitle")}</p>
      </div>
      {status.installed && (
        <Button onClick={() => setCreateOpen(true)} disabled={!status.reachable}>
          <PlusIcon />
          {t("database.create")}
        </Button>
      )}
    </div>
  )

  if (!status.installed) {
    return (
      <div className="flex flex-col gap-4">
        {header}
        <Alert>
          <DatabaseIcon />
          <AlertTitle>{t("database.notInstalledTitle")}</AlertTitle>
          <AlertDescription>
            {user.role === "user" ? (
              t("database.notInstalledAskAdmin")
            ) : (
              <span>
                {t("database.notInstalledBody")}{" "}
                <Link to="/admin/database" className="underline underline-offset-4">
                  {t("database.goToAdmin")}
                </Link>
              </span>
            )}
          </AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="flex flex-col gap-4">
      {header}
      {!status.reachable && (
        <Alert variant="destructive">
          <AlertTitle>{t("database.unreachableTitle")}</AlertTitle>
          <AlertDescription>{status.error || t("errors.database_unavailable")}</AlertDescription>
        </Alert>
      )}
      <p className="text-xs text-muted-foreground">
        {server.category === "fivem"
          ? t("database.hintFivem")
          : server.category === "minecraft"
            ? t("database.hintMinecraft")
            : t("database.hintOther")}
      </p>
      {items.length === 0 ? (
        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center gap-3 py-10 text-center">
            <DatabaseIcon className="size-8 text-muted-foreground" />
            <p className="text-sm text-muted-foreground">{t("database.empty")}</p>
            <Button onClick={() => setCreateOpen(true)} disabled={!status.reachable}>
              <PlusIcon />
              {t("database.create")}
            </Button>
          </CardContent>
        </Card>
      ) : (
        <div className="flex flex-col gap-2">
          {items.map((db) => (
            <Card key={db.id}>
              <CardContent className="flex flex-wrap items-center justify-between gap-3">
                <div className="flex min-w-0 items-center gap-3">
                  <span className="flex size-9 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                    <DatabaseIcon className="size-4" />
                  </span>
                  <div className="min-w-0">
                    <div className="truncate font-mono text-sm font-medium">{db.name}</div>
                    <div className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                      {db.label && <span className="text-foreground/80">{db.label}</span>}
                      <span>{formatBytes(db.sizeBytes) || "0 B"}</span>
                      <span>{t("database.tables", { count: db.tables })}</span>
                      <span>{formatDateTime(db.createdAt, i18n.language)}</span>
                    </div>
                  </div>
                </div>
                <div className="flex flex-wrap gap-1.5">
                  <Button size="sm" variant="outline" onClick={() => void show(db)} disabled={loadingId === db.id}>
                    {loadingId === db.id ? <Loader2Icon className="animate-spin" /> : <KeyRoundIcon />}
                    {t("database.showCredentials")}
                  </Button>
                  <Button size="sm" variant="ghost" onClick={() => setConfirmRotate(db)} disabled={!status.reachable}>
                    <RefreshCwIcon />
                    {t("database.rotate")}
                  </Button>
                  <Button
                    size="sm"
                    variant="destructive"
                    onClick={() => {
                      setDeleteText("")
                      setDeleting(db)
                    }}
                    disabled={!status.reachable}
                  >
                    <Trash2Icon />
                    {t("database.delete")}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ))}
        </div>
      )}

      <CreateDialog
        serverId={server.id}
        suggested={list.data?.suggestedName ?? ""}
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(c) => {
          setRotated(false)
          setCreds(c)
        }}
      />
      <CredentialsDialog creds={creds} category={server.category} rotated={rotated} onClose={() => setCreds(null)} />

      <Dialog open={!!confirmRotate} onOpenChange={(o) => !o && setConfirmRotate(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("database.rotateTitle")}</DialogTitle>
            <DialogDescription>{t("database.rotateBody", { name: confirmRotate?.name })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setConfirmRotate(null)}>
              {t("database.cancel")}
            </Button>
            <Button
              disabled={rotate.isPending}
              onClick={() =>
                confirmRotate &&
                rotate.mutate(confirmRotate.id, {
                  onSuccess: (c) => {
                    setConfirmRotate(null)
                    setRotated(true)
                    setCreds(c)
                  },
                  onError: onErr,
                })
              }
            >
              {rotate.isPending && <Loader2Icon className="animate-spin" />}
              {t("database.rotateConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("database.deleteTitle")}</DialogTitle>
            <DialogDescription>{t("database.deleteBody", { name: deleting?.name })}</DialogDescription>
          </DialogHeader>
          <Field>
            <FieldLabel htmlFor="db-delete-confirm">{t("database.deleteTypeName", { name: deleting?.name })}</FieldLabel>
            <Input
              id="db-delete-confirm"
              value={deleteText}
              onChange={(e) => setDeleteText(e.target.value)}
              className="font-mono"
              autoComplete="off"
              spellCheck={false}
            />
          </Field>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleting(null)}>
              {t("database.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={!deleting || deleteText !== deleting.name || del.isPending}
              onClick={() =>
                deleting &&
                del.mutate(deleting.id, {
                  onSuccess: () => {
                    toast.success(t("database.deleted"))
                    setDeleting(null)
                  },
                  onError: onErr,
                })
              }
            >
              {del.isPending && <Loader2Icon className="animate-spin" />}
              {t("database.deleteConfirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function CreateDialog({
  serverId,
  suggested,
  open,
  onOpenChange,
  onCreated,
}: {
  serverId: string
  suggested: string
  open: boolean
  onOpenChange: (o: boolean) => void
  onCreated: (c: DatabaseCredentials) => void
}) {
  const { t } = useTranslation()
  const create = useCreateDatabase(serverId)
  const [name, setName] = useState("")
  const [label, setLabel] = useState("")
  const [localErr, setLocalErr] = useState<string | null>(null)
  const apiField = isApiError(create.error) ? create.error.field : undefined
  const apiMsg = create.error ? describeError(t, create.error) : undefined

  function close(o: boolean) {
    if (!o) {
      setName("")
      setLabel("")
      setLocalErr(null)
      create.reset()
    }
    onOpenChange(o)
  }

  function submit(e: FormEvent) {
    e.preventDefault()
    const suffix = name.trim().toLowerCase()
    if (suffix && !SUFFIX_RE.test(suffix)) {
      setLocalErr(t("database.nameInvalid"))
      return
    }
    create.mutate(
      { name: suffix, label: label.trim() },
      {
        onSuccess: (c) => {
          toast.success(t("database.created"))
          close(false)
          onCreated(c)
        },
      },
    )
  }

  const nameErr = localErr ?? (apiField === "name" ? apiMsg : undefined)
  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("database.createTitle")}</DialogTitle>
          <DialogDescription>{t("database.createBody")}</DialogDescription>
        </DialogHeader>
        <form id="db-create" onSubmit={submit} noValidate>
          <FieldGroup>
            <Field data-invalid={!!nameErr || undefined}>
              <FieldLabel htmlFor="db-name">{t("database.name")}</FieldLabel>
              <Input
                id="db-name"
                value={name}
                onChange={(e) => {
                  setName(e.target.value)
                  setLocalErr(null)
                }}
                placeholder={suggested || "esx"}
                maxLength={24}
                className="font-mono"
                autoComplete="off"
                spellCheck={false}
                aria-invalid={!!nameErr}
                autoFocus
              />
              <FieldDescription>
                {t("database.namePreview")} <code className="font-mono text-foreground">{previewName(name, suggested)}</code>
              </FieldDescription>
              <FieldError>{nameErr}</FieldError>
            </Field>
            <Field data-invalid={apiField === "label" || undefined}>
              <FieldLabel htmlFor="db-label">{t("database.label")}</FieldLabel>
              <Input
                id="db-label"
                value={label}
                onChange={(e) => setLabel(e.target.value)}
                maxLength={80}
                placeholder={t("database.labelPlaceholder")}
              />
              <FieldError>{apiField === "label" ? apiMsg : undefined}</FieldError>
            </Field>
            {create.error && !apiField && (
              <Alert variant="destructive">
                <AlertDescription>{apiMsg}</AlertDescription>
              </Alert>
            )}
          </FieldGroup>
        </form>
        <DialogFooter>
          <Button variant="ghost" onClick={() => close(false)} disabled={create.isPending}>
            {t("database.cancel")}
          </Button>
          <Button type="submit" form="db-create" disabled={create.isPending}>
            {create.isPending && <Loader2Icon className="animate-spin" />}
            {t("database.createConfirm")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
