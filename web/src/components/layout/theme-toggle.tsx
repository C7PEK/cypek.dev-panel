import { MoonIcon, SunIcon } from "lucide-react"
import { useTheme } from "next-themes"
import { useTranslation } from "react-i18next"

import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"

export function ThemeToggle() {
  const { theme, setTheme } = useTheme()
  const { t } = useTranslation()
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" size="icon" aria-label={t("nav.theme")}>
          <SunIcon className="dark:hidden" />
          <MoonIcon className="hidden dark:block" />
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end">
        <DropdownMenuRadioGroup value={theme ?? "dark"} onValueChange={setTheme}>
          <DropdownMenuRadioItem value="dark">{t("nav.themeDark")}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="light">{t("nav.themeLight")}</DropdownMenuRadioItem>
          <DropdownMenuRadioItem value="system">{t("nav.themeSystem")}</DropdownMenuRadioItem>
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
