import { useQuery } from "@tanstack/react-query"
import { ChevronDownIcon, ExternalLinkIcon, KeyRoundIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"

import { CopyField } from "@/components/databases/copy-field"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { api } from "@/lib/api"

interface SFTPInfo {
  enabled: boolean
  port: number
  fingerprint?: string
}

export function SftpCard({ serverId, username }: { serverId: string; username: string }) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const info = useQuery({ queryKey: ["system", "sftp"], queryFn: () => api<SFTPInfo>("GET", "/system/sftp"), staleTime: 5 * 60_000 })
  if (!info.data?.enabled) return null
  const host = window.location.hostname
  const login = `${username}.${serverId}`
  const url = `sftp://${encodeURIComponent(login)}@${host}:${info.data.port}/`
  return (
    <Card className="mb-4 py-3">
      <CardContent className="px-4">
        <button type="button" className="flex w-full items-center gap-2 text-left text-sm" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
          <KeyRoundIcon className="size-4 text-primary" />
          <span className="font-medium">{t("sftp.title")}</span>
          <span className="hidden text-muted-foreground sm:inline">· {t("sftp.apps")}</span>
          <ChevronDownIcon className={`ml-auto size-4 text-muted-foreground transition-transform ${open ? "rotate-180" : ""}`} />
        </button>
        {open && (
          <div className="mt-4 flex flex-col gap-4">
            <div className="grid gap-3 sm:grid-cols-3">
              <CopyField label={t("sftp.host")} value={host} />
              <CopyField label={t("sftp.port")} value={String(info.data.port)} />
              <CopyField label={t("sftp.login")} value={login} />
            </div>
            <p className="text-sm text-muted-foreground">{t("sftp.password")}</p>
            <p className="text-sm text-muted-foreground">{t("sftp.allServers", { username })}</p>
            <div className="flex flex-wrap items-center gap-2">
              <Button asChild size="sm" variant="outline">
                <a href={url}>
                  <ExternalLinkIcon />
                  {t("sftp.open")}
                </a>
              </Button>
              <span className="text-xs text-muted-foreground">{t("sftp.openHint")}</span>
            </div>
            {info.data.fingerprint && <CopyField label={t("sftp.fingerprint")} value={info.data.fingerprint} />}
            <p className="text-xs text-muted-foreground">{t("sftp.fingerprintHint")}</p>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
