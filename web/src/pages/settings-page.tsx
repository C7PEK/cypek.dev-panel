import { SettingsIcon } from "lucide-react"
import { useTranslation } from "react-i18next"
import { useNavigate, useParams } from "react-router"

import { AppearanceCard } from "@/components/admin/appearance-card"
import { MachineInfoCard } from "@/components/admin/machine-info-card"
import { PanelUpdateCard } from "@/components/admin/panel-update-card"
import { AppShell } from "@/components/layout/app-shell"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { AlertsSettings } from "@/pages/admin-alerts-page"
import { SecurityCard } from "@/pages/admin-users-page"
import { usePanelUpdate } from "@/lib/system"
import type { User } from "@/lib/types"

const TABS = ["general", "appearance", "security", "alerts"] as const
type Tab = (typeof TABS)[number]

// Panel-wide settings (admin and owner): machine info and updates, login
// security, Discord alerts. The dashboard stays servers-only.
export function SettingsPage({ user }: { user: User }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const params = useParams()
  const owner = user.role === "owner"
  const tabs = TABS.filter((tab) => tab !== "security" || owner)
  const tab: Tab = tabs.includes(params.tab as Tab) ? (params.tab as Tab) : "general"
  const update = usePanelUpdate(owner)

  return (
    <AppShell user={user}>
      <div className="flex flex-col gap-6">
        <h1 className="flex items-center gap-2 font-heading text-3xl font-semibold tracking-[0.04em] uppercase">
          <SettingsIcon className="size-6 text-primary" />
          {t("settings.title")}
        </h1>
        <Tabs value={tab} onValueChange={(v) => navigate(v === "general" ? "/settings" : `/settings/${v}`, { replace: true })}>
          <TabsList className="w-full justify-start overflow-x-auto">
            {tabs.map((id) => (
              <TabsTrigger key={id} value={id}>
                {t(`settings.tabs.${id}`)}
                {id === "general" && update.data?.available && (
                  <span className="ml-1.5 inline-block size-1.5 rounded-full bg-brand-cyan shadow-[0_0_8px_var(--brand-cyan)]" />
                )}
              </TabsTrigger>
            ))}
          </TabsList>
          <TabsContent value="general" className="flex flex-col gap-6 pt-3">
            {owner && <PanelUpdateCard />}
            <MachineInfoCard />
          </TabsContent>
          <TabsContent value="appearance" className="pt-3">
            <AppearanceCard />
          </TabsContent>
          {owner && (
            <TabsContent value="security" className="pt-3">
              <SecurityCard owner={user} />
            </TabsContent>
          )}
          <TabsContent value="alerts" className="pt-3">
            <AlertsSettings />
          </TabsContent>
        </Tabs>
      </div>
    </AppShell>
  )
}
