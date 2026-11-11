import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import type { ModInstallResult } from "@/lib/types"

export function useInstallToast() {
  const { t } = useTranslation()
  return (res: ModInstallResult, fallbackName: string) => {
    const main = res.installed.find((i) => !i.dependency) ?? res.installed[0]
    const deps = res.installed.filter((i) => i.dependency).length
    const name = main?.title || fallbackName
    const base = t("mods.toast.installed", { name, version: main?.version ?? "" })
    toast.success(deps > 0 ? `${base} ${t("mods.toast.withDeps", { count: deps })}` : base)
  }
}
