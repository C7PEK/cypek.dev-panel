import { EyeIcon, KeyRoundIcon, ShieldOffIcon, WandSparklesIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CopyField } from "@/components/databases/copy-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isApiError } from "@/lib/api"
import { useDisableRootLogin, useRevealRootPassword, useRootLogin, useSetRootPassword } from "@/lib/databases"
import { describeError } from "@/lib/errors"

export function RootLoginCard({ phpMyAdminUrl }: { phpMyAdminUrl?: string }) {
  const { t } = useTranslation()
  const root = useRootLogin(true)
  const set = useSetRootPassword()
  const reveal = useRevealRootPassword()
  const disable = useDisableRootLogin()
  const [custom, setCustom] = useState("")
  const [shown, setShown] = useState<string | null>(null)
  const [confirmOff, setConfirmOff] = useState(false)
  const enabled = root.data?.enabled ?? false
  const fieldErr = isApiError(set.error) && set.error.field === "password" ? describeError(t, set.error) : undefined

  function apply(password: string) {
    set.mutate(password, {
      onSuccess: (r) => {
        setShown(r.password ?? null)
        setCustom("")
        toast.success(t("database.root.saved"))
      },
      onError: (e) => {
        if (!(isApiError(e) && e.field)) toast.error(describeError(t, e))
      },
    })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex flex-wrap items-center gap-2 font-heading text-base font-medium tracking-[0.06em] uppercase">
          <KeyRoundIcon className="size-4 text-muted-foreground" />
          {t("database.root.title")}
          <Badge variant="outline" className={enabled ? "border-amber-500/40 bg-amber-500/10 text-amber-300" : "text-muted-foreground"}>
            {enabled ? t("database.root.on") : t("database.root.off")}
          </Badge>
        </CardTitle>
        <CardDescription>{t("database.root.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Alert>
          <AlertDescription>{t("database.root.warning")}</AlertDescription>
        </Alert>

        {shown && (
          <div className="flex flex-col gap-2 rounded-lg border border-primary/40 bg-primary/5 p-3">
            <CopyField label={t("database.root.user")} value="root" />
            <CopyField label={t("database.root.password")} value={shown} secret />
            {phpMyAdminUrl && (
              <a className="text-sm text-[#8b95ff] underline-offset-4 hover:underline" href={phpMyAdminUrl} target="_blank" rel="noreferrer">
                {t("database.root.openPma")}
              </a>
            )}
          </div>
        )}

        <Field data-invalid={!!fieldErr || undefined}>
          <FieldLabel htmlFor="root-pw">{enabled ? t("database.root.changeLabel") : t("database.root.setLabel")}</FieldLabel>
          <Input
            id="root-pw"
            type="password"
            autoComplete="new-password"
            value={custom}
            onChange={(e) => setCustom(e.target.value)}
            placeholder={t("database.root.placeholder")}
            aria-invalid={!!fieldErr}
          />
          <FieldDescription>{t("database.root.rules")}</FieldDescription>
          <FieldError>{fieldErr}</FieldError>
        </Field>

        <div className="flex flex-wrap gap-2">
          <Button onClick={() => apply(custom)} disabled={set.isPending || custom.length === 0}>
            <KeyRoundIcon />
            {t("database.root.useMine")}
          </Button>
          <Button variant="outline" onClick={() => apply("")} disabled={set.isPending}>
            <WandSparklesIcon />
            {t("database.root.generate")}
          </Button>
          {enabled && !shown && (
            <Button
              variant="outline"
              disabled={reveal.isPending}
              onClick={() => reveal.mutate(undefined, { onSuccess: (r) => setShown(r.password ?? null), onError: (e) => toast.error(describeError(t, e)) })}
            >
              <EyeIcon />
              {t("database.root.reveal")}
            </Button>
          )}
          {enabled &&
            (confirmOff ? (
              <span className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/40 bg-destructive/10 px-2 py-1 text-xs text-destructive">
                {t("database.root.disableConfirm")}
                <Button
                  size="xs"
                  variant="destructive"
                  disabled={disable.isPending}
                  onClick={() =>
                    disable.mutate(undefined, {
                      onSuccess: () => {
                        setShown(null)
                        setConfirmOff(false)
                        toast.success(t("database.root.disabled"))
                      },
                      onError: (e) => toast.error(describeError(t, e)),
                    })
                  }
                >
                  {t("power.confirm")}
                </Button>
                <Button size="xs" variant="ghost" onClick={() => setConfirmOff(false)}>
                  {t("power.cancel")}
                </Button>
              </span>
            ) : (
              <Button variant="destructive" onClick={() => setConfirmOff(true)}>
                <ShieldOffIcon />
                {t("database.root.disable")}
              </Button>
            ))}
        </div>
      </CardContent>
    </Card>
  )
}
