import { PlusIcon, ServerIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { AppShell } from "@/components/layout/app-shell"
import { ServerTile } from "@/components/servers/server-tile"
import { HostBar } from "@/components/stats/host-bar"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { describeError } from "@/lib/errors"
import { useLiveStats } from "@/lib/events"
import { useServers } from "@/lib/servers"
import type { User } from "@/lib/types"

export function DashboardPage({ user }: { user: User }) {
  const { t } = useTranslation()
  const servers = useServers()
  const live = useLiveStats()
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

        <HostBar host={live.host} connected={live.connected} />

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
              <ServerTile key={s.id} server={s} live={live.servers[s.id]} />
            ))}
          </section>
        )}

      </div>
    </AppShell>
  )
}
