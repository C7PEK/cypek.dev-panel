import { CheckCircle2Icon, Loader2Icon, XCircleIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { CodeEditor } from "@/components/files/code-editor"
import type { ValidationState } from "@/lib/template-validation"
import { cn } from "@/lib/utils"

export function ValidationLine({ state }: { state: ValidationState }) {
  const { t } = useTranslation()
  if (state.status === "idle") return null
  if (state.status === "checking")
    return (
      <p className="flex items-center gap-1.5 text-xs text-muted-foreground">
        <Loader2Icon className="size-3.5 animate-spin" />
        {t("templateLibrary.validating")}
      </p>
    )
  if (state.status === "ok")
    return (
      <p className="flex items-center gap-1.5 text-xs text-emerald-400">
        <CheckCircle2Icon className="size-3.5" />
        {t("templateLibrary.valid", { name: state.template.name, id: state.template.id })}
      </p>
    )
  return (
    <p className="flex items-start gap-1.5 text-xs text-destructive">
      <XCircleIcon className="mt-0.5 size-3.5 shrink-0" />
      <span className="font-mono break-all">{state.message}</span>
    </p>
  )
}

/** CodeMirror in YAML mode, sized for dialogs. */
export function YamlEditor({
  value,
  onChange,
  onSave,
  readOnly,
  className,
}: {
  value: string
  onChange?: (v: string) => void
  onSave?: () => void
  readOnly?: boolean
  className?: string
}) {
  return (
    <CodeEditor
      fileName="template.yaml"
      value={value}
      onChange={(v) => onChange?.(v)}
      onSave={() => onSave?.()}
      readOnly={readOnly}
      className={cn("h-[50svh] min-h-64 overflow-hidden rounded-lg border [&_.cm-editor]:h-full", className)}
    />
  )
}
