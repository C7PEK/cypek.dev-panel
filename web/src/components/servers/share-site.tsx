import { CloudIcon, Loader2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import { CopyField } from "@/components/databases/copy-field"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldError, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import { describeError } from "@/lib/errors"
import { useCreateServer } from "@/lib/servers"
import type { Server } from "@/lib/types"

const tokenRe = /eyJ[A-Za-z0-9+/=_-]{20,}/

export function sitePort(server: Server) {
  return server.ports.find((p) => p.protocol === "tcp")?.port
}

export function ShareSiteButton({ server, size, className }: { server: Server; size?: "sm" | "default"; className?: string }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const create = useCreateServer()
  const [open, setOpen] = useState(false)
  const [command, setCommand] = useState("")
  const [quick, setQuick] = useState(false)
  const [name, setName] = useState(`${server.name} tunnel`)
  const port = sitePort(server)
  const local = port ? `http://localhost:${port}` : ""
  const hasToken = tokenRe.test(command)
  const invalid = !quick && !hasToken

  function submit() {
    create.mutate(
      {
        name: name.trim() || `${server.name} tunnel`,
        templateId: "cloudflare-tunnel",
        variables: quick ? { TUNNEL_TOKEN: "", TARGET_URL: local || "http://localhost:8080" } : { TUNNEL_TOKEN: command.trim(), TARGET_URL: local || "http://localhost:8080" },
        autostart: true,
        startAfterInstall: true,
      },
      {
        onSuccess: (created) => {
          toast.success(t("share.created"))
          setOpen(false)
          void navigate(`/servers/${created.id}`)
        },
      },
    )
  }

  return (
    <>
      <Button type="button" variant="outline" size={size} className={className} onClick={() => setOpen(true)}>
        <CloudIcon className="text-orange-500" />
        {t("share.button")}
      </Button>
      <Dialog open={open} onOpenChange={(o) => !create.isPending && setOpen(o)}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("share.title")}</DialogTitle>
            <DialogDescription>{t("share.description")}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-4">
            <Field orientation="horizontal">
              <FieldContent>
                <FieldLabel htmlFor={`share-quick-${server.id}`}>{t("share.quick")}</FieldLabel>
                <FieldDescription>{t("share.quickHelp")}</FieldDescription>
              </FieldContent>
              <Switch id={`share-quick-${server.id}`} checked={quick} onCheckedChange={setQuick} />
            </Field>
            {!quick && (
              <>
                <ol className="list-decimal space-y-1 pl-5 text-sm text-muted-foreground">
                  <li>{t("share.step1")}</li>
                  <li>{t("share.step2")}</li>
                </ol>
                <Field data-invalid={command !== "" && !hasToken}>
                  <FieldLabel htmlFor={`share-cmd-${server.id}`}>{t("share.commandLabel")}</FieldLabel>
                  <Textarea
                    id={`share-cmd-${server.id}`}
                    value={command}
                    onChange={(e) => setCommand(e.target.value)}
                    placeholder="cloudflared service install eyJhIjoi..."
                    className="min-h-20 font-mono text-xs"
                    autoComplete="off"
                    spellCheck={false}
                  />
                  {command !== "" && !hasToken ? <FieldError>{t("share.noToken")}</FieldError> : <FieldDescription>{t("share.commandHelp")}</FieldDescription>}
                </Field>
                {local && (
                  <div className="flex flex-col gap-1.5">
                    <p className="text-sm text-muted-foreground">{t("share.step3")}</p>
                    <CopyField label={t("share.serviceLabel")} value={local} />
                  </div>
                )}
              </>
            )}
            <Field>
              <FieldLabel htmlFor={`share-name-${server.id}`}>{t("share.nameLabel")}</FieldLabel>
              <Input id={`share-name-${server.id}`} value={name} onChange={(e) => setName(e.target.value)} maxLength={64} />
            </Field>
            {create.error && <p className="text-sm text-destructive">{describeError(t, create.error)}</p>}
          </div>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={create.isPending}>
              {t("servers.cancel")}
            </Button>
            <Button type="button" onClick={submit} disabled={invalid || create.isPending}>
              {create.isPending ? <Loader2Icon className="animate-spin" /> : <CloudIcon />}
              {t("share.create")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
