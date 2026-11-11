import { ChevronRightIcon, ShieldIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { CopyField } from "@/components/databases/copy-field"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { useFirewall, useSystemInfo } from "@/lib/system"
import type { Port } from "@/lib/types"

export function PortHelp({ ports }: { ports: Pick<Port, "port" | "protocol">[] }) {
  const { t } = useTranslation()
  const fw = useFirewall(ports)
  const info = useSystemInfo()
  if (ports.length === 0) return null
  const lan = (info.data?.addresses ?? []).filter((a) => a.kind === "lan").map((a) => a.ip)
  const kind = fw.data?.firewall.kind
  const active = fw.data?.firewall.active && (kind === "ufw" || kind === "firewalld")
  const commands = (fw.data?.ports ?? []).flatMap((p) => p.commands)

  return (
    <div className="flex flex-col gap-3">
      {active && commands.length > 0 ? (
        <div className="rounded-lg border border-amber-500/40 bg-amber-500/10 p-3">
          <p className="mb-2 flex items-center gap-2 text-sm text-amber-200">
            <ShieldIcon className="size-4 shrink-0" />
            {t("portHelp.firewallActive", { kind })}
          </p>
          <CopyField label={t("portHelp.commands")} value={commands.join("\n")} multiline />
          <p className="mt-2 text-xs text-muted-foreground">{t("portHelp.notAutomatic")}</p>
        </div>
      ) : kind === "none" || (kind && !active && kind !== "unknown") ? (
        <p className="flex items-center gap-2 text-xs text-muted-foreground">
          <ShieldIcon className="size-3.5 shrink-0" />
          {t("portHelp.noFirewall")}
        </p>
      ) : null}

      <details className="group rounded-lg border bg-muted/20 px-3 py-2 text-sm">
        <summary className="flex cursor-pointer list-none items-center gap-2 font-medium [&::-webkit-details-marker]:hidden">
          <ChevronRightIcon className="size-4 shrink-0 transition-transform group-open:rotate-90" />
          {t("portHelp.howToTitle")}
        </summary>
        <ol className="mt-2 ml-6 list-decimal space-y-1.5 text-muted-foreground">
          <li>
            {t("portHelp.step1", { ports: ports.map((p) => `${p.port}/${p.protocol}`).join(", ") })}
            {lan.length > 0 && (
              <span className="ml-1">
                {t("portHelp.lanAddress")}{" "}
                {lan.map((ip) => (
                  <code key={ip} className="mr-1 rounded bg-muted px-1 font-mono text-xs text-foreground">
                    {ip}
                  </code>
                ))}
              </span>
            )}
          </li>
          <li>{t("portHelp.step2")}</li>
          <li>{t("portHelp.step3")}</li>
          <li>{t("portHelp.step4")}</li>
        </ol>
      </details>
    </div>
  )
}

export function PortHelpCard({ ports }: { ports: Port[] }) {
  const { t } = useTranslation()
  if (ports.length === 0) return null
  return (
    <Card>
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("portHelp.cardTitle")}</CardTitle>
        <CardDescription className="flex flex-wrap gap-2">
          {ports.map((p) => (
            <span key={`${p.name}-${p.port}`} className="rounded-md border bg-muted/40 px-2 py-0.5 font-mono text-xs">
              {p.port}/{p.protocol} <span className="text-muted-foreground">· {p.name}</span>
            </span>
          ))}
        </CardDescription>
      </CardHeader>
      <CardContent>
        <PortHelp ports={ports} />
      </CardContent>
    </Card>
  )
}
