import { CheckIcon, RotateCcwIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"

import { Logo } from "@/components/layout/logo"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { ACCENT_PRESETS, applyAccent, DEFAULT_ACCENT, DEFAULT_NAME, useBranding, useSaveBranding } from "@/lib/branding"
import { describeError } from "@/lib/errors"
import { cn } from "@/lib/utils"

const titleClass = "font-heading text-lg font-medium tracking-[0.04em] uppercase"

// Panel name and accent colour; the colour previews live on the whole panel
// and is reverted when leaving without saving.
export function AppearanceCard() {
  const saved = useBranding()
  if (!saved.data) return null
  return <AppearanceForm key={`${saved.data.name}|${saved.data.accent}`} initialName={saved.data.name} initialAccent={saved.data.accent} />
}

function AppearanceForm({ initialName, initialAccent }: { initialName: string; initialAccent: string }) {
  const { t } = useTranslation()
  const save = useSaveBranding()
  const [name, setName] = useState(initialName)
  const [accent, setAccent] = useState(initialAccent || DEFAULT_ACCENT)
  const [hexText, setHexText] = useState(initialAccent || DEFAULT_ACCENT)
  const dirty = name.trim() !== initialName || accent !== (initialAccent || DEFAULT_ACCENT)

  // Live preview; restore the saved colour if the page is left unsaved.
  useEffect(() => {
    applyAccent(accent)
  }, [accent])
  useEffect(() => () => applyAccent(initialAccent), [initialAccent])

  function pick(hex: string) {
    setAccent(hex.toLowerCase())
    setHexText(hex.toLowerCase())
  }

  function submit() {
    save.mutate(
      { name: name.trim(), accent: accent === DEFAULT_ACCENT ? "" : accent },
      { onSuccess: () => toast.success(t("appearance.saved")), onError: (e) => toast.error(describeError(t, e)) },
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className={titleClass}>{t("appearance.title")}</CardTitle>
        <CardDescription>{t("appearance.description")}</CardDescription>
      </CardHeader>
      <CardContent className="flex flex-col gap-6">
        <div className="flex flex-col gap-2">
          <Label htmlFor="brand-name">{t("appearance.name")}</Label>
          <Input id="brand-name" value={name} maxLength={40} placeholder={DEFAULT_NAME} onChange={(e) => setName(e.target.value)} className="max-w-sm" />
          <p className="text-xs text-muted-foreground">{t("appearance.nameHint")}</p>
        </div>

        <div className="flex flex-col gap-3">
          <Label>{t("appearance.accent")}</Label>
          <div className="flex flex-wrap items-center gap-2">
            {ACCENT_PRESETS.map((hex) => (
              <button
                key={hex}
                type="button"
                title={hex}
                aria-label={hex}
                onClick={() => pick(hex)}
                className={cn(
                  "flex size-8 items-center justify-center rounded-full ring-offset-2 ring-offset-background transition-transform hover:scale-110",
                  accent === hex && "ring-2 ring-foreground",
                )}
                style={{ backgroundColor: hex }}
              >
                {accent === hex && <CheckIcon className="size-4" style={{ color: hex === "#eab308" ? "#0a0a0b" : "#fff" }} />}
              </button>
            ))}
            <label className="relative flex size-8 cursor-pointer items-center justify-center overflow-hidden rounded-full border border-dashed" title={t("appearance.custom")}>
              <input type="color" value={accent} onChange={(e) => pick(e.target.value)} className="absolute inset-0 size-full cursor-pointer opacity-0" />
              <span className="text-xs text-muted-foreground">+</span>
            </label>
            <Input
              value={hexText}
              onChange={(e) => {
                setHexText(e.target.value)
                if (/^#[0-9a-f]{6}$/i.test(e.target.value)) setAccent(e.target.value.toLowerCase())
              }}
              className="w-28 font-mono text-xs"
              aria-label={t("appearance.hex")}
            />
          </div>
        </div>

        <div className="flex flex-col gap-3 rounded-lg border bg-muted/30 p-4">
          <span className="label-caps text-[0.65rem] text-muted-foreground">{t("appearance.preview")}</span>
          <Logo name={name} />
          <div className="flex flex-wrap items-center gap-2">
            <Button size="sm">{t("appearance.sampleButton")}</Button>
            <Button size="sm" variant="outline">
              {t("appearance.sampleOutline")}
            </Button>
            <Badge>{t("appearance.sampleBadge")}</Badge>
            <Switch checked aria-label="preview" />
          </div>
        </div>

        <div className="flex flex-wrap gap-2">
          <Button onClick={submit} disabled={!dirty || save.isPending}>
            {t("appearance.save")}
          </Button>
          <Button
            variant="outline"
            onClick={() => {
              setName("")
              pick(DEFAULT_ACCENT)
            }}
            disabled={name === "" && accent === DEFAULT_ACCENT}
          >
            <RotateCcwIcon />
            {t("appearance.reset")}
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
