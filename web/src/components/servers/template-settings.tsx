import { RotateCcwIcon } from "lucide-react"
import { type FormEvent, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { VariableFields } from "@/components/servers/variable-fields"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { canDo, isInstalling, useReinstall, useUpdateServer } from "@/lib/servers"
import { useTemplates } from "@/lib/templates"
import type { Server, TemplateVariable } from "@/lib/types"

/** Settings tab for servers created from a template: variables + limits + reinstall. */
export function TemplateSettings({ server }: { server: Server }) {
  const { t } = useTranslation()
  const templates = useTemplates()
  const tpl = templates.data?.find((x) => x.id === server.templateId)

  if (templates.isPending) return <p className="text-sm text-muted-foreground">{t("app.loading")}</p>
  if (!tpl) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{t("errors.template_error")}</AlertDescription>
      </Alert>
    )
  }
  return (
    <>
      <SettingsForm key={server.updatedAt} server={server} tplId={tpl.id} variables={tpl.variables} />
      <ReinstallCard server={server} />
    </>
  )
}

function SettingsForm({ server, tplId, variables }: { server: Server; tplId: string; variables: TemplateVariable[] }) {
  const { t } = useTranslation()
  const update = useUpdateServer(server.id)
  const [name, setName] = useState(server.name)
  const [values, setValues] = useState<Record<string, string>>(() => {
    const out: Record<string, string> = {}
    for (const v of variables) out[v.key] = server.variables[v.key] ?? v.default ?? (v.type === "bool" ? "false" : "")
    return out
  })
  const [memoryMb, setMemoryMb] = useState(String(server.limits?.memoryMb ?? 0))
  const [cpuPercent, setCpuPercent] = useState(String(server.limits?.cpuPercent ?? 0))
  const [autostart, setAutostart] = useState(server.autostart)
  const [localErrors, setLocalErrors] = useState<Record<string, string | undefined>>({})

  const apiField = isApiError(update.error) ? update.error.field : undefined
  const apiMessage = update.error ? describeError(t, update.error) : undefined
  const variableErrors = useMemo(() => {
    const out: Record<string, string | undefined> = { ...localErrors }
    if (apiField?.startsWith("variables.") && apiMessage) out[apiField.slice("variables.".length)] = apiMessage
    return out
  }, [localErrors, apiField, apiMessage])
  const generalError = update.error && !(apiField && (apiField === "name" || apiField.startsWith("variables."))) ? apiMessage : undefined
  const notReady = server.installState !== "ready"

  function submit(e: FormEvent) {
    e.preventDefault()
    const errors: Record<string, string | undefined> = {}
    if (!name.trim()) errors.__name = t("servers.validation.required")
    if (!/^\d+$/.test(memoryMb.trim())) errors.__memoryMb = t("servers.validation.integer")
    if (!/^\d+$/.test(cpuPercent.trim())) errors.__cpuPercent = t("servers.validation.integer")
    setLocalErrors(errors)
    if (Object.values(errors).some(Boolean)) return
    // Masked secrets go back masked; the backend keeps the stored value then.
    const vars = Object.fromEntries(variables.map((v) => [v.key, (values[v.key] ?? "").trim()]))
    update.mutate(
      { name: name.trim(), variables: vars, limits: { memoryMb: Number(memoryMb), cpuPercent: Number(cpuPercent) }, autostart },
      { onSuccess: () => toast.success(t("servers.saved")) },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("templates.settingsTitle")}</CardTitle>
        <CardDescription>{t("templates.settingsDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} noValidate>
          <FieldGroup>
            {notReady && (
              <Alert>
                <AlertDescription>{t("errors.not_installed")}</AlertDescription>
              </Alert>
            )}
            <Field data-invalid={!!(localErrors.__name ?? (apiField === "name" ? apiMessage : undefined)) || undefined}>
              <FieldLabel htmlFor="ts-name">{t("servers.name")}</FieldLabel>
              <Input id="ts-name" value={name} onChange={(e) => setName(e.target.value)} maxLength={64} />
              <FieldError>{localErrors.__name ?? (apiField === "name" ? apiMessage : undefined)}</FieldError>
            </Field>
            <VariableFields
              templateId={tplId}
              variables={variables}
              values={values}
              onChange={(k, v) => setValues((p) => ({ ...p, [k]: v }))}
              errors={variableErrors}
              lockConsent
              idPrefix="ts"
            />
            <div className="grid gap-5 sm:grid-cols-2">
              <Field data-invalid={!!localErrors.__memoryMb || undefined}>
                <FieldLabel htmlFor="ts-memoryMb">{t("servers.memoryMb")}</FieldLabel>
                <Input id="ts-memoryMb" type="number" inputMode="numeric" min={0} value={memoryMb} onChange={(e) => setMemoryMb(e.target.value)} />
                <FieldDescription>{t("templates.memoryLimitHelp")}</FieldDescription>
                <FieldError>{localErrors.__memoryMb}</FieldError>
              </Field>
              <Field data-invalid={!!localErrors.__cpuPercent || undefined}>
                <FieldLabel htmlFor="ts-cpuPercent">{t("servers.cpuPercent")}</FieldLabel>
                <Input id="ts-cpuPercent" type="number" inputMode="numeric" min={0} value={cpuPercent} onChange={(e) => setCpuPercent(e.target.value)} />
                <FieldDescription>{t("servers.cpuPercentHint")}</FieldDescription>
                <FieldError>{localErrors.__cpuPercent}</FieldError>
              </Field>
            </div>
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor="ts-autostart">{t("servers.autostart")}</FieldLabel>
                <FieldDescription>{t("servers.autostartHelp")}</FieldDescription>
              </FieldContent>
              <Switch id="ts-autostart" checked={autostart} onCheckedChange={setAutostart} />
            </Field>
            {generalError && (
              <Alert variant="destructive">
                <AlertDescription>{generalError}</AlertDescription>
              </Alert>
            )}
            <div className="flex flex-wrap items-center justify-end gap-3">
              {server.status.state !== "stopped" && server.status.state !== "crashed" && (
                <p className="mr-auto text-xs text-muted-foreground">{t("servers.appliesOnRestart")}</p>
              )}
              <Button type="submit" disabled={update.isPending || notReady}>
                {update.isPending ? t("servers.saving") : t("servers.save")}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}

function ReinstallCard({ server }: { server: Server }) {
  const { t } = useTranslation()
  const reinstall = useReinstall(server.id)
  const stopped = canDo(server.status.state, "ready").delete
  const busy = isInstalling(server) || reinstall.isPending
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("templates.reinstallTitle")}</CardTitle>
        <CardDescription>{t("templates.reinstallDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        {!stopped && !busy && <p className="text-sm text-muted-foreground">{t("servers.deleteRequiresStopped")}</p>}
        <Button
          variant="outline"
          className="ml-auto"
          disabled={!stopped || busy}
          onClick={() =>
            reinstall.mutate(undefined, {
              onSuccess: () => toast.success(t("templates.reinstallStarted")),
              onError: (e) => toast.error(describeError(t, e)),
            })
          }
        >
          <RotateCcwIcon />
          {t("templates.reinstall")}
        </Button>
      </CardContent>
    </Card>
  )
}
