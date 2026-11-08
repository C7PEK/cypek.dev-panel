import { useState } from "react"
import { useTranslation } from "react-i18next"

import { NativeSelect } from "@/components/mods/platform-card"
import { Input } from "@/components/ui/input"
import { buildMention, type MentionMode, parseMention, validMention } from "@/lib/alerts"

// Builds a Discord mention string ("<@&id>", "@here", ...) from a simple
// picker. Remount it (key) to reset it from a new value.
export function MentionBuilder({
  id,
  value,
  onChange,
  emptyLabel,
}: {
  id: string
  value: string
  onChange: (v: string) => void
  /** Label for the "" option (e.g. "Nikogo" or "Jak w ustawieniach panelu"). */
  emptyLabel?: string
}) {
  const { t } = useTranslation()
  const initial = parseMention(value)
  const [mode, setMode] = useState<MentionMode>(initial.mode)
  const [targetId, setTargetId] = useState(initial.id)
  const [raw, setRaw] = useState(initial.mode === "raw" ? value : "")

  function update(next: { mode?: MentionMode; id?: string; raw?: string }) {
    const m = next.mode ?? mode
    const i = next.id ?? targetId
    const r = next.raw ?? raw
    if (next.mode !== undefined) setMode(m)
    if (next.id !== undefined) setTargetId(i)
    if (next.raw !== undefined) setRaw(r)
    onChange(buildMention(m, i, r))
  }

  const rawInvalid = mode === "raw" && !validMention(raw)

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-col gap-2 sm:flex-row">
        <NativeSelect id={id} value={mode} onChange={(v) => update({ mode: v as MentionMode })} className="sm:w-56">
          <option value="none">{emptyLabel ?? t("alerts.mention.none")}</option>
          <option value="everyone">@everyone</option>
          <option value="here">@here</option>
          <option value="role">{t("alerts.mention.role")}</option>
          <option value="user">{t("alerts.mention.user")}</option>
          <option value="raw">{t("alerts.mention.raw")}</option>
        </NativeSelect>
        {(mode === "role" || mode === "user") && (
          <Input
            inputMode="numeric"
            value={targetId}
            onChange={(e) => update({ id: e.target.value.replace(/\D/g, "").slice(0, 24) })}
            placeholder={mode === "role" ? t("alerts.mention.roleIdPlaceholder") : t("alerts.mention.userIdPlaceholder")}
            aria-label={mode === "role" ? t("alerts.mention.roleId") : t("alerts.mention.userId")}
            className="font-mono sm:flex-1"
          />
        )}
        {mode === "raw" && (
          <Input
            value={raw}
            onChange={(e) => update({ raw: e.target.value })}
            placeholder="<@&123456789> <@987654321>"
            aria-invalid={rawInvalid}
            aria-label={t("alerts.mention.raw")}
            className="font-mono sm:flex-1"
          />
        )}
      </div>
      {(mode === "role" || mode === "user") && <p className="text-xs text-muted-foreground">{t("alerts.mention.idHint")}</p>}
      {mode === "raw" && <p className={rawInvalid ? "text-xs text-destructive" : "text-xs text-muted-foreground"}>{t("alerts.mention.rawHint")}</p>}
    </div>
  )
}
