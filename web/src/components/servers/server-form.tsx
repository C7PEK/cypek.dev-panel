import { type FormEvent, type ReactNode, useState } from "react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Field, FieldContent, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import type { CreateServerInput, Server } from "@/lib/types"

export type FormField =
  | "name"
  | "command"
  | "args"
  | "env"
  | "stopCommand"
  | "stopTimeout"
  | "readyRegex"
  | "memoryMb"
  | "cpuPercent"
  | "autostart"

export interface ServerFormValues {
  name: string
  command: string
  args: string
  env: string
  stopCommand: string
  stopTimeout: string
  readyRegex: string
  memoryMb: string
  cpuPercent: string
  autostart: boolean
}

export const defaultValues: ServerFormValues = {
  name: "",
  command: "",
  args: "",
  env: "",
  stopCommand: "",
  stopTimeout: "60",
  readyRegex: "",
  memoryMb: "0",
  cpuPercent: "0",
  autostart: false,
}

export function valuesFromServer(s: Server): ServerFormValues {
  return {
    name: s.name,
    command: s.run.command,
    args: (s.run.args ?? []).join("\n"),
    env: Object.entries(s.run.env ?? {})
      .map(([k, v]) => `${k}=${v}`)
      .join("\n"),
    stopCommand: s.run.stop?.command ?? "",
    stopTimeout: String(s.run.stop?.timeoutSec ?? 60),
    readyRegex: s.run.readyRegex ?? "",
    memoryMb: String(s.limits?.memoryMb ?? 0),
    cpuPercent: String(s.limits?.cpuPercent ?? 0),
    autostart: s.autostart,
  }
}

const FIELD_MAP: Record<string, FormField> = {
  name: "name",
  "run.command": "command",
  "run.args": "args",
  "run.env": "env",
  "run.stop": "stopCommand",
  "run.stop.command": "stopCommand",
  "run.stop.timeoutSec": "stopTimeout",
  "run.readyRegex": "readyRegex",
  "limits.memoryMb": "memoryMb",
  "limits.cpuPercent": "cpuPercent",
  autostart: "autostart",
}

function mapApiField(field: string | undefined): FormField | undefined {
  if (!field) return undefined
  return FIELD_MAP[field.replace(/\[\d+\]/g, "").replace(/\.\d+(?=\.|$)/g, "")]
}

function parseNonNegativeInt(raw: string): number | undefined {
  const s = raw.trim()
  return /^\d+$/.test(s) ? Number(s) : undefined
}

type Errors = Partial<Record<FormField, string>>

interface Props {
  formId: string
  initial: ServerFormValues
  onSubmit: (input: CreateServerInput) => void
  error: unknown
  autoFocusName?: boolean
  footer?: ReactNode
}

export function ServerForm({ formId, initial, onSubmit, error, autoFocusName, footer }: Props) {
  const { t } = useTranslation()
  const [values, setValues] = useState<ServerFormValues>(initial)
  const [localErrors, setLocalErrors] = useState<Errors>({})

  const set = <K extends keyof ServerFormValues>(key: K, value: ServerFormValues[K]) => {
    setValues((v) => ({ ...v, [key]: value }))
    setLocalErrors((e) => (e[key] ? { ...e, [key]: undefined } : e))
  }

  const apiField = isApiError(error) ? mapApiField(error.field) : undefined
  const apiMessage = error
    ? isApiError(error) && error.code === "validation_failed" && error.field
      ? error.message
      : describeError(t, error)
    : undefined
  const unmappedApiField = isApiError(error) && error.field && !apiField ? error.field : undefined
  const errorFor = (f: FormField) => localErrors[f] ?? (apiField === f ? apiMessage : undefined)
  const generalError = error && !apiField ? apiMessage : undefined

  function build(): CreateServerInput | null {
    const errors: Errors = {}
    const name = values.name.trim()
    const command = values.command.trim()
    if (!name) errors.name = t("servers.validation.required")
    if (!command) errors.command = t("servers.validation.required")

    const args = values.args
      .split("\n")
      .map((l) => l.replace(/\r$/, ""))
      .filter((l) => l.trim() !== "")

    const env: Record<string, string> = {}
    values.env.split("\n").forEach((rawLine, i) => {
      const line = rawLine.replace(/\r$/, "")
      if (line.trim() === "") return
      const eq = line.indexOf("=")
      const key = eq > 0 ? line.slice(0, eq).trim() : ""
      if (!key || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(key)) {
        errors.env ??= t("servers.validation.envLine", { line: i + 1 })
        return
      }
      env[key] = line.slice(eq + 1)
    })

    const timeoutSec = parseNonNegativeInt(values.stopTimeout)
    if (timeoutSec === undefined || timeoutSec < 1) errors.stopTimeout = t("servers.validation.integer")
    const memoryMb = parseNonNegativeInt(values.memoryMb)
    if (memoryMb === undefined) errors.memoryMb = t("servers.validation.integer")
    const cpuPercent = parseNonNegativeInt(values.cpuPercent)
    if (cpuPercent === undefined) errors.cpuPercent = t("servers.validation.integer")

    const readyRegex = values.readyRegex.trim()
    if (readyRegex) {
      try {
        new RegExp(readyRegex)
      } catch {
        errors.readyRegex = t("servers.validation.regex")
      }
    }
    if (Object.keys(errors).length > 0) {
      setLocalErrors(errors)
      return null
    }
    return {
      name,
      run: {
        command,
        args,
        env,
        stop: { command: values.stopCommand.trim(), timeoutSec: timeoutSec! },
        readyRegex,
      },
      limits: { memoryMb: memoryMb!, cpuPercent: cpuPercent! },
      autostart: values.autostart,
    }
  }

  function submit(e: FormEvent) {
    e.preventDefault()
    const input = build()
    if (input) onSubmit(input)
  }

  const field = (key: FormField, label: string, control: ReactNode, help?: string) => {
    const err = errorFor(key)
    return (
      <Field data-invalid={!!err || undefined}>
        <FieldLabel htmlFor={`${formId}-${key}`}>{label}</FieldLabel>
        {control}
        {help && <FieldDescription>{help}</FieldDescription>}
        <FieldError>{err}</FieldError>
      </Field>
    )
  }
  const inputProps = (key: FormField) => ({ id: `${formId}-${key}`, "aria-invalid": !!errorFor(key) })

  return (
    <form id={formId} onSubmit={submit} noValidate>
      <FieldGroup>
        <Alert>
          <AlertDescription>{t("servers.noShell")}</AlertDescription>
        </Alert>
        {field(
          "name",
          t("servers.name"),
          <Input
            {...inputProps("name")}
            value={values.name}
            onChange={(e) => set("name", e.target.value)}
            placeholder={t("servers.namePlaceholder")}
            autoFocus={autoFocusName}
            maxLength={64}
          />,
        )}
        {field(
          "command",
          t("servers.command"),
          <Input
            {...inputProps("command")}
            value={values.command}
            onChange={(e) => set("command", e.target.value)}
            placeholder="/usr/bin/java"
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
          />,
          t("servers.commandHelp"),
        )}
        {field(
          "args",
          t("servers.args"),
          <Textarea
            {...inputProps("args")}
            value={values.args}
            onChange={(e) => set("args", e.target.value)}
            placeholder={"-Xmx2G\n-jar\nserver.jar\nnogui"}
            className="min-h-24 font-mono"
            spellCheck={false}
          />,
          t("servers.argsHelp"),
        )}
        {field(
          "env",
          t("servers.env"),
          <Textarea
            {...inputProps("env")}
            value={values.env}
            onChange={(e) => set("env", e.target.value)}
            placeholder="NODE_ENV=production"
            className="min-h-16 font-mono"
            spellCheck={false}
          />,
          t("servers.envHelp"),
        )}
        <div className="grid gap-5 sm:grid-cols-[1fr_minmax(0,10rem)]">
          {field(
            "stopCommand",
            t("servers.stopCommand"),
            <Input
              {...inputProps("stopCommand")}
              value={values.stopCommand}
              onChange={(e) => set("stopCommand", e.target.value)}
              placeholder="stop"
              className="font-mono"
              autoComplete="off"
            />,
            t("servers.stopCommandHelp"),
          )}
          {field(
            "stopTimeout",
            t("servers.stopTimeout"),
            <Input
              {...inputProps("stopTimeout")}
              type="number"
              inputMode="numeric"
              min={1}
              step={1}
              value={values.stopTimeout}
              onChange={(e) => set("stopTimeout", e.target.value)}
            />,
            t("servers.stopTimeoutHelp"),
          )}
        </div>
        {field(
          "readyRegex",
          t("servers.readyRegex"),
          <Input
            {...inputProps("readyRegex")}
            value={values.readyRegex}
            onChange={(e) => set("readyRegex", e.target.value)}
            placeholder={"Done \\(\\d+\\.\\d+s\\)!"}
            className="font-mono"
            autoComplete="off"
            spellCheck={false}
          />,
          t("servers.readyRegexHelp"),
        )}
        <div className="grid gap-5 sm:grid-cols-2">
          {field(
            "memoryMb",
            t("servers.memoryMb"),
            <Input
              {...inputProps("memoryMb")}
              type="number"
              inputMode="numeric"
              min={0}
              step={1}
              value={values.memoryMb}
              onChange={(e) => set("memoryMb", e.target.value)}
            />,
            t("servers.unlimitedHint"),
          )}
          {field(
            "cpuPercent",
            t("servers.cpuPercent"),
            <Input
              {...inputProps("cpuPercent")}
              type="number"
              inputMode="numeric"
              min={0}
              step={1}
              value={values.cpuPercent}
              onChange={(e) => set("cpuPercent", e.target.value)}
            />,
            t("servers.cpuPercentHint"),
          )}
        </div>
        <Field orientation="horizontal">
          <FieldContent>
            <FieldLabel htmlFor={`${formId}-autostart`}>{t("servers.autostart")}</FieldLabel>
            <FieldDescription>{t("servers.autostartHelp")}</FieldDescription>
          </FieldContent>
          <Switch id={`${formId}-autostart`} checked={values.autostart} onCheckedChange={(v) => set("autostart", v)} />
        </Field>
        {generalError && (
          <Alert variant="destructive">
            <AlertDescription>{unmappedApiField ? `${unmappedApiField}: ${generalError}` : generalError}</AlertDescription>
          </Alert>
        )}
        {footer}
      </FieldGroup>
    </form>
  )
}
