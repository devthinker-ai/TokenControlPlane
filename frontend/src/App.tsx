import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { AppLayout } from '@/components/AppLayout'
import { ProtectedRoute } from '@/components/ProtectedRoute'
import { ToastProvider } from '@/components/Toast'
import { LoginPage } from '@/pages/LoginPage'
import { RegisterPage } from '@/pages/RegisterPage'
import { ResetPasswordPage } from '@/pages/ResetPasswordPage'
import { OverviewPage } from '@/pages/OverviewPage'
import { ServersPage } from '@/pages/ServersPage'
import { ProvidersPage } from '@/pages/ProvidersPage'
import { KeysPage } from '@/pages/KeysPage'
import { BillingPage } from '@/pages/BillingPage'
import { ActivityPage } from '@/pages/ActivityPage'
import { MembersPage } from '@/pages/MembersPage'
import { SettingsPage } from '@/pages/SettingsPage'

export default function App() {
  return (
    <ToastProvider>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage />} />
          <Route path="/register" element={<RegisterPage />} />
          <Route path="/reset-password" element={<ResetPasswordPage />} />
          <Route element={<ProtectedRoute />}>
            <Route element={<AppLayout />}>
              <Route path="/" element={<OverviewPage />} />
              <Route path="/servers" element={<ServersPage />} />
              <Route path="/providers" element={<ProvidersPage />} />
              <Route path="/keys" element={<KeysPage />} />
              <Route path="/members" element={<MembersPage />} />
              <Route path="/settings" element={<SettingsPage />} />
              <Route path="/activity" element={<ActivityPage />} />
              <Route path="/billing" element={<BillingPage />} />
            </Route>
          </Route>
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </ToastProvider>
  )
}
