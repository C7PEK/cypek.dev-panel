import { ScrollTextIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Badge } from "@/components/ui/badge"
import { sourceKind } from "@/lib/templates"
import type { Template } from "@/lib/types"
import { cn } from "@/lib/utils"

const STYLES: Record<string, string> = {
  builtin: "text-muted-foreground",
  custom: "border-[#5865f2]/40 bg-[#5865f2]/10 text-[#a3abff]",
  pterodactyl: "border-sky-500/40 bg-sky-500/10 text-sky-300",
  pelican: "border-teal-500/40 bg-teal-500/10 text-teal-300",
  pufferpanel: "border-orange-500/40 bg-orange-500/10 text-orange-300",
}

/** Where a template comes from: built-in, custom or imported from another panel. */
export function SourceBadge({ template, className }: { template: Template; className?: string }) {
  const { t } = useTranslation()
  const kind = sourceKind(template)
  return (
    <Badge variant="outline" className={cn("label-caps text-[0.6rem]", STYLES[kind], className)}>
      {t(`templateLibrary.sources.${kind}`)}
      {template.overridesBuiltin && ` · ${t("templateLibrary.override")}`}
    </Badge>
  )
}

/** Shown when the install step runs a shell script. */
export function ScriptBadge({ className }: { className?: string }) {
  const { t } = useTranslation()
  return (
    <Badge variant="outline" className={cn("label-caps gap-1 border-amber-500/40 bg-amber-500/10 text-[0.6rem] text-amber-300", className)}>
      <ScrollTextIcon className="size-3" />
      {t("templateLibrary.usesScript")}
    </Badge>
  )
}
