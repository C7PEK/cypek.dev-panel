import { Loader2Icon } from "lucide-react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { describeError } from "@/lib/errors"
import { formatDateTime } from "@/lib/format"
import { useModVersions } from "@/lib/mods"
import type { ModSearchHit } from "@/lib/types"

interface Props {
  serverId: string
  hit: ModSearchHit | null
  installing: boolean
  onClose: () => void
  onInstall: (versionId: string) => void
}

/** Lets the user pick an older or pre-release version instead of the newest one. */
export function VersionsDialog({ serverId, hit, installing, onClose, onInstall }: Props) {
  const { t, i18n } = useTranslation()
  const versions = useModVersions(serverId, hit?.projectId)

  return (
    <Dialog open={!!hit} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>{t("mods.versions.title", { name: hit?.title ?? "" })}</DialogTitle>
          <DialogDescription>{t("mods.versions.body")}</DialogDescription>
        </DialogHeader>
        {versions.error ? (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, versions.error)}</AlertDescription>
          </Alert>
        ) : versions.isPending ? (
          <p className="flex items-center gap-2 py-4 text-sm text-muted-foreground">
            <Loader2Icon className="size-4 animate-spin" />
            {t("mods.search.loading")}
          </p>
        ) : versions.data.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">{t("mods.versions.none")}</p>
        ) : (
          <ul className="-mx-1 flex max-h-[55vh] flex-col gap-1 overflow-y-auto px-1">
            {versions.data.map((v, i) => (
              <li key={v.id} className="flex items-center gap-3 rounded-lg border px-3 py-2">
                <div className="min-w-0 flex-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="truncate font-mono text-sm">{v.number}</span>
                    {i === 0 && <Badge className="text-[0.62rem]">{t("mods.versions.newest")}</Badge>}
                    {v.type !== "release" && (
                      <Badge variant="outline" className="border-amber-500/40 text-[0.62rem] text-amber-300">
                        {t(`mods.versions.types.${v.type}`)}
                      </Badge>
                    )}
                  </div>
                  <p className="truncate text-xs text-muted-foreground">
                    {v.name !== v.number ? `${v.name} · ` : ""}
                    {formatDateTime(v.date, i18n.language)}
                    {v.gameVersions.length > 0 && ` · MC ${summarize(v.gameVersions)}`}
                  </p>
                </div>
                <Button size="sm" variant="outline" disabled={installing} onClick={() => onInstall(v.id)}>
                  {t("mods.search.install")}
                </Button>
              </li>
            ))}
          </ul>
        )}
      </DialogContent>
    </Dialog>
  )
}

/** "1.21.1 – 1.21.4" style summary; versions are newest-first or unsorted. */
function summarize(list: string[]): string {
  if (list.length <= 3) return list.join(", ")
  return `${list[0]} – ${list[list.length - 1]} (${list.length})`
}
