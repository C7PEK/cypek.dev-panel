import { TriangleAlertIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

export function PortConflictNotice({
  message,
  suggestion,
  onUseSuggestion,
  forceLabel,
  onForce,
  pending,
  error,
  className,
}: {
  message: string
  suggestion?: number
  onUseSuggestion?: () => void
  forceLabel?: string
  onForce?: () => void
  pending?: boolean
  error?: boolean
  className?: string
}) {
  const { t } = useTranslation()
  return (
    <Alert
      className={cn(
        error ? "border-destructive/50 bg-destructive/10" : "border-amber-500/40 bg-amber-500/10 text-amber-200",
        className,
      )}
    >
      <TriangleAlertIcon />
      <AlertDescription className={cn("flex flex-col gap-2", !error && "text-amber-200/90")}>
        <span>{message}</span>
        {((suggestion && onUseSuggestion) || onForce) && (
          <span className="flex flex-wrap gap-2">
            {suggestion && onUseSuggestion ? (
              <Button type="button" size="sm" variant="outline" onClick={onUseSuggestion} disabled={pending}>
                {t("ports.useSuggestion", { port: suggestion })}
              </Button>
            ) : null}
            {onForce && (
              <Button type="button" size="sm" variant="ghost" onClick={onForce} disabled={pending}>
                {forceLabel ?? t("ports.saveAnyway")}
              </Button>
            )}
          </span>
        )}
      </AlertDescription>
    </Alert>
  )
}
