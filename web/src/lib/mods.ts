import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { api } from "@/lib/api"
import type {
  ModInstallResult,
  ModPlatform,
  ModSearchPage,
  ModSort,
  ModUpdate,
  ModVersion,
  ModsList,
} from "@/lib/types"

export const modsKey = (serverId: string) => ["servers", serverId, "mods"] as const
const base = (serverId: string) => `/servers/${encodeURIComponent(serverId)}/mods`
const filePath = (serverId: string, file: string) => `${base(serverId)}/files/${encodeURIComponent(file)}`

/** Loaders the backend understands, with display names, grouped by folder kind. */
export const PLUGIN_LOADERS = ["paper", "purpur", "folia", "spigot", "bukkit", "velocity"] as const
export const MOD_LOADERS = ["fabric", "quilt", "forge", "neoforge"] as const

const LOADER_NAMES: Record<string, string> = {
  paper: "Paper",
  purpur: "Purpur",
  folia: "Folia",
  spigot: "Spigot",
  bukkit: "Bukkit",
  velocity: "Velocity",
  fabric: "Fabric",
  quilt: "Quilt",
  forge: "Forge",
  neoforge: "NeoForge",
}

export function loaderName(loader: string): string {
  return LOADER_NAMES[loader] ?? loader
}

export function useMods(serverId: string, enabled = true) {
  return useQuery({
    queryKey: modsKey(serverId),
    queryFn: () => api<ModsList>("GET", base(serverId)),
    enabled,
    retry: false,
  })
}

export function useModUpdates(serverId: string, enabled: boolean) {
  return useQuery({
    queryKey: [...modsKey(serverId), "updates"],
    queryFn: async () => (await api<{ updates: Record<string, ModUpdate> }>("GET", `${base(serverId)}/updates`)).updates ?? {},
    enabled,
    retry: false,
    staleTime: 5 * 60_000,
  })
}

const PAGE = 20

export function useModSearch(serverId: string, q: string, sort: ModSort, platform: ModPlatform | undefined) {
  return useInfiniteQuery({
    // The platform is part of the key: a new loader/version means new results.
    queryKey: [...modsKey(serverId), "search", q, sort, platform?.loader ?? "", platform?.gameVersion ?? ""],
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ q, sort, offset: String(pageParam), limit: String(PAGE) })
      return api<ModSearchPage>("GET", `${base(serverId)}/search?${params.toString()}`)
    },
    initialPageParam: 0,
    getNextPageParam: (last) => {
      const next = last.offset + last.hits.length
      return last.hits.length > 0 && next < last.total ? next : undefined
    },
    enabled: !!platform?.loader,
    retry: false,
    staleTime: 60_000,
  })
}

export function useModVersions(serverId: string, projectId: string | undefined) {
  return useQuery({
    queryKey: [...modsKey(serverId), "versions", projectId],
    queryFn: async () =>
      (await api<{ versions: ModVersion[] }>("GET", `${base(serverId)}/projects/${encodeURIComponent(projectId ?? "")}/versions`)).versions ?? [],
    enabled: !!projectId,
    retry: false,
  })
}

export function useGameVersions(enabled: boolean) {
  return useQuery({
    queryKey: ["mods", "game-versions"],
    queryFn: async () => (await api<{ versions: string[] }>("GET", "/mods/game-versions")).versions ?? [],
    enabled,
    staleTime: 60 * 60_000,
  })
}

function useInvalidateMods(serverId: string) {
  const qc = useQueryClient()
  return () => void qc.invalidateQueries({ queryKey: modsKey(serverId) })
}

export function useInstallMod(serverId: string) {
  const invalidate = useInvalidateMods(serverId)
  return useMutation({
    mutationFn: (v: { projectId: string; versionId?: string }) => api<ModInstallResult>("POST", base(serverId), v),
    onSuccess: invalidate,
  })
}

export function useUpdateMod(serverId: string) {
  const invalidate = useInvalidateMods(serverId)
  return useMutation({
    mutationFn: (file: string) => api<ModInstallResult>("POST", `${filePath(serverId, file)}/update`),
    onSuccess: invalidate,
  })
}

export function useToggleMod(serverId: string) {
  const invalidate = useInvalidateMods(serverId)
  return useMutation({
    mutationFn: (v: { file: string; enabled: boolean }) => api<{ file: string }>("PATCH", filePath(serverId, v.file), { enabled: v.enabled }),
    onSuccess: invalidate,
  })
}

export function useDeleteMod(serverId: string) {
  const invalidate = useInvalidateMods(serverId)
  return useMutation({
    mutationFn: (file: string) => api<void>("DELETE", filePath(serverId, file)),
    onSuccess: invalidate,
  })
}

export function useSetModPlatform(serverId: string) {
  const invalidate = useInvalidateMods(serverId)
  return useMutation({
    mutationFn: (v: { loader: string; gameVersion: string }) => api<ModPlatform>("PUT", `${base(serverId)}/platform`, v),
    onSuccess: invalidate,
  })
}

/** "1.2M", "34K", "980". */
export function formatCount(n: number, locale: string): string {
  try {
    return new Intl.NumberFormat(locale, { notation: "compact", maximumFractionDigits: 1 }).format(n)
  } catch {
    return String(n)
  }
}
