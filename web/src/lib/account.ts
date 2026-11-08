import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api, setCsrfToken } from "@/lib/api"
import { meKey, setupStatusKey } from "@/lib/auth"
import type { ApiKey, LoginResult, SessionResponse, TotpSetup, TotpStatus } from "@/lib/types"

export const totpKey = ["auth", "totp"] as const
export const apiKeysKey = ["auth", "api-keys"] as const

/** Login that may stop at the 2FA step. */
export function useLoginStep1() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: { username: string; password: string }) => api<LoginResult>("POST", "/auth/login", vars),
    onSuccess: (r) => {
      if ("totpRequired" in r) return
      setCsrfToken(r.csrfToken)
      qc.setQueryData(meKey, r.user)
      qc.setQueryData(setupStatusKey, { setupRequired: false })
    },
  })
}

function useSession<V>(path: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: V) => api<SessionResponse>("POST", path, vars),
    onSuccess: (s) => {
      setCsrfToken(s.csrfToken)
      qc.setQueryData(meKey, s.user)
    },
  })
}

export function useLoginTotp() {
  return useSession<{ token: string; code: string }>("/auth/login/totp")
}

export function useChangePassword() {
  return useSession<{ current: string; new: string }>("/auth/password")
}

export function useTotpStatus() {
  return useQuery({ queryKey: totpKey, queryFn: () => api<TotpStatus>("GET", "/auth/totp") })
}

export function useTotpSetup() {
  return useMutation({ mutationFn: () => api<TotpSetup>("POST", "/auth/totp/setup") })
}

export function useTotpEnable() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (code: string) => api<{ backupCodes: string[] }>("POST", "/auth/totp/enable", { code }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: totpKey })
      void qc.invalidateQueries({ queryKey: meKey })
    },
  })
}

export function useTotpDisable() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (code: string) => api<void>("POST", "/auth/totp/disable", { code }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: totpKey })
      void qc.invalidateQueries({ queryKey: meKey })
    },
  })
}

export function useApiKeys() {
  return useQuery({
    queryKey: apiKeysKey,
    queryFn: () => api<{ items: ApiKey[]; permissions: string[] }>("GET", "/auth/api-keys"),
  })
}

export function useCreateApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: { name: string; scope?: string[]; expiresInDays?: number }) =>
      api<{ key: ApiKey; token: string }>("POST", "/auth/api-keys", vars),
    onSuccess: () => void qc.invalidateQueries({ queryKey: apiKeysKey }),
  })
}

export function useDeleteApiKey() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (id: number) => api<void>("DELETE", `/auth/api-keys/${id}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: apiKeysKey }),
  })
}
