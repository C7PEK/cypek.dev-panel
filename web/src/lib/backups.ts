import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api, apiBase } from "@/lib/api"

export interface Backup {
  id: number
  serverId: string
  fileName: string
  size: number
  note?: string
  trigger: "manual" | "schedule"
  createdAt: string
}

export interface BackupSettings {
  excludes: string[]
  keepLast: number
  keepDays: number
  flushMinecraft: boolean
}

export interface BackupJob {
  kind: "backup" | "restore"
  startedAt: string
  files: number
  bytes: number
  error?: string
  done: boolean
}

export interface BackupList {
  items: Backup[]
  settings: BackupSettings
  dir: string
  writable: boolean
  job?: BackupJob
}

export const backupsKey = (serverId: string) => ["servers", serverId, "backups"] as const
const base = (serverId: string) => `/servers/${encodeURIComponent(serverId)}/backups`

export function useBackups(serverId: string) {
  return useQuery({
    queryKey: backupsKey(serverId),
    queryFn: () => api<BackupList>("GET", base(serverId) + "/"),
    refetchInterval: (q) => (q.state.data?.job && !q.state.data.job.done ? 2000 : 10_000),
  })
}

function useInvalidate(serverId: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: backupsKey(serverId) })
}

export function useCreateBackup(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({ mutationFn: (note: string) => api<void>("POST", base(serverId) + "/", note ? { note } : {}), onSuccess: inv })
}

export function useUpdateBackupSettings(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({ mutationFn: (s: BackupSettings) => api<BackupSettings>("PUT", base(serverId) + "/settings", s), onSuccess: inv })
}

export function useDeleteBackup(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({ mutationFn: (id: number) => api<void>("DELETE", `${base(serverId)}/${id}`), onSuccess: inv })
}

export function useRestoreBackup(serverId: string) {
  const inv = useInvalidate(serverId)
  return useMutation({ mutationFn: (id: number) => api<void>("POST", `${base(serverId)}/${id}/restore`), onSuccess: inv })
}

export function backupDownloadUrl(serverId: string, id: number): string {
  return `${apiBase}${base(serverId)}/${id}/download`
}
