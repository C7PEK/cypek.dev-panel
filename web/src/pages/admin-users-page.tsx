import { KeyRoundIcon, PlusIcon, ShieldCheckIcon } from "lucide-react"
import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { AppShell } from "@/components/layout/app-shell"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import { useCreateUser, useDeleteUser, useSecurity, useSetSecurity, useUpdateUser, useUsers } from "@/lib/users"
import type { Role, TotpPolicy, User } from "@/lib/types"

export function AdminUsersPage({ user }: { user: User }) {
  const { t, i18n } = useTranslation()
  const users = useUsers()
  const create = useCreateUser()
  const update = useUpdateUser()
  const del = useDeleteUser()
  const isOwner = user.role === "owner"

  const [createOpen, setCreateOpen] = useState(false)
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [role, setRole] = useState<Role>("user")
  const [resetFor, setResetFor] = useState<User | null>(null)
  const [newPw, setNewPw] = useState("")
  const [deleteFor, setDeleteFor] = useState<User | null>(null)

  const fieldErr = (m: typeof create | typeof update, f: string) =>
    isApiError(m.error) && m.error.field === f ? describeError(t, m.error) : undefined

  function submitCreate(e: FormEvent) {
    e.preventDefault()
    create.mutate(
      { username: username.trim(), password, role },
      {
        onSuccess: () => {
          toast.success(t("users.created"))
          setCreateOpen(false)
          setUsername("")
          setPassword("")
          setRole("user")
        },
      },
    )
  }

  const canManage = (target: User) => target.role !== "owner" && target.id !== user.id && (isOwner || target.role === "user")

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("users.title")}</h1>
          <Button onClick={() => setCreateOpen(true)}>
            <PlusIcon /> {t("users.create")}
          </Button>
        </div>

        {users.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, users.error)}</AlertDescription>
          </Alert>
        )}
        <Card>
          <CardContent className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="label-caps text-left text-[0.62rem] text-muted-foreground">
                <tr>
                  <th className="py-1 pr-3">{t("users.username")}</th>
                  <th className="py-1 pr-3">{t("users.role")}</th>
                  <th className="py-1 pr-3">{t("users.status")}</th>
                  <th className="py-1 pr-3">{t("users.lastLogin")}</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {(users.data ?? []).map((u) => (
                  <tr key={u.id} className="border-t">
                    <td className="py-2 pr-3 font-medium">
                      {u.username}
                      {u.id === user.id && <span className="ml-2 text-xs text-muted-foreground">({t("users.you")})</span>}
                    </td>
                    <td className="py-2 pr-3">
                      <Badge variant={u.role === "owner" ? "default" : u.role === "admin" ? "secondary" : "outline"}>{t(`roles.${u.role}`)}</Badge>
                    </td>
                    <td className="py-2 pr-3">
                      <div className="flex flex-wrap gap-1">
                        {u.totpEnabled && (
                          <Badge variant="outline" className="gap-1 border-emerald-500/40 text-emerald-300">
                            <ShieldCheckIcon /> 2FA
                          </Badge>
                        )}
                        {u.disabled && <Badge variant="destructive">{t("users.disabled")}</Badge>}
                      </div>
                    </td>
                    <td className="py-2 pr-3 text-muted-foreground">{u.lastLoginAt ? formatDateTime(u.lastLoginAt, i18n.language) : t("users.never")}</td>
                    <td className="py-2 text-right">
                      {canManage(u) && (
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button size="sm" variant="outline">
                              {t("users.actions")}
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            {isOwner && (
                              <DropdownMenuItem
                                onSelect={() =>
                                  update.mutate(
                                    { id: u.id, role: u.role === "admin" ? "user" : "admin" },
                                    { onError: (e) => toast.error(describeError(t, e)) },
                                  )
                                }
                              >
                                {u.role === "admin" ? t("users.makeUser") : t("users.makeAdmin")}
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuItem
                              onSelect={() =>
                                update.mutate({ id: u.id, disabled: !u.disabled }, { onError: (e) => toast.error(describeError(t, e)) })
                              }
                            >
                              {u.disabled ? t("users.enable") : t("users.disable")}
                            </DropdownMenuItem>
                            <DropdownMenuItem onSelect={() => setResetFor(u)}>
                              <KeyRoundIcon /> {t("users.resetPassword")}
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                            <DropdownMenuItem variant="destructive" onSelect={() => setDeleteFor(u)}>
                              {t("users.delete")}
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>

      </div>

      <Dialog open={createOpen} onOpenChange={(o) => { setCreateOpen(o); if (!o) create.reset() }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("users.create")}</DialogTitle>
            <DialogDescription>{t("users.createDescription")}</DialogDescription>
          </DialogHeader>
          <form id="create-user" onSubmit={submitCreate} noValidate>
            <FieldGroup>
              <Field data-invalid={!!fieldErr(create, "username") || undefined}>
                <FieldLabel htmlFor="cu-name">{t("users.username")}</FieldLabel>
                <Input id="cu-name" value={username} onChange={(e) => setUsername(e.target.value)} autoComplete="off" autoFocus />
                <FieldDescription>{t("setup.usernameHelp")}</FieldDescription>
                <FieldError>{fieldErr(create, "username")}</FieldError>
              </Field>
              <Field data-invalid={!!fieldErr(create, "password") || undefined}>
                <FieldLabel htmlFor="cu-pw">{t("users.password")}</FieldLabel>
                <Input id="cu-pw" type="password" value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
                <FieldDescription>{t("setup.passwordHelp")}</FieldDescription>
                <FieldError>{fieldErr(create, "password")}</FieldError>
              </Field>
              <Field>
                <FieldLabel htmlFor="cu-role">{t("users.role")}</FieldLabel>
                <select id="cu-role" value={role} onChange={(e) => setRole(e.target.value as Role)} className="h-8 rounded-lg border bg-background px-2 text-sm text-foreground">
                  <option value="user">{t("roles.user")}</option>
                  {isOwner && <option value="admin">{t("roles.admin")}</option>}
                </select>
                <FieldDescription>{t("users.roleHelp")}</FieldDescription>
              </Field>
              {create.error && !(isApiError(create.error) && create.error.field) && (
                <Alert variant="destructive">
                  <AlertDescription>{describeError(t, create.error)}</AlertDescription>
                </Alert>
              )}
            </FieldGroup>
          </form>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setCreateOpen(false)}>
              {t("profile.cancel")}
            </Button>
            <Button type="submit" form="create-user" disabled={create.isPending || !username.trim() || !password}>
              {create.isPending ? t("profile.saving") : t("users.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!resetFor} onOpenChange={(o) => { if (!o) { setResetFor(null); setNewPw(""); update.reset() } }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("users.resetPassword")}</DialogTitle>
            <DialogDescription>{t("users.resetBody", { name: resetFor?.username ?? "" })}</DialogDescription>
          </DialogHeader>
          <Field data-invalid={!!fieldErr(update, "password") || undefined}>
            <FieldLabel htmlFor="rp-pw">{t("users.newPassword")}</FieldLabel>
            <Input id="rp-pw" type="password" value={newPw} onChange={(e) => setNewPw(e.target.value)} autoComplete="new-password" autoFocus />
            <FieldError>{fieldErr(update, "password")}</FieldError>
          </Field>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setResetFor(null)}>
              {t("profile.cancel")}
            </Button>
            <Button
              disabled={update.isPending || newPw.length < 8}
              onClick={() =>
                resetFor &&
                update.mutate(
                  { id: resetFor.id, password: newPw },
                  {
                    onSuccess: () => {
                      toast.success(t("users.passwordReset"))
                      setResetFor(null)
                      setNewPw("")
                    },
                  },
                )
              }
            >
              {t("users.resetPassword")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <Dialog open={!!deleteFor} onOpenChange={(o) => !o && setDeleteFor(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("users.deleteTitle")}</DialogTitle>
            <DialogDescription>{t("users.deleteBody", { name: deleteFor?.username ?? "" })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDeleteFor(null)}>
              {t("profile.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={del.isPending}
              onClick={() =>
                deleteFor &&
                del.mutate(deleteFor.id, {
                  onSuccess: () => {
                    toast.success(t("users.deleted"))
                    setDeleteFor(null)
                  },
                  onError: (e) => toast.error(describeError(t, e)),
                })
              }
            >
              {t("users.delete")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </AppShell>
  )
}

export function SecurityCard({ owner }: { owner: User }) {
  const { t } = useTranslation()
  const sec = useSecurity()
  const set = useSetSecurity()
  const policies: TotpPolicy[] = ["none", "admins", "all"]
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("security.title")}</CardTitle>
        <CardDescription>{t("security.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {!owner.totpEnabled && (
          <Alert>
            <AlertTitle>{t("security.ownerFirstTitle")}</AlertTitle>
            <AlertDescription>{t("security.ownerFirstBody")}</AlertDescription>
          </Alert>
        )}
        {sec.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, sec.error)}</AlertDescription>
          </Alert>
        )}
        <div role="radiogroup" className="flex flex-col gap-2">
          {policies.map((p) => (
            <label key={p} className="flex items-start gap-2 text-sm">
              <input
                type="radio"
                name="require-totp"
                className="mt-0.5 size-4 accent-primary"
                checked={(sec.data?.requireTotp ?? "none") === p}
                disabled={set.isPending || !owner.totpEnabled}
                onChange={() => set.mutate(p, { onSuccess: () => toast.success(t("security.saved")), onError: (e) => toast.error(describeError(t, e)) })}
              />
              <span>
                <span className="block">{t(`security.policies.${p}`)}</span>
                <span className="block text-xs text-muted-foreground">{t(`security.policyHelp.${p}`)}</span>
              </span>
            </label>
          ))}
        </div>
      </CardContent>
    </Card>
  )
}
