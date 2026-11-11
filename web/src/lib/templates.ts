import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"
import i18n from "i18next"

import { api } from "@/lib/api"
import type { LocalizedText, ServerCategory, SourceOption, Template, TemplateImportResult, TemplatePort, TemplateSourceKind, TemplateYaml } from "@/lib/types"

export const templatesKey = ["templates"] as const

export function useTemplates() {
  return useQuery({
    queryKey: templatesKey,
    queryFn: async () => (await api<{ items: Template[] }>("GET", "/templates")).items,
    staleTime: 5 * 60_000,
  })
}

export function useTemplateSource(templateId: string | undefined, source: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["templates", templateId, "sources", source],
    queryFn: async () =>
      (
        await api<{ options: SourceOption[] }>(
          "GET",
          `/templates/${encodeURIComponent(templateId ?? "")}/sources/${encodeURIComponent(source ?? "")}`,
        )
      ).options,
    enabled: enabled && !!templateId && !!source,
    staleTime: 5 * 60_000,
    retry: 1,
  })
}

export function localized(text: LocalizedText | undefined, lang = i18n.resolvedLanguage ?? i18n.language): string {
  if (!text) return ""
  const short = (lang ?? "en").split("-")[0]
  return text[short] ?? text.en ?? Object.values(text)[0] ?? ""
}

export function renderPort(port: TemplatePort, values: Record<string, string>): string {
  return port.port.replace(/\{\{([A-Za-z0-9_]+)\}\}/g, (_, key: string) => values[key] ?? "?")
}

const tplPath = (id: string) => `/templates/${encodeURIComponent(id)}`

export function sourceKind(t: Template): TemplateSourceKind {
  return t.source?.kind ?? (t.builtin ? "builtin" : "custom")
}

export function isImported(t: Template): boolean {
  const k = sourceKind(t)
  return k === "pterodactyl" || k === "pelican" || k === "pufferpanel"
}

export function fetchTemplateYaml(id: string) {
  return api<TemplateYaml>("GET", `${tplPath(id)}/yaml`)
}

export function useTemplateYaml(id: string | undefined) {
  return useQuery({
    queryKey: ["templates", id, "yaml"],
    queryFn: () => fetchTemplateYaml(id ?? ""),
    enabled: !!id,
    staleTime: 0,
  })
}

export function validateTemplate(yaml: string) {
  return api<{ template: Template }>("POST", "/templates/validate", { yaml })
}

export function useSaveTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: async (v: { id?: string; yaml: string; override?: boolean }) => {
      const res = v.id
        ? await api<{ template: Template }>("PUT", tplPath(v.id), { yaml: v.yaml })
        : await api<{ template: Template }>("POST", "/templates", { yaml: v.yaml, override: v.override })
      return res.template
    },
    onSuccess: () => void qc.invalidateQueries({ queryKey: templatesKey }),
  })
}

export function useDeleteTemplate() {
  const qc = useQueryClient()
  return useMutation({
    mutationFn: (v: { id: string; force?: boolean }) => api<void>("DELETE", `${tplPath(v.id)}?force=${v.force ? 1 : 0}`),
    onSuccess: () => void qc.invalidateQueries({ queryKey: templatesKey }),
  })
}

export function useImportTemplate() {
  return useMutation({
    mutationFn: (v: { content?: string; url?: string }) => api<TemplateImportResult>("POST", "/templates/import", v),
  })
}

export function yamlId(yaml: string): string | undefined {
  const m = yaml.match(/^id:\s*["']?([A-Za-z0-9_-]+)["']?\s*$/m)
  return m?.[1]
}

export function duplicateYaml(yaml: string, newId: string): string {
  let out = /^id:/m.test(yaml) ? yaml.replace(/^id:.*$/m, `id: ${newId}`) : `id: ${newId}\n${yaml}`
  out = out.replace(/^name:\s*(.*)$/m, (_l, n: string) => {
    const name = n.trim().replace(/^["']|["']$/g, "")
    return `name: ${JSON.stringify(name + " (copy)")}`
  })
  return out
}

export const CATEGORY_ORDER: ServerCategory[] = ["minecraft", "fivem", "discord-bot", "other"]
export const GROUP_ORDER = ["plugins", "mods", "vanilla", "proxy", "bedrock", "fxserver", "node", "python"]

function rankOf(list: readonly string[], v: string) {
  const i = list.indexOf(v)
  return i === -1 ? list.length : i
}

export function compareTemplates(a: Template, b: Template) {
  return (
    rankOf(CATEGORY_ORDER, a.category) - rankOf(CATEGORY_ORDER, b.category) ||
    rankOf(GROUP_ORDER, a.group ?? "") - rankOf(GROUP_ORDER, b.group ?? "") ||
    (a.order ?? 100) - (b.order ?? 100) ||
    a.name.localeCompare(b.name)
  )
}
