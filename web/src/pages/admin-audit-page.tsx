import { RefreshCwIcon } from "lucide-react"
import { Fragment, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"

import { AppShell } from "@/components/layout/app-shell"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import { useAudit } from "@/lib/users"
import type { User } from "@/lib/types"

export function AdminAuditPage({ user }: { user: User }) {
  const { t, i18n } = useTranslation()
  const audit = useAudit()
  const [filter, setFilter] = useState("")
  const [open, setOpen] = useState<number | null>(null)

  const items = useMemo(() => {
    const q = filter.trim().toLowerCase()
    const all = audit.data ?? []
    if (!q) return all
    return all.filter((e) =>
      [e.username, e.action, e.target, e.ip, JSON.stringify(e.details)].some((v) => (v ?? "").toLowerCase().includes(q)),
    )
  }, [audit.data, filter])

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("audit.title")}</h1>
            <p className="text-sm text-muted-foreground">{t("audit.description")}</p>
          </div>
          <div className="flex items-center gap-2">
            <Input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder={t("audit.filter")} className="w-56" />
            <Button variant="outline" size="icon" aria-label={t("app.retry")} onClick={() => void audit.refetch()}>
              <RefreshCwIcon />
            </Button>
          </div>
        </div>
        {audit.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, audit.error)}</AlertDescription>
          </Alert>
        )}
        <Card>
          <CardContent className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead className="label-caps text-left text-[0.62rem] text-muted-foreground">
                <tr>
                  <th className="py-1 pr-3">{t("audit.time")}</th>
                  <th className="py-1 pr-3">{t("audit.user")}</th>
                  <th className="py-1 pr-3">{t("audit.action")}</th>
                  <th className="py-1 pr-3">{t("audit.target")}</th>
                  <th className="py-1 pr-3">IP</th>
                </tr>
              </thead>
              <tbody>
                {items.length === 0 && (
                  <tr>
                    <td colSpan={5} className="py-6 text-center text-muted-foreground">
                      {t("audit.empty")}
                    </td>
                  </tr>
                )}
                {items.map((e) => {
                  const hasDetails = e.details && Object.keys(e.details).length > 0
                  return (
                    <Fragment key={e.id}>
                      <tr
                        className={hasDetails ? "cursor-pointer border-t hover:bg-muted/40" : "border-t"}
                        onClick={() => hasDetails && setOpen(open === e.id ? null : e.id)}
                      >
                        <td className="py-2 pr-3 whitespace-nowrap text-muted-foreground">{formatDateTime(e.createdAt, i18n.language)}</td>
                        <td className="py-2 pr-3">{e.username || "-"}</td>
                        <td className="py-2 pr-3 font-mono text-xs">{e.action}</td>
                        <td className="py-2 pr-3 font-mono text-xs break-all">{e.target}</td>
                        <td className="py-2 pr-3 font-mono text-xs">{e.ip}</td>
                      </tr>
                      {open === e.id && (
                        <tr>
                          <td colSpan={5} className="pb-3">
                            <pre className="overflow-x-auto rounded bg-muted p-2 font-mono text-xs">{JSON.stringify(e.details, null, 2)}</pre>
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </CardContent>
        </Card>
      </div>
    </AppShell>
  )
}
