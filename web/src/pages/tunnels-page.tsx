import { CloudIcon, GlobeIcon, PlusIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { AppShell } from "@/components/layout/app-shell"
import { CopyField } from "@/components/databases/copy-field"
import { ServerTile } from "@/components/servers/server-tile"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { describeError } from "@/lib/errors"
import { useLiveStats } from "@/lib/events"
import { useServers } from "@/lib/servers"
import type { Server, User } from "@/lib/types"

function localAddress(s: Server) {
  const http = s.ports.find((p) => p.protocol === "tcp")
  return http ? `http://localhost:${http.port}` : ""
}

export function TunnelsPage({ user }: { user: User }) {
  const { t } = useTranslation()
  const servers = useServers()
  const live = useLiveStats()
  const canCreate = user.role !== "user"
  const tunnels = servers.data?.filter((s) => s.category === "tunnel") ?? []
  const sites = servers.data?.filter((s) => s.category === "website" && localAddress(s)) ?? []

  const createButton = (className?: string) =>
    canCreate ? (
      <Button className={className} asChild>
        <Link to="/servers/new?template=cloudflare-tunnel">
          <PlusIcon />
          {t("tunnels.create")}
        </Link>
      </Button>
    ) : null

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("tunnels.title")}</h1>
            <p className="mt-1 max-w-2xl text-sm text-muted-foreground">{t("tunnels.subtitle")}</p>
          </div>
          {createButton()}
        </div>

        {servers.error ? (
          <Alert variant="destructive">
            <AlertTitle>{t("servers.listError")}</AlertTitle>
            <AlertDescription>{describeError(t, servers.error)}</AlertDescription>
          </Alert>
        ) : servers.isPending ? (
          <div className="h-36 animate-pulse rounded-xl bg-muted/50" aria-busy />
        ) : tunnels.length === 0 ? (
          <Card className="border-dashed">
            <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
              <div className="flex size-12 items-center justify-center rounded-full bg-muted">
                <CloudIcon className="size-6 text-orange-500" />
              </div>
              <h2 className="font-heading text-xl font-medium tracking-[0.04em] uppercase">{t("tunnels.emptyTitle")}</h2>
              <p className="max-w-md text-sm text-muted-foreground">{t("tunnels.emptyBody")}</p>
              {createButton("mt-2")}
            </CardContent>
          </Card>
        ) : (
          <section aria-label={t("tunnels.title")} className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
            {tunnels.map((s) => (
              <ServerTile key={s.id} server={s} live={live.servers[s.id]} />
            ))}
          </section>
        )}

        <div className="grid gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader>
              <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("tunnels.howTitle")}</CardTitle>
              <CardDescription>{t("tunnels.howIntro")}</CardDescription>
            </CardHeader>
            <CardContent>
              <ol className="list-decimal space-y-2 pl-5 text-sm">
                <li>{t("tunnels.step1")}</li>
                <li>{t("tunnels.step2")}</li>
                <li>{t("tunnels.step3")}</li>
                <li>{t("tunnels.step4")}</li>
              </ol>
              <p className="mt-4 text-sm text-muted-foreground">{t("tunnels.quick")}</p>
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("tunnels.sitesTitle")}</CardTitle>
              <CardDescription>{t("tunnels.sitesIntro")}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {sites.length === 0 ? (
                <div className="flex flex-col items-start gap-3">
                  <p className="text-sm text-muted-foreground">{t("tunnels.noSites")}</p>
                  {canCreate && (
                    <Button variant="outline" size="sm" asChild>
                      <Link to="/servers/new?template=website-static">
                        <GlobeIcon />
                        {t("tunnels.createSite")}
                      </Link>
                    </Button>
                  )}
                </div>
              ) : (
                sites.map((s) => <CopyField key={s.id} label={s.name} value={localAddress(s)} />)
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </AppShell>
  )
}
