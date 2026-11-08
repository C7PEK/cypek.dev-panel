import { CopyIcon, DownloadIcon, ShieldCheckIcon, ShieldOffIcon } from "lucide-react"
import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useTotpDisable, useTotpEnable, useTotpSetup, useTotpStatus } from "@/lib/account"
import { isApiError } from "@/lib/api"
import { copyText } from "@/lib/clipboard"
import { describeError } from "@/lib/errors"
import type { TotpSetup } from "@/lib/types"

async function copyWithToast(text: string, okMsg: string, failMsg: string, anchor?: Element | null) {
  if (await copyText(text, anchor)) toast.success(okMsg)
  else toast.error(failMsg)
}

export function TwoFactorCard({ enrollHint }: { enrollHint?: boolean }) {
  const { t } = useTranslation()
  const status = useTotpStatus()
  const setup = useTotpSetup()
  const enable = useTotpEnable()
  const disable = useTotpDisable()
  const [pending, setPending] = useState<TotpSetup | null>(null)
  const [code, setCode] = useState("")
  const [backup, setBackup] = useState<string[] | null>(null)
  const [saved, setSaved] = useState(false)
  const [disableCode, setDisableCode] = useState("")

  const fieldErr = (m: typeof enable | typeof disable) =>
    isApiError(m.error) && m.error.field === "code" ? describeError(t, m.error) : undefined

  function begin() {
    setup.mutate(undefined, {
      onSuccess: (s) => {
        setPending(s)
        setCode("")
      },
      onError: (e) => toast.error(describeError(t, e)),
    })
  }

  function confirm(e: FormEvent) {
    e.preventDefault()
    enable.mutate(code.trim(), {
      onSuccess: (r) => {
        setBackup(r.backupCodes)
        setPending(null)
        setSaved(false)
        toast.success(t("twoFactor.enabled"))
      },
    })
  }

  function off(e: FormEvent) {
    e.preventDefault()
    disable.mutate(disableCode.trim(), {
      onSuccess: () => {
        setDisableCode("")
        toast.success(t("twoFactor.disabled"))
      },
    })
  }

  const s = status.data
  const required = s?.required ?? enrollHint

  return (
    <Card id="two-factor">
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("twoFactor.title")}</CardTitle>
        <CardDescription>{t("twoFactor.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-5">
        {required && !s?.enabled && (
          <Alert>
            <AlertTitle>{t("twoFactor.requiredTitle")}</AlertTitle>
            <AlertDescription>{t("twoFactor.requiredBody")}</AlertDescription>
          </Alert>
        )}
        {status.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, status.error)}</AlertDescription>
          </Alert>
        )}
        {s && (
          <div className="flex flex-wrap items-center gap-2 text-sm">
            {s.enabled ? (
              <Badge variant="outline" className="gap-1 border-emerald-500/40 text-emerald-300">
                <ShieldCheckIcon /> {t("twoFactor.statusOn")}
              </Badge>
            ) : (
              <Badge variant="outline" className="gap-1 text-muted-foreground">
                <ShieldOffIcon /> {t("twoFactor.statusOff")}
              </Badge>
            )}
            {s.enabled && <span className="text-muted-foreground">{t("twoFactor.backupLeft", { count: s.backupCodesLeft })}</span>}
          </div>
        )}

        {backup && (
          <Alert>
            <AlertTitle>{t("twoFactor.backupTitle")}</AlertTitle>
            <AlertDescription className="space-y-3">
              <p>{t("twoFactor.backupBody")}</p>
              <div className="grid grid-cols-2 gap-1 font-mono text-sm sm:grid-cols-4">
                {backup.map((c) => (
                  <span key={c} className="rounded bg-muted px-2 py-1">
                    {c}
                  </span>
                ))}
              </div>
              <div className="flex flex-wrap gap-2">
                <Button size="sm" variant="outline" onClick={(e) => void copyWithToast(backup.join("\n"), t("profile.copied"), t("profile.copyFailed"), e.currentTarget)}>
                  <CopyIcon /> {t("profile.copy")}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    const blob = new Blob([`panel.cypek.dev backup codes\n\n${backup.join("\n")}\n`], { type: "text/plain" })
                    const a = document.createElement("a")
                    a.href = URL.createObjectURL(blob)
                    a.download = "panel-backup-codes.txt"
                    a.click()
                    URL.revokeObjectURL(a.href)
                  }}
                >
                  <DownloadIcon /> {t("twoFactor.download")}
                </Button>
              </div>
              <label className="flex items-center gap-2 text-sm">
                <input type="checkbox" className="size-4 accent-primary" checked={saved} onChange={(e) => setSaved(e.target.checked)} />
                {t("twoFactor.savedConfirm")}
              </label>
              <Button size="sm" disabled={!saved} onClick={() => setBackup(null)}>
                {t("twoFactor.done")}
              </Button>
            </AlertDescription>
          </Alert>
        )}

        {s && !s.enabled && !pending && !backup && (
          <div>
            <Button onClick={begin} disabled={setup.isPending}>
              <ShieldCheckIcon /> {setup.isPending ? t("profile.saving") : t("twoFactor.enable")}
            </Button>
          </div>
        )}

        {pending && (
          <form onSubmit={confirm} noValidate className="grid gap-5 md:grid-cols-[auto_1fr]">
            <img
              src={`data:image/png;base64,${pending.qrPng}`}
              alt={t("twoFactor.qrAlt")}
              width={200}
              height={200}
              className="rounded-md border bg-white p-2"
            />
            <FieldGroup>
              <p className="text-sm text-muted-foreground">{t("twoFactor.scanHelp")}</p>
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <span className="text-muted-foreground">{t("twoFactor.secret")}</span>
                <code className="rounded bg-muted px-2 py-1 font-mono break-all">{pending.secret}</code>
                <Button size="xs" variant="ghost" onClick={(e) => void copyWithToast(pending.secret, t("profile.copied"), t("profile.copyFailed"), e.currentTarget)} type="button">
                  <CopyIcon /> {t("profile.copy")}
                </Button>
              </div>
              <Field data-invalid={!!fieldErr(enable) || undefined}>
                <FieldLabel htmlFor="totp-code">{t("twoFactor.codeLabel")}</FieldLabel>
                <Input
                  id="totp-code"
                  value={code}
                  onChange={(e) => setCode(e.target.value)}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="123456"
                  className="max-w-48 font-mono tracking-[0.3em]"
                  autoFocus
                />
                <FieldDescription>{t("twoFactor.codeHelp")}</FieldDescription>
                <FieldError>{fieldErr(enable)}</FieldError>
              </Field>
              {enable.error && !fieldErr(enable) && (
                <Alert variant="destructive">
                  <AlertDescription>{describeError(t, enable.error)}</AlertDescription>
                </Alert>
              )}
              <div className="flex gap-2">
                <Button type="submit" disabled={enable.isPending || code.trim().length < 6}>
                  {enable.isPending ? t("profile.saving") : t("twoFactor.confirm")}
                </Button>
                <Button type="button" variant="ghost" onClick={() => setPending(null)}>
                  {t("profile.cancel")}
                </Button>
              </div>
            </FieldGroup>
          </form>
        )}

        {s?.enabled && !backup && (
          <form onSubmit={off} noValidate className="max-w-md border-t pt-4">
            <FieldGroup>
              <Field data-invalid={!!fieldErr(disable) || undefined}>
                <FieldLabel htmlFor="totp-disable">{t("twoFactor.disableLabel")}</FieldLabel>
                <Input
                  id="totp-disable"
                  value={disableCode}
                  onChange={(e) => setDisableCode(e.target.value)}
                  inputMode="numeric"
                  autoComplete="one-time-code"
                  placeholder="123456"
                  className="max-w-48 font-mono tracking-[0.3em]"
                />
                <FieldDescription>{s.required ? t("twoFactor.disableBlocked") : t("twoFactor.disableHelp")}</FieldDescription>
                <FieldError>{fieldErr(disable)}</FieldError>
              </Field>
              <div>
                <Button type="submit" variant="destructive" disabled={disable.isPending || !disableCode.trim() || s.required}>
                  <ShieldOffIcon /> {t("twoFactor.disable")}
                </Button>
              </div>
            </FieldGroup>
          </form>
        )}
      </CardContent>
    </Card>
  )
}
