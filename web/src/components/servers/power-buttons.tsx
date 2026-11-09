import { Loader2Icon, PlayIcon, RotateCcwIcon, SquareIcon, ZapOffIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"
import { canDo, usePower } from "@/lib/servers"
import type { PowerAction, Server } from "@/lib/types"
import { cn } from "@/lib/utils"

interface Props {
  server: Server
  /** Compact icon-only buttons for tiles. */
  compact?: boolean
  /** Show the force-stop button (with inline confirm). */
  withKill?: boolean
  className?: string
}

export function PowerButtons({ server, compact = false, withKill = false, className }: Props) {
  const { t } = useTranslation()
  const power = usePower(server.id)
  const [confirmKill, setConfirmKill] = useState(false)
  const can = canDo(server.status.state, server.installState)
  // An expired server is locked until an admin extends it.
  const allowed = server.expired ? { ...can, start: false, restart: false } : can
  const pending = power.isPending ? power.variables : null

  function run(action: PowerAction) {
    setConfirmKill(false)
    power.mutate(action, {
      onError: (e) => toast.error(t("power.failed", { message: describeError(t, e) })),
    })
  }

  const size = compact ? "icon-sm" : "sm"
  const btn = (action: PowerAction, Icon: typeof PlayIcon, variant: "default" | "outline" | "destructive") => (
    <Button
      key={action}
      size={size}
      variant={variant}
      disabled={!allowed[action] || power.isPending}
      onClick={(e) => {
        e.preventDefault()
        e.stopPropagation()
        run(action)
      }}
      aria-label={t(`power.${action}`)}
      title={server.expired && (action === "start" || action === "restart") ? t("expiry.expiredTitle") : t(`power.${action}`)}
    >
      {pending === action ? <Loader2Icon className="animate-spin" /> : <Icon />}
      {!compact && t(`power.${action}`)}
    </Button>
  )

  return (
    <div className={cn("flex flex-wrap items-center gap-1.5", className)}>
      {btn("start", PlayIcon, "default")}
      {btn("stop", SquareIcon, "outline")}
      {btn("restart", RotateCcwIcon, "outline")}
      {withKill &&
        (confirmKill ? (
          <span className="flex flex-wrap items-center gap-1.5 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1 text-xs text-destructive">
            {t("power.killConfirm")}
            <Button size="xs" variant="destructive" onClick={() => run("kill")}>
              {t("power.confirm")}
            </Button>
            <Button size="xs" variant="ghost" onClick={() => setConfirmKill(false)}>
              {t("power.cancel")}
            </Button>
          </span>
        ) : (
          <Button
            size={size}
            variant="destructive"
            disabled={!allowed.kill || power.isPending}
            onClick={() => setConfirmKill(true)}
            aria-label={t("power.kill")}
            title={t("power.kill")}
          >
            {pending === "kill" ? <Loader2Icon className="animate-spin" /> : <ZapOffIcon />}
            {!compact && t("power.kill")}
          </Button>
        ))}
    </div>
  )
}
