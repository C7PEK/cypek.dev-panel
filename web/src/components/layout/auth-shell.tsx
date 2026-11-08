import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { LanguageSwitcher } from "@/components/layout/language-switcher"
import { Logo } from "@/components/layout/logo"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

// Centered card layout for login and first-run setup.
export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="flex min-h-svh flex-col bg-background text-foreground">
      <header className="flex items-center justify-between px-4 py-3 sm:px-6">
        <Logo />
        <div className="flex items-center gap-1">
          <LanguageSwitcher />
          <ThemeToggle />
        </div>
      </header>
      <main className="flex flex-1 items-center justify-center px-4 pb-12">
        <Card className="w-full max-w-md">
          <CardHeader>
            <CardTitle className="text-xl">{title}</CardTitle>
            <CardDescription>{description}</CardDescription>
          </CardHeader>
          <CardContent>{children}</CardContent>
        </Card>
      </main>
      <footer className="px-4 pb-4 text-center text-xs text-muted-foreground">{t("app.tagline")}</footer>
    </div>
  )
}
