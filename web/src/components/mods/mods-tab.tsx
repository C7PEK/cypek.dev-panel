import { Loader2Icon, RotateCcwIcon } from "lucide-react"
import { useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { InstalledList } from "@/components/mods/installed-list"
import { PlatformCard } from "@/components/mods/platform-card"
import { SearchSection } from "@/components/mods/search-section"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { describeError } from "@/lib/errors"
import { useMods } from "@/lib/mods"
import { usePower } from "@/lib/servers"
import type { ModInstallResult, Server } from "@/lib/types"

/**
 * Plugins (Paper & co.) or mods (Fabric, Forge, ...) from Modrinth.
 * The backend enforces files.read / files.write; canWrite only hides buttons.
 */
export function ModsTab({ server, canWrite }: { server: Server; canWrite: boolean }) {
  const { t } = useTranslation()
  const mods = useMods(server.id)
  const power = usePower(server.id)
  const [needsRestart, setNeedsRestart] = useState(false)
  const searchRef = useRef<HTMLElement>(null)
  const running = server.status.state === "running" || server.status.state === "starting"

  function changed(res?: ModInstallResult) {
    if (!res || res.restartNeeded !== false) setNeedsRestart(true)
  }

  if (mods.isPending) {
    return (
      <p className="flex items-center gap-2 text-sm text-muted-foreground">
        <Loader2Icon className="size-4 animate-spin" />
        {t("app.loading")}
      </p>
    )
  }
  if (mods.error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{describeError(t, mods.error)}</AlertDescription>
      </Alert>
    )
  }

  const { platform, installed } = mods.data
  const kind = platform.kind === "mods" ? "mods" : "plugins"

  return (
    <div className="flex flex-col gap-5">
      {needsRestart && running && (
        <Alert className="border-amber-500/40 bg-amber-500/10">
          <AlertTitle className="text-amber-200">{t("mods.restart.title")}</AlertTitle>
          <AlertDescription className="flex flex-wrap items-center justify-between gap-3">
            <span>{t("mods.restart.body")}</span>
            <span className="flex gap-2">
              <Button
                size="sm"
                disabled={power.isPending}
                onClick={() =>
                  power.mutate("restart", {
                    onSuccess: () => {
                      setNeedsRestart(false)
                      toast.success(t("mods.restart.started"))
                    },
                    onError: (e) => toast.error(t("power.failed", { message: describeError(t, e) })),
                  })
                }
              >
                {power.isPending ? <Loader2Icon className="animate-spin" /> : <RotateCcwIcon />}
                {t("mods.restart.now")}
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setNeedsRestart(false)}>
                {t("mods.restart.later")}
              </Button>
            </span>
          </AlertDescription>
        </Alert>
      )}

      <PlatformCard serverId={server.id} platform={platform} canWrite={canWrite} />

      {(platform.loader || installed.length > 0) && (
        <InstalledList
            serverId={server.id}
            items={installed}
            canWrite={canWrite}
            kind={kind}
            onChanged={changed}
            onBrowse={() => searchRef.current?.scrollIntoView({ behavior: "smooth", block: "start" })}
          />
      )}
      {platform.loader && <SearchSection ref={searchRef} serverId={server.id} platform={platform} canWrite={canWrite} onChanged={changed} />}
    </div>
  )
}
