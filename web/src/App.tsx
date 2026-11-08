import { useEffect } from "react"
import { Navigate, Route, Routes, useLocation, useNavigate } from "react-router"

import { FullPageError, FullPageLoading } from "@/components/layout/full-page-state"
import { TOTP_REQUIRED_EVENT } from "@/lib/api"
import { useMe, useSetupStatus } from "@/lib/auth"
import type { User } from "@/lib/types"
import { AdminAuditPage } from "@/pages/admin-audit-page"
import { AdminDatabasePage } from "@/pages/admin-database-page"
import { AdminUsersPage } from "@/pages/admin-users-page"
import { DashboardPage } from "@/pages/dashboard-page"
import { LoginPage } from "@/pages/login-page"
import { NewServerPage } from "@/pages/new-server-page"
import { ProfilePage } from "@/pages/profile-page"
import { ServerPage } from "@/pages/server-page"
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
  const navigate = useNavigate()
  const location = useLocation()
  // The backend answers totp_required until the user enrols: send them to the 2FA card.
  useEffect(() => {
    const onRequired = () => {
      if (!location.pathname.startsWith("/profile")) void navigate("/profile?enroll=1", { replace: true })
    }
    window.addEventListener(TOTP_REQUIRED_EVENT, onRequired)
    return () => window.removeEventListener(TOTP_REQUIRED_EVENT, onRequired)
  }, [navigate, location.pathname])

  const admin = user.role !== "user"
  return (
    <Routes>
      <Route path="/" element={<DashboardPage user={user} />} />
      <Route path="/servers/new" element={admin ? <NewServerPage user={user} /> : <Navigate to="/" replace />} />
      <Route path="/servers/:id" element={<ServerPage user={user} />} />
      <Route path="/profile" element={<ProfilePage user={user} />} />
      <Route path="/admin/users" element={admin ? <AdminUsersPage user={user} /> : <Navigate to="/" replace />} />
      <Route path="/admin/audit" element={admin ? <AdminAuditPage user={user} /> : <Navigate to="/" replace />} />
      <Route path="/admin/database" element={admin ? <AdminDatabasePage user={user} /> : <Navigate to="/" replace />} />
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
