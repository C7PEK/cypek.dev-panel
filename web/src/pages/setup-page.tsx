import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import { AuthShell } from "@/components/layout/auth-shell"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Field, FieldDescription, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isApiError } from "@/lib/api"
import { useSetup } from "@/lib/auth"
import { describeError } from "@/lib/errors"

export function SetupPage() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const setup = useSetup()
  const [token, setToken] = useState("")
  const [username, setUsername] = useState("")
  const [password, setPassword] = useState("")
  const [repeat, setRepeat] = useState("")
  const [localError, setLocalError] = useState<string | null>(null)

  const fieldError = (name: string) =>
    isApiError(setup.error) && setup.error.field === name ? describeError(t, setup.error) : undefined
  const generalError =
    setup.error && !(isApiError(setup.error) && setup.error.field) ? describeError(t, setup.error) : null

  function onSubmit(e: FormEvent) {
    e.preventDefault()
    setLocalError(null)
    if (password !== repeat) {
      setLocalError(t("setup.passwordMismatch"))
      return
    }
    setup.mutate(
      { token: token.trim(), username: username.trim(), password },
      {
        onSuccess: () => {
          toast.success(t("setup.done"))
          void navigate("/", { replace: true })
        },
      },
    )
  }

  return (
    <AuthShell title={t("setup.title")} description={t("setup.subtitle")}>
      <form onSubmit={onSubmit} noValidate>
        <FieldGroup>
          <Field data-invalid={!!fieldError("token") || undefined}>
            <FieldLabel htmlFor="token">{t("setup.tokenLabel")}</FieldLabel>
            <Input
              id="token"
              value={token}
              onChange={(e) => setToken(e.target.value)}
              placeholder={t("setup.tokenPlaceholder")}
              autoComplete="off"
              autoFocus
              required
              aria-invalid={!!fieldError("token")}
              className="font-mono"
            />
            <FieldDescription>{t("setup.tokenHelp")}</FieldDescription>
            <FieldError>{fieldError("token")}</FieldError>
          </Field>
          <Field data-invalid={!!fieldError("username") || undefined}>
            <FieldLabel htmlFor="username">{t("setup.usernameLabel")}</FieldLabel>
            <Input
              id="username"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete="username"
              required
              aria-invalid={!!fieldError("username")}
            />
            <FieldDescription>{t("setup.usernameHelp")}</FieldDescription>
            <FieldError>{fieldError("username")}</FieldError>
          </Field>
          <Field data-invalid={!!fieldError("password") || undefined}>
            <FieldLabel htmlFor="password">{t("setup.passwordLabel")}</FieldLabel>
            <Input
              id="password"
              type="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
              aria-invalid={!!fieldError("password")}
            />
            <FieldDescription>{t("setup.passwordHelp")}</FieldDescription>
            <FieldError>{fieldError("password")}</FieldError>
          </Field>
          <Field data-invalid={!!localError || undefined}>
            <FieldLabel htmlFor="repeat">{t("setup.passwordRepeatLabel")}</FieldLabel>
            <Input
              id="repeat"
              type="password"
              value={repeat}
              onChange={(e) => setRepeat(e.target.value)}
              autoComplete="new-password"
              required
              aria-invalid={!!localError}
            />
            <FieldError>{localError}</FieldError>
          </Field>
          {generalError && (
            <Alert variant="destructive">
              <AlertDescription>{generalError}</AlertDescription>
            </Alert>
          )}
          <Button type="submit" size="lg" className="w-full" disabled={setup.isPending}>
            {setup.isPending ? t("setup.submitting") : t("setup.submit")}
          </Button>
        </FieldGroup>
      </form>
    </AuthShell>
  )
}
