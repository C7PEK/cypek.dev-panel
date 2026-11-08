import { HelpCircleIcon, Loader2Icon, PencilIcon } from "lucide-react"
import { type ReactNode, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { describeError } from "@/lib/errors"
import { MOD_LOADERS, PLUGIN_LOADERS, loaderName, useGameVersions, useSetModPlatform } from "@/lib/mods"
import type { ModPlatform } from "@/lib/types"
import { cn } from "@/lib/utils"

/** Native select styled like the inputs (works well on phones). */
export function NativeSelect({
  id,
  value,
  onChange,
  children,
  className,
  "aria-label": ariaLabel,
}: {
  id?: string
  value: string
  onChange: (v: string) => void
  children: ReactNode
  className?: string
  "aria-label"?: string
}) {
  return (
    <select
      id={id}
      value={value}
      aria-label={ariaLabel}
      onChange={(e) => onChange(e.target.value)}
      className={cn(
        "h-8 w-full rounded-lg border border-input bg-background px-2.5 text-sm text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        "dark:bg-input/30 [&>option]:bg-background [&>option]:text-foreground",
        className,
      )}
    >
      {children}
    </select>
  )
}

/** Shows what the panel thinks the server runs, with a way to correct it. */
export function PlatformCard({ serverId, platform, canWrite }: { serverId: string; platform: ModPlatform; canWrite: boolean }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const unknown = !platform.loader

  if (unknown) {
    return (
      <Card className="border-amber-500/30 bg-amber-500/5">
        <CardContent className="flex flex-col gap-4">
          <div className="flex items-start gap-3">
            <HelpCircleIcon className="mt-0.5 size-5 shrink-0 text-amber-400" />
            <div>
              <h3 className="font-heading text-base font-medium tracking-[0.04em] uppercase">{t("mods.platform.unknownTitle")}</h3>
              <p className="mt-1 text-sm text-muted-foreground">{t("mods.platform.unknownBody")}</p>
            </div>
          </div>
          {canWrite ? (
            <PlatformForm serverId={serverId} platform={platform} inline />
          ) : (
            <p className="text-xs text-muted-foreground">{t("mods.platform.askAdmin")}</p>
          )}
        </CardContent>
      </Card>
    )
  }

  return (
    <Card size="sm">
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex min-w-0 flex-wrap items-center gap-2 text-sm">
          <span className="label-caps text-[0.62rem] text-muted-foreground">{t("mods.platform.label")}</span>
          <span className="font-medium">{loaderName(platform.loader)}</span>
          {platform.gameVersion && (
            <>
              <span className="text-muted-foreground/50">·</span>
              <span className="font-mono">{platform.gameVersion}</span>
            </>
          )}
          {platform.dir && (
            <>
              <span className="text-muted-foreground/50">·</span>
              <span className="font-mono text-muted-foreground">{platform.dir.replace(/\/?$/, "/")}</span>
            </>
          )}
          <Badge variant="outline" className="text-[0.62rem]">
            {platform.overridden ? t("mods.platform.manual") : t("mods.platform.auto")}
          </Badge>
        </div>
        {canWrite && (
          <Button size="sm" variant="outline" onClick={() => setOpen(true)}>
            <PencilIcon />
            {t("mods.platform.change")}
          </Button>
        )}
      </CardContent>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("mods.platform.dialogTitle")}</DialogTitle>
            <DialogDescription>{t("mods.platform.dialogBody")}</DialogDescription>
          </DialogHeader>
          <PlatformForm serverId={serverId} platform={platform} onDone={() => setOpen(false)} />
        </DialogContent>
      </Dialog>
    </Card>
  )
}

function PlatformForm({
  serverId,
  platform,
  inline,
  onDone,
}: {
  serverId: string
  platform: ModPlatform
  inline?: boolean
  onDone?: () => void
}) {
  const { t } = useTranslation()
  const versions = useGameVersions(true)
  const save = useSetModPlatform(serverId)
  // Start from the current override; "" means "detect automatically".
  const [loader, setLoader] = useState(platform.overridden ? platform.loader : "")
  const [gameVersion, setGameVersion] = useState(platform.overridden ? platform.gameVersion : "")

  function submit() {
    save.mutate(
      { loader, gameVersion },
      {
        onSuccess: () => {
          toast.success(t("mods.platform.saved"))
          onDone?.()
        },
        onError: (e) => toast.error(describeError(t, e)),
      },
    )
  }

  const versionList = versions.data ?? []
  // Keep a manually typed/unknown version selectable.
  const options = gameVersion && !versionList.includes(gameVersion) ? [gameVersion, ...versionList] : versionList

  const fields = (
    <FieldGroup className={cn(inline && "sm:flex-row sm:items-end")}>
      <Field>
        <FieldLabel htmlFor="mods-loader">{t("mods.platform.loader")}</FieldLabel>
        <NativeSelect id="mods-loader" value={loader} onChange={setLoader}>
          <option value="">{t("mods.platform.autoDetect")}</option>
          <optgroup label={t("mods.platform.pluginLoaders")}>
            {PLUGIN_LOADERS.map((l) => (
              <option key={l} value={l}>
                {loaderName(l)}
              </option>
            ))}
          </optgroup>
          <optgroup label={t("mods.platform.modLoaders")}>
            {MOD_LOADERS.map((l) => (
              <option key={l} value={l}>
                {loaderName(l)}
              </option>
            ))}
          </optgroup>
        </NativeSelect>
      </Field>
      <Field>
        <FieldLabel htmlFor="mods-version">{t("mods.platform.gameVersion")}</FieldLabel>
        <NativeSelect id="mods-version" value={gameVersion} onChange={setGameVersion}>
          <option value="">{t("mods.platform.autoDetect")}</option>
          {options.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </NativeSelect>
      </Field>
      {inline && (
        <Button onClick={submit} disabled={save.isPending} className="shrink-0">
          {save.isPending && <Loader2Icon className="animate-spin" />}
          {t("mods.platform.save")}
        </Button>
      )}
    </FieldGroup>
  )

  if (inline) return fields
  return (
    <>
      {fields}
      <DialogFooter>
        <Button variant="outline" onClick={onDone}>
          {t("mods.cancel")}
        </Button>
        <Button onClick={submit} disabled={save.isPending}>
          {save.isPending && <Loader2Icon className="animate-spin" />}
          {t("mods.platform.save")}
        </Button>
      </DialogFooter>
    </>
  )
}
