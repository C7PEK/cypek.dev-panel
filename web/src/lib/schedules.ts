import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { CronPreview, Schedule, ScheduleAction, ScheduleInput, ScheduleRun } from "@/lib/types"

export const schedulesKey = (serverId: string) => ["servers", serverId, "schedules"] as const
export const runsKey = (serverId: string, id: number) => ["servers", serverId, "schedules", id, "runs"] as const

const base = (serverId: string) => `/servers/${encodeURIComponent(serverId)}/schedules`

export interface ScheduleList {
  items: Schedule[]
  timezone: string
}

export function useSchedules(serverId: string) {
  return useQuery({
    queryKey: schedulesKey(serverId),
    queryFn: () => api<ScheduleList>("GET", base(serverId) + "/"),
    refetchInterval: 5000,
  })
}

export function useScheduleRuns(serverId: string, id: number, enabled: boolean) {
  return useQuery({
    queryKey: runsKey(serverId, id),
    queryFn: async () => (await api<{ items: ScheduleRun[] }>("GET", `${base(serverId)}/${id}/runs`)).items,
    enabled,
    refetchInterval: enabled ? 3000 : false,
  })
}

function useInvalidate(serverId: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: schedulesKey(serverId) })
}

export function useCreateSchedule(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({
    mutationFn: (input: ScheduleInput) => api<Schedule>("POST", base(serverId) + "/", input),
    onSuccess: inv,
  })
}

export function useUpdateSchedule(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({
    mutationFn: ({ id, input }: { id: number; input: ScheduleInput }) => api<Schedule>("PATCH", `${base(serverId)}/${id}`, input),
    onSuccess: inv,
  })
}

export function useDeleteSchedule(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({
    mutationFn: (id: number) => api<void>("DELETE", `${base(serverId)}/${id}`),
    onSuccess: inv,
  })
}

export function useRunSchedule(serverId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>("POST", `${base(serverId)}/${id}/run`),
    onSuccess: (_d, id) => {
      void qc.invalidateQueries({ queryKey: schedulesKey(serverId) })
      void qc.invalidateQueries({ queryKey: runsKey(serverId, id) })
    },
  })
}

export function useCancelSchedule(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({
    mutationFn: (id: number) => api<void>("POST", `${base(serverId)}/${id}/cancel`),
    onSuccess: inv,
  })
}

/** Next fire times for a cron expression; `enabled` lets callers debounce. */
export function useCronPreview(expr: string, enabled: boolean) {
  return useQuery({
    queryKey: ["cron", "preview", expr],
    queryFn: () => api<CronPreview>("GET", `/system/cron/preview?expr=${encodeURIComponent(expr)}`),
    enabled: enabled && expr.trim() !== "",
    retry: false,
    staleTime: 60_000,
  })
}

// --- Presets -----------------------------------------------------------------

export interface CronPreset {
  key: string
  cron: string
}

export const CRON_PRESETS: CronPreset[] = [
  { key: "daily4", cron: "0 4 * * *" },
  { key: "every6h", cron: "0 */6 * * *" },
  { key: "hourly", cron: "0 * * * *" },
  { key: "monday5", cron: "0 5 * * 1" },
]

/** Preset key matching a cron expression, or "custom". */
export function presetFor(cron: string): string {
  return CRON_PRESETS.find((p) => p.cron === cron.trim())?.key ?? "custom"
}

/** The "restart with warnings" chain (warn 10/5/1 min, then restart). */
export function restartWithWarnings(messages: { ten: string; five: string; one: string }): ScheduleAction[] {
  return [
    { type: "warn", message: messages.ten },
    { type: "wait", seconds: 300 },
    { type: "warn", message: messages.five },
    { type: "wait", seconds: 240 },
    { type: "warn", message: messages.one },
    { type: "wait", seconds: 60 },
    { type: "restart" },
  ]
}

/** Total time a chain spends waiting, in seconds. */
export function chainWaitSeconds(actions: ScheduleAction[]): number {
  return actions.reduce((sum, a) => sum + (a.type === "wait" ? a.seconds ?? 0 : 0), 0)
}

/**
 * Human summary for the common shapes ("daily at 04:00", "every 6 hours",
 * "every hour", "Mondays at 05:00"); returns null for anything else so the
 * UI falls back to the raw expression.
 */
export function describeCron(cron: string): { key: string; params?: Record<string, string | number> } | null {
  const parts = cron.trim().split(/\s+/)
  if (parts.length !== 5) return null
  const [min, hour, dom, mon, dow] = parts
  const two = (n: string) => n.padStart(2, "0")
  const isNum = (s: string) => /^\d+$/.test(s)
  if (isNum(min) && isNum(hour) && dom === "*" && mon === "*" && dow === "*") return { key: "daily", params: { time: `${two(hour)}:${two(min)}` } }
  if (isNum(min) && hour === "*" && dom === "*" && mon === "*" && dow === "*") return { key: "hourly", params: { minute: two(min) } }
  const everyH = /^\*\/(\d+)$/.exec(hour)
  if (isNum(min) && everyH && dom === "*" && mon === "*" && dow === "*") return { key: "everyHours", params: { hours: Number(everyH[1]), minute: two(min) } }
  const everyM = /^\*\/(\d+)$/.exec(min)
  if (everyM && hour === "*" && dom === "*" && mon === "*" && dow === "*") return { key: "everyMinutes", params: { minutes: Number(everyM[1]) } }
  if (isNum(min) && isNum(hour) && dom === "*" && mon === "*" && /^[0-6]$/.test(dow)) return { key: "weekly", params: { day: Number(dow), time: `${two(hour)}:${two(min)}` } }
  if (isNum(min) && isNum(hour) && isNum(dom) && mon === "*" && dow === "*") return { key: "monthly", params: { day: Number(dom), time: `${two(hour)}:${two(min)}` } }
  return null
}
