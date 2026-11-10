import { ChevronDownIcon, DownloadCloudIcon } from "lucide-react"
import { type FormEvent, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { type LinkKind, detectLink, isArchive, useFetchLink } from "@/lib/files"
import { cn } from "@/lib/utils"

interface Props {
  serverId: string
  /** Current folder of the file browser ("" = root). */
  path: string
  open: boolean
  onOpenChange: (open: boolean) => void
}

const BROKEN: LinkKind[] = ["mega", "wetransfer"]

// "Download from link": the server fetches the file, so it does not go
// through the user's (usually slow) upload. Mounted only while open, so
// every opening starts with an empty form.
export function FetchLinkDialog({ serverId, path, open, onOpenChange }: Props) {
  const { t } = useTranslation()
  const fetchLink = useFetchLink(serverId)
  const [url, setUrl] = useState("")
  const [extract, setExtract] = useState(false)
  const [deleteArchive, setDeleteArchive] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)

  const kind = url.trim() ? detectLink(url) : null
  const broken = kind !== null && BROKEN.includes(kind)
  const looksArchive = isArchive(url.split(/[?#]/)[0] ?? "")
  const fieldError = isApiError(fetchLink.error) && fetchLink.error.field === "url" ? fetchLink.error.message : null
  const otherError = fetchLink.error && !fieldError ? fetchLink.error : null

  function submit(e: FormEvent) {
    e.preventDefault()
    if (!kind || kind === "invalid" || broken) return
    fetchLink.mutate(
      { url: url.trim(), path, extract, deleteArchive: extract && deleteArchive },
      {
        onSuccess: () => {
          toast.success(t("fetchLink.started"))
          onOpenChange(false)
        },
      },
    )
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !fetchLink.isPending && onOpenChange(o)}>
      <DialogContent className="sm:max-w-lg">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("fetchLink.title")}</DialogTitle>
          <DialogDescription>{t("fetchLink.description")}</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="fetch-url">{t("fetchLink.url")}</Label>
            <Input
              id="fetch-url"
              value={url}
              onChange={(e) => {
                setUrl(e.target.value)
                if (fetchLink.error) fetchLink.reset()
              }}
              placeholder="https://drive.google.com/file/d/…"
              autoFocus
              spellCheck={false}
              className="font-mono text-xs"
              aria-invalid={!!fieldError || broken}
            />
            {kind && (
              <div className="flex flex-wrap items-center gap-2 text-xs">
                <Badge variant={broken || kind === "invalid" ? "destructive" : "secondary"}>{t(`fetchLink.sources.${kind}`)}</Badge>
                <span className={cn(broken || kind === "invalid" ? "text-destructive" : "text-muted-foreground")}>{t(`fetchLink.tips.${kind}`)}</span>
              </div>
            )}
            {fieldError && <p className="text-xs text-destructive">{fieldError}</p>}
          </div>

          <p className="text-sm text-muted-foreground">
            {t("fetchLink.saveTo")} <span className="font-mono text-foreground">/{path}</span>
          </p>

          <div className="flex flex-col gap-3 rounded-lg border p-3">
            <label className="flex items-center justify-between gap-3 text-sm">
              <span>
                {t("fetchLink.extract")}
                {!looksArchive && <span className="block text-xs text-muted-foreground">{t("fetchLink.extractHint")}</span>}
              </span>
              <Switch checked={extract} onCheckedChange={setExtract} />
            </label>
            {extract && (
              <label className="flex items-center justify-between gap-3 pl-3 text-sm">
                <span>{t("fetchLink.deleteArchive")}</span>
                <Switch checked={deleteArchive} onCheckedChange={setDeleteArchive} />
              </label>
            )}
          </div>

          <div className="rounded-lg bg-muted/30 text-sm">
            <button type="button" className="flex w-full items-center gap-2 px-3 py-2 text-left font-medium" onClick={() => setHelpOpen((o) => !o)} aria-expanded={helpOpen}>
              {t("fetchLink.helpTitle")}
              <ChevronDownIcon className={cn("ml-auto size-4 text-muted-foreground transition-transform", helpOpen && "rotate-180")} />
            </button>
            {helpOpen && (
              <div className="flex flex-col gap-2 px-3 pb-3 text-muted-foreground">
                <ul className="list-disc space-y-1 pl-5">
                  <li>{t("fetchLink.help.dropbox")}</li>
                  <li>{t("fetchLink.help.gdrive")}</li>
                  <li>{t("fetchLink.help.onedrive")}</li>
                  <li>{t("fetchLink.help.github")}</li>
                  <li>{t("fetchLink.help.direct")}</li>
                  <li className="text-destructive">{t("fetchLink.help.broken")}</li>
                </ul>
                <p className="text-xs">{t("fetchLink.help.speed")}</p>
              </div>
            )}
          </div>

          {otherError ? (
            <Alert variant="destructive">
              <AlertDescription>{describeError(t, otherError)}</AlertDescription>
            </Alert>
          ) : null}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)} disabled={fetchLink.isPending}>
              {t("files.cancel")}
            </Button>
            <Button type="submit" disabled={!kind || kind === "invalid" || broken || fetchLink.isPending}>
              <DownloadCloudIcon />
              {t("fetchLink.submit")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
