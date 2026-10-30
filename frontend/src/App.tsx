import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { ThemeProvider } from '@/app/providers/ThemeProvider'
import { SessionProvider } from '@/app/providers/SessionProvider'
import { AuthGate } from '@/features/auth/AuthGate'
import { AppShell } from '@/app/layout/AppShell'
import { HomeScreen } from '@/features/home/HomeScreen'
import { CalendarScreen } from '@/features/calendar/CalendarScreen'
import { SubjectsListScreen } from '@/features/subjects/SubjectsListScreen'
import { SubjectDetailScreen } from '@/features/subjects/SubjectDetailScreen'
import { SubjectOverviewTab } from '@/features/subjects/tabs/SubjectOverviewTab'
import { SubjectPlanTab } from '@/features/subjects/tabs/SubjectPlanTab'
import { PlannerScreen } from '@/features/planner/PlannerScreen'
import { PomodoroScreen } from '@/features/pomodoro/PomodoroScreen'
import { SettingsScreen } from '@/features/settings/SettingsScreen'

export default function App() {
  return (
    <ThemeProvider>
      <SessionProvider>
        <BrowserRouter>
          <AuthGate>
            <Routes>
              <Route element={<AppShell />}>
                <Route index element={<HomeScreen />} />
                <Route path="calendario" element={<CalendarScreen />} />
                <Route path="materias" element={<SubjectsListScreen />} />
                <Route path="materias/:subjectId" element={<SubjectDetailScreen />}>
                  <Route index element={<SubjectOverviewTab />} />
                  <Route path="plan" element={<SubjectPlanTab />} />
                </Route>
                <Route path="plan" element={<PlannerScreen />} />
                <Route path="pomodoro" element={<PomodoroScreen />} />
                <Route path="configuracion" element={<SettingsScreen />} />
                <Route path="*" element={<Navigate to="/" replace />} />
              </Route>
            </Routes>
          </AuthGate>
        </BrowserRouter>
      </SessionProvider>
    </ThemeProvider>
  )
}
