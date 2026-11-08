import { useQuery } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { ServerEventsResponse, ServerStatsResponse } from "@/lib/types"

export function useServerStats(id: string, minutes: number) {
  return useQuery({
    queryKey: ["servers", id, "stats", minutes],
    queryFn: () => api<ServerStatsResponse>("GET", `/servers/${encodeURIComponent(id)}/stats?minutes=${minutes}`),
    refetchInterval: 5000,
    placeholderData: (prev) => prev,
  })
}

export function useServerEvents(id: string) {
  return useQuery({
    queryKey: ["servers", id, "events"],
    queryFn: () => api<ServerEventsResponse>("GET", `/servers/${encodeURIComponent(id)}/events?limit=200`),
    refetchInterval: 10000,
  })
}
