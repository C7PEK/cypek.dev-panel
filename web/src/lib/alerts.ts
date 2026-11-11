import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api, isApiError } from "@/lib/api"
import type { AlertHistoryItem, AlertSettings, AlertSettingsInput, ServerAlerts, ServerAlertsInput } from "@/lib/types"

const alertsKey = ["alerts"] as const
const serverAlertsKey = (id: string) => ["servers", id, "alerts"] as const

export function useAlerts() {
  return useQuery({
    queryKey: alertsKey,
    queryFn: () => api<{ settings: AlertSettings; history: AlertHistoryItem[] }>("GET", "/alerts"),
    refetchInterval: 15_000,
  })
}

export function useSaveAlerts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (input: AlertSettingsInput) => (await api<{ settings: AlertSettings }>("PUT", "/alerts", input)).settings,
    onSuccess: () => void qc.invalidateQueries({ queryKey: alertsKey }),
  })
}

export function useTestAlerts() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<{ ok: boolean }>("POST", "/alerts/test", {}),
    onSettled: () => void qc.invalidateQueries({ queryKey: alertsKey }),
  })
}

export function useServerAlerts(serverId: string) {
  return useQuery({
    queryKey: serverAlertsKey(serverId),
    queryFn: () => api<ServerAlerts>("GET", `/servers/${serverId}/alerts`),
    retry: (count, e) => !(isApiError(e) && (e.status === 403 || e.status === 404)) && count < 2,
  })
}

export function useSaveServerAlerts(serverId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ServerAlertsInput) => api<ServerAlerts>("PUT", `/servers/${serverId}/alerts`, input),
    onSuccess: (data) => qc.setQueryData(serverAlertsKey(serverId), data),
  })
}

export function useTestServerAlerts(serverId: string) {
  return useMutation({
    mutationFn: () => api<{ ok: boolean }>("POST", `/servers/${serverId}/alerts/test`, {}),
  })
}

export const WEBHOOK_RE = /^https:\/\/(?:(?:ptb|canary)\.)?discord(?:app)?\.com\/api\/webhooks\/\d+\/[\w-]+\/?$/

export function validWebhook(url: string): boolean {
  return WEBHOOK_RE.test(url.trim())
}

export type MentionMode = "none" | "everyone" | "here" | "role" | "user" | "raw"

export function parseMention(m: string): { mode: MentionMode; id: string } {
  const s = m.trim()
  if (s === "") return { mode: "none", id: "" }
  if (s === "@everyone") return { mode: "everyone", id: "" }
  if (s === "@here") return { mode: "here", id: "" }
  let r = /^<@&(\d+)>$/.exec(s)
  if (r) return { mode: "role", id: r[1] }
  r = /^<@!?(\d+)>$/.exec(s)
  if (r) return { mode: "user", id: r[1] }
  return { mode: "raw", id: "" }
}

export function buildMention(mode: MentionMode, id: string, raw: string): string {
  switch (mode) {
    case "none":
      return ""
    case "everyone":
      return "@everyone"
    case "here":
      return "@here"
    case "role":
      return id ? `<@&${id}>` : ""
    case "user":
      return id ? `<@${id}>` : ""
    case "raw":
      return raw.trim()
  }
}

const MENTION_TOKEN = /^(@everyone|@here|<@&\d+>|<@!?\d+>)$/

export function validMention(m: string): boolean {
  const s = m.trim()
  if (s === "") return true
  return s.split(/\s+/).every((tok) => MENTION_TOKEN.test(tok))
}

export interface WebhookDraft {
  text: string
  removed: boolean
}

export const emptyWebhookDraft: WebhookDraft = { text: "", removed: false }

export function webhookPayload(d: WebhookDraft): string | undefined {
  if (d.removed) return ""
  const s = d.text.trim()
  return s === "" ? undefined : s
}

export function webhookDraftInvalid(d: WebhookDraft): boolean {
  const s = d.text.trim()
  return !d.removed && s !== "" && !validWebhook(s)
}
