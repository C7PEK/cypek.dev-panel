import { ArrowUpCircleIcon, CheckCircle2Icon, Loader2Icon } from "lucide-react"
import { useState } from "react"
import { type TFunction } from "i18next"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { describeError } from "@/lib/errors"
import { isInstalling, useGameUpdate, useGameUpdateInfo } from "@/lib/servers"
import type { GameUpdateInfo, GameVersion, Server } from "@/lib/types"

function versionText(t: TFunction, v?: GameVersion) {
  if (!v) return "-"
  const parts: string[] = []
  if (v.build) parts.push(t("gameUpdate.build", { build: v.build }))
  if (v.txAdmin) parts.push(`txAdmin ${v.txAdmin}`)
  return parts.length ? parts.join(", ") : "-"
}

function isFiveM(info?: GameUpdateInfo) {
  return info?.kind === "fivem" || info?.kind === "fivem-enhanced"
}

export function GameUpdateButton({ server, size, className, info: given }: { server: Server; size?: "sm" | "default"; className?: string; info?: GameUpdateInfo }) {
  const { t } = useTranslation()
  const fetched = useGameUpdateInfo(server.id, !given && !!server.gameUpdate)
  const info = given ?? fetched.data
  const update = useGameUpdate(server.id)
  const [open, setOpen] = useState(false)
  const [restart, setRestart] = useState(true)
  const busy = isInstalling(server) || update.isPending

  function run() {
    update.mutate(restart, {
      onSuccess: () => {
        toast.success(t("gameUpdate.started"))
        setOpen(false)
      },
      onError: (e) => toast.error(describeError(t, e)),
    })
  }

  return (
    <>
      <Button type="button" variant={info?.upToDate === false ? "default" : "outline"} size={size} className={className} disabled={busy} onClick={() => setOpen(true)}>
        {busy ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
        {t("gameUpdate.button")}
      </Button>
      <Dialog open={open} onOpenChange={(o) => !update.isPending && setOpen(o)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("gameUpdate.confirmTitle")}</DialogTitle>
            <DialogDescription>{isFiveM(info) ? t("gameUpdate.confirmFiveM") : t("gameUpdate.confirm")}</DialogDescription>
          </DialogHeader>
          {info?.latest && (
            <p className="text-sm">
              <span className="text-muted-foreground">{t("gameUpdate.installed")}: </span>
              {versionText(t, info.current)}
              <br />
              <span className="text-muted-foreground">{t("gameUpdate.latest")}: </span>
              {versionText(t, info.latest)}
            </p>
          )}
          <Field orientation="horizontal">
            <FieldContent>
              <FieldLabel htmlFor={`game-restart-${server.id}`}>{t("gameUpdate.restart")}</FieldLabel>
              <FieldDescription>{t("gameUpdate.restartHelp")}</FieldDescription>
            </FieldContent>
            <Switch id={`game-restart-${server.id}`} checked={restart} onCheckedChange={setRestart} />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={update.isPending}>
              {t("servers.cancel")}
            </Button>
            <Button type="button" onClick={run} disabled={update.isPending}>
              {update.isPending ? <Loader2Icon className="animate-spin" /> : <ArrowUpCircleIcon />}
              {t("gameUpdate.run")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function GameUpdateCard({ server }: { server: Server }) {
  const { t } = useTranslation()
  const info = useGameUpdateInfo(server.id, true)
  const d = info.data
  const fivem = isFiveM(d)

  return (
    <Card>
      <CardHeader>
        <div className="flex flex-wrap items-center gap-2">
          <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{fivem ? t("gameUpdate.titleFiveM") : t("gameUpdate.title")}</CardTitle>
          {d?.upToDate === true && (
            <Badge variant="outline" className="gap-1">
              <CheckCircle2Icon className="size-3.5 text-emerald-500" />
              {t("gameUpdate.upToDate")}
            </Badge>
          )}
          {d?.upToDate === false && <Badge>{t("gameUpdate.available")}</Badge>}
        </div>
        <CardDescription>{fivem ? t("gameUpdate.descriptionFiveM") : t("gameUpdate.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {fivem && (
          <dl className="grid gap-x-6 gap-y-1 text-sm sm:grid-cols-[auto_1fr]">
            <dt className="text-muted-foreground">{t("gameUpdate.installed")}</dt>
            <dd>{versionText(t, d?.current)}</dd>
            <dt className="text-muted-foreground">{t("gameUpdate.latest")}</dt>
            <dd>
              {info.isPending ? t("app.loading") : versionText(t, d?.latest)}
              {d?.channel && <span className="text-muted-foreground"> ({d.channel})</span>}
            </dd>
          </dl>
        )}
        {d?.checkError && <p className="text-sm text-muted-foreground">{t("gameUpdate.checkFailed", { error: d.checkError })}</p>}
        {d?.kind === "fivem" && <p className="text-xs text-muted-foreground">{t("gameUpdate.channelHint")}</p>}
        <div className="flex justify-end">
          <GameUpdateButton server={server} info={d} />
        </div>
      </CardContent>
    </Card>
  )
}
