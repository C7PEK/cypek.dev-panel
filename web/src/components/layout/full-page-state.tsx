import { Loader2Icon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"

export function FullPageLoading() {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-svh items-center justify-center bg-background text-muted-foreground">
      <Loader2Icon className="mr-2 size-4 animate-spin" />
      {t("app.loading")}
    </div>
  )
}

export function FullPageError({ error, onRetry }: { error: unknown; onRetry?: () => void }) {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-svh items-center justify-center bg-background p-4">
      <Alert variant="destructive" className="max-w-md">
        <AlertTitle>{t("errors.title")}</AlertTitle>
        <AlertDescription>
          <p>{describeError(t, error)}</p>
          {onRetry && (
            <Button variant="outline" size="sm" className="mt-3" onClick={onRetry}>
              {t("app.retry")}
            </Button>
          )}
        </AlertDescription>
      </Alert>
    </div>
  )
}
