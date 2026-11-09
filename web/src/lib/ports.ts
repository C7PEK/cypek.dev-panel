import { useQuery } from "@tanstack/react-query"
import type { TFunction } from "i18next"
import { useEffect, useState } from "react"

import { api } from "@/lib/api"
import { describePortConflict } from "@/lib/errors"
import type { Port, PortConflict, Template } from "@/lib/types"

/** Value that follows `value` after it stopped changing for `ms`. */
export function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

/** Template ports with the current values; ports that do not resolve to a number are left out. */
export function resolvePorts(template: Template, values: Record<string, string>): Port[] {
  const out: Port[] = []
  for (const p of template.ports ?? []) {
    const raw = p.port.replace(/\{\{([A-Za-z0-9_]+)\}\}/g, (_, key: string) => (values[key] ?? "").trim())
    const n = Number(raw)
    if (Number.isInteger(n) && n >= 1 && n <= 65535) out.push({ name: p.name, port: n, protocol: p.protocol as Port["protocol"] })
  }
  return out
}

/** Variable a template port comes from, when the port is exactly "{{KEY}}". */
export function portVariable(template: Template, portName: string): string | undefined {
  const p = template.ports?.find((x) => x.name === portName)
  const m = p?.port.match(/^\{\{([A-Za-z0-9_]+)\}\}$/)
  return m?.[1]
}

/** Live conflict check (admin endpoint), debounced by 500 ms. */
export function usePortCheck(ports: Port[], serverId?: string, enabled = true) {
  const key = JSON.stringify(ports.map((p) => [p.port, p.protocol, p.name]))
  const debounced = useDebounced(key, 500)
  return useQuery({
    queryKey: ["ports", "check", serverId ?? "", debounced],
    queryFn: async () => {
      const list = (JSON.parse(debounced) as [number, string, string][]).map(([port, protocol, name]) => ({ port, protocol, name }))
      return (await api<{ conflicts: PortConflict[] }>("POST", "/ports/check", { ports: list, serverId })).conflicts
    },
    enabled: enabled && debounced !== "[]",
    staleTime: 5000,
    retry: false,
  })
}

/** Message for a conflict from the live /ports/check. */
export function describeLiveConflict(t: TFunction, c: PortConflict): string {
  return describePortConflict(t, c.host ? "port_in_use_host" : "port_in_use", {
    port: c.port,
    suggestion: c.suggestion,
    server: c.serverName ?? "",
    running: c.running && !c.host,
  })
}
