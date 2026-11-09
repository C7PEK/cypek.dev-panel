import { useEffect } from "react"

import { applyAccent, DEFAULT_NAME, useBranding } from "@/lib/branding"

// Applies the saved name (tab title) and accent colour everywhere,
// including the login screen.
export function BrandingEffect() {
  const b = useBranding()
  useEffect(() => {
    if (!b.data) return
    applyAccent(b.data.accent)
    document.title = b.data.name || DEFAULT_NAME
  }, [b.data])
  return null
}
