import { useEffect, useState } from "react"

import { isApiError } from "@/lib/api"
import { validateTemplate } from "@/lib/templates"
import type { Template } from "@/lib/types"

export type ValidationState =
  | { status: "idle" }
  | { status: "checking" }
  | { status: "ok"; template: Template }
  | { status: "error"; message: string }

/** Debounced server-side validation of a template YAML. */
export function useYamlValidation(yaml: string, enabled = true): ValidationState {
  const [state, setState] = useState<ValidationState>({ status: "idle" })
  useEffect(() => {
    if (!enabled || !yaml.trim()) return
    let cancelled = false
    const timer = setTimeout(() => {
      setState({ status: "checking" })
      validateTemplate(yaml).then(
        (r) => !cancelled && setState({ status: "ok", template: r.template }),
        (e: unknown) => !cancelled && setState({ status: "error", message: isApiError(e) ? e.message : String(e) }),
      )
    }, 500)
    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [yaml, enabled])
  if (!enabled || !yaml.trim()) return { status: "idle" }
  return state
}
