import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { ApiError, api, apiBase, apiRaw, apiText, getCsrfToken } from "@/lib/api"

export type FileType = "file" | "dir" | "symlink" | "other"

export interface FileEntry {
  name: string
  type: FileType
  size: number
  modifiedAt: string
  mode: string
  editable?: boolean
}

export interface FileListing {
  path: string
  entries: FileEntry[]
}

const base = (id: string) => `/servers/${encodeURIComponent(id)}/files`
const q = (path: string) => `?path=${encodeURIComponent(path)}`

export const filesKey = (id: string, path: string) => ["servers", id, "files", path] as const

/** Joins a directory path and a name into a root-relative path ("" = root). */
export function joinPath(dir: string, name: string): string {
  return dir ? `${dir}/${name}` : name
}

export function parentPath(path: string): string {
  const i = path.lastIndexOf("/")
  return i < 0 ? "" : path.slice(0, i)
}

export function baseName(path: string): string {
  const i = path.lastIndexOf("/")
  return i < 0 ? path : path.slice(i + 1)
}

export function useFiles(id: string, path: string, enabled = true) {
  return useQuery({
    queryKey: filesKey(id, path),
    queryFn: () => api<FileListing>("GET", `${base(id)}/${q(path)}`),
    enabled,
    staleTime: 5_000,
  })
}

export function readFile(id: string, path: string) {
  return apiText(`${base(id)}/content${q(path)}`)
}

export function writeFile(id: string, path: string, content: string) {
  return apiRaw<FileEntry>("PUT", `${base(id)}/content${q(path)}`, content, "text/plain; charset=utf-8")
}

export function downloadUrl(id: string, path: string) {
  return `${apiBase}${base(id)}/download${q(path)}`
}

/** Invalidates the listing of a directory (and nothing else). */
function useInvalidateDir(id: string) {
  const qc = useQueryClient()
  return (dir: string) => qc.invalidateQueries({ queryKey: filesKey(id, dir) })
}

export function useMkdir(id: string) {
  const invalidate = useInvalidateDir(id)
  return useMutation({
    mutationFn: (path: string) => api<void>("POST", `${base(id)}/mkdir`, { path }),
    onSuccess: (_d, path) => void invalidate(parentPath(path)),
  })
}

export function useRename(id: string) {
  const invalidate = useInvalidateDir(id)
  return useMutation({
    mutationFn: (v: { from: string; to: string }) => api<void>("POST", `${base(id)}/rename`, v),
    onSuccess: (_d, v) => {
      void invalidate(parentPath(v.from))
      if (parentPath(v.to) !== parentPath(v.from)) void invalidate(parentPath(v.to))
    },
  })
}

export function useDeleteFiles(id: string) {
  const invalidate = useInvalidateDir(id)
  return useMutation({
    mutationFn: (paths: string[]) => api<void>("POST", `${base(id)}/delete`, { paths }),
    onSuccess: (_d, paths) => {
      for (const dir of new Set(paths.map(parentPath))) void invalidate(dir)
    },
  })
}

export function useExtract(id: string) {
  const invalidate = useInvalidateDir(id)
  return useMutation({
    mutationFn: (v: { path: string; to?: string }) => api<void>("POST", `${base(id)}/extract`, v),
    onSuccess: (_d, v) => void invalidate(v.to ?? parentPath(v.path)),
  })
}

export function useWriteFile(id: string) {
  const invalidate = useInvalidateDir(id)
  return useMutation({
    mutationFn: (v: { path: string; content: string }) => writeFile(id, v.path, v.content),
    onSuccess: (_d, v) => void invalidate(parentPath(v.path)),
  })
}

export const ARCHIVE_RE = /\.(zip|tar\.gz|tgz|tar\.xz|txz|tar)$/i

export function isArchive(name: string) {
  return ARCHIVE_RE.test(name)
}

/**
 * Uploads files with XHR so progress events are available. onProgress gets
 * 0..100 for the whole batch. Rejects with ApiError like the JSON client.
 */
export function uploadFiles(id: string, dir: string, files: File[], onProgress?: (pct: number) => void) {
  return new Promise<{ uploaded: FileEntry[] }>((resolve, reject) => {
    const form = new FormData()
    for (const f of files) form.append("files", f, f.name)
    const xhr = new XMLHttpRequest()
    xhr.open("POST", `${apiBase}${base(id)}/upload${q(dir)}`)
    xhr.withCredentials = true
    const csrf = getCsrfToken()
    if (csrf) xhr.setRequestHeader("X-CSRF-Token", csrf)
    xhr.setRequestHeader("Accept", "application/json")
    xhr.upload.onprogress = (ev) => {
      if (ev.lengthComputable && onProgress) onProgress(Math.round((ev.loaded / ev.total) * 100))
    }
    xhr.onerror = () => reject(new ApiError(0, "network", "network error"))
    xhr.onload = () => {
      let data: unknown = null
      try {
        data = xhr.responseText ? JSON.parse(xhr.responseText) : null
      } catch {
        // fall through with null
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        resolve((data as { uploaded: FileEntry[] }) ?? { uploaded: [] })
        return
      }
      const err = (data as { error?: { code?: string; message?: string } } | null)?.error
      reject(new ApiError(xhr.status, err?.code ?? "internal_error", err?.message ?? xhr.statusText))
    }
    xhr.send(form)
  })
}
