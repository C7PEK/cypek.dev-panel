import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { FirewallInfo, PanelUpdateInfo, Port, SystemInfo } from "@/lib/types"

export function useSystemInfo() {
  return useQuery({ queryKey: ["system", "info"], queryFn: () => api<SystemInfo>("GET", "/system/info"), staleTime: 60_000 })
}

export function useFirewall(ports: Pick<Port, "port" | "protocol">[]) {
  const specs = ports.map((p) => `${p.port}/${p.protocol}`)
  return useQuery({
    queryKey: ["system", "firewall", specs],
    queryFn: () => {
      const q = new URLSearchParams()
      for (const s of specs) q.append("port", s)
      return api<FirewallInfo>("GET", `/system/firewall?${q.toString()}`)
    },
    enabled: specs.length > 0,
    staleTime: 60_000,
  })
}

export const panelUpdateKey = ["system", "update"] as const

export function usePanelUpdate(enabled: boolean) {
  return useQuery({
    queryKey: panelUpdateKey,
    queryFn: () => api<PanelUpdateInfo>("GET", "/system/update"),
    enabled,
    staleTime: 15 * 60_000,
    retry: false,
  })
}

export function useRefreshPanelUpdate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: () => api<PanelUpdateInfo>("GET", "/system/update?refresh=1"),
    onSuccess: (data) => qc.setQueryData(panelUpdateKey, data),
  })
}

export function useStartPanelUpdate() {
  return useMutation({ mutationFn: () => api<void>("POST", "/system/update") })
}
