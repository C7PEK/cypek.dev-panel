import { GitPullRequestArrowIcon, Loader2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Field, FieldContent, FieldDescription, FieldLabel } from "@/components/ui/field"
import { Switch } from "@/components/ui/switch"
import { describeError } from "@/lib/errors"
import { isInstalling, useGitUpdate } from "@/lib/servers"
import type { Server } from "@/lib/types"

export function GitUpdateButton({ server, size, className }: { server: Server; size?: "sm" | "default"; className?: string }) {
  const { t } = useTranslation()
  const gitUpdate = useGitUpdate(server.id)
  const [open, setOpen] = useState(false)
  const [restart, setRestart] = useState(true)
  const busy = isInstalling(server) || gitUpdate.isPending

  function run() {
    gitUpdate.mutate(restart, {
      onSuccess: () => {
        toast.success(t("gitUpdate.started"))
        setOpen(false)
      },
      onError: (e) => toast.error(describeError(t, e)),
    })
  }

  return (
    <>
      <Button type="button" variant="outline" size={size} className={className} disabled={busy} onClick={() => setOpen(true)}>
        {busy ? <Loader2Icon className="animate-spin" /> : <GitPullRequestArrowIcon />}
        {t("gitUpdate.button")}
      </Button>
      <Dialog open={open} onOpenChange={(o) => !gitUpdate.isPending && setOpen(o)}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{t("gitUpdate.confirmTitle")}</DialogTitle>
            <DialogDescription>{t("gitUpdate.confirm")}</DialogDescription>
          </DialogHeader>
          <Field orientation="horizontal">
            <FieldContent>
              <FieldLabel htmlFor={`git-restart-${server.id}`}>{t("gitUpdate.restart")}</FieldLabel>
              <FieldDescription>{t("gitUpdate.restartHelp")}</FieldDescription>
            </FieldContent>
            <Switch id={`git-restart-${server.id}`} checked={restart} onCheckedChange={setRestart} />
          </Field>
          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => setOpen(false)} disabled={gitUpdate.isPending}>
              {t("servers.cancel")}
            </Button>
            <Button type="button" onClick={run} disabled={gitUpdate.isPending}>
              {gitUpdate.isPending ? <Loader2Icon className="animate-spin" /> : <GitPullRequestArrowIcon />}
              {t("gitUpdate.run")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
