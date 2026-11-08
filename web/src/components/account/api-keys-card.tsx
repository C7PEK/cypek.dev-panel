import { CopyIcon, KeyRoundIcon, PlusIcon, Trash2Icon } from "lucide-react"
import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useApiKeys, useCreateApiKey, useDeleteApiKey } from "@/lib/account"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import { PERMISSIONS } from "@/lib/users"
import type { ApiKey } from "@/lib/types"

const EXPIRY = [0, 30, 90, 365]

export function ApiKeysCard() {
  const { t, i18n } = useTranslation()
  const keys = useApiKeys()
  const create = useCreateApiKey()
  const del = useDeleteApiKey()
  const [open, setOpen] = useState(false)
  const [name, setName] = useState("")
  const [all, setAll] = useState(true)
  const [scope, setScope] = useState<string[]>([])
  const [expires, setExpires] = useState(0)
  const [token, setToken] = useState<string | null>(null)
  const [toDelete, setToDelete] = useState<ApiKey | null>(null)

  function reset() {
    setName("")
    setAll(true)
    setScope([])
    setExpires(0)
    create.reset()
  }

  function submit(e: FormEvent) {
    e.preventDefault()
    create.mutate(
      { name: name.trim(), scope: all ? ["*"] : scope, expiresInDays: expires },
      {
        onSuccess: (r) => {
          setToken(r.token)
          setOpen(false)
          reset()
        },
      },
    )
  }

  const fieldErr = (f: string) => (isApiError(create.error) && create.error.field === f ? describeError(t, create.error) : undefined)
  const curl = `curl -H "Authorization: Bearer ${token ?? "hp_..."}" ${location.origin}/api/v1/servers`

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("apiKeys.title")}</CardTitle>
        <CardDescription>{t("apiKeys.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {token && (
          <Alert>
            <AlertTitle>{t("apiKeys.tokenTitle")}</AlertTitle>
            <AlertDescription className="space-y-2">
              <p>{t("apiKeys.tokenBody")}</p>
              <div className="flex flex-wrap items-center gap-2">
                <code className="rounded bg-muted px-2 py-1 font-mono text-xs break-all">{token}</code>
                <Button
                  size="xs"
                  variant="outline"
                  onClick={() => void navigator.clipboard.writeText(token).then(() => toast.success(t("profile.copied")), () => toast.error(t("profile.copyFailed")))}
                >
                  <CopyIcon /> {t("profile.copy")}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">{t("apiKeys.usage")}</p>
              <pre className="overflow-x-auto rounded bg-muted p-2 font-mono text-xs">{curl}</pre>
              <Button size="sm" variant="ghost" onClick={() => setToken(null)}>
                {t("twoFactor.done")}
              </Button>
            </AlertDescription>
          </Alert>
        )}

        {keys.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, keys.error)}</AlertDescription>
          </Alert>
        )}
        {keys.data && keys.data.items.length === 0 && <p className="text-sm text-muted-foreground">{t("apiKeys.empty")}</p>}
        {keys.data && keys.data.items.length > 0 && (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="label-caps text-left text-[0.62rem] text-muted-foreground">
                <tr>
                  <th className="py-1 pr-3">{t("apiKeys.name")}</th>
                  <th className="py-1 pr-3">{t("apiKeys.scope")}</th>
                  <th className="py-1 pr-3">{t("apiKeys.created")}</th>
                  <th className="py-1 pr-3">{t("apiKeys.lastUsed")}</th>
                  <th className="py-1 pr-3">{t("apiKeys.expires")}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {keys.data.items.map((k) => (
                  <tr key={k.id} className="border-t">
                    <td className="py-2 pr-3">
                      <div className="font-medium">{k.name}</div>
                      <div className="font-mono text-xs text-muted-foreground">{k.prefix}…</div>
                    </td>
                    <td className="py-2 pr-3">
                      <div className="flex flex-wrap gap-1">
                        {k.scope.includes("*") ? (
                          <Badge variant="secondary">{t("apiKeys.scopeAll")}</Badge>
                        ) : (
                          k.scope.map((p) => (
                            <Badge key={p} variant="outline" className="font-mono text-[0.65rem]">
                              {p}
                            </Badge>
                          ))
                        )}
                      </div>
                    </td>
                    <td className="py-2 pr-3 text-muted-foreground">{formatDateTime(k.createdAt, i18n.language)}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{k.lastUsedAt ? formatDateTime(k.lastUsedAt, i18n.language) : t("apiKeys.never")}</td>
                    <td className="py-2 pr-3 text-muted-foreground">{k.expiresAt ? formatDateTime(k.expiresAt, i18n.language) : t("apiKeys.never")}</td>
                    <td className="py-2 text-right">
                      <Button size="icon-sm" variant="ghost" aria-label={t("apiKeys.delete")} onClick={() => setToDelete(k)}>
                        <Trash2Icon />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <div>
          <Button onClick={() => setOpen(true)}>
            <PlusIcon /> {t("apiKeys.create")}
          </Button>
        </div>

        <Dialog
          open={open}
          onOpenChange={(o) => {
            setOpen(o)
            if (!o) reset()
          }}
        >
          <DialogContent className="sm:max-w-lg">
            <DialogHeader>
              <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("apiKeys.create")}</DialogTitle>
              <DialogDescription>{t("apiKeys.createDescription")}</DialogDescription>
            </DialogHeader>
            <form id="apikey-form" onSubmit={submit} noValidate>
              <FieldGroup>
                <Field data-invalid={!!fieldErr("name") || undefined}>
                  <FieldLabel htmlFor="ak-name">{t("apiKeys.name")}</FieldLabel>
                  <Input id="ak-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="ci-deploy" maxLength={64} autoFocus />
                  <FieldError>{fieldErr("name")}</FieldError>
                </Field>
                <Field>
                  <FieldLabel>{t("apiKeys.scope")}</FieldLabel>
                  <label className="flex items-center gap-2 text-sm">
                    <input type="checkbox" className="size-4 accent-primary" checked={all} onChange={(e) => setAll(e.target.checked)} />
                    {t("apiKeys.scopeAll")}
                  </label>
                  {!all && (
                    <div className="grid grid-cols-2 gap-1 pl-1 sm:grid-cols-3">
                      {PERMISSIONS.map((p) => (
                        <label key={p} className="flex items-center gap-2 font-mono text-xs">
                          <input
                            type="checkbox"
                            className="size-4 accent-primary"
                            checked={scope.includes(p)}
                            onChange={(e) => setScope((s) => (e.target.checked ? [...s, p] : s.filter((x) => x !== p)))}
                          />
                          {p}
                        </label>
                      ))}
                    </div>
                  )}
                  <FieldDescription>{t("apiKeys.scopeHelp")}</FieldDescription>
                </Field>
                <Field>
                  <FieldLabel htmlFor="ak-exp">{t("apiKeys.expires")}</FieldLabel>
                  <select
                    id="ak-exp"
                    value={expires}
                    onChange={(e) => setExpires(Number(e.target.value))}
                    className="h-8 rounded-lg border bg-background px-2 text-sm text-foreground"
                  >
                    {EXPIRY.map((d) => (
                      <option key={d} value={d}>
                        {d === 0 ? t("apiKeys.never") : t("apiKeys.days", { count: d })}
                      </option>
                    ))}
                  </select>
                </Field>
                {create.error && !fieldErr("name") && (
                  <Alert variant="destructive">
                    <AlertDescription>{describeError(t, create.error)}</AlertDescription>
                  </Alert>
                )}
              </FieldGroup>
            </form>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                {t("profile.cancel")}
              </Button>
              <Button type="submit" form="apikey-form" disabled={create.isPending || !name.trim() || (!all && scope.length === 0)}>
                <KeyRoundIcon /> {create.isPending ? t("profile.saving") : t("apiKeys.create")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>

        <Dialog open={!!toDelete} onOpenChange={(o) => !o && setToDelete(null)}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>{t("apiKeys.deleteTitle")}</DialogTitle>
              <DialogDescription>{t("apiKeys.deleteBody", { name: toDelete?.name ?? "" })}</DialogDescription>
            </DialogHeader>
            <DialogFooter>
              <Button variant="ghost" onClick={() => setToDelete(null)}>
                {t("profile.cancel")}
              </Button>
              <Button
                variant="destructive"
                disabled={del.isPending}
                onClick={() =>
                  toDelete &&
                  del.mutate(toDelete.id, {
                    onSuccess: () => setToDelete(null),
                    onError: (e) => toast.error(describeError(t, e)),
                  })
                }
              >
                {t("apiKeys.delete")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}
