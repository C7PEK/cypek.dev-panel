import { ArrowLeftIcon, ArrowRightIcon, CheckIcon, ChevronDownIcon, Loader2Icon, ScrollTextIcon } from "lucide-react"
import { type FormEvent, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, useNavigate } from "react-router"
import { toast } from "sonner"

import { AppShell } from "@/components/layout/app-shell"
import { ExpiryField } from "@/components/servers/expiry"
import { CategoryIcon } from "@/components/servers/category-icon"
import { CreateServerDialog } from "@/components/servers/create-server-dialog"
import { PortConflictNotice } from "@/components/servers/port-conflict-notice"
import { VariableFields } from "@/components/servers/variable-fields"
import { SourceBadge } from "@/components/templates/source-badge"
import { TemplatePicker } from "@/components/templates/template-picker"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { isApiError } from "@/lib/api"
import { describeError, describePortConflict, portConflictOf } from "@/lib/errors"
import { PortHelp } from "@/components/servers/port-help"
import { describeLiveConflict, portVariable, resolvePorts, usePortCheck } from "@/lib/ports"
import { type ExpiryChoice, expiryDays, useCreateServer } from "@/lib/servers"
import { localized, renderPort, sourceKind, useTemplates } from "@/lib/templates"
import type { CreateServerInput, Template, User } from "@/lib/types"
import { cn } from "@/lib/utils"

type Step = 1 | 2 | 3

interface Advanced {
  memoryMb: string
  cpuPercent: string
  autostart: boolean
}

const defaultAdvanced: Advanced = { memoryMb: "0", cpuPercent: "0", autostart: false }

function defaultsFor(t: Template): Record<string, string> {
  const out: Record<string, string> = {}
  for (const v of t.variables) out[v.key] = v.default ?? (v.type === "bool" ? "false" : "")
  return out
}

export function NewServerPage({ user }: { user: User }) {
  const { t, i18n } = useTranslation()
  const navigate = useNavigate()
  const templates = useTemplates()
  const create = useCreateServer()

  const [step, setStep] = useState<Step>(1)
  const [template, setTemplate] = useState<Template | null>(null)
  const [name, setName] = useState("")
  const [values, setValues] = useState<Record<string, string>>({})
  const [advanced, setAdvanced] = useState<Advanced>(defaultAdvanced)
  const [showAdvanced, setShowAdvanced] = useState(false)
  const [localErrors, setLocalErrors] = useState<Record<string, string | undefined>>({})
  const [customOpen, setCustomOpen] = useState(false)
  const [expiryChoice, setExpiryChoice] = useState<ExpiryChoice>("none")
  const [expiryCustom, setExpiryCustom] = useState("")

  const apiErr = create.error
  const apiField = isApiError(apiErr) ? apiErr.field : undefined
  const apiMessage = apiErr ? describeError(t, apiErr) : undefined
  const variableErrors = useMemo(() => {
    const out: Record<string, string | undefined> = { ...localErrors }
    if (apiField?.startsWith("variables.") && apiMessage) out[apiField.slice("variables.".length)] = apiMessage
    return out
  }, [localErrors, apiField, apiMessage])
  const nameError = localErrors.__name ?? (apiField === "name" ? apiMessage : undefined)
  const generalError =
    apiErr && !(apiField && (apiField === "name" || apiField === "expiresInDays" || apiField.startsWith("variables."))) ? apiMessage : undefined
  const expiryError = localErrors.__expiry ?? (apiField === "expiresInDays" ? apiMessage : undefined)

  const livePorts = useMemo(() => (template ? resolvePorts(template, values) : []), [template, values])
  const portCheck = usePortCheck(livePorts, undefined, step >= 2 && !!template)
  const liveConflicts = portCheck.data ?? []
  const createConflict = portConflictOf(apiErr)
  const conflictPorts = new Set(liveConflicts.map((c) => c.port))

  function pick(tpl: Template) {
    setTemplate(tpl)
    setValues(defaultsFor(tpl))
    setLocalErrors({})
    create.reset()
    setStep(2)
  }

  function setValue(key: string, value: string) {
    setValues((v) => ({ ...v, [key]: value }))
    setLocalErrors((e) => (e[key] ? { ...e, [key]: undefined } : e))
    if (apiField === `variables.${key}`) create.reset()
  }

  function applySuggestedPort(portName: string, port: number) {
    const key = template ? portVariable(template, portName) : undefined
    if (key) setValue(key, String(port))
  }

  function validateSettings(): boolean {
    if (!template) return false
    const errors: Record<string, string | undefined> = {}
    if (!name.trim()) errors.__name = t("servers.validation.required")
    for (const v of template.variables) {
      if (v.hidden) continue
      const raw = (values[v.key] ?? "").trim()
      if (v.type === "bool" && v.consent && raw !== "true") errors[v.key] = t("templates.consentRequired")
      if (v.type === "number") {
        if (raw === "" && v.required) errors[v.key] = t("servers.validation.required")
        else if (raw !== "") {
          const n = Number(raw)
          if (!Number.isFinite(n)) errors[v.key] = t("servers.validation.integer")
          else if (v.min !== undefined && n < v.min) errors[v.key] = t("templates.min", { min: v.min })
          else if (v.max !== undefined && n > v.max) errors[v.key] = t("templates.max", { max: v.max })
        }
      }
      if ((v.type === "string" || v.type === "secret") && v.required && raw === "") errors[v.key] = t("servers.validation.required")
      if (v.type === "select" && raw === "") errors[v.key] = t("templates.choose")
    }
    if (expiryDays(expiryChoice, expiryCustom) === null) errors.__expiry = t("expiry.wizard.invalid")
    for (const k of ["memoryMb", "cpuPercent"] as const) {
      if (!/^\d+$/.test(advanced[k].trim())) errors[`__${k}`] = t("servers.validation.integer")
    }
    setLocalErrors(errors)
    return Object.values(errors).every((e) => !e)
  }

  function next(e: FormEvent) {
    e.preventDefault()
    if (validateSettings()) setStep(3)
  }

  function submit(forcePorts = false) {
    if (!template) return
    const input: CreateServerInput = {
      name: name.trim(),
      forcePorts: forcePorts || undefined,
      templateId: template.id,
      variables: Object.fromEntries(template.variables.map((v) => [v.key, (values[v.key] ?? "").trim()])),
      limits: { memoryMb: Number(advanced.memoryMb) || 0, cpuPercent: Number(advanced.cpuPercent) || 0 },
      autostart: advanced.autostart,
      expiresInDays: expiryDays(expiryChoice, expiryCustom) ?? undefined,
    }
    create.mutate(input, {
      onSuccess: (server) => {
        toast.success(t("templates.creating"))
        void navigate(`/servers/${encodeURIComponent(server.id)}`)
      },
      onError: (err) => {
        if (isApiError(err) && err.field && (err.field === "name" || err.field === "expiresInDays" || err.field.startsWith("variables."))) setStep(2)
      },
    })
  }

  const steps: { n: Step; label: string }[] = [
    { n: 1, label: t("templates.step1") },
    { n: 2, label: t("templates.step2") },
    { n: 3, label: t("templates.step3") },
  ]

  return (
    <AppShell user={user}>
      <div className={cn("mx-auto flex w-full flex-col gap-6", step === 1 ? "max-w-5xl" : "max-w-3xl")}>
        <Link to="/" className="label-caps inline-flex items-center gap-1.5 text-[0.66rem] text-muted-foreground hover:text-foreground">
          <ArrowLeftIcon className="size-3" />
          {t("serverPage.back")}
        </Link>
        <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("templates.title")}</h1>

        <ol className="flex flex-wrap items-center gap-2">
          {steps.map((s, i) => {
            const state = s.n < step ? "done" : s.n === step ? "current" : "todo"
            return (
              <li key={s.n} className="flex items-center gap-2">
                <button
                  type="button"
                  disabled={state === "todo" || (s.n === 1 && step === 1)}
                  onClick={() => setStep(s.n)}
                  className={cn(
                    "label-caps inline-flex items-center gap-2 rounded-md border px-2.5 py-1 text-[0.66rem] transition-colors",
                    state === "current" && "border-primary/50 bg-primary/10 text-foreground",
                    state === "done" && "border-border text-muted-foreground hover:text-foreground",
                    state === "todo" && "border-border/60 text-muted-foreground/50",
                  )}
                >
                  <span
                    className={cn(
                      "flex size-4 items-center justify-center rounded-full text-[0.6rem] font-semibold",
                      state === "done" ? "bg-primary text-primary-foreground" : "bg-muted text-foreground",
                    )}
                  >
                    {state === "done" ? <CheckIcon className="size-3" /> : s.n}
                  </span>
                  {s.label}
                </button>
                {i < steps.length - 1 && <span className="h-px w-4 bg-border" />}
              </li>
            )
          })}
        </ol>

        {step === 1 && (
          <section className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">{t("templates.pickHelp")}</p>
            {templates.isPending ? (
              <div className="grid gap-4 sm:grid-cols-2" aria-busy>
                {[0, 1].map((i) => (
                  <div key={i} className="h-32 animate-pulse rounded-xl bg-muted/50" />
                ))}
              </div>
            ) : templates.error ? (
              <Alert variant="destructive">
                <AlertTitle>{t("templates.loadError")}</AlertTitle>
                <AlertDescription>
                  <p>{describeError(t, templates.error)}</p>
                  <Button variant="outline" size="sm" className="mt-3" onClick={() => void templates.refetch()}>
                    {t("app.retry")}
                  </Button>
                </AlertDescription>
              </Alert>
            ) : (
              <TemplatePicker templates={templates.data} onPick={pick} onCustom={() => setCustomOpen(true)} />
            )}
            <CreateServerDialog open={customOpen} onOpenChange={setCustomOpen} />
          </section>
        )}

        {step === 2 && template && (
          <form onSubmit={next} noValidate>
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em] uppercase">
                  <CategoryIcon category={template.category} templateId={template.id} className="size-4" />
                  {template.name}
                  {sourceKind(template) !== "builtin" && <SourceBadge template={template} className="ml-1" />}
                </CardTitle>
                <CardDescription>{t("templates.settingsHelp")}</CardDescription>
              </CardHeader>
              <CardContent>
                <FieldGroup>
                  <Field data-invalid={!!nameError || undefined}>
                    <FieldLabel htmlFor="new-name">{t("servers.name")}</FieldLabel>
                    <Input
                      id="new-name"
                      value={name}
                      onChange={(e) => {
                        setName(e.target.value)
                        setLocalErrors((er) => ({ ...er, __name: undefined }))
                      }}
                      placeholder={t("servers.namePlaceholder")}
                      maxLength={64}
                      autoFocus
                      aria-invalid={!!nameError}
                    />
                    <FieldError>{nameError}</FieldError>
                  </Field>
                  <VariableFields
                    templateId={template.id}
                    variables={template.variables}
                    values={values}
                    onChange={setValue}
                    errors={variableErrors}
                    idPrefix="new"
                  />
                  {createConflict ? (
                    <PortConflictNotice
                      error
                      message={`${describePortConflict(t, createConflict.code, createConflict)} ${t("ports.anywayHint")}`}
                      suggestion={createConflict.suggestion}
                      onUseSuggestion={() => {
                        const key = createConflict.field?.startsWith("variables.") ? createConflict.field.slice("variables.".length) : undefined
                        if (key && createConflict.suggestion) setValue(key, String(createConflict.suggestion))
                      }}
                      forceLabel={t("ports.createAnyway")}
                      onForce={() => submit(true)}
                      pending={create.isPending}
                    />
                  ) : (
                    liveConflicts.map((c) => (
                      <PortConflictNotice
                        key={`${c.name}-${c.port}`}
                        message={describeLiveConflict(t, c)}
                        suggestion={portVariable(template, c.name) ? c.suggestion : undefined}
                        onUseSuggestion={() => c.suggestion && applySuggestedPort(c.name, c.suggestion)}
                      />
                    ))
                  )}
                  <ExpiryField
                    idPrefix="new"
                    choice={expiryChoice}
                    custom={expiryCustom}
                    onChoice={(c) => {
                      setExpiryChoice(c)
                      setLocalErrors((e) => ({ ...e, __expiry: undefined }))
                    }}
                    onCustom={(v) => {
                      setExpiryCustom(v)
                      setLocalErrors((e) => ({ ...e, __expiry: undefined }))
                    }}
                    error={expiryError}
                  />
                  <button
                    type="button"
                    onClick={() => setShowAdvanced((v) => !v)}
                    className="label-caps inline-flex items-center gap-1.5 self-start text-[0.66rem] text-muted-foreground hover:text-foreground"
                    aria-expanded={showAdvanced}
                  >
                    <ChevronDownIcon className={cn("size-3 transition-transform", showAdvanced && "rotate-180")} />
                    {t("templates.advanced")}
                  </button>
                  {showAdvanced && (
                    <div className="flex flex-col gap-5 rounded-lg border p-4">
                      <div className="grid gap-5 sm:grid-cols-2">
                        <Field data-invalid={!!localErrors.__memoryMb || undefined}>
                          <FieldLabel htmlFor="new-memoryMb">{t("servers.memoryMb")}</FieldLabel>
                          <Input
                            id="new-memoryMb"
                            type="number"
                            inputMode="numeric"
                            min={0}
                            value={advanced.memoryMb}
                            onChange={(e) => setAdvanced((a) => ({ ...a, memoryMb: e.target.value }))}
                          />
                          <FieldDescription>{t("templates.memoryLimitHelp")}</FieldDescription>
                          <FieldError>{localErrors.__memoryMb}</FieldError>
                        </Field>
                        <Field data-invalid={!!localErrors.__cpuPercent || undefined}>
                          <FieldLabel htmlFor="new-cpuPercent">{t("servers.cpuPercent")}</FieldLabel>
                          <Input
                            id="new-cpuPercent"
                            type="number"
                            inputMode="numeric"
                            min={0}
                            value={advanced.cpuPercent}
                            onChange={(e) => setAdvanced((a) => ({ ...a, cpuPercent: e.target.value }))}
                          />
                          <FieldDescription>{t("servers.cpuPercentHint")}</FieldDescription>
                          <FieldError>{localErrors.__cpuPercent}</FieldError>
                        </Field>
                      </div>
                      <Field orientation="horizontal">
                        <FieldContent>
                          <FieldLabel htmlFor="new-autostart">{t("servers.autostart")}</FieldLabel>
                          <FieldDescription>{t("servers.autostartHelp")}</FieldDescription>
                        </FieldContent>
                        <Switch
                          id="new-autostart"
                          checked={advanced.autostart}
                          onCheckedChange={(c) => setAdvanced((a) => ({ ...a, autostart: c }))}
                        />
                      </Field>
                    </div>
                  )}
                  <div className="flex flex-wrap justify-between gap-2">
                    <Button type="button" variant="ghost" onClick={() => setStep(1)}>
                      <ArrowLeftIcon />
                      {t("templates.back")}
                    </Button>
                    <Button type="submit">
                      {t("templates.next")}
                      <ArrowRightIcon />
                    </Button>
                  </div>
                </FieldGroup>
              </CardContent>
            </Card>
          </form>
        )}

        {step === 3 && template && (
          <Card>
            <CardHeader>
              <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("templates.summaryTitle")}</CardTitle>
              <CardDescription>{t("templates.summaryHelp")}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-6">
              <dl className="grid gap-2 text-sm sm:grid-cols-[minmax(8rem,auto)_1fr] sm:gap-x-6">
                <dt className="text-muted-foreground">{t("servers.name")}</dt>
                <dd className="font-medium">{name}</dd>
                <dt className="text-muted-foreground">{t("templates.template")}</dt>
                <dd className="flex flex-wrap items-center gap-2">
                  {template.name}
                  <SourceBadge template={template} />
                </dd>
                {template.variables.filter((v) => !v.hidden).map((v) => (
                  <SummaryRow key={v.key} label={localized(v.label, i18n.resolvedLanguage) || v.key} value={displayValue(v.type, values[v.key] ?? "", t)} />
                ))}
                <dt className="text-muted-foreground">{t("expiry.wizard.label")}</dt>
                <dd>
                  {expiryDays(expiryChoice, expiryCustom)
                    ? t("expiry.wizard.summary", { count: expiryDays(expiryChoice, expiryCustom) ?? 0 })
                    : t("expiry.wizard.none")}
                </dd>
                {(advanced.memoryMb !== "0" || advanced.cpuPercent !== "0" || advanced.autostart) && (
                  <>
                    <dt className="text-muted-foreground">{t("templates.advanced")}</dt>
                    <dd className="text-muted-foreground">
                      {[
                        advanced.memoryMb !== "0" && `${t("servers.memoryMb")}: ${advanced.memoryMb}`,
                        advanced.cpuPercent !== "0" && `${t("servers.cpuPercent")}: ${advanced.cpuPercent}`,
                        advanced.autostart && t("servers.autostart"),
                      ]
                        .filter(Boolean)
                        .join(" · ")}
                    </dd>
                  </>
                )}
              </dl>

              <div className="rounded-lg border bg-muted/30 p-4">
                <div className="label-caps mb-2 text-[0.66rem] text-muted-foreground">{t("templates.portsTitle")}</div>
                {template.ports && template.ports.length > 0 ? (
                  <ul className="flex flex-wrap gap-2">
                    {template.ports.map((p) => {
                      const taken = conflictPorts.has(Number(renderPort(p, values)))
                      return (
                      <li
                        key={p.name}
                        className={cn(
                          "inline-flex items-center gap-2 rounded-md border bg-background px-2 py-1 font-mono text-xs",
                          taken && "border-amber-500/50 text-amber-200",
                        )}
                      >
                        <span
                          className={cn(
                            "size-1.5 rounded-full",
                            taken ? "bg-amber-400" : "brand-pulse bg-brand-cyan shadow-[0_0_8px_var(--brand-cyan)]",
                          )}
                        />
                        {renderPort(p, values)}/{p.protocol}
                        <span className="text-muted-foreground">· {p.name}</span>
                      </li>
                      )
                    })}
                  </ul>
                ) : (
                  <p className="text-sm text-muted-foreground">{t("templates.noPorts")}</p>
                )}
                <p className="mt-2 text-xs text-muted-foreground">{t("templates.portsHelp")}</p>
                {livePorts.length > 0 && (
                  <div className="mt-3">
                    <PortHelp ports={livePorts} />
                  </div>
                )}
              </div>
              {!createConflict &&
                liveConflicts.map((c) => (
                  <PortConflictNotice
                    key={`${c.name}-${c.port}`}
                    message={describeLiveConflict(t, c)}
                    suggestion={portVariable(template, c.name) ? c.suggestion : undefined}
                    onUseSuggestion={() => c.suggestion && applySuggestedPort(c.name, c.suggestion)}
                  />
                ))}

              {template.usesScript && (
                <Alert className="border-amber-500/40 bg-amber-500/10 text-amber-200">
                  <ScrollTextIcon />
                  <AlertTitle>{t("templateLibrary.scriptWarningTitle")}</AlertTitle>
                  <AlertDescription className="text-amber-200/90">
                    {t("templateLibrary.scriptWarning", { source: scriptSource(template, t) })}
                  </AlertDescription>
                </Alert>
              )}

              <p className="text-sm text-muted-foreground">{t("templates.installNote")}</p>

              {createConflict && !createConflict.field?.startsWith("variables.") ? (
                <PortConflictNotice
                  error
                  message={`${describePortConflict(t, createConflict.code, createConflict)} ${t("ports.anywayHint")}`}
                  forceLabel={t("ports.createAnyway")}
                  onForce={() => submit(true)}
                  pending={create.isPending}
                />
              ) : (
                generalError && (
                  <Alert variant="destructive">
                    <AlertDescription>{generalError}</AlertDescription>
                  </Alert>
                )
              )}
              <div className="flex flex-wrap justify-between gap-2">
                <Button type="button" variant="ghost" onClick={() => setStep(2)} disabled={create.isPending}>
                  <ArrowLeftIcon />
                  {t("templates.back")}
                </Button>
                <Button type="button" onClick={() => submit()} disabled={create.isPending}>
                  {create.isPending ? <Loader2Icon className="animate-spin" /> : <CheckIcon />}
                  {create.isPending ? t("servers.submitting") : t("templates.create")}
                </Button>
              </div>
            </CardContent>
          </Card>
        )}
      </div>
    </AppShell>
  )
}

function scriptSource(tpl: Template, t: (k: string) => string): string {
  const label = t(`templateLibrary.sources.${sourceKind(tpl)}`)
  const origin = tpl.source?.url || tpl.source?.author
  return origin ? `${label} (${origin})` : label
}

function SummaryRow({ label, value }: { label: string; value: string }) {
  return (
    <>
      <dt className="text-muted-foreground">{label}</dt>
      <dd className="font-mono text-xs sm:text-sm">{value || "-"}</dd>
    </>
  )
}

function displayValue(type: string, value: string, t: (k: string) => string): string {
  if (type === "secret") return value ? "••••••••" : ""
  if (type === "bool") return value === "true" ? t("templates.yes") : t("templates.no")
  return value
}
