import { Loader2Icon, SaveIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ValidationLine, YamlEditor } from "@/components/templates/yaml-editor"
import { useYamlValidation } from "@/lib/template-validation"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { useSaveTemplate, useTemplates, yamlId } from "@/lib/templates"

export interface EditorTarget {
  id?: string
  yaml: string
  title: string
}

export const NEW_TEMPLATE_EXAMPLE = `# A minimal template. Full reference: docs/templates.md
id: my-server            # lowercase letters, digits and dashes; unique
name: My server
category: other          # fivem | minecraft | discord-bot | other
description:
  pl: Krótki opis widoczny w kreatorze.
  en: Short description shown in the wizard.

variables:
  - key: PORT
    label: { pl: Port, en: Port }
    type: number
    default: 27015
    min: 1024
    max: 65535

runtime:
  kind: none             # java | node | python | none

install: []              # download / extract / write / properties / git / exec steps

start:
  command: ./server      # a program, never a shell line
  args: ["--port", "{{PORT}}"]
stop:
  command: stop          # sent to the console; empty = SIGTERM
  timeoutSec: 30

ports:
  - { name: game, port: "{{PORT}}", protocol: tcp+udp }
`

export function TemplateEditorDialog({ target, onClose }: { target: EditorTarget | null; onClose: () => void }) {
  return target ? <EditorBody key={`${target.id ?? "new"}-${target.title}`} target={target} onClose={onClose} /> : null
}

function EditorBody({ target, onClose }: { target: EditorTarget; onClose: () => void }) {
  const { t } = useTranslation()
  const [yaml, setYaml] = useState(target.yaml)
  const [confirmDiscard, setConfirmDiscard] = useState(false)
  const [confirmOverride, setConfirmOverride] = useState(false)
  const validation = useYamlValidation(yaml)
  const save = useSaveTemplate()
  const templates = useTemplates()
  const dirty = yaml !== target.yaml
  const id = yamlId(yaml)
  const builtinClash = !target.id && !!templates.data?.some((tpl) => tpl.id === id && tpl.builtin && !tpl.overridesBuiltin)

  function close(force = false) {
    if (dirty && !force && !save.isSuccess) {
      setConfirmDiscard(true)
      return
    }
    onClose()
  }

  function submit(override = false) {
    if (save.isPending) return
    if (target.id && id && id !== target.id) {
      toast.error(t("templateLibrary.idChanged", { id: target.id }))
      return
    }
    save.mutate(
      { id: target.id, yaml, override },
      {
        onSuccess: (tpl) => {
          toast.success(t("templateLibrary.saved", { name: tpl.name }))
          onClose()
        },
        onError: (e) => {
          if (isApiError(e) && e.code === "template_exists" && builtinClash && !override) {
            setConfirmOverride(true)
            return
          }
          if (!(isApiError(e) && e.code === "template_invalid")) toast.error(describeError(t, e))
        },
      },
    )
  }

  const saveError = save.error && isApiError(save.error) && save.error.code === "template_invalid" ? save.error.message : null

  return (
    <Dialog open onOpenChange={(o) => !o && close()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{target.title}</DialogTitle>
          <DialogDescription>
            {t("templateLibrary.editorHelp")} <code className="font-mono text-xs">docs/templates.md</code>
          </DialogDescription>
        </DialogHeader>
        <YamlEditor value={yaml} onChange={setYaml} onSave={() => submit()} />
        <ValidationLine state={validation} />
        {saveError && (
          <Alert variant="destructive">
            <AlertDescription className="font-mono text-xs break-all">{saveError}</AlertDescription>
          </Alert>
        )}
        {(confirmOverride || (builtinClash && !target.id)) && (
          <Alert className="border-amber-500/40 bg-amber-500/10">
            <AlertDescription className="flex flex-wrap items-center gap-2 text-amber-200">
              {t("templateLibrary.overrideBuiltin", { id })}
              {confirmOverride && (
                <Button size="xs" variant="outline" onClick={() => submit(true)} disabled={save.isPending}>
                  {t("templateLibrary.overrideConfirm")}
                </Button>
              )}
            </AlertDescription>
          </Alert>
        )}
        {confirmDiscard && (
          <Alert variant="destructive">
            <AlertDescription className="flex flex-wrap items-center gap-2">
              {t("templateLibrary.discardChanges")}
              <Button size="xs" variant="destructive" onClick={() => close(true)}>
                {t("templateLibrary.discard")}
              </Button>
              <Button size="xs" variant="ghost" onClick={() => setConfirmDiscard(false)}>
                {t("templateLibrary.keepEditing")}
              </Button>
            </AlertDescription>
          </Alert>
        )}
        <DialogFooter>
          <Button variant="outline" onClick={() => close()}>
            {t("power.cancel")}
          </Button>
          <Button onClick={() => submit(confirmOverride)} disabled={save.isPending || validation.status === "error"}>
            {save.isPending ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
            {t("templateLibrary.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
