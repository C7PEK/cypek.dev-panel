import { type FormEvent, type ReactNode, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"

import { Alert, AlertDescription } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { describeError } from "@/lib/errors"

interface NameDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  description?: string
  label: string
  initial?: string
  submitLabel: string
  pending: boolean
  error: unknown
  onSubmit: (name: string) => void
}

export function NameDialog({ open, onOpenChange, title, description, label, initial = "", submitLabel, pending, error, onSubmit }: NameDialogProps) {
  const [name, setName] = useState(initial)
  useEffect(() => {
    if (open) setName(initial)
  }, [open, initial])
  const { t } = useTranslation()
  const invalid = name.trim() === "" || name.includes("/") || name.includes("\\") || name === "." || name === ".."

  function submit(e: FormEvent) {
    e.preventDefault()
    if (invalid) return
    onSubmit(name.trim())
  }

  return (
    <Dialog open={open} onOpenChange={(o) => !pending && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{title}</DialogTitle>
          {description && <DialogDescription>{description}</DialogDescription>}
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="file-name">{label}</Label>
            <Input id="file-name" value={name} onChange={(e) => setName(e.target.value)} autoFocus className="font-mono" spellCheck={false} />
            {name && invalid && <p className="text-xs text-destructive">{t("files.nameInvalid")}</p>}
          </div>
          {error ? (
            <Alert variant="destructive">
              <AlertDescription>{describeError(t, error)}</AlertDescription>
            </Alert>
          ) : null}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
              {t("files.cancel")}
            </Button>
            <Button type="submit" disabled={invalid || pending}>
              {submitLabel}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

interface ConfirmDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  title: string
  body: ReactNode
  confirmLabel: string
  pending: boolean
  error: unknown
  onConfirm: () => void
}

export function ConfirmDialog({ open, onOpenChange, title, body, confirmLabel, pending, error, onConfirm }: ConfirmDialogProps) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !pending && onOpenChange(o)}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="font-heading text-xl font-semibold tracking-[0.04em] uppercase">{title}</DialogTitle>
          <DialogDescription asChild>
            <div>{body}</div>
          </DialogDescription>
        </DialogHeader>
        {error ? (
          <Alert variant="destructive">
            <AlertDescription>{describeError(t, error)}</AlertDescription>
          </Alert>
        ) : null}
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)} disabled={pending}>
            {t("files.cancel")}
          </Button>
          <Button variant="destructive" onClick={onConfirm} disabled={pending}>
            {confirmLabel}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
