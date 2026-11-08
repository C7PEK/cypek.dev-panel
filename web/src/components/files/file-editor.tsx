import { ArrowLeftIcon, Loader2Icon, SaveIcon } from "lucide-react"
import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CodeEditor } from "@/components/files/code-editor"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { baseName, readFile, useWriteFile } from "@/lib/files"

interface Props {
  serverId: string
  path: string
  onClose: () => void
}

/** Loads a text file, edits it in CodeMirror and saves it back (Ctrl/Cmd+S or the button). */
export function FileEditor({ serverId, path, onClose }: Props) {
  const { t } = useTranslation()
  const write = useWriteFile(serverId)
  const [loaded, setLoaded] = useState<string | null>(null)
  const [value, setValue] = useState("")
  const [loadError, setLoadError] = useState<unknown>(null)
  const [readOnly, setReadOnly] = useState(false)
  const dirty = loaded !== null && value !== loaded

  useEffect(() => {
    let cancelled = false
    setLoaded(null)
    setLoadError(null)
    readFile(serverId, path)
      .then((text) => {
        if (cancelled) return
        setLoaded(text)
        setValue(text)
      })
      .catch((e: unknown) => {
        if (!cancelled) setLoadError(e)
      })
    return () => {
      cancelled = true
    }
  }, [serverId, path])

  // Warn on tab close with unsaved changes.
  useEffect(() => {
    if (!dirty) return
    const handler = (e: BeforeUnloadEvent) => {
      e.preventDefault()
    }
    window.addEventListener("beforeunload", handler)
    return () => window.removeEventListener("beforeunload", handler)
  }, [dirty])

  const save = useCallback(() => {
    if (loaded === null || write.isPending || readOnly) return
    write.mutate(
      { path, content: value },
      {
        onSuccess: () => {
          setLoaded(value)
          toast.success(t("files.saved", { name: baseName(path) }))
        },
        onError: (e) => {
          if (isApiError(e) && e.status === 403) setReadOnly(true)
          toast.error(describeError(t, e))
        },
      },
    )
  }, [loaded, write, readOnly, path, value, t])

  function close() {
    if (dirty && !window.confirm(t("files.discardConfirm"))) return
    onClose()
  }

  return (
    <div className="flex min-h-[60svh] flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          <Button variant="ghost" size="sm" onClick={close}>
            <ArrowLeftIcon />
            {t("files.back")}
          </Button>
          <span className="truncate font-mono text-sm" title={path}>
            /{path}
          </span>
          {dirty && <span className="label-caps text-[0.62rem] text-amber-400">{t("files.unsaved")}</span>}
          {readOnly && <span className="label-caps text-[0.62rem] text-muted-foreground">{t("files.readOnly")}</span>}
        </div>
        <Button onClick={save} disabled={!dirty || write.isPending || readOnly}>
          {write.isPending ? <Loader2Icon className="animate-spin" /> : <SaveIcon />}
          {t("files.save")}
          <kbd className="ml-1 hidden rounded border px-1 text-[0.6rem] text-muted-foreground sm:inline">Ctrl+S</kbd>
        </Button>
      </div>
      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{describeError(t, loadError)}</AlertDescription>
        </Alert>
      ) : loaded === null ? (
        <p className="text-sm text-muted-foreground">{t("app.loading")}</p>
      ) : (
        <div className="min-h-[55svh] flex-1 overflow-hidden rounded-lg border">
          <CodeEditor fileName={path} value={value} onChange={setValue} onSave={save} readOnly={readOnly} className="h-[60svh]" />
        </div>
      )}
    </div>
  )
}
