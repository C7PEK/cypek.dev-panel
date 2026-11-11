import { CheckIcon, CopyIcon, EyeIcon, EyeOffIcon } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { copyText, selectText } from "@/lib/clipboard"
import { cn } from "@/lib/utils"

export function CopyField({ label, value, secret, multiline }: { label: string; value: string; secret?: boolean; multiline?: boolean }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [shown, setShown] = useState(!secret)
  const codeRef = useRef<HTMLElement>(null)

  async function copy(button: HTMLElement) {
    if (await copyText(value, button)) {
      setCopied(true)
      setTimeout(() => setCopied(false), 1500)
      return
    }
    setShown(true)
    requestAnimationFrame(() => selectText(codeRef.current))
    toast.error(t("database.copyManual"))
  }

  return (
    <div className="flex flex-col gap-1">
      <span className="label-caps text-[0.6rem] text-muted-foreground">{label}</span>
      <div className="flex items-start gap-1.5">
        <code
          ref={codeRef}
          className={cn(
            "min-w-0 flex-1 rounded-md border bg-muted/40 px-2 py-1.5 font-mono text-xs select-all",
            multiline ? "break-all whitespace-pre-wrap" : "truncate",
          )}
        >
          {shown ? value : "•".repeat(Math.min(value.length, 24))}
        </code>
        {secret && (
          <Button type="button" size="icon-sm" variant="ghost" aria-label={shown ? t("database.hide") : t("database.show")} onClick={() => setShown((s) => !s)}>
            {shown ? <EyeOffIcon /> : <EyeIcon />}
          </Button>
        )}
        <Button
          type="button"
          size="icon-sm"
          variant="outline"
          aria-label={t("database.copy")}
          title={t("database.copy")}
          onClick={(e) => void copy(e.currentTarget)}
          className={copied ? "border-emerald-500/50 text-emerald-400" : undefined}
        >
          {copied ? <CheckIcon /> : <CopyIcon />}
        </Button>
      </div>
    </div>
  )
}
