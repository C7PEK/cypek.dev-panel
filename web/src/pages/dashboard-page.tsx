import { useQuery } from "@tanstack/react-query"
import { PlusIcon, ServerIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { AppShell } from "@/components/layout/app-shell"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { api } from "@/lib/api"
import type { SystemInfo, User } from "@/lib/types"

export function DashboardPage({ user }: { user: User }) {
  const { t } = useTranslation()
  const info = useQuery({ queryKey: ["system", "info"], queryFn: () => api<SystemInfo>("GET", "/system/info") })

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("dashboard.title")}</h1>
          <Button disabled title={t("dashboard.comingSoon")}>
            <PlusIcon />
            {t("dashboard.createServer")}
          </Button>
        </div>

        <Card className="border-dashed">
          <CardContent className="flex flex-col items-center gap-3 py-12 text-center">
            <div className="flex size-12 items-center justify-center rounded-full bg-muted">
              <ServerIcon className="size-6 text-muted-foreground" />
            </div>
            <div className="space-y-1">
              <h2 className="font-heading text-xl font-medium tracking-[0.04em] uppercase">{t("dashboard.emptyTitle")}</h2>
              <p className="max-w-md text-sm text-muted-foreground">{t("dashboard.emptyBody")}</p>
            </div>
            <Button disabled className="mt-2">
              <PlusIcon />
              {t("dashboard.createServer")}
            </Button>
            <p className="text-xs text-muted-foreground">{t("dashboard.comingSoon")}</p>
          </CardContent>
        </Card>

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
