import { PlusIcon, Trash2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { PermissionPicker } from "@/components/admin/permission-picker"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { describeError } from "@/lib/errors"
import { PRESETS, presetOf, useServerGrants, useSetGrant, useUsers } from "@/lib/users"
import type { Permission, User, UserGrant } from "@/lib/types"

export function ServerUsersTab({ serverId }: { serverId: string }) {
  const { t } = useTranslation()
  const grants = useServerGrants(serverId)
  const users = useUsers()
  const set = useSetGrant(serverId)
  const [adding, setAdding] = useState<number | "">("")

  const granted = new Set(grants.data?.items.map((g) => g.user.id) ?? [])
  const candidates = (users.data ?? []).filter((u) => u.role === "user" && !granted.has(u.id))

  function add() {
    if (adding === "") return
    set.mutate(
      { userId: Number(adding), permissions: [...PRESETS.view] },
      { onSuccess: () => setAdding(""), onError: (e) => toast.error(describeError(t, e)) },
    )
  }

  return (
    <div className="flex flex-col gap-4">
      <Card>
        <CardHeader>
          <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("permissions.title")}</CardTitle>
          <CardDescription>{t("permissions.description")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          {(grants.error || users.error) && (
            <Alert variant="destructive">
              <AlertDescription>{describeError(t, grants.error ?? users.error)}</AlertDescription>
            </Alert>
          )}
          <div className="flex flex-wrap items-center gap-2">
            <select
              value={adding}
              onChange={(e) => setAdding(e.target.value === "" ? "" : Number(e.target.value))}
              className="h-8 min-w-48 rounded-lg border bg-background px-2 text-sm text-foreground"
              aria-label={t("permissions.addUser")}
            >
              <option value="">{candidates.length ? t("permissions.pickUser") : t("permissions.noCandidates")}</option>
              {candidates.map((u) => (
                <option key={u.id} value={u.id}>
                  {u.username}
                </option>
              ))}
            </select>
            <Button size="sm" onClick={add} disabled={adding === "" || set.isPending}>
              <PlusIcon /> {t("permissions.addUser")}
            </Button>
          </div>
          {grants.data && grants.data.items.length === 0 && <p className="text-sm text-muted-foreground">{t("permissions.empty")}</p>}
          {grants.data?.items.map((g) => (
            <GrantRow key={g.user.id} grant={g} serverId={serverId} />
          ))}
        </CardContent>
      </Card>
    </div>
  )
}

function GrantRow({ grant, serverId }: { grant: UserGrant; serverId: string }) {
  const { t } = useTranslation()
  const set = useSetGrant(serverId)
  const [perms, setPerms] = useState<Permission[]>(grant.permissions)
  const dirty = perms.length !== grant.permissions.length || perms.some((p) => !grant.permissions.includes(p))
  const preset = presetOf(grant.permissions)
  const u: User = grant.user
  return (
    <div className="rounded-lg border p-3">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="flex items-center gap-2">
          <span className="font-medium">{u.username}</span>
          <Badge variant="outline">{preset ? t(`permissions.presets.${preset}`) : t("permissions.custom")}</Badge>
          {u.disabled && <Badge variant="destructive">{t("users.disabled")}</Badge>}
        </div>
        <div className="flex gap-1.5">
          <Button
            size="sm"
            disabled={!dirty || set.isPending}
            onClick={() =>
              set.mutate(
                { userId: u.id, permissions: perms },
                { onSuccess: () => toast.success(t("permissions.saved")), onError: (e) => toast.error(describeError(t, e)) },
              )
            }
          >
            {t("permissions.save")}
          </Button>
          <Button
            size="sm"
            variant="destructive"
            disabled={set.isPending}
            onClick={() =>
              set.mutate(
                { userId: u.id, permissions: [] },
                { onSuccess: () => toast.success(t("permissions.removed")), onError: (e) => toast.error(describeError(t, e)) },
              )
            }
          >
            <Trash2Icon /> {t("permissions.remove")}
          </Button>
        </div>
      </div>
      <PermissionPicker value={perms} onChange={setPerms} disabled={set.isPending} />
    </div>
  )
}
