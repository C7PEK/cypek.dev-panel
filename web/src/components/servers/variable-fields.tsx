import { ExternalLinkIcon, Loader2Icon } from "lucide-react"
import { type ReactNode, useState } from "react"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { Field, FieldContent, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { localized, useTemplateSource } from "@/lib/templates"
import type { TemplateVariable } from "@/lib/types"
import { cn } from "@/lib/utils"

interface Props {
  templateId: string
  variables: TemplateVariable[]
  values: Record<string, string>
  onChange: (key: string, value: string) => void
  /** Field errors keyed by variable key (from API "variables.KEY" or local checks). */
  errors?: Record<string, string | undefined>
  /** Consent variables already accepted (editing an installed server) are shown locked. */
  lockConsent?: boolean
  idPrefix?: string
}

/** Renders a template's variables as form fields. Values are always strings, like the API wants. */
export function VariableFields({ templateId, variables, values, onChange, errors = {}, lockConsent = false, idPrefix = "var" }: Props) {
  return (
    <>
      {variables.filter((v) => !v.hidden).map((v) => (
        <VariableField
          key={v.key}
          templateId={templateId}
          variable={v}
          value={values[v.key] ?? v.default ?? ""}
          onChange={(val) => onChange(v.key, val)}
          error={errors[v.key]}
          lockConsent={lockConsent}
          id={`${idPrefix}-${v.key}`}
        />
      ))}
    </>
  )
}

interface FieldProps {
  templateId: string
  variable: TemplateVariable
  value: string
  onChange: (value: string) => void
  error?: string
  lockConsent: boolean
  id: string
}

function VariableField({ templateId, variable: v, value, onChange, error, lockConsent, id }: FieldProps) {
  const { t, i18n } = useTranslation()
  const label = localized(v.label, i18n.resolvedLanguage) || v.key
  const help = localized(v.help, i18n.resolvedLanguage)
  const invalid = !!error || undefined

  if (v.type === "bool" && v.consent) {
    const checked = value === "true"
    const locked = lockConsent && checked
    return (
      <Field data-invalid={invalid}>
        <label
          className={cn(
            "flex cursor-pointer items-start gap-3 rounded-lg border p-3 text-sm transition-colors",
            checked ? "border-primary/40 bg-primary/5" : "border-border hover:bg-muted/40",
            locked && "cursor-default opacity-80",
          )}
        >
          <input
            id={id}
            type="checkbox"
            className="mt-0.5 size-4 shrink-0 accent-primary"
            checked={checked}
            disabled={locked}
            onChange={(e) => onChange(e.target.checked ? "true" : "false")}
            aria-invalid={invalid}
          />
          <span className="flex flex-col gap-0.5">
            <span className="font-medium">{linkify(label)}</span>
            {help && <span className="text-xs text-muted-foreground">{linkify(help)}</span>}
            {locked && <span className="text-xs text-muted-foreground">{t("templates.consentAccepted")}</span>}
          </span>
        </label>
        <FieldError>{error}</FieldError>
      </Field>
    )
  }

  if (v.type === "bool") {
    return (
      <Field orientation="horizontal" data-invalid={invalid}>
        <FieldContent>
          <FieldLabel htmlFor={id}>{label}</FieldLabel>
          {help && <FieldDescription>{help}</FieldDescription>}
          <FieldError>{error}</FieldError>
        </FieldContent>
        <Switch id={id} checked={value === "true"} onCheckedChange={(c) => onChange(c ? "true" : "false")} />
      </Field>
    )
  }

  let control: ReactNode
  switch (v.type) {
    case "number":
      control = (
        <Input
          id={id}
          type="number"
          inputMode="numeric"
          min={v.min}
          max={v.max}
          step={1}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          aria-invalid={invalid}
        />
      )
      break
    case "secret":
      control = (
        <Input
          id={id}
          type="password"
          value={value}
          onChange={(e) => onChange(e.target.value)}
          autoComplete="new-password"
          spellCheck={false}
          className="font-mono"
          aria-invalid={invalid}
        />
      )
      break
    case "select":
      control = v.source ? (
        <SourceSelect id={id} templateId={templateId} source={v.source} value={value} onChange={onChange} invalid={invalid} />
      ) : (
        <NativeSelect id={id} value={value} onChange={onChange} invalid={invalid}>
          {(v.options ?? []).map((o) => (
            <option key={o} value={o}>
              {o}
            </option>
          ))}
        </NativeSelect>
      )
      break
    default:
      control = (
        <Input
          id={id}
          value={value}
          onChange={(e) => onChange(e.target.value)}
          maxLength={v.maxLength || undefined}
          aria-invalid={invalid}
        />
      )
  }

  return (
    <Field data-invalid={invalid}>
      <FieldLabel htmlFor={id}>{label}</FieldLabel>
      {control}
      {help && <FieldDescription>{help}</FieldDescription>}
      {v.type === "number" && (v.min !== undefined || v.max !== undefined) && !help && (
        <FieldDescription>{t("templates.range", { min: v.min ?? "–", max: v.max ?? "–" })}</FieldDescription>
      )}
      <FieldError>{error}</FieldError>
    </Field>
  )
}

function NativeSelect({
  id,
  value,
  onChange,
  invalid,
  children,
}: {
  id: string
  value: string
  onChange: (v: string) => void
  invalid?: boolean
  children: ReactNode
}) {
  return (
    <select
      id={id}
      value={value}
      onChange={(e) => onChange(e.target.value)}
      aria-invalid={invalid}
      className={cn(
        "h-8 w-full rounded-lg border border-input bg-background px-2.5 text-sm text-foreground outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50",
        "dark:bg-input/30 aria-invalid:border-destructive [&>option]:bg-background [&>option]:text-foreground",
      )}
    >
      {children}
    </select>
  )
}

function SourceSelect({
  id,
  templateId,
  source,
  value,
  onChange,
  invalid,
}: {
  id: string
  templateId: string
  source: string
  value: string
  onChange: (v: string) => void
  invalid?: boolean
}) {
  const { t } = useTranslation()
  const [showPre, setShowPre] = useState(false)
  const q = useTemplateSource(templateId, source)

  if (q.isPending) {
    return (
      <div className="flex h-8 items-center gap-2 text-sm text-muted-foreground">
        <Loader2Icon className="size-4 animate-spin" />
        {t("templates.loadingOptions")}
      </div>
    )
  }
  if (q.error || !q.data) {
    return (
      <div className="flex flex-wrap items-center gap-2 text-sm text-destructive">
        {t("templates.optionsError")}
        <Button type="button" size="xs" variant="outline" onClick={() => void q.refetch()}>
          {t("app.retry")}
        </Button>
      </div>
    )
  }
  const current = q.data.find((o) => o.value === value)
  const visible = q.data.filter((o) => showPre || !o.prerelease || o.value === value)
  const hasPre = q.data.some((o) => o.prerelease)
  return (
    <div className="flex flex-col gap-1.5">
      <NativeSelect id={id} value={current ? value : ""} onChange={onChange} invalid={invalid}>
        {!current && <option value="">{t("templates.choose")}</option>}
        {visible.map((o) => (
          <option key={o.value} value={o.value}>
            {o.label}
          </option>
        ))}
      </NativeSelect>
      {hasPre && (
        <label className="flex items-center gap-2 text-xs text-muted-foreground">
          <input type="checkbox" className="size-3.5 accent-primary" checked={showPre} onChange={(e) => setShowPre(e.target.checked)} />
          {t("templates.showPrereleases")}
        </label>
      )}
    </div>
  )
}

/** Turns bare URLs in label text into links (EULA, license pages). */
function linkify(text: string): ReactNode {
  const parts = text.split(/(https?:\/\/[^\s)]+)/g)
  return parts.map((part, i) =>
    /^https?:\/\//.test(part) ? (
      <a
        key={i}
        href={part}
        target="_blank"
        rel="noreferrer noopener"
        className="inline-flex items-center gap-0.5 text-primary underline-offset-2 hover:underline"
        onClick={(e) => e.stopPropagation()}
      >
        {part.replace(/^https?:\/\//, "")}
        <ExternalLinkIcon className="size-3" />
      </a>
    ) : (
      part
    ),
  )
}
