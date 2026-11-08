import { useQuery } from "@tanstack/react-query"
import { PlusIcon, ServerIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { AppShell } from "@/components/layout/app-shell"
import { ServerTile } from "@/components/servers/server-tile"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { useServers } from "@/lib/servers"
import type { SystemInfo, User } from "@/lib/types"

export function DashboardPage({ user }: { user: User }) {
  const { t } = useTranslation()
  const info = useQuery({ queryKey: ["system", "info"], queryFn: () => api<SystemInfo>("GET", "/system/info") })
  const servers = useServers()
  const canCreate = user.role !== "user"

  const createButton = (className?: string) =>
    canCreate ? (
      <Button className={className} asChild>
        <Link to="/servers/new">
          <PlusIcon />
          {t("dashboard.createServer")}
        </Link>
      </Button>
    ) : null

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("dashboard.title")}</h1>
          {createButton()}
        </div>

        {servers.error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("servers.listError")}</AlertTitle>
            <AlertDescription>
              <p>{describeError(t, servers.error)}</p>
              <Button variant="outline" size="sm" className="mt-3" onClick={() => void servers.refetch()}>
                {t("app.retry")}
              </Button>
            </AlertDescription>
          </Alert>
        ) : servers.isPending ? (
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-busy>
            {[0, 1, 2].map((i) => (
              <div key={i} className="h-36 animate-pulse rounded-xl bg-muted/50" />
            ))}
          </div>
        ) : servers.data.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
              <div className="flex size-12 items-center justify-center rounded-full bg-muted">
                <ServerIcon className="size-6 text-muted-foreground" />
              </div>
              <div className="space-y-1">
                <h2 className="font-heading text-xl font-medium tracking-[0.04em] uppercase">
                  {t("dashboard.emptyTitle")}
                </h2>
                <p className="max-w-md text-sm text-muted-foreground">
                  {canCreate ? t("dashboard.emptyBody") : t("dashboard.emptyBodyUser")}
                </p>
              </div>
              {createButton("mt-2")}
            </CardContent>
          </Card>
        ) : (
          <section aria-label={t("dashboard.servers")} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {servers.data.map((s) => (
              <ServerTile key={s.id} server={s} />
            ))}
          </section>
        )}

        {info.data && (
          <Card>
            <CardHeader>
              <CardTitle className="label-caps text-xs text-muted-foreground">{t("dashboard.host")}</CardTitle>
              <CardDescription>
                {info.data.hostname} · {info.data.os}/{info.data.arch} · {t("app.version", { version: info.data.version })}
                {info.data.dev && <> · {t("dashboard.devMode")}</>}
              </CardDescription>
            </CardHeader>
            <CardContent>
              <dl className="grid gap-2 text-sm sm:grid-cols-[auto_1fr] sm:gap-x-6">
                <dt className="text-muted-foreground">{t("dashboard.addresses")}</dt>
                <dd className="flex flex-wrap gap-2">
                  {info.data.urls.map((u, i) => {
                    const kind = info.data.addresses[i]?.kind
                    return (
                      <a
                        key={u}
                        href={u}
                        className="inline-flex items-center gap-2 rounded-md border bg-muted/40 px-2 py-1 font-mono text-xs hover:bg-muted"
                      >
                        <span className="brand-pulse size-1.5 rounded-full bg-brand-cyan shadow-[0_0_8px_var(--brand-cyan)]" />
                        {u}
                        {kind && <span className="text-muted-foreground">· {t(`dashboard.addressKind.${kind}`)}</span>}
                      </a>
                    )
                  })}
                </dd>
              </dl>
            </CardContent>
          </Card>
        )}
      </div>
    </AppShell>
  )
}
