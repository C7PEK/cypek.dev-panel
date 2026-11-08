import { useTranslation } from "react-i18next"
import { useSearchParams } from "react-router"

import { ApiKeysCard } from "@/components/account/api-keys-card"
import { PasswordCard } from "@/components/account/password-card"
import { TwoFactorCard } from "@/components/account/two-factor-card"
import { AppShell } from "@/components/layout/app-shell"
import { Badge } from "@/components/ui/badge"
import type { User } from "@/lib/types"

export function ProfilePage({ user }: { user: User }) {
  const { t } = useTranslation()
  const [params] = useSearchParams()
  const enroll = params.get("enroll") === "1"
  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <div>
          <h1 className="font-heading text-3xl font-semibold tracking-[0.04em] uppercase">{t("profile.title")}</h1>
          <p className="mt-1 flex items-center gap-2 text-sm text-muted-foreground">
            {user.username} <Badge variant="outline">{t(`roles.${user.role}`)}</Badge>
          </p>
        </div>
        <TwoFactorCard enrollHint={enroll} />
        <PasswordCard />
        <ApiKeysCard />
      </div>
    </AppShell>
  )
}
