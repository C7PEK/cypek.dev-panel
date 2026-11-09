import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { ConsoleHints, CreateServerInput, ExpiryInfo, ExpiryInput, InstallJob, InstallState, LiveServer, PowerAction, Server, ServerState, ServerStatus, UpdateServerInput } from "@/lib/types"

/** Prefer the events socket's fresher status/players over the 3 s REST snapshot. */
export function mergeLive(server: Server, live?: LiveServer): Server {
  if (!live) return server
  return { ...server, status: { ...server.status, ...live.status }, players: live.players ?? server.players }
}

export const serversKey = ["servers"] as const
export const serverKey = (id: string) => ["servers", id] as const

const POLL_MS = 3000

const serverPath = (id: string) => `/servers/${encodeURIComponent(id)}`

export function useServers() {
  return useQuery({
    queryKey: serversKey,
    queryFn: async () => (await api<{ items: Server[] }>("GET", "/servers")).items,
    refetchInterval: POLL_MS,
  })
}

export function useServer(id: string) {
  return useQuery({
    queryKey: serverKey(id),
    queryFn: () => api<Server>("GET", serverPath(id)),
    refetchInterval: POLL_MS,
    // A deleted server returns 404 forever; retrying only delays the message.
    retry: false,
  })
}

export function useCreateServer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: CreateServerInput) => api<Server>("POST", "/servers", input),
    onSuccess: (server) => {
      qc.setQueryData(serverKey(server.id), server)
      void qc.invalidateQueries({ queryKey: serversKey })
    },
  })
}

export function useUpdateServer(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: UpdateServerInput) => api<Server>("PATCH", serverPath(id), input),
    onSuccess: (server) => {
      qc.setQueryData(serverKey(id), server)
      void qc.invalidateQueries({ queryKey: serversKey })
    },
  })
}

export function useDeleteServer() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, files }: { id: string; files: boolean }) =>
      api<void>("DELETE", `${serverPath(id)}?files=${files ? 1 : 0}`),
    onSuccess: (_data, { id }) => {
      qc.removeQueries({ queryKey: serverKey(id) })
      void qc.invalidateQueries({ queryKey: serversKey })
    },
  })
}

/** Sets, extends or removes a server's end date (admin). */
export function useSetExpiry(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: ExpiryInput) => api<ExpiryInfo>("PUT", `${serverPath(id)}/expiry`, input),
    onSuccess: (info) => {
      qc.setQueryData<Server>(serverKey(id), (old) =>
        old ? { ...old, expiresAt: info.expiresAt, expired: info.expired, expiredAt: info.expiredAt } : old,
      )
      void qc.invalidateQueries({ queryKey: serversKey })
    },
  })
}

export type ExpiryChoice = "none" | "7" | "14" | "30" | "60" | "90" | "custom"

export const EXPIRY_PRESETS = ["7", "14", "30", "60", "90"] as const

/** Days to send (undefined = no limit, null = invalid custom value). */
export function expiryDays(choice: ExpiryChoice, custom: string): number | undefined | null {
  if (choice === "none") return undefined
  if (choice !== "custom") return Number(choice)
  const n = Number(custom.trim())
  return Number.isInteger(n) && n >= 1 && n <= 3650 ? n : null
}

export type ExpiryStatus =
  | { kind: "none" }
  | { kind: "expired" }
  | { kind: "today" }
  | { kind: "soon" | "later"; days: number }

/** Where a server stands relative to its end date ("soon" = 3 days or less). */
export function expiryStatus(s: Pick<Server, "expiresAt" | "expired">): ExpiryStatus {
  if (s.expired) return { kind: "expired" }
  if (!s.expiresAt) return { kind: "none" }
  const ms = new Date(s.expiresAt).getTime() - Date.now()
  if (Number.isNaN(ms)) return { kind: "none" }
  if (ms <= 24 * 3600 * 1000) return { kind: "today" }
  const days = Math.ceil(ms / (24 * 3600 * 1000))
  return { kind: days <= 3 ? "soon" : "later", days }
}

export function usePower(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (action: PowerAction) => api<{ status: ServerStatus }>("POST", `${serverPath(id)}/power`, { action }),
    onSuccess: ({ status }) => {
      qc.setQueryData<Server>(serverKey(id), (old) => (old ? { ...old, status } : old))
      void qc.invalidateQueries({ queryKey: serversKey })
    },
  })
}

/** Which actions make sense for a state. A server that is not installed yet can only be deleted. */
export function canDo(state: ServerState, installState: InstallState = "ready") {
  const off = state === "stopped" || state === "crashed"
  const on = state === "running" || state === "starting"
  if (installState !== "ready") {
    return { start: false, stop: false, restart: false, kill: false, delete: installState === "failed" || installState === "pending" }
  }
  return {
    start: off || state === "unknown",
    stop: on,
    restart: on,
    kill: on || state === "stopping" || state === "unknown",
    delete: off,
  }
}

export const isInstalling = (s: Pick<Server, "installState">) => s.installState === "pending" || s.installState === "installing"

export const installJobKey = (id: string) => ["servers", id, "install"] as const

/** Polls the installation job every second while enabled. */
export function useInstallJob(id: string, enabled: boolean) {
  return useQuery({
    queryKey: installJobKey(id),
    queryFn: () => api<InstallJob>("GET", `${serverPath(id)}/install`),
    enabled,
    refetchInterval: enabled ? 1000 : false,
    retry: false,
  })
}

export function useReinstall(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>("POST", `${serverPath(id)}/install`),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: serverKey(id) })
      void qc.invalidateQueries({ queryKey: installJobKey(id) })
    },
  })
}

/** Pull the latest code from git, reinstall dependencies, start again. */
export function useGitUpdate(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (restart: boolean) => api<void>("POST", `${serverPath(id)}/git-update`, { restart }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: serverKey(id) })
      void qc.invalidateQueries({ queryKey: installJobKey(id) })
    },
  })
}

export const consoleHintsKey = (id: string) => ["servers", id, "console-hints"] as const

/** Command list + online players for console autocompletion; polls every 5 s while enabled. */
export function useConsoleHints(id: string, enabled: boolean) {
  return useQuery({
    queryKey: consoleHintsKey(id),
    queryFn: () => api<ConsoleHints>("GET", `${serverPath(id)}/console/hints`),
    enabled,
    staleTime: 4000,
    refetchInterval: enabled ? 5000 : false,
    retry: false,
  })
}
