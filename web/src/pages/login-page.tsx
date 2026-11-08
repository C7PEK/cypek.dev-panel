import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { useLocation, useNavigate } from "react-router"

import { AuthShell } from "@/components/layout/auth-shell"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useLogin } from "@/lib/auth"
import { describeError } from "@/lib/errors"

export function LoginPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const location = useLocation() as { state?: { from?: string } }
  const login = useLogin()
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    login.mutate(
      { username: username.trim(), password },
      { onSuccess: () => void navigate(location.state?.from ?? "/", { replace: true }) },
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
