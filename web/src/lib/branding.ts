import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"

export interface Branding {
  name: string
  accent: string
}

export const DEFAULT_NAME = "panel.cypek.dev"
export const DEFAULT_ACCENT = "#5865f2"

export const ACCENT_PRESETS = ["#5865f2", "#8b5cf6", "#ec4899", "#ef4444", "#f97316", "#eab308", "#22c55e", "#14b8a6", "#06b6d4", "#3b82f6"]

const brandingKey = ["branding"]

export function useBranding() {
  return useQuery({ queryKey: brandingKey, queryFn: () => api<Branding>("GET", "/branding"), staleTime: 5 * 60_000 })
}

export function useSaveBranding() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (b: Branding) => api<Branding>("PUT", "/settings/branding", b),
    onSuccess: (b) => qc.setQueryData(brandingKey, b),
  })
}

const ACCENT_VARS = ["--primary", "--ring", "--chart-1", "--sidebar-primary", "--sidebar-ring"]

export function applyAccent(hex: string) {
  const root = document.documentElement.style
  if (!/^#[0-9a-f]{6}$/i.test(hex) || hex.toLowerCase() === DEFAULT_ACCENT) {
    for (const v of [...ACCENT_VARS, "--primary-foreground"]) root.removeProperty(v)
    return
  }
  for (const v of ACCENT_VARS) root.setProperty(v, hex)
  root.setProperty("--primary-foreground", readableOn(hex))
}

export function readableOn(hex: string): string {
  const n = parseInt(hex.slice(1), 16)
  const lin = (c: number) => {
    const s = c / 255
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4
  }
  const l = 0.2126 * lin((n >> 16) & 255) + 0.7152 * lin((n >> 8) & 255) + 0.0722 * lin(n & 255)
  return l > 0.45 ? "#0a0a0b" : "#ffffff"
}

export function splitName(name: string): [string, string] {
  const n = name.trim() || DEFAULT_NAME
  const i = n.indexOf(".")
  return i > 0 ? [n.slice(0, i), n.slice(i)] : [n, ""]
}
