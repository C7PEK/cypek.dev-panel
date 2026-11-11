import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api, isApiError, setCsrfToken } from "@/lib/api"
import type { SessionResponse, SetupStatus, User } from "@/lib/types"

export const meKey = ["auth", "me"] as const
export const setupStatusKey = ["setup", "status"] as const

async function fetchMe(): Promise<User | null> {
  try {
    const s = await api<SessionResponse>("GET", "/auth/me")
    setCsrfToken(s.csrfToken)
    return s.user
  } catch (e) {
    if (isApiError(e) && e.status === 401) return null
    throw e
  }
}

export function useMe() {
  return useQuery({ queryKey: meKey, queryFn: fetchMe, staleTime: 60_000, retry: false })
}

export function useSetupStatus() {
  return useQuery({
    queryKey: setupStatusKey,
    queryFn: () => api<SetupStatus>("GET", "/setup/status"),
    staleTime: 60_000,
  })
}

function useSessionMutation<V>(path: string) {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (vars: V) => api<SessionResponse>("POST", path, vars),
    onSuccess: (s) => {
      setCsrfToken(s.csrfToken)
      qc.setQueryData(meKey, s.user)
      qc.setQueryData(setupStatusKey, { setupRequired: false })
    },
  })
}

export function useLogin() {
  return useSessionMutation<{ username: string; password: string }>("/auth/login")
}

export function useSetup() {
  return useSessionMutation<{ token: string; username: string; password: string }>("/setup")
}

export function useLogout() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<void>("POST", "/auth/logout"),
    onSettled: () => {
      clearConsoleHistory()
      setCsrfToken("")
      qc.setQueryData(meKey, null)
      void qc.invalidateQueries()
    },
  })
}

function clearConsoleHistory() {
  try {
    for (let i = localStorage.length - 1; i >= 0; i--) {
      const k = localStorage.key(i)
      if (k?.startsWith("homepanel-history-")) localStorage.removeItem(k)
    }
  } catch {
  }
}
