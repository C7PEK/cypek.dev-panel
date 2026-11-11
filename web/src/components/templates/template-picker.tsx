import { SearchIcon, TerminalIcon } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { CategoryIcon } from "@/components/servers/category-icon"
import { ScriptBadge, SourceBadge } from "@/components/templates/source-badge"
import { Input } from "@/components/ui/input"
import { CATEGORY_ORDER, GROUP_ORDER, isImported, localized, sourceKind } from "@/lib/templates"
import type { ServerCategory, Template } from "@/lib/types"
import { cn } from "@/lib/utils"

// Groups the panel knows how to name; anything else is shown as written.
const KNOWN_GROUPS = new Set(GROUP_ORDER)

type Tab = "all" | ServerCategory

interface Section {
  key: string
  category: ServerCategory
  group: string
  items: Template[]
}

function rank(list: string[], v: string) {
  const i = list.indexOf(v)
  return i === -1 ? list.length : i
}

/**
 * Step 1 of the create wizard: templates in category tabs, each category
 * split into groups (Minecraft: plugins, mods, vanilla, proxy, Bedrock).
 */
export function TemplatePicker({ templates, onPick, onCustom }: { templates: Template[]; onPick: (t: Template) => void; onCustom: () => void }) {
  const { t, i18n } = useTranslation()
  const [tab, setTab] = useState<Tab>("all")
  const [query, setQuery] = useState("")

  const counts = useMemo(() => {
    const c: Partial<Record<ServerCategory, number>> = {}
    for (const tpl of templates) c[tpl.category] = (c[tpl.category] ?? 0) + 1
    return c
  }, [templates])

  const sections = useMemo(() => {
    const q = query.trim().toLowerCase()
    const lang = i18n.resolvedLanguage
    const visible = templates.filter((tpl) => {
      if (tab !== "all" && tpl.category !== tab) return false
      if (!q) return true
      return [tpl.name, tpl.id, localized(tpl.description, lang)].some((s) => s?.toLowerCase().includes(q))
    })
    const byKey = new Map<string, Section>()
    for (const tpl of visible) {
      // Imported and custom templates go to their own section, apart from the built-ins.
      const group = sourceKind(tpl) !== "builtin" ? "imported" : (tpl.group ?? "")
      const key = `${tpl.category}/${group}`
      let s = byKey.get(key)
      if (!s) byKey.set(key, (s = { key, category: tpl.category, group, items: [] }))
      s.items.push(tpl)
    }
    const out = [...byKey.values()]
    for (const s of out) s.items.sort((a, b) => (a.order ?? 100) - (b.order ?? 100) || a.name.localeCompare(b.name))
    out.sort(
      (a, b) =>
        rank(CATEGORY_ORDER, a.category) - rank(CATEGORY_ORDER, b.category) ||
        (a.group === "imported" ? 1 : 0) - (b.group === "imported" ? 1 : 0) ||
        rank(GROUP_ORDER, a.group) - rank(GROUP_ORDER, b.group) ||
        a.group.localeCompare(b.group),
    )
    return out
  }, [templates, tab, query, i18n.resolvedLanguage])

  const tabs: Tab[] = ["all", ...CATEGORY_ORDER.filter((c) => c === "other" || counts[c])]
  const showCustom = (tab === "all" || tab === "other") && !query.trim()

  function sectionTitle(s: Section) {
    const category = t(`servers.category.${s.category}`)
    if (s.group === "imported") return { title: t("templates.groups.imported"), hint: category }
    if (KNOWN_GROUPS.has(s.group)) return { title: t(`templates.groups.${s.group}.title`), hint: t(`templates.groups.${s.group}.hint`) }
    if (s.group) return { title: s.group.charAt(0).toUpperCase() + s.group.slice(1).replace(/-/g, " "), hint: category }
    return { title: category, hint: "" }
  }

  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div role="tablist" aria-label={t("templates.categoriesLabel")} className="flex flex-wrap gap-1.5">
          {tabs.map((c) => {
            const active = tab === c
            const n = c === "all" ? templates.length : (counts[c] ?? 0)
            return (
              <button
                key={c}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setTab(c)}
                className={cn(
                  "label-caps inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-[0.66rem] transition-colors focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none",
                  active ? "border-primary bg-primary/10 text-foreground" : "border-border text-muted-foreground hover:border-primary/40 hover:text-foreground",
                )}
              >
                {c !== "all" && <CategoryIcon category={c} className="size-3.5" />}
                {c === "all" ? t("templates.allCategories") : t(`servers.category.${c}`)}
                {n > 0 && <span className="text-muted-foreground">{n}</span>}
              </button>
            )
          })}
        </div>
        <div className="relative sm:w-56">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("templates.searchPlaceholder")} aria-label={t("templates.searchPlaceholder")} className="pl-8" />
        </div>
      </div>

      {sections.length === 0 && !showCustom && <p className="text-sm text-muted-foreground">{t("templates.noMatch")}</p>}

      {sections.map((s) => {
        const { title, hint } = sectionTitle(s)
        return (
          <section key={s.key} className="flex flex-col gap-3" aria-label={title}>
            <div className="flex flex-wrap items-baseline gap-x-3 gap-y-0.5">
              <h2 className="font-heading text-base font-medium tracking-[0.06em] uppercase">
                {tab === "all" && s.group !== "" && s.category !== "other" && <span className="text-muted-foreground">{t(`servers.category.${s.category}`)} · </span>}
                {title}
              </h2>
              {hint && <p className="text-xs text-muted-foreground">{hint}</p>}
            </div>
            <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
              {s.items.map((tpl) => (
                <TemplateCard key={tpl.id} tpl={tpl} onPick={onPick} />
              ))}
            </div>
          </section>
        )
      })}

      {showCustom && (
        <section className="flex flex-col gap-3" aria-label={t("templates.customTitle")}>
          <h2 className="font-heading text-base font-medium tracking-[0.06em] uppercase">{t("templates.groups.custom")}</h2>
          <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <button
              type="button"
              onClick={onCustom}
              className="group flex flex-col gap-3 rounded-xl border border-dashed bg-card/50 p-4 text-left transition-colors hover:border-primary/50 hover:bg-primary/5 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
            >
              <div className="flex items-center gap-3">
                <span className="flex size-10 items-center justify-center rounded-lg bg-muted text-muted-foreground group-hover:text-foreground">
                  <TerminalIcon className="size-5" />
                </span>
                <div className="font-heading text-lg font-medium tracking-[0.03em]">{t("templates.customTitle")}</div>
              </div>
              <p className="text-sm text-muted-foreground">{t("templates.customDescription")}</p>
            </button>
          </div>
        </section>
      )}
    </div>
  )
}

function TemplateCard({ tpl, onPick }: { tpl: Template; onPick: (t: Template) => void }) {
  const { i18n } = useTranslation()
  return (
    <button
      type="button"
      onClick={() => onPick(tpl)}
      className="group flex flex-col gap-3 rounded-xl border bg-card p-4 text-left transition-colors hover:border-primary/50 hover:bg-primary/5 focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none"
    >
      <div className="flex items-center gap-3">
        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground group-hover:text-foreground">
          <CategoryIcon category={tpl.category} templateId={tpl.id} className="size-5" />
        </span>
        <div className="min-w-0 font-heading text-lg leading-tight font-medium tracking-[0.03em]">{tpl.name}</div>
      </div>
      <p className="text-sm text-muted-foreground">{localized(tpl.description, i18n.resolvedLanguage)}</p>
      {(isImported(tpl) || sourceKind(tpl) === "custom" || tpl.usesScript) && (
        <div className="mt-auto flex flex-wrap gap-1.5">
          {sourceKind(tpl) !== "builtin" && <SourceBadge template={tpl} />}
          {tpl.usesScript && <ScriptBadge />}
        </div>
      )}
    </button>
  )
}
