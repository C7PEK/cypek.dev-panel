import { useTranslation } from "react-i18next"

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useSystemInfo } from "@/lib/system"

// Hostname, version and the addresses the panel is reachable at.
export function MachineInfoCard() {
  const { t } = useTranslation()
  const info = useSystemInfo()
  if (!info.data) return null
  return (
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
  )
}
