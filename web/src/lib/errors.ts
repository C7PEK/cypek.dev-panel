import type { TFunction } from "i18next"

import { isApiError } from "@/lib/api"

// Turns any thrown error into a translated, human message.
export function describeError(t: TFunction, e: unknown): string {
  if (isApiError(e)) {
    if (e.code === "network") return t("errors.network")
    const key = `errors.${e.code}`
    const translated = t(key)
    return translated === key ? e.message : translated
  }
  if (e instanceof Error && e.message) return e.message
  return t("errors.title")
}
