import { MemoryStickIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { CategoryIcon } from "@/components/servers/category-icon"
import { PowerButtons } from "@/components/servers/power-buttons"
import { StateBadge } from "@/components/servers/state-badge"
import { Card, CardContent } from "@/components/ui/card"
import { formatBytes } from "@/lib/format"
import type { Server } from "@/lib/types"

// Dashboard tile. The name is a "stretched link" covering the card so the
// buttons stay real <button>s instead of being nested inside an <a>.
export function ServerTile({ server }: { server: Server }) {
  const { t } = useTranslation()
  const { status } = server
  const running = status.state === "running" || status.state === "starting"
  const memory = running ? formatBytes(status.memoryBytes) : ""

  return (
    <Card className="relative transition-colors hover:border-foreground/25 has-[a:focus-visible]:ring-3 has-[a:focus-visible]:ring-ring/50">
      <CardContent className="flex flex-col gap-4">
        <div className="flex items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
            <CategoryIcon category={server.category} className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <Link
              to={`/servers/${encodeURIComponent(server.id)}`}
              className="block truncate font-heading text-lg font-medium tracking-[0.03em] outline-none after:absolute after:inset-0 after:rounded-xl after:content-['']"
            >
              {server.name}
            </Link>
            <div className="label-caps mt-0.5 text-[0.62rem] text-muted-foreground">
              {t(`servers.category.${server.category}`)}
            </div>
          </div>
          <StateBadge state={status.state} className="shrink-0" />
        </div>

        <div className="flex flex-wrap items-center justify-between gap-2">
          <div className="flex items-center gap-1.5 font-mono text-xs text-muted-foreground">
            {memory ? (
              <>
                <MemoryStickIcon className="size-3.5" />
                {memory}
              </>
            ) : status.state === "crashed" && status.exitCode !== undefined ? (
              t("servers.exitCode", { code: status.exitCode })
            ) : (
              <span className="text-muted-foreground/50">—</span>
            )}
          </div>
          <PowerButtons server={server} compact className="relative z-10" />
        </div>
      </CardContent>
    </Card>
  )
}
