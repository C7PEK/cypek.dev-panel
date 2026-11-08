import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"

import { Particles } from "@/components/brand/particles"
import { LanguageSwitcher } from "@/components/layout/language-switcher"
import { Logo } from "@/components/layout/logo"
import { ThemeToggle } from "@/components/layout/theme-toggle"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"

// Centered card over the cypek.dev backdrop (particles, grid, vignette) for login and setup.
export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  const { t } = useTranslation()
  return (
    <div className="relative flex min-h-svh flex-col overflow-hidden bg-background text-foreground">
      <Particles className="pointer-events-none absolute inset-0 z-0 size-full" />
      <div className="brand-grid pointer-events-none absolute -inset-px z-1" />
      <div className="brand-vignette pointer-events-none absolute inset-0 z-2" />

      <header className="relative z-10 flex items-center justify-between px-4 py-3 sm:px-6">
        <Logo />
        <div className="flex items-center gap-1">
          <LanguageSwitcher />
          <ThemeToggle />
        </div>
      </header>

      <main className="relative z-10 flex flex-1 flex-col items-center justify-center gap-6 px-4 pb-12">
        <p className="label-caps flex items-center gap-2 text-[0.72rem] text-muted-foreground">
          {t("app.status")}
          <span className="caret-blink -ml-1 inline-block h-[1em] w-[0.42em] bg-muted-foreground" />
        </p>
        <Card className="w-full max-w-md bg-card/80 backdrop-blur-md">
          <CardHeader>
            <CardTitle className="font-heading text-2xl font-semibold tracking-[0.04em] uppercase">{title}</CardTitle>
            <CardDescription>{description}</CardDescription>
          </CardHeader>
          <CardContent>{children}</CardContent>
        </Card>
      </main>

      <footer className="label-caps relative z-10 px-4 pb-5 text-center text-[0.68rem] text-muted-foreground/70">
        {t("app.company")}
      </footer>
    </div>
  )
}
