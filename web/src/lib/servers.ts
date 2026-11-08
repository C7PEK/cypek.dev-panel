import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type { CreateServerInput, PowerAction, Server, ServerState, ServerStatus, UpdateServerInput } from "@/lib/types"

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

/** Which actions make sense for a state. */
export function canDo(state: ServerState) {
  const off = state === "stopped" || state === "crashed"
  const on = state === "running" || state === "starting"
  return {
    start: off || state === "unknown",
    stop: on,
    restart: on,
    kill: on || state === "stopping" || state === "unknown",
    delete: off,
  }
}
