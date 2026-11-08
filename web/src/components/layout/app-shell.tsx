import { LogOutIcon, UserIcon } from "lucide-react"
import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "react-router"

import { LanguageSwitcher } from "@/components/layout/language-switcher"
import { Logo } from "@/components/layout/logo"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { useLogout } from "@/lib/auth"
import type { User } from "@/lib/types"

// Main application frame: top bar with navigation and the user menu.
export function AppShell({ user, children }: { user: User; children: ReactNode }) {
  const { t } = useTranslation()
  const logout = useLogout()
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="sticky top-0 z-10 border-b bg-background/80 backdrop-blur-md">
        <div className="mx-auto flex h-14 w-full max-w-6xl items-center justify-between gap-4 px-4 sm:px-6">
          <nav className="flex items-center gap-6">
            <Link to="/" className="shrink-0">
              <Logo />
            </Link>
            <Link
              to="/"
              className="label-caps hidden text-[0.72rem] text-muted-foreground transition-colors hover:text-foreground sm:block"
            >
              {t("nav.dashboard")}
            </Link>
          </nav>
          <div className="flex items-center gap-1">
            <LanguageSwitcher />
            <ThemeToggle />
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="outline" size="sm" className="ml-1 gap-2">
                  <UserIcon />
                  <span className="max-w-32 truncate">{user.username}</span>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="min-w-48">
                <DropdownMenuLabel className="font-normal">
                  <div className="text-sm font-medium">{user.username}</div>
                  <div className="text-xs text-muted-foreground">{t(`roles.${user.role}`)}</div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />
                <DropdownMenuItem onSelect={() => logout.mutate()} disabled={logout.isPending}>
                  <LogOutIcon />
                  {t("nav.logout")}
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </div>
      </header>
      <main className="mx-auto w-full max-w-6xl flex-1 px-4 py-6 sm:px-6">{children}</main>
      <footer className="label-caps px-4 pb-5 text-center text-[0.66rem] text-muted-foreground/60">{t("app.company")}</footer>
    </div>
  )
}
