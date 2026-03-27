import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'

export default function Layout() {
  const { logout } = useAuth()

  return (
    <div className="min-h-screen bg-surface">
      <nav className="border-b border-border bg-surface sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-6 h-14 flex items-center justify-between">
          <div className="flex items-center gap-8">
            <span className="text-lg font-semibold tracking-tight text-text">Short Links</span>
            <div className="flex gap-6">
              <NavLink
                to="/"
                end
                className={({ isActive }) =>
                  `text-sm ${isActive ? 'text-accent font-medium' : 'text-text-muted hover:text-text'}`
                }
              >
                Dashboard
              </NavLink>
              <NavLink
                to="/links"
                className={({ isActive }) =>
                  `text-sm ${isActive ? 'text-accent font-medium' : 'text-text-muted hover:text-text'}`
                }
              >
                Links
              </NavLink>
            </div>
          </div>
          <button
            onClick={logout}
            className="text-sm text-text-muted hover:text-text cursor-pointer"
          >
            Logout
          </button>
        </div>
      </nav>
      <main className="max-w-5xl mx-auto px-6 py-8">
        <Outlet />
      </main>
    </div>
  )
}
