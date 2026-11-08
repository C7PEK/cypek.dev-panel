import { CheckIcon, ChevronDownIcon, DownloadIcon, ExternalLinkIcon, Loader2Icon, SearchIcon } from "lucide-react"
import { forwardRef, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { ModIcon } from "@/components/mods/mod-icon"
import { useInstallToast } from "@/components/mods/use-install-toast"
import { NativeSelect } from "@/components/mods/platform-card"
import { VersionsDialog } from "@/components/mods/versions-dialog"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { describeError } from "@/lib/errors"
import { formatCount, useInstallMod, useModSearch } from "@/lib/mods"
import type { ModInstallResult, ModPlatform, ModSearchHit, ModSort } from "@/lib/types"

const SORTS: ModSort[] = ["relevance", "downloads", "updated", "newest"]

function useDebounced<T>(value: T, ms: number): T {
  const [v, setV] = useState(value)
  useEffect(() => {
    const id = setTimeout(() => setV(value), ms)
    return () => clearTimeout(id)
  }, [value, ms])
  return v
}

interface Props {
  serverId: string
  platform: ModPlatform
  canWrite: boolean
  onChanged: (result?: ModInstallResult) => void
}

export const SearchSection = forwardRef<HTMLElement, Props>(function SearchSection({ serverId, platform, canWrite, onChanged }, ref) {
  const { t, i18n } = useTranslation()
  const [query, setQuery] = useState("")
  const [sort, setSort] = useState<ModSort>("relevance")
  const q = useDebounced(query.trim(), 300)
  const search = useModSearch(serverId, q, sort, platform)
  const install = useInstallMod(serverId)
  const showToast = useInstallToast()
  const [versionsFor, setVersionsFor] = useState<ModSearchHit | null>(null)
  const hits = search.data?.pages.flatMap((p) => p.hits) ?? []
  const total = search.data?.pages[0]?.total ?? 0

  function doInstall(hit: ModSearchHit, versionId?: string) {
    install.mutate(
      { projectId: hit.projectId, versionId },
      {
        onSuccess: (res) => {
          showToast(res, hit.title)
          setVersionsFor(null)
          onChanged(res)
        },
        onError: (e) => toast.error(describeError(t, e)),
      },
    )
  }

  const kind = platform.kind === "mods" ? "mods" : "plugins"

  return (
    <section ref={ref} className="flex flex-col gap-3">
      <div>
        <h2 className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("mods.search.title")}</h2>
        <p className="text-xs text-muted-foreground">{t(`mods.search.subtitle_${kind}`)}</p>
      </div>
      <div className="flex flex-col gap-2 sm:flex-row">
        <div className="relative flex-1">
          <SearchIcon className="pointer-events-none absolute top-1/2 left-2.5 size-3.5 -translate-y-1/2 text-muted-foreground" />
          <Input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            placeholder={t(`mods.search.placeholder_${kind}`)}
            className="pl-8"
            maxLength={100}
            aria-label={t("mods.search.title")}
          />
        </div>
        <NativeSelect value={sort} onChange={(v) => setSort(v as ModSort)} className="sm:w-48" aria-label={t("mods.search.sort")}>
          {SORTS.map((s) => (
            <option key={s} value={s}>
              {t(`mods.search.sorts.${s}`)}
            </option>
          ))}
        </NativeSelect>
      </div>

      {search.error ? (
        <Alert variant="destructive">
          <AlertDescription>{describeError(t, search.error)}</AlertDescription>
        </Alert>
      ) : search.isPending ? (
        <p className="flex items-center gap-2 py-6 text-sm text-muted-foreground">
          <Loader2Icon className="size-4 animate-spin" />
          {t("mods.search.loading")}
        </p>
      ) : hits.length === 0 ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t("mods.search.empty")}</p>
      ) : (
        <>
          {q && <p className="text-xs text-muted-foreground">{t("mods.search.results", { count: total })}</p>}
          <ul className="grid gap-2 md:grid-cols-2">
            {hits.map((hit) => {
              const busy = install.isPending && install.variables?.projectId === hit.projectId
              return (
                <li key={hit.projectId} className="flex gap-3 rounded-xl border bg-card p-3">
                  <ModIcon url={hit.iconUrl} className="size-12" />
                  <div className="flex min-w-0 flex-1 flex-col gap-1">
                    <div className="flex min-w-0 items-baseline gap-2">
                      <span className="truncate font-medium">{hit.title}</span>
                      {hit.author && <span className="truncate text-xs text-muted-foreground">{t("mods.search.by", { author: hit.author })}</span>}
                    </div>
                    <p className="line-clamp-2 text-xs text-muted-foreground">{hit.description}</p>
                    <div className="mt-auto flex flex-wrap items-center justify-between gap-2 pt-1">
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span className="inline-flex items-center gap-1" title={t("mods.search.downloads")}>
                          <DownloadIcon className="size-3" />
                          {formatCount(hit.downloads, i18n.language)}
                        </span>
                        <a href={hit.url} target="_blank" rel="noreferrer noopener" className="inline-flex items-center gap-0.5 hover:text-foreground">
                          Modrinth
                          <ExternalLinkIcon className="size-3" />
                        </a>
                      </div>
                      {canWrite &&
                        (hit.installed ? (
                          <Button size="sm" variant="outline" disabled>
                            <CheckIcon />
                            {t("mods.search.installed")}
                          </Button>
                        ) : (
                          <div className="flex">
                            <Button size="sm" className="rounded-r-none" disabled={busy || install.isPending} onClick={() => doInstall(hit)}>
                              {busy ? <Loader2Icon className="animate-spin" /> : <DownloadIcon />}
                              {t("mods.search.install")}
                            </Button>
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button size="sm" className="rounded-l-none border-l border-l-primary-foreground/20 px-1.5" disabled={install.isPending} aria-label={t("mods.search.chooseVersion")}>
                                  <ChevronDownIcon />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem onSelect={() => setVersionsFor(hit)}>{t("mods.search.chooseVersion")}</DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </div>
                        ))}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
          {search.hasNextPage && (
            <Button variant="outline" className="self-center" onClick={() => void search.fetchNextPage()} disabled={search.isFetchingNextPage}>
              {search.isFetchingNextPage && <Loader2Icon className="animate-spin" />}
              {t("mods.search.more")}
            </Button>
          )}
        </>
      )}

      <VersionsDialog
        serverId={serverId}
        hit={versionsFor}
        installing={install.isPending}
        onClose={() => setVersionsFor(null)}
        onInstall={(versionId) => versionsFor && doInstall(versionsFor, versionId)}
      />
    </section>
  )
})
