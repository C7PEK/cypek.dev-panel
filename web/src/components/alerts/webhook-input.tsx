import { Undo2Icon, XIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { emptyWebhookDraft, type WebhookDraft, webhookDraftInvalid } from "@/lib/alerts"

export function WebhookInput({
  id,
  isSet,
  hint,
  draft,
  onChange,
  emptyPlaceholder,
  error,
}: {
  id: string
  isSet: boolean
  hint: string
  draft: WebhookDraft
  onChange: (d: WebhookDraft) => void
  emptyPlaceholder?: string
  error?: string
}) {
  const { t } = useTranslation()
  const invalid = webhookDraftInvalid(draft)
  const placeholder = draft.removed
    ? t("alerts.webhook.willRemove")
    : isSet
      ? t("alerts.webhook.savedPlaceholder", { hint })
      : (emptyPlaceholder ?? "https://discord.com/api/webhooks/…")
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex gap-2">
        <Input
          id={id}
          type="password"
          autoComplete="off"
          spellCheck={false}
          value={draft.text}
          disabled={draft.removed}
          placeholder={placeholder}
          aria-invalid={invalid || !!error}
          onChange={(e) => onChange({ text: e.target.value, removed: false })}
          className="font-mono"
        />
        {isSet && !draft.removed && (
          <Button type="button" variant="outline" onClick={() => onChange({ text: "", removed: true })}>
            <XIcon />
            {t("alerts.webhook.remove")}
          </Button>
        )}
        {draft.removed && (
          <Button type="button" variant="outline" onClick={() => onChange(emptyWebhookDraft)}>
            <Undo2Icon />
            {t("alerts.webhook.undo")}
          </Button>
        )}
      </div>
      {invalid && <p className="text-xs text-destructive">{t("alerts.webhook.invalid")}</p>}
      {error && !invalid && <p className="text-xs text-destructive">{error}</p>}
    </div>
  )
}
