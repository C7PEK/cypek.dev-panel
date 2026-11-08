import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { useLocation, useNavigate } from "react-router"

import { AuthShell } from "@/components/layout/auth-shell"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLoginStep1, useLoginTotp } from "@/lib/account"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation() as { state?: { from?: string } }
  const login = useLoginStep1()
  const second = useLoginTotp()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [token, setToken] = useState<string | null>(null)
  const [code, setCode] = useState("")

  const done = () => void navigate(location.state?.from ?? "/", { replace: true })

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    login.mutate(
      { username: username.trim(), password },
      {
        onSuccess: (r) => {
          if ("totpRequired" in r) {
            setToken(r.token)
            setCode("")
            return
          }
          done()
        },
      },
    )
  }

  function onCode(e: FormEvent) {
    e.preventDefault()
    if (!token) return
    second.mutate({ token, code: code.trim() }, { onSuccess: done })
  }

  function back() {
    setToken(null)
    setCode("")
    second.reset()
  }

  if (token) {
    const expired = isApiError(second.error) && second.error.status === 401
    return (
      <AuthShell title={t("login.totpTitle")} description={t("login.totpSubtitle")}>
        <form onSubmit={onCode} noValidate>
          <FieldGroup>
            <Field>
              <FieldLabel htmlFor="code">{t("login.totpCode")}</FieldLabel>
              <Input
                id="code"
                value={code}
                onChange={(e) => setCode(e.target.value)}
                autoComplete="one-time-code"
                inputMode="numeric"
                placeholder="123456"
                className="font-mono tracking-[0.3em]"
                autoFocus
                required
              />
              <FieldDescription>{t("login.totpHelp")}</FieldDescription>
            </Field>
            {second.error && (
              <Alert variant="destructive">
                <AlertDescription>{describeError(t, second.error)}</AlertDescription>
              </Alert>
            )}
            <Button type="submit" size="lg" className="w-full" disabled={second.isPending || expired}>
              {second.isPending ? t("login.submitting") : t("login.totpSubmit")}
            </Button>
            <Button type="button" variant="ghost" className="w-full" onClick={back}>
              {t("login.totpBack")}
            </Button>
          </FieldGroup>
        </form>
      </AuthShell>
    )
  }

  return (
    <AuthShell title={t("login.title")} description={t("login.subtitle")}>
      <form onSubmit={onSubmit} noValidate>
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="username">{t("login.usernameLabel")}</FieldLabel>
            <Input
              id="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              autoFocus
              required
            />
          </Field>
          <Field>
            <FieldLabel htmlFor="password">{t("login.passwordLabel")}</FieldLabel>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="current-password"
              required
            />
          </Field>
          {login.error && (
            <Alert variant="destructive">
              <AlertDescription>{describeError(t, login.error)}</AlertDescription>
            </Alert>
          )}
          <Button type="submit" size="lg" className="w-full" disabled={login.isPending}>
            {login.isPending ? t("login.submitting") : t("login.submit")}
          </Button>
        </FieldGroup>
      </form>
    </AuthShell>
  )
}
