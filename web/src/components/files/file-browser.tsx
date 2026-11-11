import {
  ChevronRightIcon,
  DownloadCloudIcon,
  DownloadIcon,
  FilePlusIcon,
  FolderArchiveIcon,
  FolderPlusIcon,
  HomeIcon,
  Loader2Icon,
  MoreHorizontalIcon,
  PencilIcon,
  RefreshCwIcon,
  Trash2Icon,
  UploadIcon,
} from "lucide-react"
import { type DragEvent, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { FetchLinkDialog } from "@/components/files/fetch-link-dialog"
import { ConfirmDialog, NameDialog } from "@/components/files/file-dialogs"
import { FileEditor } from "@/components/files/file-editor"
import { FileIconFor } from "@/components/files/file-icon"
import { FileJobs } from "@/components/files/file-jobs"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { describeError } from "@/lib/errors"
import {
  type FileEntry,
  baseName,
  downloadUrl,
  isArchive,
  joinPath,
  parentPath,
  uploadFiles,
  useDeleteFiles,
  useExtract,
  useFiles,
  useMkdir,
  useRename,
  useWriteFile,
} from "@/lib/files"
import { formatBytes, formatDateTime } from "@/lib/format"
import { cn } from "@/lib/utils"

type DialogState =
  | { kind: "none" }
  | { kind: "mkdir" }
  | { kind: "newfile" }
  | { kind: "rename"; entry: FileEntry }
  | { kind: "delete"; entries: FileEntry[] }

interface Props {
  serverId: string
  path: string
  onPathChange: (path: string) => void
}

export function FileBrowser({ serverId, path, onPathChange }: Props) {
  const { t, i18n } = useTranslation()
  const [editing, setEditing] = useState<string | null>(null)
  const [dialog, setDialog] = useState<DialogState>({ kind: "none" })
  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [uploadPct, setUploadPct] = useState<number | null>(null)
  const [dragOver, setDragOver] = useState(false)
  const [fetchOpen, setFetchOpen] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const listing = useFiles(serverId, path, editing === null)
  const mkdir = useMkdir(serverId)
  const rename = useRename(serverId)
  const del = useDeleteFiles(serverId)
  const extract = useExtract(serverId)
  const write = useWriteFile(serverId)

  const closeDialog = () => {
    setDialog({ kind: "none" })
    mkdir.reset()
    rename.reset()
    del.reset()
  }

  function navigate(to: string) {
    setSelected(new Set())
    onPathChange(to)
  }

  function open(entry: FileEntry) {
    const full = joinPath(path, entry.name)
    if (entry.type === "dir") navigate(full)
    else if (entry.type === "file" && entry.editable) setEditing(full)
    else if (entry.type === "file") download(full)
  }

  function download(full: string) {
    const a = document.createElement("a")
    a.href = downloadUrl(serverId, full)
    a.download = baseName(full)
    a.rel = "noopener"
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  async function upload(files: File[]) {
    if (files.length === 0) return
    setUploadPct(0)
    try {
      const res = await uploadFiles(serverId, path, files, setUploadPct)
      toast.success(t("files.uploaded", { count: res.uploaded.length }))
      void listing.refetch()
    } catch (e) {
      toast.error(describeError(t, e))
    } finally {
      setUploadPct(null)
    }
  }

  function onDrop(e: DragEvent) {
    e.preventDefault()
    setDragOver(false)
    void upload(Array.from(e.dataTransfer.files))
  }

  function doExtract(entry: FileEntry) {
    const full = joinPath(path, entry.name)
    extract.mutate(
      { path: full, to: path },
      {
        onSuccess: () => toast.success(t("fileJobs.extractStarted", { name: entry.name })),
        onError: (err) => toast.error(describeError(t, err)),
      },
    )
  }

  function toggle(name: string) {
    setSelected((s) => {
      const n = new Set(s)
      if (n.has(name)) n.delete(name)
      else n.add(name)
      return n
    })
  }

  if (editing !== null) {
    return <FileEditor serverId={serverId} path={editing} onClose={() => setEditing(null)} />
  }

  const segments = path ? path.split("/") : []
  const entries = listing.data?.entries ?? []
  const selectedEntries = entries.filter((e) => selected.has(e.name))

  return (
    <div
      className={cn("flex flex-col gap-3 rounded-lg transition-colors", dragOver && "ring-2 ring-primary/60")}
      onDragOver={(e) => {
        e.preventDefault()
        setDragOver(true)
      }}
      onDragLeave={() => setDragOver(false)}
      onDrop={onDrop}
    >
      <nav className="flex flex-wrap items-center gap-1 text-sm" aria-label={t("files.path")}>
        <button
          type="button"
          className={cn("inline-flex items-center gap-1 rounded px-1.5 py-0.5 hover:bg-muted", path === "" && "font-medium")}
          onClick={() => navigate("")}
        >
          <HomeIcon className="size-3.5" />
          <span className="font-mono">/</span>
        </button>
        {segments.map((seg, i) => {
          const to = segments.slice(0, i + 1).join("/")
          const last = i === segments.length - 1
          return (
            <span key={to} className="inline-flex items-center gap-1">
              <ChevronRightIcon className="size-3 text-muted-foreground" />
              <button
                type="button"
                className={cn("max-w-48 truncate rounded px-1.5 py-0.5 font-mono hover:bg-muted", last && "font-medium")}
                onClick={() => navigate(to)}
                disabled={last}
              >
                {seg}
              </button>
            </span>
          )
        })}
      </nav>

      <div className="flex flex-wrap items-center gap-2">
        <input
          ref={inputRef}
          type="file"
          multiple
          className="hidden"
          onChange={(e) => {
            void upload(Array.from(e.target.files ?? []))
            e.target.value = ""
          }}
        />
        <Button size="sm" onClick={() => inputRef.current?.click()} disabled={uploadPct !== null}>
          {uploadPct !== null ? <Loader2Icon className="animate-spin" /> : <UploadIcon />}
          {uploadPct !== null ? t("files.uploading", { pct: uploadPct }) : t("files.upload")}
        </Button>
        <Button size="sm" variant="outline" onClick={() => setFetchOpen(true)}>
          <DownloadCloudIcon />
          {t("fetchLink.button")}
        </Button>
        <Button size="sm" variant="outline" onClick={() => setDialog({ kind: "mkdir" })}>
          <FolderPlusIcon />
          {t("files.newFolder")}
        </Button>
        <Button size="sm" variant="outline" onClick={() => setDialog({ kind: "newfile" })}>
          <FilePlusIcon />
          {t("files.newFile")}
        </Button>
        <Button size="sm" variant="ghost" onClick={() => void listing.refetch()} aria-label={t("files.refresh")} title={t("files.refresh")}>
          <RefreshCwIcon className={cn(listing.isFetching && "animate-spin")} />
        </Button>
        {selectedEntries.length > 0 && (
          <Button size="sm" variant="destructive" className="ml-auto" onClick={() => setDialog({ kind: "delete", entries: selectedEntries })}>
            <Trash2Icon />
            {t("files.deleteSelected", { count: selectedEntries.length })}
          </Button>
        )}
      </div>

      <FileJobs serverId={serverId} onOpenFolder={navigate} />
      {fetchOpen && <FetchLinkDialog serverId={serverId} path={path} open={fetchOpen} onOpenChange={setFetchOpen} />}

      {uploadPct !== null && (
        <div className="h-1.5 w-full overflow-hidden rounded bg-muted">
          <div className="h-full bg-primary transition-[width]" style={{ width: `${uploadPct}%` }} />
        </div>
      )}

      {listing.error ? (
        <Alert variant="destructive">
          <AlertDescription>
            <p>{describeError(t, listing.error)}</p>
            <div className="mt-2 flex gap-2">
              <Button size="sm" variant="outline" onClick={() => void listing.refetch()}>
                {t("app.retry")}
              </Button>
              {path !== "" && (
                <Button size="sm" variant="ghost" onClick={() => navigate(parentPath(path))}>
                  {t("files.goUp")}
                </Button>
              )}
            </div>
          </AlertDescription>
        </Alert>
      ) : listing.isPending ? (
        <p className="py-6 text-center text-sm text-muted-foreground">{t("app.loading")}</p>
      ) : entries.length === 0 ? (
        <div className="rounded-lg border border-dashed py-10 text-center text-sm text-muted-foreground">
          <p>{t("files.empty")}</p>
          <p className="mt-1 text-xs">{t("files.emptyHint")}</p>
        </div>
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <table className="w-full text-sm">
            <thead className="label-caps text-[0.62rem] text-muted-foreground">
              <tr className="border-b">
                <th className="w-8 px-2 py-2">
                  <input
                    type="checkbox"
                    className="size-3.5 accent-primary"
                    aria-label={t("files.selectAll")}
                    checked={selected.size > 0 && selected.size === entries.length}
                    onChange={(e) => setSelected(e.target.checked ? new Set(entries.map((x) => x.name)) : new Set())}
                  />
                </th>
                <th className="px-2 py-2 text-left font-normal">{t("files.name")}</th>
                <th className="hidden px-2 py-2 text-right font-normal sm:table-cell">{t("files.size")}</th>
                <th className="hidden px-2 py-2 text-left font-normal md:table-cell">{t("files.modified")}</th>
                <th className="w-10 px-2 py-2" />
              </tr>
            </thead>
            <tbody>
              {path !== "" && (
                <tr className="border-b hover:bg-muted/40">
                  <td />
                  <td className="px-2 py-1.5" colSpan={3}>
                    <button type="button" className="inline-flex items-center gap-2 font-mono text-muted-foreground hover:text-foreground" onClick={() => navigate(parentPath(path))}>
                      <span className="w-4 text-center">..</span>
                      {t("files.goUp")}
                    </button>
                  </td>
                  <td />
                </tr>
              )}
              {entries.map((entry) => (
                <tr key={entry.name} className={cn("border-b last:border-0 hover:bg-muted/40", selected.has(entry.name) && "bg-primary/5")}>
                  <td className="px-2 py-1.5">
                    <input
                      type="checkbox"
                      className="size-3.5 accent-primary"
                      aria-label={entry.name}
                      checked={selected.has(entry.name)}
                      onChange={() => toggle(entry.name)}
                    />
                  </td>
                  <td className="max-w-0 px-2 py-1.5">
                    <button
                      type="button"
                      className={cn("flex w-full min-w-0 items-center gap-2 text-left", entry.type === "other" && "cursor-default")}
                      onClick={() => open(entry)}
                      title={entry.name}
                    >
                      <FileIconFor entry={entry} className={cn("size-4 shrink-0", entry.type === "dir" ? "text-amber-300" : "text-muted-foreground")} />
                      <span className="truncate font-mono">{entry.name}</span>
                      {entry.type === "file" && entry.editable === false && (
                        <span className="label-caps hidden text-[0.58rem] text-muted-foreground sm:inline">{t("files.tooLargeToEdit")}</span>
                      )}
                    </button>
                  </td>
                  <td className="hidden px-2 py-1.5 text-right font-mono text-xs text-muted-foreground sm:table-cell">
                    {entry.type === "dir" ? "" : formatBytes(entry.size) || `${entry.size} B`}
                  </td>
                  <td className="hidden px-2 py-1.5 text-xs text-muted-foreground md:table-cell">{formatDateTime(entry.modifiedAt, i18n.language)}</td>
                  <td className="px-1 py-1.5 text-right">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon-sm" aria-label={t("files.actions")}>
                          <MoreHorizontalIcon />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        {entry.type === "dir" ? (
                          <DropdownMenuItem onSelect={() => open(entry)}>{t("files.open")}</DropdownMenuItem>
                        ) : (
                          <>
                            {entry.editable && <DropdownMenuItem onSelect={() => open(entry)}>{t("files.edit")}</DropdownMenuItem>}
                            <DropdownMenuItem onSelect={() => download(joinPath(path, entry.name))}>
                              <DownloadIcon />
                              {t("files.download")}
                            </DropdownMenuItem>
                            {isArchive(entry.name) && (
                              <DropdownMenuItem onSelect={() => doExtract(entry)} disabled={extract.isPending}>
                                <FolderArchiveIcon />
                                {t("files.extract")}
                              </DropdownMenuItem>
                            )}
                          </>
                        )}
                        <DropdownMenuItem onSelect={() => setDialog({ kind: "rename", entry })}>
                          <PencilIcon />
                          {t("files.rename")}
                        </DropdownMenuItem>
                        <DropdownMenuSeparator />
                        <DropdownMenuItem variant="destructive" onSelect={() => setDialog({ kind: "delete", entries: [entry] })}>
                          <Trash2Icon />
                          {t("files.delete")}
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <NameDialog
        open={dialog.kind === "mkdir"}
        onOpenChange={(o) => !o && closeDialog()}
        title={t("files.newFolder")}
        label={t("files.folderName")}
        submitLabel={t("files.create")}
        pending={mkdir.isPending}
        error={mkdir.error}
        onSubmit={(name) => mkdir.mutate(joinPath(path, name), { onSuccess: closeDialog })}
      />
      <NameDialog
        open={dialog.kind === "newfile"}
        onOpenChange={(o) => !o && closeDialog()}
        title={t("files.newFile")}
        label={t("files.fileName")}
        submitLabel={t("files.create")}
        pending={write.isPending}
        error={write.error}
        onSubmit={(name) =>
          write.mutate(
            { path: joinPath(path, name), content: "" },
            {
              onSuccess: () => {
                closeDialog()
                setEditing(joinPath(path, name))
              },
            },
          )
        }
      />
      <NameDialog
        open={dialog.kind === "rename"}
        onOpenChange={(o) => !o && closeDialog()}
        title={t("files.rename")}
        label={t("files.newName")}
        initial={dialog.kind === "rename" ? dialog.entry.name : ""}
        submitLabel={t("files.rename")}
        pending={rename.isPending}
        error={rename.error}
        onSubmit={(name) => {
          if (dialog.kind !== "rename") return
          rename.mutate({ from: joinPath(path, dialog.entry.name), to: joinPath(path, name) }, { onSuccess: closeDialog })
        }}
      />
      <ConfirmDialog
        open={dialog.kind === "delete"}
        onOpenChange={(o) => !o && closeDialog()}
        title={t("files.deleteTitle")}
        body={
          dialog.kind === "delete" ? (
            <div className="space-y-2">
              <p>{t("files.deleteBody", { count: dialog.entries.length })}</p>
              <ul className="max-h-40 overflow-auto rounded border bg-muted/30 p-2 font-mono text-xs">
                {dialog.entries.map((e) => (
                  <li key={e.name} className="truncate">
                    {e.type === "dir" ? `${e.name}/` : e.name}
                  </li>
                ))}
              </ul>
              {dialog.entries.some((e) => e.type === "dir") && <p className="text-xs text-destructive">{t("files.deleteDirsWarning")}</p>}
            </div>
          ) : null
        }
        confirmLabel={t("files.delete")}
        pending={del.isPending}
        error={del.error}
        onConfirm={() => {
          if (dialog.kind !== "delete") return
          del.mutate(
            dialog.entries.map((e) => joinPath(path, e.name)),
            {
              onSuccess: () => {
                setSelected(new Set())
                closeDialog()
              },
            },
          )
        }}
      />
    </div>
  )
}
