import { Trash2Icon } from "lucide-react"
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Label } from "@/components/ui/label"
import { describeError } from "@/lib/errors"
import { canDo, useDeleteServer } from "@/lib/servers"
import type { Server } from "@/lib/types"

export function DeleteServerDialog({ server, className, size }: { server: Server; className?: string; size?: "sm" | "default" }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const del = useDeleteServer()
  const [open, setOpen] = useState(false)
  const [files, setFiles] = useState(true)
  const stopped = canDo(server.status.state, server.installState).delete

  return (
    <Dialog
      open={open}
      onOpenChange={(o) => {
        setOpen(o)
        if (!o) del.reset()
      }}
    >
      <DialogTrigger asChild>
        <Button variant="destructive" size={size} disabled={!stopped} className={className}>
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
  )
}
