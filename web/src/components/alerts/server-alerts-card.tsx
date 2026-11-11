import { BellIcon, Loader2Icon, SendIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { MentionBuilder } from "@/components/alerts/mention-builder"
import { WebhookInput } from "@/components/alerts/webhook-input"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { emptyWebhookDraft, useSaveServerAlerts, useServerAlerts, useTestServerAlerts, validMention, webhookDraftInvalid, webhookPayload } from "@/lib/alerts"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import type { ServerAlerts } from "@/lib/types"

export function ServerAlertsCard({ serverId, admin }: { serverId: string; admin: boolean }) {
  const q = useServerAlerts(serverId)
  if (q.isError && isApiError(q.error) && (q.error.status === 403 || q.error.status === 404)) return null
  if (!q.data) return null
  const d = q.data
  return <AlertsForm key={`${d.enabled}-${d.webhookSet}-${d.webhookHint}-${d.mention}`} serverId={serverId} data={d} admin={admin} />
}

function AlertsForm({ serverId, data, admin }: { serverId: string; data: ServerAlerts; admin: boolean }) {
  const { t } = useTranslation()
  const save = useSaveServerAlerts(serverId)
  const test = useTestServerAlerts(serverId)
  const [enabled, setEnabled] = useState(data.enabled)
  const [webhook, setWebhook] = useState(emptyWebhookDraft)
  const [mention, setMention] = useState(data.mention)
  const fieldError = isApiError(save.error) ? save.error : undefined

  const dirty = enabled !== data.enabled || webhookPayload(webhook) !== undefined || mention !== data.mention
  const invalid = webhookDraftInvalid(webhook) || !validMention(mention)
  const willHaveWebhook = (data.webhookSet && !webhook.removed) || webhookPayload(webhook) !== undefined
  const noWebhook = !data.globalWebhookSet && !willHaveWebhook

  function submit() {
    save.mutate(
      { enabled, webhook: webhookPayload(webhook), mention },
      {
        onSuccess: () => toast.success(t("alerts.saved")),
        onError: (e) => toast.error(describeError(t, e)),
      },
    )
  }

  function sendTest() {
    test.mutate(undefined, {
      onSuccess: () => toast.success(t("alerts.testSent")),
      onError: (e) => toast.error(describeError(t, e)),
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em] uppercase">
          <BellIcon className="size-4 text-primary" />
          {t("alerts.server.title")}
        </CardTitle>
        <CardDescription>{t("alerts.server.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        <label className="flex items-center justify-between gap-3">
          <span className="text-sm">{t("alerts.server.enabled")}</span>
          <Switch checked={enabled} onCheckedChange={setEnabled} />
        </label>

        {noWebhook && (
          <Alert>
            <AlertDescription>
              {admin ? (
                <span>
                  {t("alerts.server.noWebhookAdmin")}{" "}
                  <Link to="/settings/alerts" className="text-primary underline-offset-4 hover:underline">
                    {t("alerts.server.openSettings")}
                  </Link>
                </span>
              ) : (
                t("alerts.server.noWebhook")
              )}
            </AlertDescription>
          </Alert>
        )}

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`alerts-webhook-${serverId}`}>{t("alerts.server.ownWebhook")}</Label>
          <WebhookInput
            id={`alerts-webhook-${serverId}`}
            isSet={data.webhookSet}
            hint={data.webhookHint}
            draft={webhook}
            onChange={setWebhook}
            emptyPlaceholder={t("alerts.server.webhookEmpty")}
            error={fieldError?.field === "webhook" ? fieldError.message : undefined}
          />
          <p className="text-xs text-muted-foreground">{t("alerts.server.webhookHint")}</p>
        </div>

        <div className="flex flex-col gap-1.5">
          <Label htmlFor={`alerts-mention-${serverId}`}>{t("alerts.server.mention")}</Label>
          <MentionBuilder id={`alerts-mention-${serverId}`} value={mention} onChange={setMention} emptyLabel={t("alerts.server.mentionGlobal")} />
          {fieldError?.field === "mention" && <p className="text-xs text-destructive">{fieldError.message}</p>}
        </div>

        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={sendTest}
            disabled={test.isPending || dirty || noWebhook}
            title={dirty ? t("alerts.saveFirst") : undefined}
          >
            {test.isPending ? <Loader2Icon className="animate-spin" /> : <SendIcon />}
            {t("alerts.test")}
          </Button>
          <Button type="button" onClick={submit} disabled={!dirty || invalid || save.isPending}>
            {save.isPending ? t("alerts.saving") : t("alerts.save")}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
