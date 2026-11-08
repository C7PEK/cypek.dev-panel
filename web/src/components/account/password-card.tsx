import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldError, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { useChangePassword } from "@/lib/account"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"

export function PasswordCard() {
  const { t } = useTranslation()
  const change = useChangePassword()
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [repeat, setRepeat] = useState("")
  const [mismatch, setMismatch] = useState(false)

  const fieldErr = (f: string) => (isApiError(change.error) && change.error.field === f ? describeError(t, change.error) : undefined)
  const general = change.error && !(isApiError(change.error) && change.error.field) ? describeError(t, change.error) : null

  function submit(e: FormEvent) {
    e.preventDefault()
    if (next !== repeat) {
      setMismatch(true)
      return
    }
    setMismatch(false)
    change.mutate(
      { current, new: next },
      {
        onSuccess: () => {
          toast.success(t("profile.passwordChanged"))
          setCurrent("")
          setNext("")
          setRepeat("")
        },
      },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("profile.passwordTitle")}</CardTitle>
        <CardDescription>{t("profile.passwordDescription")}</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} noValidate className="max-w-md">
          <FieldGroup>
            <Field data-invalid={!!fieldErr("current") || undefined}>
              <FieldLabel htmlFor="pw-current">{t("profile.currentPassword")}</FieldLabel>
              <Input id="pw-current" type="password" value={current} onChange={(e) => setCurrent(e.target.value)} autoComplete="current-password" required />
              <FieldError>{fieldErr("current")}</FieldError>
            </Field>
            <Field data-invalid={!!fieldErr("password") || undefined}>
              <FieldLabel htmlFor="pw-new">{t("profile.newPassword")}</FieldLabel>
              <Input id="pw-new" type="password" value={next} onChange={(e) => setNext(e.target.value)} autoComplete="new-password" required />
              <FieldError>{fieldErr("password")}</FieldError>
            </Field>
            <Field data-invalid={mismatch || undefined}>
              <FieldLabel htmlFor="pw-repeat">{t("profile.repeatPassword")}</FieldLabel>
              <Input id="pw-repeat" type="password" value={repeat} onChange={(e) => setRepeat(e.target.value)} autoComplete="new-password" required />
              <FieldError>{mismatch ? t("setup.passwordMismatch") : undefined}</FieldError>
            </Field>
            {general && (
              <Alert variant="destructive">
                <AlertDescription>{general}</AlertDescription>
              </Alert>
            )}
            <div>
              <Button type="submit" disabled={change.isPending || !current || !next}>
                {change.isPending ? t("profile.saving") : t("profile.changePassword")}
              </Button>
            </div>
          </FieldGroup>
        </form>
      </CardContent>
    </Card>
  )
}
