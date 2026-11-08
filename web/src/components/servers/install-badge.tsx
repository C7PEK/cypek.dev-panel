import { useTranslation } from "react-i18next"

import { Badge } from "@/components/ui/badge"
import type { InstallState } from "@/lib/types"
import { cn } from "@/lib/utils"

const styles: Record<Exclude<InstallState, "ready">, { badge: string; dot: string }> = {
  pending: { badge: "border-amber-500/40 bg-amber-500/15 text-amber-300", dot: "bg-amber-400 brand-pulse" },
  installing: { badge: "border-sky-500/40 bg-sky-500/15 text-sky-300", dot: "bg-sky-400 brand-pulse" },
  failed: { badge: "border-red-500/40 bg-red-500/15 text-red-300", dot: "bg-red-400" },
}

export function InstallBadge({ state, className }: { state: InstallState; className?: string }) {
  const { t } = useTranslation()
  if (state === "ready") return null
  const s = styles[state]
  return (
    <Badge variant="outline" className={cn("label-caps gap-1.5 text-[0.62rem]", s.badge, className)}>
      <span className={cn("size-1.5 rounded-full", s.dot)} />
      {t(`install.badge.${state}`)}
    </Badge>
  )
}
