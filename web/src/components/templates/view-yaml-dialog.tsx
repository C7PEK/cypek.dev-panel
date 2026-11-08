import { CopyPlusIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { YamlEditor } from "@/components/templates/yaml-editor"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { describeError } from "@/lib/errors"
import { useTemplateYaml } from "@/lib/templates"
import type { Template } from "@/lib/types"

/** Read-only YAML of a (built-in) template, with a way to duplicate it. */
export function ViewYamlDialog({
  template,
  onClose,
  onDuplicate,
}: {
  template: Template | null
  onClose: () => void
  onDuplicate: (yaml: string) => void
}) {
  const { t } = useTranslation()
  const yaml = useTemplateYaml(template?.id)
  return (
    <Dialog open={!!template} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-4xl">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{template?.name}</DialogTitle>
          <DialogDescription>{t("templateLibrary.viewHelp")}</DialogDescription>
        </DialogHeader>
        {yaml.error ? (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, yaml.error)}</AlertDescription>
          </Alert>
        ) : yaml.isPending ? (
          <p className="text-sm text-muted-foreground">{t("app.loading")}</p>
        ) : (
          <YamlEditor value={yaml.data.yaml} readOnly />
        )}
        <DialogFooter>
          <Button variant="outline" onClick={onClose}>
            {t("templateLibrary.close")}
          </Button>
          <Button disabled={!yaml.data} onClick={() => yaml.data && onDuplicate(yaml.data.yaml)}>
            <CopyPlusIcon />
            {t("templateLibrary.duplicate")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
