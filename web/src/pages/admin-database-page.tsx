import { CheckCircle2Icon, DatabaseIcon, ExternalLinkIcon, Loader2Icon, RotateCcwIcon, WrenchIcon } from "lucide-react"
import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { CopyField } from "@/components/databases/copy-field"
import { RootLoginCard } from "@/components/databases/root-login-card"
import { AppShell } from "@/components/layout/app-shell"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { isApiError } from "@/lib/api"
import { phpMyAdminUrl, useDatabaseSystem, useInstallDatabase, useRestartPhpMyAdmin } from "@/lib/databases"
import { describeError } from "@/lib/errors"
import type { User } from "@/lib/types"
import { cn } from "@/lib/utils"

const INSTALL_COMMAND = "sudo homepanel install-database"

export function AdminDatabasePage({ user }: { user: User }) {
  const { t } = useTranslation()
  const sys = useDatabaseSystem()
  const install = useInstallDatabase()
  const restart = useRestartPhpMyAdmin()
  const logRef = useRef<HTMLPreElement>(null)
  const owner = user.role === "owner"
  const data = sys.data
  const st = data?.status
  const job = data?.install
  const needsTerminal = isApiError(install.error) && install.error.code === "addon_needs_systemd"
  const lines = job?.lines ?? []
  const lastLines = lines.join("\n")

  useEffect(() => {
    const el = logRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [lastLines])

  const step = [...lines].reverse().map((l) => l.match(/Step (\d)\/(\d)/)).find(Boolean)
  const pma = phpMyAdminUrl(st?.phpMyAdminPort)

  function runInstall() {
    install.mutate(undefined, {
      onSuccess: () => toast.success(t("database.installStarted")),
      onError: (e) => {
        if (!(isApiError(e) && e.code === "addon_needs_systemd")) toast.error(describeError(t, e))
      },
    })
  }

  const installButton = (repair: boolean) =>
    owner ? (
      <Button onClick={runInstall} disabled={install.isPending || job?.running} variant={repair ? "outline" : "default"}>
        {install.isPending || job?.running ? <Loader2Icon className="animate-spin" /> : repair ? <WrenchIcon /> : <DatabaseIcon />}
        {job?.running ? t("database.installing") : repair ? t("database.repair") : t("database.install")}
      </Button>
    ) : (
      <p className="text-sm text-muted-foreground">{t("database.askOwner")}</p>
    )

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div>
          <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("database.adminTitle")}</h1>
          <p className="text-sm text-muted-foreground">{t("database.adminDescription")}</p>
        </div>

        {sys.error && (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, sys.error)}</AlertDescription>
          </Alert>
        )}
        {sys.isPending && <p className="text-sm text-muted-foreground">{t("app.loading")}</p>}

        {st && st.installed && (
          <Card>
            <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-3">
              <div>
                <CardTitle className="flex items-center gap-2 font-heading text-lg font-medium tracking-[0.04em] uppercase">
                  <DatabaseIcon className="size-5" />
                  MariaDB
                  {st.reachable ? (
                    <Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/15 text-emerald-300">
                      {t("database.statusRunning")}
                    </Badge>
                  ) : (
                    <Badge variant="outline" className="border-red-500/40 bg-red-500/15 text-red-300">
                      {t("database.statusDown")}
                    </Badge>
                  )}
                </CardTitle>
                <CardDescription className="font-mono">
                  {st.version ? `${st.version} · ` : ""}
                  {st.host ?? "127.0.0.1"}:{st.port ?? 3306}
                </CardDescription>
              </div>
              {installButton(true)}
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              {!st.reachable && st.error && (
                <Alert variant="destructive">
                  <AlertDescription className="font-mono text-xs">{st.error}</AlertDescription>
                </Alert>
              )}
              <div className="flex flex-wrap items-center justify-between gap-3 rounded-lg border p-3">
                <div>
                  <div className="flex items-center gap-2 text-sm font-medium">
                    phpMyAdmin
                    {data?.phpMyAdminRunning === true && (
                      <Badge variant="outline" className="border-emerald-500/40 bg-emerald-500/15 text-emerald-300">
                        {t("database.statusRunning")}
                      </Badge>
                    )}
                    {data?.phpMyAdminRunning === false && (
                      <Badge variant="outline" className="border-red-500/40 bg-red-500/15 text-red-300">
                        {t("database.statusDown")}
                      </Badge>
                    )}
                  </div>
                  <p className="text-xs text-muted-foreground">{t("database.phpMyAdminInfo", { port: st.phpMyAdminPort ?? 8081 })}</p>
                </div>
                <div className="flex flex-wrap gap-2">
                  {pma && (
                    <Button asChild variant="outline" size="sm">
                      <a href={pma} target="_blank" rel="noreferrer">
                        <ExternalLinkIcon />
                        {t("database.openPhpMyAdmin")}
                      </a>
                    </Button>
                  )}
                  <Button
                    size="sm"
                    variant="ghost"
                    disabled={restart.isPending}
                    onClick={() => restart.mutate(undefined, { onSuccess: () => toast.success(t("database.phpMyAdminRestarted")), onError: (e) => toast.error(describeError(t, e)) })}
                  >
                    <RotateCcwIcon />
                    {t("database.restartPhpMyAdmin")}
                  </Button>
                </div>
              </div>
              <TuningNote />
            </CardContent>
          </Card>
        )}

        {st?.installed && st.reachable && owner && <RootLoginCard phpMyAdminUrl={pma ?? undefined} />}

        {st && !st.installed && (
          <Card>
            <CardHeader>
              <CardTitle className="font-heading text-lg font-medium tracking-[0.04em] uppercase">{t("database.notInstalledTitle")}</CardTitle>
              <CardDescription>{t("database.explainIntro")}</CardDescription>
            </CardHeader>
            <CardContent className="flex flex-col gap-4">
              <ul className="ml-5 list-disc space-y-1 text-sm">
                <li>{t("database.explainMariadb")}</li>
                <li>{t("database.explainPhp")}</li>
                <li>{t("database.explainPma")}</li>
                <li>{t("database.explainRam")}</li>
                <li>{t("database.explainTime")}</li>
              </ul>
              <TuningNote />
              <div>{installButton(false)}</div>
            </CardContent>
          </Card>
        )}

        {needsTerminal && (
          <Alert>
            <AlertTitle>{t("database.terminalTitle")}</AlertTitle>
            <AlertDescription className="flex flex-col gap-2">
              <span>{t("database.terminalBody")}</span>
              <CopyField label={t("database.command")} value={INSTALL_COMMAND} />
            </AlertDescription>
          </Alert>
        )}

        {job && (job.running || lines.length > 0) && (
          <Card>
            <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-2">
              <CardTitle className="flex items-center gap-2 text-base font-medium">
                {job.running && <Loader2Icon className="size-4 animate-spin" />}
                {t("database.installLog")}
              </CardTitle>
              {step && job.running && (
                <span className="font-mono text-xs text-muted-foreground">{t("database.step", { n: step[1], total: step[2] })}</span>
              )}
            </CardHeader>
            <CardContent className="flex flex-col gap-3">
              {!job.running && job.done && (
                <Alert>
                  <CheckCircle2Icon />
                  <AlertTitle>{t("database.installDone")}</AlertTitle>
                  <AlertDescription>{t("database.installDoneBody")}</AlertDescription>
                </Alert>
              )}
              {!job.running && job.failed && (
                <Alert variant="destructive">
                  <AlertTitle>{t("database.installFailed")}</AlertTitle>
                  <AlertDescription>{t("database.installFailedBody")}</AlertDescription>
                </Alert>
              )}
              <pre
                ref={logRef}
                className={cn("max-h-80 overflow-auto rounded-md border bg-[#050506] p-3 font-mono text-[11px] leading-relaxed whitespace-pre-wrap text-zinc-300")}
              >
                {lastLines || "…"}
              </pre>
            </CardContent>
          </Card>
        )}
      </div>
    </AppShell>
  )
}

function TuningNote() {
  const { t } = useTranslation()
  return <p className="rounded-md border border-dashed p-3 text-xs text-muted-foreground">{t("database.tuningNote")}</p>
}
