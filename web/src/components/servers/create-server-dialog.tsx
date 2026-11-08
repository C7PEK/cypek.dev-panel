import { useState } from "react"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router"
import { toast } from "sonner"

import { ServerForm, defaultValues } from "@/components/servers/server-form"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { useCreateServer } from "@/lib/servers"

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
}

// Temporary "custom command" creator until the template wizard (M2) lands.
export function CreateServerDialog({ open, onOpenChange }: Props) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const create = useCreateServer()
  // Remounting the form on each open resets its local state.
  const [formKey, setFormKey] = useState(0)

  function handleOpenChange(next: boolean) {
    if (!next && create.isPending) return
    if (!next) {
      create.reset()
      setFormKey((k) => k + 1)
    }
    onOpenChange(next)
  }

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-h-[calc(100svh-2rem)] overflow-y-auto sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">
            {t("servers.createTitle")}
          </DialogTitle>
          <DialogDescription>{t("servers.createDescription")}</DialogDescription>
        </DialogHeader>
        <ServerForm
          key={formKey}
          formId="create-server-form"
          initial={defaultValues}
          error={create.error}
          autoFocusName
          onSubmit={(input) =>
            create.mutate(input, {
              onSuccess: (server) => {
                toast.success(t("servers.created"))
                handleOpenChange(false)
                void navigate(`/servers/${encodeURIComponent(server.id)}`)
              },
            })
          }
        />
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => handleOpenChange(false)} disabled={create.isPending}>
            {t("servers.cancel")}
          </Button>
          <Button type="submit" form="create-server-form" disabled={create.isPending}>
            {create.isPending ? t("servers.submitting") : t("servers.submit")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
