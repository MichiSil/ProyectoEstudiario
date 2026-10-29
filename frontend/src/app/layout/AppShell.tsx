import { Outlet } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { BottomNav } from './BottomNav'
import { MobileTopBar } from './MobileTopBar'

export function AppShell() {
  return (
    <div className="flex h-svh overflow-hidden bg-paper">
      <Sidebar />
      <div className="flex min-w-0 flex-1 flex-col overflow-hidden [contain:layout]">
        <MobileTopBar />
        <main className="mx-auto w-full flex-1 overflow-y-auto pb-24 sm:pb-8">
          <Outlet />
        </main>
      </div>
      <BottomNav />
    </div>
  )
}
