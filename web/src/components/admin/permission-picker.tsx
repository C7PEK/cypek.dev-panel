import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import { PERMISSIONS, PRESETS, presetOf } from "@/lib/users"
import type { Permission } from "@/lib/types"
import { cn } from "@/lib/utils"

/** Checkbox grid for per-server permissions with the three presets. */
export function PermissionPicker({
  value,
  onChange,
  disabled,
}: {
  value: Permission[]
  onChange: (next: Permission[]) => void
  disabled?: boolean
}) {
  const { t } = useTranslation()
  const active = presetOf(value)
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap gap-1.5">
        {(Object.keys(PRESETS) as (keyof typeof PRESETS)[]).map((p) => (
          <Button key={p} type="button" size="xs" variant={active === p ? "default" : "outline"} disabled={disabled} onClick={() => onChange([...PRESETS[p]])}>
            {t(`permissions.presets.${p}`)}
          </Button>
        ))}
      </div>
      <div className="grid gap-1.5 sm:grid-cols-3">
        {PERMISSIONS.map((p) => (
          <label key={p} className={cn("flex items-start gap-2 rounded-md border px-2 py-1.5 text-sm", value.includes(p) && "border-primary/40 bg-primary/5")}>
            <input
              type="checkbox"
              className="mt-0.5 size-4 accent-primary"
              checked={value.includes(p)}
              disabled={disabled}
              onChange={(e) => onChange(e.target.checked ? [...value, p] : value.filter((x) => x !== p))}
            />
            <span>
              <span className="block">{t(`permissions.labels.${p}`)}</span>
              <span className="block font-mono text-[0.65rem] text-muted-foreground">{p}</span>
            </span>
          </label>
        ))}
      </div>
    </div>
  )
}
