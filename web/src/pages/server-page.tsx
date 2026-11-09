import { ArrowLeftIcon, CpuIcon, MemoryStickIcon, Trash2Icon, UsersIcon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Link, Navigate, useNavigate, useParams } from "react-router"
import { toast } from "sonner"

import { AppShell } from "@/components/layout/app-shell"
import { CategoryIcon } from "@/components/servers/category-icon"
import { Console } from "@/components/console/console"
import { FileBrowser } from "@/components/files/file-browser"
import { InstallBadge } from "@/components/servers/install-badge"
import { InstallPanel } from "@/components/servers/install-panel"
import { TemplateSettings } from "@/components/servers/template-settings"
import { ServerAlertsCard } from "@/components/alerts/server-alerts-card"
import { GitUpdateButton } from "@/components/servers/git-update-button"
import { PowerButtons } from "@/components/servers/power-buttons"
import { ServerForm, valuesFromServer } from "@/components/servers/server-form"
import { StateBadge } from "@/components/servers/state-badge"
import { ScheduleTab } from "@/components/schedules/schedule-tab"
import { ServerUsersTab } from "@/components/admin/server-users-tab"
import { BackupsTab } from "@/components/backups/backups-tab"
import { ServerDatabasesTab } from "@/components/databases/server-databases-tab"
import { ModsTab } from "@/components/mods/mods-tab"
import { EventsPanel } from "@/components/servers/events-panel"
import { ResourceCharts } from "@/components/stats/resource-charts"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { isApiError } from "@/lib/api"
import { describeError } from "@/lib/errors"
import { useLiveStats } from "@/lib/events"
import { useMods } from "@/lib/mods"
import { formatBytes, formatPercent } from "@/lib/format"
import { canDo, mergeLive, useDeleteServer, useServer, useUpdateServer } from "@/lib/servers"
import { useTemplates } from "@/lib/templates"
import type { Server, User } from "@/lib/types"

export function ServerPage({ user }: { user: User }) {
  const { id = "" } = useParams()
  const { t } = useTranslation()
  const server = useServer(id)

  if (server.isPending) {
    return (
      <AppShell user={user}>
        <p className="text-sm text-muted-foreground">{t("app.loading")}</p>
      </AppShell>
    )
  }
  if (server.error) {
    if (isApiError(server.error) && server.error.status === 404) return <Navigate to="/" replace />
    return (
      <AppShell user={user}>
        <Alert variant="destructive">
          <AlertDescription>{describeError(t, server.error)}</AlertDescription>
        </Alert>
      </AppShell>
    )
  }
  return <ServerView server={server.data} user={user} />
}

function ServerView({ server: raw, user }: { server: Server; user: User }) {
  const { t } = useTranslation()
  const live = useLiveStats()
  const liveServer = live.servers[raw.id]
  const s = mergeLive(raw, liveServer)
  const ready = s.installState === "ready"
  const running = s.status.state === "running" || s.status.state === "starting"
  const ramMax = s.limits?.memoryMb > 0 ? s.limits.memoryMb * 1024 * 1024 : undefined
  // The Files tab keeps its folder while the page stays open.
  const [filesPath, setFilesPath] = useState("")
  // Plugins/mods tab: Minecraft only, and hidden when the API refuses (no files.read).
  const isMinecraft = s.category === "minecraft"
  const mods = useMods(s.id, isMinecraft && ready)
  const modsRefused = isApiError(mods.error) && (mods.error.status === 403 || mods.error.code === "not_minecraft")
  const showMods = isMinecraft && ready && !modsRefused
  const modsLabel = mods.data?.platform.kind === "mods" ? t("serverPage.tabs.mods") : t("serverPage.tabs.plugins")
  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-5">
        <Link to="/" className="label-caps inline-flex items-center gap-1.5 text-[0.66rem] text-muted-foreground hover:text-foreground">
          <ArrowLeftIcon className="size-3" />
          {t("serverPage.back")}
        </Link>
        <header className="flex flex-wrap items-start justify-between gap-4">
          <div className="flex min-w-0 items-center gap-3">
            <span className="flex size-11 shrink-0 items-center justify-center rounded-lg border bg-muted/40">
              <CategoryIcon category={s.category} className="size-5" />
            </span>
            <div className="min-w-0">
              <h1 className="truncate font-heading text-2xl font-semibold tracking-[0.04em] uppercase sm:text-3xl">{s.name}</h1>
              <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                <StateBadge state={s.status.state} />
                {!ready && <InstallBadge state={s.installState} />}
                <TemplateLabel server={s} />
                {s.status.pid ? <span className="font-mono">PID {s.status.pid}</span> : null}
                {s.players && (
                  <span className="inline-flex items-center gap-1 font-mono" title={t("console.players.title")}>
                    <UsersIcon className="size-3.5" />
                    {s.players.online}/{s.players.max}
                  </span>
                )}
                {running && (liveServer?.sample?.memoryBytes ?? s.status.memoryBytes) !== undefined && (
                  <span className="inline-flex items-center gap-1 font-mono" title={t("stats.ram")}>
                    <MemoryStickIcon className="size-3.5" />
                    {formatBytes(liveServer?.sample?.memoryBytes ?? s.status.memoryBytes)}
                    {ramMax ? <span className="text-muted-foreground/60">/ {formatBytes(ramMax)}</span> : null}
                  </span>
                )}
                {running && liveServer?.sample && (
                  <span className="inline-flex items-center gap-1 font-mono" title={t("stats.cpu")}>
                    <CpuIcon className="size-3.5" />
                    {formatPercent(liveServer.sample.cpuPercent)}
                  </span>
                )}
              </div>
              {s.status.state === "crashed" && s.status.message && (
                <p className="mt-1 max-w-xl text-xs text-destructive">{s.status.message}</p>
              )}
            </div>
          </div>
          <div className="flex flex-col items-end gap-2">
            <PowerButtons server={s} withKill />
            {s.gitUpdate && <GitUpdateButton server={s} size="sm" className="hidden sm:inline-flex" />}
          </div>
        </header>

        <Tabs defaultValue="console">
          <TabsList className="w-full justify-start overflow-x-auto">
            <TabsTrigger value="console">{t("serverPage.tabs.console")}</TabsTrigger>
            <TabsTrigger value="files">{t("serverPage.tabs.files")}</TabsTrigger>
            {showMods && <TabsTrigger value="mods">{modsLabel}</TabsTrigger>}
            <TabsTrigger value="schedule">{t("serverPage.tabs.schedule")}</TabsTrigger>
            <TabsTrigger value="backups">{t("serverPage.tabs.backups")}</TabsTrigger>
            <TabsTrigger value="databases">{t("serverPage.tabs.databases")}</TabsTrigger>
            <TabsTrigger value="settings">{t("serverPage.tabs.settings")}</TabsTrigger>
            {user.role !== "user" && <TabsTrigger value="users">{t("serverPage.tabs.users")}</TabsTrigger>}
          </TabsList>
          <TabsContent value="console" className="flex flex-col gap-5 pt-3">
            {ready ? <Console serverId={s.id} initialState={s.status.state} /> : <InstallPanel server={s} />}
            {ready && <ResourceCharts serverId={s.id} />}
            {ready && <EventsPanel serverId={s.id} />}
          </TabsContent>
          <TabsContent value="files" className="pt-3">
            <FileBrowser serverId={s.id} path={filesPath} onPathChange={setFilesPath} />
          </TabsContent>
          {showMods && (
            <TabsContent value="mods" className="pt-3">
              <ModsTab server={s} canWrite />
            </TabsContent>
          )}
          <TabsContent value="schedule" className="pt-3">
            <ScheduleTab serverId={s.id} isCustom={s.templateId === "custom"} />
          </TabsContent>
          <TabsContent value="backups" className="pt-3">
            <BackupsTab server={s} />
          </TabsContent>
          <TabsContent value="databases" className="pt-3">
            <ServerDatabasesTab server={s} user={user} />
          </TabsContent>
          <TabsContent value="settings" className="pt-3">
            <SettingsTab server={s} admin={user.role !== "user"} />
          </TabsContent>
          {user.role !== "user" && (
            <TabsContent value="users" className="pt-3">
              <ServerUsersTab serverId={s.id} />
            </TabsContent>
          )}
        </Tabs>
      </div>
    </AppShell>
  )
}

/** "Minecraft (Paper) · Minecraft" for template servers, just the category for custom ones. */
function TemplateLabel({ server }: { server: Server }) {
  const { t } = useTranslation()
  const templates = useTemplates()
  const tpl = templates.data?.find((x) => x.id === server.templateId)
  const category = t(`servers.category.${server.category}`)
  if (server.templateId === "custom" || !tpl) return <span>{category}</span>
  return (
    <span>
      {tpl.name} · {category}
    </span>
  )
}

function SettingsTab({ server, admin }: { server: Server; admin: boolean }) {
  const { t } = useTranslation()
  const update = useUpdateServer(server.id)
  if (server.templateId !== "custom") {
    return (
      <div className="flex flex-col gap-6">
        <TemplateSettings server={server} />
        <ServerAlertsCard serverId={server.id} admin={admin} />
        <DangerZone server={server} />
      </div>
    )
  }
  return (
    <div className="flex flex-col gap-6">
      <Card>
        <CardHeader>
          <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("servers.settingsTitle")}</CardTitle>
          <CardDescription>{t("servers.settingsDescription")}</CardDescription>
        </CardHeader>
        <CardContent>
          <ServerForm
            key={server.updatedAt}
            formId={`settings-${server.id}`}
            initial={valuesFromServer(server)}
            error={update.error}
            onSubmit={(input) => update.mutate(input, { onSuccess: () => toast.success(t("servers.saved")) })}
            footer={
              <div className="flex flex-wrap items-center justify-end gap-3">
                {server.status.state !== "stopped" && server.status.state !== "crashed" && (
                  <p className="mr-auto text-xs text-muted-foreground">{t("servers.appliesOnRestart")}</p>
                )}
                <Button type="submit" disabled={update.isPending}>
                  {update.isPending ? t("servers.saving") : t("servers.save")}
                </Button>
              </div>
            }
          />
        </CardContent>
      </Card>
      <ServerAlertsCard serverId={server.id} admin={admin} />
      <DangerZone server={server} />
    </div>
  )
}

function DangerZone({ server }: { server: Server }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const del = useDeleteServer()
  const [open, setOpen] = useState(false)
  const [files, setFiles] = useState(true)
  const stopped = canDo(server.status.state, server.installState).delete

  return (
    <Card className="border-destructive/40">
      <CardHeader>
        <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] text-destructive uppercase">{t("servers.dangerTitle")}</CardTitle>
        <CardDescription>{t("servers.dangerDescription")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-wrap items-center justify-between gap-3">
        {!stopped && <p className="text-sm text-muted-foreground">{t("servers.deleteRequiresStopped")}</p>}
        <Dialog
          open={open}
          onOpenChange={(o) => {
            setOpen(o)
            if (!o) del.reset()
          }}
        >
          <DialogTrigger asChild>
            <Button variant="destructive" disabled={!stopped} className="ml-auto">
              <Trash2Icon />
              {t("power.delete")}
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("servers.deleteTitle")}</DialogTitle>
              <DialogDescription>{t("servers.deleteDescription", { name: server.name })}</DialogDescription>
            </DialogHeader>
            <label className="flex items-center gap-2 text-sm">
              <input type="checkbox" className="size-4 accent-primary" checked={files} onChange={(e) => setFiles(e.target.checked)} />
              <Label asChild>
                <span>{t("servers.deleteFiles")}</span>
              </Label>
            </label>
            {del.error && (
              <Alert variant="destructive">
                <AlertDescription>{describeError(t, del.error)}</AlertDescription>
              </Alert>
            )}
            <DialogFooter>
              <Button variant="ghost" onClick={() => setOpen(false)}>
                {t("power.cancel")}
              </Button>
              <Button
                variant="destructive"
                disabled={del.isPending}
                onClick={() =>
                  del.mutate(
                    { id: server.id, files },
                    {
                      onSuccess: () => {
                        toast.success(t("servers.deleted"))
                        void navigate("/", { replace: true })
                      },
                    },
                  )
                }
              >
                {del.isPending ? t("servers.deleting") : t("power.confirm")}
              </Button>
            </DialogFooter>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  )
}
