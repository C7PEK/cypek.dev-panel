import { useTranslation } from "react-i18next"

import { Badge } from "@/components/ui/badge"
import type { ServerState } from "@/lib/types"
import { cn } from "@/lib/utils"

const styles: Record<ServerState, string> = {
  running: "border-emerald-500/40 bg-emerald-500/15 text-emerald-300",
  starting: "border-amber-500/40 bg-amber-500/15 text-amber-300",
  stopping: "border-amber-500/40 bg-amber-500/15 text-amber-300",
  crashed: "border-red-500/40 bg-red-500/15 text-red-300",
  stopped: "border-border bg-muted text-muted-foreground",
  unknown: "border-border bg-muted text-muted-foreground",
}

const dot: Record<ServerState, string> = {
  running: "bg-emerald-400 shadow-[0_0_8px_rgb(52_211_153)]",
  starting: "bg-amber-400 brand-pulse",
  stopping: "bg-amber-400 brand-pulse",
  crashed: "bg-red-400",
  stopped: "bg-muted-foreground/60",
  unknown: "bg-muted-foreground/60",
}

export function StateBadge({ state, className }: { state: ServerState; className?: string }) {
  const { t } = useTranslation()
  return (
    <Badge variant="outline" className={cn("label-caps gap-1.5 text-[0.62rem]", styles[state], className)}>
      <span className={cn("size-1.5 rounded-full", dot[state])} />
      {t(`state.${state}`)}
    </Badge>
  )
}
