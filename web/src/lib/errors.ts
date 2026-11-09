import type { TFunction } from "i18next"

import { isApiError } from "@/lib/api"

// Turns any thrown error into a translated, human message.
export function describeError(t: TFunction, e: unknown): string {
  if (isApiError(e)) {
    if (e.code === "network") return t("errors.network")
    if (e.code === "port_in_use" || e.code === "port_in_use_host") return describePortConflict(t, e.code, (e.params ?? {}) as PortConflictParams)
    const key = `errors.${e.code}`
    // Some codes (server_backend_error) interpolate the backend's explanation.
    const translated = t(key, { message: e.message })
    return translated === key ? e.message : translated
  }
  if (e instanceof Error && e.message) return e.message
  return t("errors.title")
}

/** Port conflict params as sent by the backend (errors and /ports/check). */
export interface PortConflictParams {
  port?: number
  suggestion?: number
  server?: string
  running?: boolean
  canForce?: boolean
}

/** "Port 30120 is used by server X. … Free port: 30121" */
export function describePortConflict(t: TFunction, code: string, p: PortConflictParams): string {
  let msg: string
  if (code === "port_in_use_host") {
    msg = t("errors.port_in_use_host", { port: p.port })
  } else {
    msg = p.server ? t("errors.port_in_use", { port: p.port, server: p.server }) : t("errors.port_in_use_other", { port: p.port })
    if (p.running) msg += " " + t("errors.port_in_use_running")
  }
  if (p.suggestion) msg += " " + t("errors.port_suggestion", { suggestion: p.suggestion })
  return msg
}

/** Reads port conflict details from an API error, if it is one. */
export function portConflictOf(e: unknown): (PortConflictParams & { code: string; field?: string }) | null {
  if (!isApiError(e) || (e.code !== "port_in_use" && e.code !== "port_in_use_host")) return null
  return { ...(e.params as PortConflictParams | undefined), code: e.code, field: e.field }
}
