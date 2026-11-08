import { ArrowUpCircleIcon, Loader2Icon, PackageSearchIcon, Trash2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ModIcon } from "@/components/mods/mod-icon"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
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
import { formatBytes } from "@/lib/format"
import { useDeleteMod, useModUpdates, useToggleMod, useUpdateMod } from "@/lib/mods"
import type { InstalledMod, ModInstallResult } from "@/lib/types"
import { cn } from "@/lib/utils"

interface Props {
  serverId: string
  items: InstalledMod[]
  canWrite: boolean
  /** Label variant: "plugins" or "mods". */
  kind: "plugins" | "mods"
  onChanged: (result?: ModInstallResult) => void
  onBrowse: () => void
}

export function InstalledList({ serverId, items, canWrite, kind, onChanged, onBrowse }: Props) {
  const { t } = useTranslation()
  const updates = useModUpdates(serverId, items.some((m) => m.project))
  const update = useUpdateMod(serverId)
  const toggle = useToggleMod(serverId)
  const remove = useDeleteMod(serverId)
  const [deleting, setDeleting] = useState<InstalledMod | null>(null)
  const onErr = (e: unknown) => toast.error(describeError(t, e))
  const updateCount = Object.keys(updates.data ?? {}).length

  return (
    <section className="flex flex-col gap-3">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t(`mods.installed.title_${kind}`)}</h2>
          <p className="text-xs text-muted-foreground">
            {t("mods.installed.count", { count: items.length })}
            {updates.isFetching && (
              <span className="ml-2 inline-flex items-center gap-1">
                <Loader2Icon className="size-3 animate-spin" />
                {t("mods.installed.checking")}
              </span>
            )}
            {!updates.isFetching && updateCount > 0 && <span className="ml-2 text-primary">{t("mods.installed.updatesAvailable", { count: updateCount })}</span>}
          </p>
        </div>
        {items.some((m) => m.project) && (
          <Button size="sm" variant="ghost" onClick={() => void updates.refetch()} disabled={updates.isFetching}>
            {t("mods.installed.checkUpdates")}
          </Button>
        )}
      </div>

      {items.length === 0 ? (
        <div className="flex flex-col items-center gap-3 rounded-xl border border-dashed px-4 py-8 text-center">
          <PackageSearchIcon className="size-7 text-muted-foreground" />
          <p className="text-sm text-muted-foreground">{t(`mods.installed.empty_${kind}`)}</p>
          <Button size="sm" variant="outline" onClick={onBrowse}>
            {t("mods.installed.browse")}
          </Button>
        </div>
      ) : (
        <ul className="divide-y rounded-xl border">
          {items.map((m) => {
            const upd = updates.data?.[m.file]
            const updating = update.isPending && update.variables === m.file
            const toggling = toggle.isPending && toggle.variables?.file === m.file
            return (
              <li key={m.file} className={cn("flex flex-wrap items-center gap-3 px-3 py-2.5 sm:flex-nowrap", !m.enabled && "opacity-60")}>
                <ModIcon url={m.project?.iconUrl} className="size-9" />
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5">
                    <span className="truncate font-medium">{m.project?.title ?? m.file}</span>
                    {m.version && <span className="font-mono text-xs text-muted-foreground">{m.version.number}</span>}
                    {!m.project && <span className="text-xs text-muted-foreground/70">{t("mods.installed.notModrinth")}</span>}
                    {!m.enabled && (
                      <Badge variant="outline" className="text-[0.62rem]">
                        {t("mods.installed.disabled")}
                      </Badge>
                    )}
                    {upd && (
                      <Badge className="text-[0.62rem]" title={upd.name}>
                        {t("mods.installed.update", { version: upd.number })}
                      </Badge>
                    )}
                  </div>
                  <p className="truncate font-mono text-[0.68rem] text-muted-foreground">
                    {m.file} · {formatBytes(m.size)}
                  </p>
                </div>
                {canWrite && (
                  <div className="ml-auto flex items-center gap-1.5">
                    {upd && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={updating}
                        onClick={() =>
                          update.mutate(m.file, {
                            onSuccess: (res) => {
                              toast.success(t("mods.toast.updated", { name: m.project?.title ?? m.file, version: upd.number }))
                              onChanged(res)
                            },
                            onError: onErr,
                          })
                        }
                      >
                        {updating ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
                        <span className="hidden sm:inline">{t("mods.installed.updateButton")}</span>
                      </Button>
                    )}
                    <Switch
                      checked={m.enabled}
                      disabled={toggling}
                      aria-label={m.enabled ? t("mods.installed.disable") : t("mods.installed.enable")}
                      title={m.enabled ? t("mods.installed.disable") : t("mods.installed.enable")}
                      onCheckedChange={(enabled) =>
                        toggle.mutate(
                          { file: m.file, enabled },
                          {
                            onSuccess: () => {
                              toast.success(enabled ? t("mods.toast.enabled", { name: m.project?.title ?? m.file }) : t("mods.toast.disabled", { name: m.project?.title ?? m.file }))
                              onChanged()
                            },
                            onError: onErr,
                          },
                        )
                      }
                    />
                    <Button size="icon-sm" variant="ghost" aria-label={t("mods.installed.delete")} title={t("mods.installed.delete")} onClick={() => setDeleting(m)}>
                      <Trash2Icon />
                    </Button>
                  </div>
                )}
              </li>
            )
          })}
        </ul>
      )}

      <Dialog open={!!deleting} onOpenChange={(o) => !o && setDeleting(null)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("mods.delete.title")}</DialogTitle>
            <DialogDescription>{t("mods.delete.body", { name: deleting?.project?.title ?? deleting?.file ?? "" })}</DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleting(null)}>
              {t("mods.cancel")}
            </Button>
            <Button
              variant="destructive"
              disabled={remove.isPending}
              onClick={() => {
                if (!deleting) return
                const name = deleting.project?.title ?? deleting.file
                remove.mutate(deleting.file, {
                  onSuccess: () => {
                    toast.success(t("mods.toast.deleted", { name }))
                    setDeleting(null)
                    onChanged()
                  },
                  onError: onErr,
                })
              }}
            >
              {remove.isPending && <Loader2Icon className="animate-spin" />}
              {t("mods.delete.confirm")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </section>
  )
}
