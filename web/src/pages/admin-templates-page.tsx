import { AlertTriangleIcon, CopyPlusIcon, EyeIcon, FilePlusIcon, ImportIcon, MoreHorizontalIcon, PencilIcon, Trash2Icon } from "lucide-react"
import { useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { AppShell } from "@/components/layout/app-shell"
import { CategoryIcon } from "@/components/servers/category-icon"
import { ImportDialog } from "@/components/templates/import-dialog"
import { ScriptBadge, SourceBadge } from "@/components/templates/source-badge"
import { type EditorTarget, NEW_TEMPLATE_EXAMPLE, TemplateEditorDialog } from "@/components/templates/template-editor-dialog"
import { ViewYamlDialog } from "@/components/templates/view-yaml-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { duplicateYaml, fetchTemplateYaml, isImported, localized, sourceKind, useDeleteTemplate, useTemplates } from "@/lib/templates"
import type { Template, User } from "@/lib/types"
import { cn } from "@/lib/utils"

type Filter = "all" | "builtin" | "custom" | "imported"

export function AdminTemplatesPage({ user }: { user: User }) {
  const { t, i18n } = useTranslation()
  const templates = useTemplates()
  const [filter, setFilter] = useState<Filter>("all")
  const [importOpen, setImportOpen] = useState(false)
  const [editor, setEditor] = useState<EditorTarget | null>(null)
  const [viewing, setViewing] = useState<Template | null>(null)
  const [deleting, setDeleting] = useState<Template | null>(null)

  const items = useMemo(() => {
    const all = templates.data ?? []
    const f = all.filter((tpl) => {
      const k = sourceKind(tpl)
      if (filter === "builtin") return k === "builtin"
      if (filter === "custom") return k === "custom"
      if (filter === "imported") return isImported(tpl)
      return true
    })
    return [...f].sort((a, b) => a.name.localeCompare(b.name))
  }, [templates.data, filter])

  async function openEditor(tpl: Template) {
    try {
      const y = await fetchTemplateYaml(tpl.id)
      setEditor({ id: tpl.id, yaml: y.yaml, title: t("templateLibrary.editTitle", { name: tpl.name }) })
    } catch (e) {
      toast.error(describeError(t, e))
    }
  }

  function startDuplicate(yaml: string, baseId: string) {
    const ids = new Set((templates.data ?? []).map((x) => x.id))
    let id = `${baseId}-copy`
    for (let i = 2; ids.has(id); i++) id = `${baseId}-copy-${i}`
    setViewing(null)
    setEditor({ yaml: duplicateYaml(yaml, id), title: t("templateLibrary.duplicateTitle") })
  }

  async function duplicate(tpl: Template) {
    try {
      const y = await fetchTemplateYaml(tpl.id)
      startDuplicate(y.yaml, tpl.id)
    } catch (e) {
      toast.error(describeError(t, e))
    }
  }

  const filters: Filter[] = ["all", "builtin", "custom", "imported"]
  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div>
            <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("templateLibrary.title")}</h1>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t("templateLibrary.subtitle")}</p>
          </div>
          <div className="flex flex-wrap gap-2">
            <Button variant="outline" onClick={() => setEditor({ yaml: NEW_TEMPLATE_EXAMPLE, title: t("templateLibrary.newTitle") })}>
              <FilePlusIcon />
              {t("templateLibrary.new")}
            </Button>
            <Button onClick={() => setImportOpen(true)}>
              <ImportIcon />
              {t("templateLibrary.import")}
            </Button>
          </div>
        </div>

        <div className="flex w-fit flex-wrap items-center gap-1 rounded-lg border p-0.5" role="group">
          {filters.map((f) => (
            <Button key={f} size="xs" variant={f === filter ? "secondary" : "ghost"} onClick={() => setFilter(f)}>
              {t(`templateLibrary.filter.${f}`)}
            </Button>
          ))}
        </div>

        {templates.error ? (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, templates.error)}</AlertDescription>
          </Alert>
        ) : templates.isPending ? (
          <p className="text-sm text-muted-foreground">{t("app.loading")}</p>
        ) : items.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="py-10 text-center text-sm text-muted-foreground">
              {filter === "imported" || filter === "custom" ? t("templateLibrary.emptyCustom") : t("templateLibrary.empty")}
            </CardContent>
          </Card>
        ) : (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {items.map((tpl) => {
              const editable = tpl.editable ?? !tpl.builtin
              const warnings = tpl.source?.warnings?.length ?? 0
              return (
                <Card key={tpl.id} className="flex flex-col">
                  <CardContent className="flex flex-1 flex-col gap-3">
                    <div className="flex items-start gap-3">
                      <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
                        <CategoryIcon category={tpl.category} className="size-5" />
                      </span>
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-heading text-lg font-medium tracking-[0.03em]">{tpl.name}</div>
                        <div className="truncate font-mono text-[0.7rem] text-muted-foreground">{tpl.id}</div>
                      </div>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button size="icon-sm" variant="ghost" aria-label={t("templateLibrary.actions")}>
                            <MoreHorizontalIcon />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          {editable ? (
                            <DropdownMenuItem onSelect={() => void openEditor(tpl)}>
                              <PencilIcon />
                              {t("templateLibrary.edit")}
                            </DropdownMenuItem>
                          ) : (
                            <DropdownMenuItem onSelect={() => setViewing(tpl)}>
                              <EyeIcon />
                              {t("templateLibrary.view")}
                            </DropdownMenuItem>
                          )}
                          <DropdownMenuItem onSelect={() => void duplicate(tpl)}>
                            <CopyPlusIcon />
                            {t("templateLibrary.duplicate")}
                          </DropdownMenuItem>
                          {editable && (
                            <>
                              <DropdownMenuSeparator />
                              <DropdownMenuItem className="text-destructive focus:text-destructive" onSelect={() => setDeleting(tpl)}>
                                <Trash2Icon />
                                {tpl.overridesBuiltin ? t("templateLibrary.restoreBuiltin") : t("templateLibrary.delete")}
                              </DropdownMenuItem>
                            </>
                          )}
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                    <div className="flex flex-wrap items-center gap-1.5">
                      <SourceBadge template={tpl} />
                      <span className="label-caps text-[0.6rem] text-muted-foreground">{t(`servers.category.${tpl.category}`)}</span>
                      {tpl.usesScript && <ScriptBadge />}
                      {warnings > 0 && (
                        <span className="inline-flex items-center gap-1 text-[0.7rem] text-amber-300" title={tpl.source?.warnings?.join("\n")}>
                          <AlertTriangleIcon className="size-3" />
                          {t("templateLibrary.warningsCount", { count: warnings })}
                        </span>
                      )}
                    </div>
                    <p className={cn("line-clamp-3 text-sm text-muted-foreground")}>{localized(tpl.description, i18n.resolvedLanguage)}</p>
                    {tpl.source?.original && tpl.source.original !== tpl.name && (
                      <p className="text-xs text-muted-foreground">
                        {t("templateLibrary.originalName", { name: tpl.source.original })}
                        {tpl.source.author ? ` · ${tpl.source.author}` : ""}
                      </p>
                    )}
                  </CardContent>
                </Card>
              )
            })}
          </div>
        )}
      </div>

      <ImportDialog open={importOpen} onOpenChange={setImportOpen} />
      <TemplateEditorDialog target={editor} onClose={() => setEditor(null)} />
      <ViewYamlDialog template={viewing} onClose={() => setViewing(null)} onDuplicate={(yaml) => viewing && startDuplicate(yaml, viewing.id)} />
      <DeleteDialog template={deleting} onClose={() => setDeleting(null)} />
    </AppShell>
  )
}

function DeleteDialog({ template, onClose }: { template: Template | null; onClose: () => void }) {
  const { t } = useTranslation()
  const del = useDeleteTemplate()
  const [inUse, setInUse] = useState<string | null>(null)

  function close() {
    setInUse(null)
    del.reset()
    onClose()
  }

  function run(force: boolean) {
    if (!template) return
    del.mutate(
      { id: template.id, force },
      {
        onSuccess: () => {
          toast.success(t("templateLibrary.deleted", { name: template.name }))
          close()
        },
        onError: (e) => {
          if (isApiError(e) && e.code === "template_in_use") {
            setInUse(e.message)
            return
          }
          toast.error(describeError(t, e))
        },
      },
    )
  }

  return (
    <Dialog open={!!template} onOpenChange={(o) => !o && close()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">
            {template?.overridesBuiltin ? t("templateLibrary.restoreTitle") : t("templateLibrary.deleteTitle")}
          </DialogTitle>
          <DialogDescription>
            {template?.overridesBuiltin
              ? t("templateLibrary.restoreBody", { name: template?.name })
              : t("templateLibrary.deleteBody", { name: template?.name })}
          </DialogDescription>
        </DialogHeader>
        {inUse && (
          <Alert className="border-amber-500/40 bg-amber-500/10">
            <AlertDescription className="text-amber-200">
              <p>{inUse}</p>
              <p className="mt-1 text-xs">{t("templateLibrary.inUseHelp")}</p>
            </AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <Button variant="ghost" onClick={close}>
            {t("power.cancel")}
          </Button>
          <Button variant="destructive" disabled={del.isPending} onClick={() => run(!!inUse)}>
            <Trash2Icon />
            {inUse ? t("templateLibrary.deleteAnyway") : template?.overridesBuiltin ? t("templateLibrary.restoreBuiltin") : t("templateLibrary.delete")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
