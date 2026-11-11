import { ExternalLinkIcon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { CopyField } from "@/components/databases/copy-field"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { type DatabaseCredentials, phpMyAdminUrl } from "@/lib/databases"
import type { ServerCategory } from "@/lib/types"

interface Props {
  creds: DatabaseCredentials | null
  category: ServerCategory
  rotated?: boolean
  onClose: () => void
}

export function CredentialsDialog({ creds, category, rotated, onClose }: Props) {
  const { t } = useTranslation()
  const pma = phpMyAdminUrl(creds?.phpMyAdminPort)
  const fivem = (
    <section>
      <h3 className="mb-1.5 text-sm font-medium">{t("database.fivemTitle")}</h3>
      <CopyField label="server.cfg" value={creds?.fivemConfig || creds?.fivemConnectionString || ""} secret multiline />
      <p className="mt-1 text-xs text-muted-foreground">{t("database.fivemHint")}</p>
    </section>
  )
  const minecraft = (
    <section>
      <h3 className="mb-1.5 text-sm font-medium">{t("database.minecraftTitle")}</h3>
      <CopyField label="JDBC" value={creds?.jdbcUrl ?? ""} multiline />
      <p className="mt-1 text-xs text-muted-foreground">{t("database.minecraftHint")}</p>
    </section>
  )
  return (
    <Dialog open={!!creds} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-2xl">
        {creds && (
          <>
            <DialogHeader>
              <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("database.credentialsTitle")}</DialogTitle>
              <DialogDescription className="font-mono">{creds.database}</DialogDescription>
            </DialogHeader>
            {rotated && (
              <Alert>
                <AlertDescription>{t("database.rotatedWarning")}</AlertDescription>
              </Alert>
            )}
            <div className="grid gap-3 sm:grid-cols-2">
              <CopyField label={t("database.host")} value={creds.host} />
              <CopyField label={t("database.port")} value={String(creds.port)} />
              <CopyField label={t("database.database")} value={creds.database} />
              <CopyField label={t("database.user")} value={creds.user} />
              <div className="sm:col-span-2">
                <CopyField label={t("database.password")} value={creds.password} secret />
              </div>
            </div>
            {category === "minecraft" ? (
              <>
                {minecraft}
                {fivem}
              </>
            ) : (
              <>
                {fivem}
                {minecraft}
              </>
            )}
            {pma && <p className="text-xs text-muted-foreground">{t("database.phpMyAdminLoginHint", { user: creds.user })}</p>}
            <DialogFooter className="gap-2 sm:justify-between">
              {pma ? (
                <Button asChild variant="outline">
                  <a href={pma} target="_blank" rel="noreferrer">
                    <ExternalLinkIcon />
                    {t("database.openPhpMyAdmin")}
                  </a>
                </Button>
              ) : (
                <span />
              )}
              <Button onClick={onClose}>{t("database.close")}</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
