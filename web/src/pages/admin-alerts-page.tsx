import { BellIcon, CheckIcon, Loader2Icon, SendIcon, XIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { MentionBuilder } from "@/components/alerts/mention-builder"
import { WebhookInput } from "@/components/alerts/webhook-input"
import { NativeSelect } from "@/components/mods/platform-card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { emptyWebhookDraft, useAlerts, useSaveAlerts, useTestAlerts, validMention, webhookDraftInvalid, webhookPayload } from "@/lib/alerts"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import type { AlertHistoryItem, AlertKind, AlertSettings } from "@/lib/types"
import { cn } from "@/lib/utils"

const titleClass = "font-heading text-lg font-medium tracking-[0.04em] uppercase"

export function AlertsSettings() {
  const { t } = useTranslation()
  const q = useAlerts()
  const s = q.data?.settings
  return (
    <div className="flex flex-col gap-6">
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <BellIcon className="size-4 text-primary" />
        {t("alerts.description")}
      </p>
      {q.isError && <p className="text-sm text-destructive">{describeError(t, q.error)}</p>}
      {q.isPending && <Loader2Icon className="animate-spin text-muted-foreground" />}
      {s && <SettingsForm key={settingsKey(s)} settings={s} />}
      {q.data && <HistoryCard items={q.data.history ?? []} />}
    </div>
  )
}

function settingsKey(s: AlertSettings): string {
  return JSON.stringify(s)
}

function SettingsForm({ settings }: { settings: AlertSettings }) {
  const { t } = useTranslation()
  const save = useSaveAlerts()
  const test = useTestAlerts()
  const [webhook, setWebhook] = useState(emptyWebhookDraft)
  const [mention, setMention] = useState(settings.mention)
  const [grace, setGrace] = useState(String(settings.graceSec || 20))
  const [notifyRecovered, setNotifyRecovered] = useState(settings.notifyRecovered)
  const [notifyAutoRestart, setNotifyAutoRestart] = useState(settings.notifyAutoRestart)
  const [includeLogs, setIncludeLogs] = useState(settings.includeLogs)
  const [notifyErrors, setNotifyErrors] = useState(settings.notifyErrors)
  const [language, setLanguage] = useState<"pl" | "en" | "de">(settings.language || "pl")
  const [panelUrl, setPanelUrl] = useState(settings.panelUrl || window.location.origin)

  const graceNum = Number(grace)
  const graceInvalid = !Number.isInteger(graceNum) || graceNum < 10 || graceNum > 600
  const urlInvalid = panelUrl.trim() !== "" && !/^https?:\/\/[^\s/]+/i.test(panelUrl.trim())
  const invalid = graceInvalid || urlInvalid || webhookDraftInvalid(webhook) || !validMention(mention)
  const fieldError = isApiError(save.error) ? save.error : undefined
  const errFor = (f: string) => (fieldError?.field === f ? fieldError.message : undefined)

  const dirty =
    webhookPayload(webhook) !== undefined ||
    mention !== settings.mention ||
    graceNum !== settings.graceSec ||
    notifyRecovered !== settings.notifyRecovered ||
    notifyAutoRestart !== settings.notifyAutoRestart ||
    includeLogs !== settings.includeLogs ||
    notifyErrors !== settings.notifyErrors ||
    language !== settings.language ||
    panelUrl.trim() !== settings.panelUrl

  function submit() {
    save.mutate(
      {
        webhook: webhookPayload(webhook),
        mention,
        graceSec: graceNum,
        notifyRecovered,
        notifyAutoRestart,
        notifyErrors,
        includeLogs,
        language,
        panelUrl: panelUrl.trim(),
      },
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
    <>
      <Card>
        <CardHeader>
          <CardTitle className={titleClass}>{t("alerts.webhook.title")}</CardTitle>
          <CardDescription>{t("alerts.webhook.description")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="alerts-webhook">{t("alerts.webhook.label")}</Label>
            <WebhookInput
              id="alerts-webhook"
              isSet={settings.webhookSet}
              hint={settings.webhookHint}
              draft={webhook}
              onChange={setWebhook}
              error={errFor("webhook")}
            />
          </div>
          <ol className="list-decimal space-y-0.5 pl-5 text-xs text-muted-foreground">
            <li>{t("alerts.webhook.how1")}</li>
            <li>{t("alerts.webhook.how2")}</li>
            <li>{t("alerts.webhook.how3")}</li>
          </ol>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className={titleClass}>{t("alerts.when.title")}</CardTitle>
          <CardDescription>{t("alerts.when.description")}</CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-5">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="alerts-mention">{t("alerts.mention.label")}</Label>
            <MentionBuilder id="alerts-mention" value={mention} onChange={setMention} />
            {errFor("mention") && <p className="text-xs text-destructive">{errFor("mention")}</p>}
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="alerts-grace">{t("alerts.grace.label")}</Label>
            <div className="flex items-center gap-2">
              <Input
                id="alerts-grace"
                type="number"
                inputMode="numeric"
                min={10}
                max={600}
                value={grace}
                onChange={(e) => setGrace(e.target.value)}
                aria-invalid={graceInvalid}
                className="w-28"
              />
              <span className="text-sm text-muted-foreground">{t("alerts.grace.unit")}</span>
            </div>
            <p className={cn("text-xs", graceInvalid ? "text-destructive" : "text-muted-foreground")}>
              {graceInvalid ? t("alerts.grace.invalid") : t("alerts.grace.hint")}
            </p>
            {errFor("graceSec") && <p className="text-xs text-destructive">{errFor("graceSec")}</p>}
          </div>

          <div className="flex flex-col gap-3">
            <SwitchRow checked={notifyRecovered} onChange={setNotifyRecovered} label={t("alerts.switches.recovered")} />
            <SwitchRow
              checked={notifyAutoRestart}
              onChange={setNotifyAutoRestart}
              label={t("alerts.switches.autoRestart")}
              hint={t("alerts.switches.autoRestartHint")}
            />
            <SwitchRow checked={includeLogs} onChange={setIncludeLogs} label={t("alerts.switches.logs")} hint={t("alerts.switches.logsHint")} />
            <SwitchRow checked={notifyErrors} onChange={setNotifyErrors} label={t("alerts.switches.errors")} hint={t("alerts.switches.errorsHint")} />
          </div>

          <div className="grid gap-4 sm:grid-cols-2">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="alerts-language">{t("alerts.language")}</Label>
              <NativeSelect id="alerts-language" value={language} onChange={(v) => setLanguage(v === "en" || v === "de" ? v : "pl")}>
                <option value="pl">Polski</option>
                <option value="en">English</option>
                <option value="de">Deutsch</option>
              </NativeSelect>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="alerts-url">{t("alerts.panelUrl.label")}</Label>
              <Input
                id="alerts-url"
                value={panelUrl}
                onChange={(e) => setPanelUrl(e.target.value)}
                placeholder={window.location.origin}
                aria-invalid={urlInvalid}
              />
              <p className={cn("text-xs", urlInvalid ? "text-destructive" : "text-muted-foreground")}>
                {urlInvalid ? t("alerts.panelUrl.invalid") : t("alerts.panelUrl.hint")}
              </p>
              {errFor("panelUrl") && <p className="text-xs text-destructive">{errFor("panelUrl")}</p>}
            </div>
          </div>

          <div className="flex flex-wrap items-center justify-end gap-2">
            {dirty && webhookPayload(webhook) && <p className="mr-auto text-xs text-muted-foreground">{t("alerts.saveFirst")}</p>}
            <Button
              type="button"
              variant="outline"
              onClick={sendTest}
              disabled={test.isPending || !settings.webhookSet || webhookPayload(webhook) !== undefined}
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
    </>
  )
}

function SwitchRow({ checked, onChange, label, hint }: { checked: boolean; onChange: (v: boolean) => void; label: string; hint?: string }) {
  return (
    <label className="flex items-start justify-between gap-4">
      <span className="flex flex-col gap-0.5">
        <span className="text-sm">{label}</span>
        {hint && <span className="text-xs text-muted-foreground">{hint}</span>}
      </span>
      <Switch checked={checked} onCheckedChange={onChange} className="mt-0.5 shrink-0" />
    </label>
  )
}

const kindClass: Record<AlertKind, string> = {
  down: "border-red-500/40 bg-red-500/10 text-red-400",
  recovered: "border-emerald-500/40 bg-emerald-500/10 text-emerald-400",
  restarted: "border-amber-500/40 bg-amber-500/10 text-amber-300",
  error: "border-orange-500/40 bg-orange-500/10 text-orange-300",
  test: "border-border bg-muted/40 text-muted-foreground",
  expired: "border-red-500/40 bg-red-500/10 text-red-400",
  expiring: "border-amber-500/40 bg-amber-500/10 text-amber-300",
}

function HistoryCard({ items }: { items: AlertHistoryItem[] }) {
  const { t, i18n } = useTranslation()
  return (
    <Card>
      <CardHeader>
        <CardTitle className={titleClass}>{t("alerts.history.title")}</CardTitle>
        <CardDescription>{t("alerts.history.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        {items.length === 0 ? (
          <p className="text-sm text-muted-foreground">{t("alerts.history.empty")}</p>
        ) : (
          <ul className="divide-y divide-border/60">
            {items.map((h, i) => (
              <li key={`${h.at}-${i}`} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
                <span className="w-36 shrink-0 font-mono text-xs text-muted-foreground">{formatDateTime(h.at, i18n.language)}</span>
                <Badge variant="outline" className={cn("label-caps text-[0.6rem]", kindClass[h.kind] ?? kindClass.test)}>
                  {t(`alerts.kinds.${h.kind}`, { defaultValue: h.kind })}
                </Badge>
                <span className="min-w-0 flex-1 truncate">{h.serverName || (h.kind === "test" ? t("alerts.history.panel") : h.serverId)}</span>
                {h.ok ? (
                  <span className="flex items-center gap-1 text-xs text-emerald-400">
                    <CheckIcon className="size-3.5" />
                    {t("alerts.history.sent")}
                  </span>
                ) : (
                  <span className="flex min-w-0 items-center gap-1 text-xs text-destructive" title={h.error}>
                    <XIcon className="size-3.5 shrink-0" />
                    <span className="truncate">{h.error || t("alerts.history.failed")}</span>
                  </span>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  )
}
