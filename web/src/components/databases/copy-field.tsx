import { CheckIcon, CopyIcon, EyeIcon, EyeOffIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// A read-only value with a copy button; secret values are masked until shown.
export function CopyField({ label, value, secret, multiline }: { label: string; value: string; secret?: boolean; multiline?: boolean }) {
  const { t } = useTranslation()
  const [copied, setCopied] = useState(false)
  const [shown, setShown] = useState(!secret)
  async function copy() {
    try {
      await navigator.clipboard.writeText(value)
    } catch {
      // Plain HTTP on a LAN IP has no clipboard API: fall back to a hidden textarea.
      const ta = document.createElement("textarea")
      ta.value = value
      document.body.appendChild(ta)
      ta.select()
      document.execCommand("copy")
      ta.remove()
    }
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <div className="flex flex-col gap-1">
      <span className="label-caps text-[0.6rem] text-muted-foreground">{label}</span>
      <div className="flex items-start gap-1.5">
        <code
          className={cn(
            "min-w-0 flex-1 rounded-md border bg-muted/40 px-2 py-1.5 font-mono text-xs",
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
        <Button type="button" size="icon-sm" variant="outline" aria-label={t("database.copy")} onClick={() => void copy()}>
          {copied ? <CheckIcon /> : <CopyIcon />}
        </Button>
      </div>
    </div>
  )
}
