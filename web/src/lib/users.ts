import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { AuditEntry, Permission, Role, SecuritySettings, TotpPolicy, User, UserGrant } from "@/lib/types"

export const usersKey = ["users"] as const
export const auditKey = ["audit"] as const
export const securityKey = ["settings", "security"] as const
export const grantsKey = (serverId: string) => ["servers", serverId, "grants"] as const

export const PERMISSIONS: Permission[] = [
  "console.read",
  "console.write",
  "power",
  "files.read",
  "files.write",
  "schedules",
  "backups",
  "settings",
  "delete",
]

export const PRESETS: Record<"view" | "moderator" | "full", Permission[]> = {
  view: ["console.read", "files.read"],
  moderator: ["console.read", "console.write", "power"],
  full: [...PERMISSIONS],
}

export function presetOf(perms: Permission[]): keyof typeof PRESETS | null {
  const set = new Set(perms)
  for (const [name, list] of Object.entries(PRESETS) as [keyof typeof PRESETS, Permission[]][]) {
    if (list.length === set.size && list.every((p) => set.has(p))) return name
  }
  return null
}

export function useUsers(enabled = true) {
  return useQuery({
    queryKey: usersKey,
    queryFn: async () => (await api<{ items: User[] }>("GET", "/users/")).items,
    enabled,
  })
}

export function useCreateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: { username: string; password: string; role: Role }) => api<User>("POST", "/users/", vars),
    onSuccess: () => void qc.invalidateQueries({ queryKey: usersKey }),
  })
}

export function useUpdateUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ id, ...vars }: { id: number; role?: Role; disabled?: boolean; password?: string }) =>
      api<User>("PATCH", `/users/${id}`, vars),
    onSuccess: () => void qc.invalidateQueries({ queryKey: usersKey }),
  })
}

export function useDeleteUser() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>("DELETE", `/users/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: usersKey }),
  })
}

export function useAudit() {
  return useQuery({
    queryKey: auditKey,
    queryFn: async () => (await api<{ items: AuditEntry[] }>("GET", "/audit")).items,
    refetchInterval: 15_000,
  })
}

export function useSecurity(enabled = true) {
  return useQuery({ queryKey: securityKey, queryFn: () => api<SecuritySettings>("GET", "/settings/security"), enabled })
}

export function useSetSecurity() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (requireTotp: TotpPolicy) => api<SecuritySettings>("PUT", "/settings/security", { requireTotp }),
    onSuccess: (s) => {
      qc.setQueryData(securityKey, s)
      void qc.invalidateQueries({ queryKey: ["auth", "totp"] })
    },
  })
}

export function useServerGrants(serverId: string, enabled = true) {
  return useQuery({
    queryKey: grantsKey(serverId),
    queryFn: () => api<{ items: UserGrant[]; all: Permission[] }>("GET", `/servers/${encodeURIComponent(serverId)}/users/`),
    enabled,
  })
}

export function useSetGrant(serverId: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: ({ userId, permissions }: { userId: number; permissions: Permission[] }) =>
      api<UserGrant | undefined>("PUT", `/servers/${encodeURIComponent(serverId)}/users/${userId}`, { permissions }),
    onSuccess: () => void qc.invalidateQueries({ queryKey: grantsKey(serverId) }),
  })
}
