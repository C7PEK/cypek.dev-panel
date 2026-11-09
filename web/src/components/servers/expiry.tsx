import { CalendarClockIcon, CalendarXIcon, Loader2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"
import { toast } from "sonner"

import { NativeSelect } from "@/components/mods/platform-card"
import { DeleteServerDialog } from "@/components/servers/delete-server-dialog"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { formatDateTime, formatRelative } from "@/lib/format"
import { EXPIRY_PRESETS, type ExpiryChoice, expiryStatus, useSetExpiry } from "@/lib/servers"
import type { ExpiryInput, Server } from "@/lib/types"
import { cn } from "@/lib/utils"

const titleClass = "font-heading text-lg font-medium tracking-[0.04em] uppercase"

/** Local YYYY-MM-DD, `offsetDays` from today (for <input type="date">). */
function isoDate(offsetDays = 0, from = new Date()): string {
  const d = new Date(from.getTime() + offsetDays * 86_400_000)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

function useExpiryMutation(server: Server) {
  const { t, i18n } = useTranslation()
  const set = useSetExpiry(server.id)
  const run = (input: ExpiryInput, done?: () => void) =>
    set.mutate(input, {
      onSuccess: (info) => {
        if (!info.expiresAt) toast.success(t("expiry.cleared"))
        else toast.success(t("expiry.saved", { date: formatDateTime(info.expiresAt, i18n.language) }))
        done?.()
      },
      onError: (e) => {
        if (!(isApiError(e) && e.field === "expiresAt")) toast.error(describeError(t, e))
      },
    })
  return { set, run }
}

/** Small badge for tiles and headers; nothing when there is no end date. */
export function ExpiryBadge({ server, className }: { server: Server; className?: string }) {
  const { t } = useTranslation()
  const st = expiryStatus(server)
  if (st.kind === "none") return null
  if (st.kind === "expired") {
    return (
      <Badge variant="destructive" className={cn("label-caps text-[0.58rem]", className)}>
        {t("expiry.badge.expired")}
      </Badge>
    )
  }
  const text = st.kind === "today" ? t("expiry.badge.today") : t("expiry.badge.days", { count: st.days })
  const warn = st.kind === "today" || st.kind === "soon"
  return (
    <Badge
      variant="outline"
      title={formatDateTime(server.expiresAt, navigator.language)}
      className={cn("label-caps text-[0.58rem]", warn ? "border-amber-500/40 bg-amber-500/10 text-amber-300" : "text-muted-foreground", className)}
    >
      <CalendarClockIcon className="size-3" />
      {text}
    </Badge>
  )
}

/** Banner on the server page: expired (with admin actions) or expiring within 3 days. */
export function ExpiryBanner({ server, admin }: { server: Server; admin: boolean }) {
  const { t, i18n } = useTranslation()
  const { set, run } = useExpiryMutation(server)
  const [dateOpen, setDateOpen] = useState(false)
  const st = expiryStatus(server)
  if (st.kind === "expired") {
    return (
      <Alert variant="destructive">
        <CalendarXIcon />
        <AlertTitle>{t("expiry.expiredTitle")}</AlertTitle>
        <AlertDescription>
          <p>{t("expiry.banner.expired", { date: formatDateTime(server.expiredAt ?? server.expiresAt, i18n.language) })}</p>
          {admin ? (
            <div className="mt-3 flex flex-wrap gap-2">
              <Button size="sm" onClick={() => run({ extendDays: 30 })} disabled={set.isPending}>
                {set.isPending && <Loader2Icon className="animate-spin" />}
                {t("expiry.extend", { count: 30 })}
              </Button>
              <Button size="sm" variant="outline" onClick={() => setDateOpen(true)}>
                {t("expiry.setDate")}
              </Button>
              <DeleteServerDialog server={server} size="sm" />
            </div>
          ) : (
            <p className="mt-1">{t("expiry.banner.contactAdmin")}</p>
          )}
          {admin && <ExpiryDateDialog server={server} open={dateOpen} onOpenChange={setDateOpen} />}
        </AlertDescription>
      </Alert>
    )
  }
  if (st.kind === "today" || st.kind === "soon") {
    return (
      <Alert className="border-amber-500/40 bg-amber-500/10 text-amber-200">
        <CalendarClockIcon />
        <AlertDescription className="text-amber-200">
          {t("expiry.banner.soon", { relative: formatRelative(server.expiresAt, i18n.language), date: formatDateTime(server.expiresAt, i18n.language) })}
          {!admin && ` ${t("expiry.banner.contactAdmin")}`}
        </AlertDescription>
      </Alert>
    )
  }
  return null
}

/** Pick an exact end date. */
export function ExpiryDateDialog({ server, open, onOpenChange }: { server: Server; open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const { set, run } = useExpiryMutation(server)
  const [date, setDate] = useState(() => (server.expiresAt && !server.expired ? isoDate(0, new Date(server.expiresAt)) : isoDate(30)))
  const fieldError = isApiError(set.error) && set.error.field === "expiresAt" ? describeError(t, set.error) : undefined
  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o)
        if (!o) set.reset()
      }}
    >
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("expiry.dialog.title")}</DialogTitle>
          <DialogDescription>{t("expiry.dialog.description")}</DialogDescription>
        </DialogHeader>
        <Field data-invalid={!!fieldError || undefined}>
          <FieldLabel htmlFor={`expiry-date-${server.id}`}>{t("expiry.card.dateLabel")}</FieldLabel>
          <Input id={`expiry-date-${server.id}`} type="date" min={isoDate(1)} value={date} onChange={(e) => setDate(e.target.value)} className="max-w-48" />
          <FieldError>{fieldError}</FieldError>
        </Field>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>
            {t("power.cancel")}
          </Button>
          <Button disabled={!date || set.isPending} onClick={() => run({ expiresAt: date }, () => onOpenChange(false))}>
            {t("expiry.card.set")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

/** Settings tab card (admins): current end date, quick extensions, exact date, remove. */
export function ExpiryCard({ server }: { server: Server }) {
  const { t, i18n } = useTranslation()
  const { set, run } = useExpiryMutation(server)
  const [date, setDate] = useState(() => (server.expiresAt && !server.expired ? isoDate(0, new Date(server.expiresAt)) : isoDate(30)))
  const fieldError = isApiError(set.error) && set.error.field === "expiresAt" ? describeError(t, set.error) : undefined
  const st = expiryStatus(server)

  let current: string
  if (st.kind === "none") current = t("expiry.card.none")
  else if (st.kind === "expired") current = t("expiry.card.expiredAt", { date: formatDateTime(server.expiredAt ?? server.expiresAt, i18n.language) })
  else current = t("expiry.card.expires", { date: formatDateTime(server.expiresAt, i18n.language), relative: formatRelative(server.expiresAt, i18n.language) })

  return (
    <Card>
      <CardHeader>
        <CardTitle className={cn(titleClass, "flex items-center gap-2")}>
          <CalendarClockIcon className="size-4 text-primary" />
          {t("expiry.card.title")}
        </CardTitle>
        <CardDescription>{t("expiry.hint")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className={cn("text-sm", st.kind === "expired" ? "text-destructive" : st.kind === "today" || st.kind === "soon" ? "text-amber-300" : "")}>{current}</p>
        <div className="flex flex-wrap gap-2">
          {[7, 30, 90].map((d) => (
            <Button key={d} size="sm" variant="outline" disabled={set.isPending} onClick={() => run({ extendDays: d })}>
              {t("expiry.card.extendBy", { count: d })}
            </Button>
          ))}
          {st.kind !== "none" && (
            <Button size="sm" variant="ghost" disabled={set.isPending} onClick={() => run({ expiresAt: null })}>
              {t("expiry.card.clear")}
            </Button>
          )}
        </div>
        <Field data-invalid={!!fieldError || undefined}>
          <FieldLabel htmlFor={`expiry-card-date-${server.id}`}>{t("expiry.card.dateLabel")}</FieldLabel>
          <div className="flex flex-wrap items-center gap-2">
            <Input id={`expiry-card-date-${server.id}`} type="date" min={isoDate(1)} value={date} onChange={(e) => setDate(e.target.value)} className="max-w-48" />
            <Button size="sm" disabled={!date || set.isPending} onClick={() => run({ expiresAt: date })}>
              {set.isPending && <Loader2Icon className="animate-spin" />}
              {t("expiry.card.set")}
            </Button>
          </div>
          <FieldError>{fieldError}</FieldError>
        </Field>
      </CardContent>
    </Card>
  )
}

/** Dashboard card for admins listing expired servers. */
export function ExpiredServersCard({ servers }: { servers: Server[] }) {
  const { t, i18n } = useTranslation()
  const expired = servers.filter((s) => s.expired)
  if (expired.length === 0) return null
  return (
    <Card className="border-destructive/40 bg-destructive/5">
      <CardHeader>
        <CardTitle className={cn(titleClass, "flex items-center gap-2 text-destructive")}>
          <CalendarXIcon className="size-4" />
          {t("expiry.dashboard.title")}
        </CardTitle>
        <CardDescription>{t("expiry.dashboard.description")}</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col divide-y">
          {expired.map((s) => (
            <li key={s.id} className="flex flex-wrap items-center justify-between gap-2 py-2 first:pt-0 last:pb-0">
              <div className="min-w-0">
                <Link to={`/servers/${encodeURIComponent(s.id)}`} className="font-medium hover:underline">
                  {s.name}
                </Link>
                <span className="ml-2 text-xs text-muted-foreground">
                  {t("expiry.dashboard.since", { relative: formatRelative(s.expiredAt ?? s.expiresAt, i18n.language) })}
                </span>
              </div>
              <ExtendButton server={s} />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function ExtendButton({ server }: { server: Server }) {
  const { t } = useTranslation()
  const { set, run } = useExpiryMutation(server)
  return (
    <div className="flex flex-wrap gap-2">
      <Button size="sm" variant="outline" disabled={set.isPending} onClick={() => run({ extendDays: 30 })}>
        {set.isPending && <Loader2Icon className="animate-spin" />}
        {t("expiry.extend", { count: 30 })}
      </Button>
      <Button size="sm" variant="ghost" asChild>
        <Link to={`/servers/${encodeURIComponent(server.id)}`}>{t("expiry.dashboard.open")}</Link>
      </Button>
    </div>
  )
}

// --- wizard -------------------------------------------------------------------

/** "Czas działania" select for the creation forms. */
export function ExpiryField({
  idPrefix,
  choice,
  custom,
  onChoice,
  onCustom,
  error,
}: {
  idPrefix: string
  choice: ExpiryChoice
  custom: string
  onChoice: (c: ExpiryChoice) => void
  onCustom: (v: string) => void
  error?: string
}) {
  const { t } = useTranslation()
  return (
    <Field data-invalid={!!error || undefined}>
      <FieldLabel htmlFor={`${idPrefix}-expiry`}>{t("expiry.wizard.label")}</FieldLabel>
      <div className="flex flex-wrap items-center gap-2">
        <NativeSelect id={`${idPrefix}-expiry`} value={choice} onChange={(v) => onChoice(v as ExpiryChoice)} className="max-w-56">
          <option value="none">{t("expiry.wizard.none")}</option>
          {EXPIRY_PRESETS.map((d) => (
            <option key={d} value={d}>
              {t("expiry.wizard.days", { count: Number(d) })}
            </option>
          ))}
          <option value="custom">{t("expiry.wizard.custom")}</option>
        </NativeSelect>
        {choice === "custom" && (
          <Input
            id={`${idPrefix}-expiry-days`}
            type="number"
            inputMode="numeric"
            min={1}
            max={3650}
            value={custom}
            onChange={(e) => onCustom(e.target.value)}
            placeholder={t("expiry.wizard.customLabel")}
            aria-label={t("expiry.wizard.customLabel")}
            className="w-32"
          />
        )}
      </div>
      <FieldDescription>{t("expiry.hint")}</FieldDescription>
      <FieldError>{error}</FieldError>
    </Field>
  )
}
