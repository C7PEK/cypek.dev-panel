import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"

export interface DatabaseStatus {
  installed: boolean
  reachable: boolean
  version?: string
  error?: string
  phpMyAdminPort?: number
  host?: string
  port?: number
}

export interface DatabaseInstallState {
  running: boolean
  done: boolean
  failed: boolean
  lines: string[]
}

export interface DatabaseSystem {
  status: DatabaseStatus
  install: DatabaseInstallState
  systemd: boolean
  phpMyAdminRunning?: boolean
}

export interface ServerDatabase {
  id: number
  serverId: string
  name: string
  label: string
  createdAt: string
  sizeBytes: number
  tables: number
  host: string
  port: number
  user: string
}

export interface DatabaseCredentials {
  id: number
  database: string
  user: string
  password: string
  host: string
  port: number
  fivemConnectionString: string
  jdbcUrl: string
  phpMyAdminPort: number
}

const systemKey = ["system", "database"] as const
const serverKey = (id: string) => ["servers", id, "databases"] as const
const base = (id: string) => `/servers/${encodeURIComponent(id)}/databases`

export function useDatabaseSystem(enabled = true) {
  return useQuery({
    queryKey: systemKey,
    queryFn: () => api<DatabaseSystem>("GET", "/system/database"),
    enabled,
    refetchInterval: (q) => (q.state.data?.install.running ? 2000 : 15000),
  })
}

export function useInstallDatabase() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>("POST", "/system/database/install"),
    onSuccess: () => void qc.invalidateQueries({ queryKey: systemKey }),
  })
}

export function useRestartPhpMyAdmin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>("POST", "/system/database/phpmyadmin/restart"),
    onSuccess: () => setTimeout(() => void qc.invalidateQueries({ queryKey: systemKey }), 1500),
  })
}

export function useServerDatabases(id: string) {
  return useQuery({
    queryKey: serverKey(id),
    queryFn: () => api<{ items: ServerDatabase[]; status: DatabaseStatus }>("GET", `${base(id)}/`),
    refetchInterval: 30000,
    retry: false,
  })
}

export function useCreateDatabase(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (input: { name: string; label: string }) => api<DatabaseCredentials>("POST", `${base(id)}/`, input),
    onSuccess: () => void qc.invalidateQueries({ queryKey: serverKey(id) }),
  })
}

export function fetchCredentials(id: string, dbid: number) {
  return api<DatabaseCredentials>("GET", `${base(id)}/${dbid}/credentials`)
}

export function useRotatePassword(id: string) {
  return useMutation({
    mutationFn: (dbid: number) => api<DatabaseCredentials>("POST", `${base(id)}/${dbid}/password`),
  })
}

export function useDeleteDatabase(id: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (dbid: number) => api<void>("DELETE", `${base(id)}/${dbid}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: serverKey(id) }),
  })
}

/** Link to phpMyAdmin on the same host the panel is reached through. */
export function phpMyAdminUrl(port: number | undefined) {
  if (!port) return ""
  return `${window.location.protocol}//${window.location.hostname}:${port}/`
}

/** The full database name the backend will create for a suffix. */
export function previewName(serverId: string, suffix: string) {
  const s = suffix.trim().toLowerCase() || "main"
  return `hp_${serverId}_${s}`
}

export const SUFFIX_RE = /^[a-z0-9_]{1,16}$/

export interface RootLogin {
  enabled: boolean
  user: string
  password?: string
}

const rootKey = ["system", "database", "root"] as const

export function useRootLogin(enabled: boolean) {
  return useQuery({ queryKey: rootKey, queryFn: () => api<RootLogin>("GET", "/system/database/root/"), enabled })
}

export function useSetRootPassword() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (password: string) => api<RootLogin>("PUT", "/system/database/root/", { password }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: rootKey }),
  })
}

export function useRevealRootPassword() {
  return useMutation({ mutationFn: () => api<RootLogin>("POST", "/system/database/root/reveal") })
}

export function useDisableRootLogin() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>("DELETE", "/system/database/root/"),
    onSuccess: () => void qc.invalidateQueries({ queryKey: rootKey }),
  })
}
