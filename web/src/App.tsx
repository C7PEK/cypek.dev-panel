import { Navigate, Route, Routes, useLocation } from "react-router"

import { FullPageError, FullPageLoading } from "@/components/layout/full-page-state"
import { useMe, useSetupStatus } from "@/lib/auth"
import type { User } from "@/lib/types"
import { DashboardPage } from "@/pages/dashboard-page"
import { LoginPage } from "@/pages/login-page"
import { SetupPage } from "@/pages/setup-page"

// Gate decides between first-run setup, login and the app based on server state.
function Gate() {
  const setup = useSetupStatus()
  const me = useMe()
  const location = useLocation()

  if (setup.isPending || me.isPending) return <FullPageLoading />
  if (setup.error) return <FullPageError error={setup.error} onRetry={() => void setup.refetch()} />
  if (me.error) return <FullPageError error={me.error} onRetry={() => void me.refetch()} />

  if (setup.data?.setupRequired) {
    return location.pathname === "/setup" ? <SetupPage /> : <Navigate to="/setup" replace />
  }

  const user = me.data ?? null
  if (!user) {
    return location.pathname === "/login" ? (
      <LoginPage />
    ) : (
      <Navigate to="/login" replace state={{ from: location.pathname }} />
    )
  }
  return <AuthedRoutes user={user} />
}

function AuthedRoutes({ user }: { user: User }) {
  return (
    <Routes>
      <Route path="/" element={<DashboardPage user={user} />} />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

export default function App() {
  return (
    <Routes>
      <Route path="*" element={<Gate />} />
    </Routes>
  )
}
