import { useQuery } from "@tanstack/react-query"
import i18n from "i18next"

import { api } from "@/lib/api"
import type { LocalizedText, SourceOption, Template, TemplatePort } from "@/lib/types"

export const templatesKey = ["templates"] as const

export function useTemplates() {
  return useQuery({
    queryKey: templatesKey,
    queryFn: async () => (await api<{ items: Template[] }>("GET", "/templates")).items,
    staleTime: 5 * 60_000,
  })
}

/** Dynamic option lists (e.g. Paper versions). */
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

/** Picks the text for the current UI language, falling back to English, then anything. */
export function localized(text: LocalizedText | undefined, lang = i18n.resolvedLanguage ?? i18n.language): string {
  if (!text) return ""
  const short = (lang ?? "en").split("-")[0]
  return text[short] ?? text.en ?? Object.values(text)[0] ?? ""
}

/** Replaces {{KEY}} placeholders in template ports with chosen values. */
export function renderPort(port: TemplatePort, values: Record<string, string>): string {
  return port.port.replace(/\{\{([A-Za-z0-9_]+)\}\}/g, (_, key: string) => values[key] ?? "?")
}
