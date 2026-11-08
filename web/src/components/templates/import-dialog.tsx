import { DownloadIcon, FileUpIcon, Loader2Icon, SaveIcon, WandSparklesIcon } from "lucide-react"
import { type ChangeEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ValidationLine, YamlEditor } from "@/components/templates/yaml-editor"
import { useYamlValidation } from "@/lib/template-validation"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Textarea } from "@/components/ui/textarea"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { useImportTemplate, useSaveTemplate } from "@/lib/templates"
import type { TemplateImportResult } from "@/lib/types"

type Mode = "paste" | "url" | "file"

const MAX_FILE = 2 * 1024 * 1024

/** Converts a Pterodactyl/Pelican egg or a PufferPanel template, previews it, then saves. */
export function ImportDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return open ? <ImportBody onClose={() => onOpenChange(false)} /> : null
}

function ImportBody({ onClose }: { onClose: () => void }) {
  const { t } = useTranslation()
  const [mode, setMode] = useState<Mode>("paste")
  const [content, setContent] = useState("")
  const [url, setUrl] = useState("")
  const [fileName, setFileName] = useState("")
  const [fileContent, setFileContent] = useState("")
  const [result, setResult] = useState<TemplateImportResult | null>(null)
  const [yaml, setYaml] = useState("")
  const [confirmOverride, setConfirmOverride] = useState(false)
  const conv = useImportTemplate()
  const save = useSaveTemplate()
  const validation = useYamlValidation(yaml, !!result)

  const input =
    mode === "paste" ? { content } : mode === "url" ? { url: url.trim() } : { content: fileContent }
  const canConvert =
    (mode === "paste" && content.trim() !== "") || (mode === "url" && /^https:\/\/\S+$/.test(url.trim())) || (mode === "file" && fileContent !== "")

  function convert() {
    conv.mutate(input, {
      onSuccess: (r) => {
        setResult(r)
        setYaml(r.yaml)
        setConfirmOverride(false)
      },
    })
  }

  function onFile(e: ChangeEvent<HTMLInputElement>) {
    const f = e.target.files?.[0]
    e.target.value = ""
    if (!f) return
    if (f.size > MAX_FILE) {
      toast.error(t("templateLibrary.fileTooLarge"))
      return
    }
    const reader = new FileReader()
    reader.onload = () => {
      setFileName(f.name)
      setFileContent(String(reader.result ?? ""))
    }
    reader.readAsText(f)
  }

  function saveTemplate(override = false) {
    save.mutate(
      { yaml, override },
      {
        onSuccess: (tpl) => {
          toast.success(t("templateLibrary.imported", { name: tpl.name }))
          onClose()
        },
        onError: (e) => {
          if (isApiError(e) && e.code === "template_exists") {
            setConfirmOverride(true)
            return
          }
          if (!(isApiError(e) && e.code === "template_invalid")) toast.error(describeError(t, e))
        },
      },
    )
  }

  const convError = conv.error ? (isApiError(conv.error) ? conv.error.message : describeError(t, conv.error)) : null
  const saveError = save.error && isApiError(save.error) && save.error.code === "template_invalid" ? save.error.message : null

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("templateLibrary.importTitle")}</DialogTitle>
          <DialogDescription>{t("templateLibrary.importHelp")}</DialogDescription>
        </DialogHeader>

        {!result ? (
          <>
            <div className="rounded-lg border bg-muted/30 p-3 text-xs text-muted-foreground">
              <p className="mb-1 font-medium text-foreground">{t("templateLibrary.whereTitle")}</p>
              <ul className="list-disc space-y-0.5 pl-4">
                <li>
                  {t("templateLibrary.wherePtero")}{" "}
                  <a className="text-[#8b95ff] underline-offset-4 hover:underline" href="https://github.com/pelican-eggs" target="_blank" rel="noreferrer">
                    github.com/pelican-eggs
                  </a>
                  ,{" "}
                  <a className="text-[#8b95ff] underline-offset-4 hover:underline" href="https://github.com/pterodactyl/game-eggs" target="_blank" rel="noreferrer">
                    github.com/pterodactyl/game-eggs
                  </a>
                </li>
                <li>
                  {t("templateLibrary.wherePuffer")}{" "}
                  <a className="text-[#8b95ff] underline-offset-4 hover:underline" href="https://github.com/pufferpanel/templates" target="_blank" rel="noreferrer">
                    github.com/pufferpanel/templates
                  </a>
                </li>
              </ul>
              <p className="mt-2">{t("templateLibrary.dockerNote")}</p>
            </div>
            <Tabs value={mode} onValueChange={(v) => setMode(v as Mode)}>
              <TabsList>
                <TabsTrigger value="paste">{t("templateLibrary.tabPaste")}</TabsTrigger>
                <TabsTrigger value="url">{t("templateLibrary.tabUrl")}</TabsTrigger>
                <TabsTrigger value="file">{t("templateLibrary.tabFile")}</TabsTrigger>
              </TabsList>
              <TabsContent value="paste" className="pt-3">
                <Textarea
                  value={content}
                  onChange={(e) => setContent(e.target.value)}
                  placeholder='{ "meta": { "version": "PTDL_v2" }, "name": "...", ... }'
                  className="min-h-56 font-mono text-xs"
                  spellCheck={false}
                />
              </TabsContent>
              <TabsContent value="url" className="pt-3">
                <Field>
                  <FieldLabel htmlFor="import-url">{t("templateLibrary.urlLabel")}</FieldLabel>
                  <Input
                    id="import-url"
                    value={url}
                    onChange={(e) => setUrl(e.target.value)}
                    placeholder="https://raw.githubusercontent.com/pelican-eggs/…/egg-….json"
                    className="font-mono text-xs"
                    autoComplete="off"
                    spellCheck={false}
                  />
                  <FieldDescription>{t("templateLibrary.urlHelp")}</FieldDescription>
                </Field>
              </TabsContent>
              <TabsContent value="file" className="pt-3">
                <label className="flex cursor-pointer flex-col items-center gap-2 rounded-lg border border-dashed p-6 text-sm text-muted-foreground hover:bg-muted/30">
                  <FileUpIcon className="size-6" />
                  {fileName ? <span className="font-mono text-foreground">{fileName}</span> : t("templateLibrary.pickFile")}
                  <input type="file" accept=".json,application/json" className="hidden" onChange={onFile} />
                </label>
              </TabsContent>
            </Tabs>
            {convError && (
              <Alert variant="destructive">
                <AlertTitle>{t("templateLibrary.convertFailed")}</AlertTitle>
                <AlertDescription className="font-mono text-xs break-all">{convError}</AlertDescription>
              </Alert>
            )}
          </>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2 text-sm">
              <Badge variant="outline" className="label-caps text-[0.6rem]">
                {t(`templateLibrary.sources.${result.format}`)}
              </Badge>
              <span className="font-medium">{result.template.name}</span>
              <span className="font-mono text-xs text-muted-foreground">{result.template.id}</span>
              {result.template.usesScript && (
                <Badge variant="outline" className="label-caps border-amber-500/40 bg-amber-500/10 text-[0.6rem] text-amber-300">
                  {t("templateLibrary.usesScript")}
                </Badge>
              )}
            </div>
            {result.warnings.length > 0 && (
              <Alert className="border-amber-500/40 bg-amber-500/10">
                <AlertTitle className="text-amber-200">{t("templateLibrary.warningsTitle", { count: result.warnings.length })}</AlertTitle>
                <AlertDescription>
                  <ul className="list-disc space-y-0.5 pl-4 text-xs text-amber-100/90">
                    {result.warnings.map((w, i) => (
                      <li key={i}>{w}</li>
                    ))}
                  </ul>
                </AlertDescription>
              </Alert>
            )}
            <p className="text-xs text-muted-foreground">{t("templateLibrary.reviewHelp")}</p>
            <YamlEditor value={yaml} onChange={setYaml} onSave={() => saveTemplate(confirmOverride)} />
            <ValidationLine state={validation} />
            {saveError && (
              <Alert variant="destructive">
                <AlertDescription className="font-mono text-xs break-all">{saveError}</AlertDescription>
              </Alert>
            )}
            {confirmOverride && (
              <Alert className="border-amber-500/40 bg-amber-500/10">
                <AlertDescription className="flex flex-wrap items-center gap-2 text-amber-200">
                  {t("templateLibrary.existsChangeId")}
                  <Button size="xs" variant="outline" onClick={() => saveTemplate(true)} disabled={save.isPending}>
                    {t("templateLibrary.overrideConfirm")}
                  </Button>
                </AlertDescription>
              </Alert>
            )}
          </>
        )}

        <DialogFooter>
          {result ? (
            <>
              <Button variant="outline" onClick={() => setResult(null)}>
                {t("templates.back")}
              </Button>
              <Button onClick={() => saveTemplate(false)} disabled={save.isPending || validation.status === "error"}>
                {save.isPending ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
                {t("templateLibrary.saveImported")}
              </Button>
            </>
          ) : (
            <>
              <Button variant="outline" onClick={onClose}>
                {t("power.cancel")}
              </Button>
              <Button onClick={convert} disabled={!canConvert || conv.isPending}>
                {conv.isPending ? <Loader2Icon className="animate-spin" /> : mode === "url" ? <DownloadIcon /> : <WandSparklesIcon />}
                {t("templateLibrary.convert")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
