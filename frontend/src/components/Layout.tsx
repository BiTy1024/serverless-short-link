import { NavLink, Outlet } from 'react-router-dom'
import { useAuth } from '../auth/AuthProvider'
import { brandLogoUrl, brandName } from '../brand'

const navItems = [
  { to: '/', label: 'Dashboard', end: true },
  { to: '/links', label: 'Links', end: false },
  { to: '/settings/domains', label: 'Domains', end: false },
]

export default function Layout() {
  const { logout } = useAuth()

  // Below sm the bar has two rows: brand and Logout on top, the nav items
  // underneath. From sm on everything sits in one row.
  return (
    <div className="min-h-screen bg-surface">
      <nav className="border-b border-border bg-surface sticky top-0 z-10">
        <div className="max-w-5xl mx-auto px-4 sm:px-6 py-1 sm:py-0 sm:h-14 flex flex-wrap sm:flex-nowrap items-center justify-between gap-x-8">
          <span className="order-1 flex min-w-0 items-center gap-2 h-12 sm:h-auto text-lg font-semibold tracking-tight text-text">
            {brandLogoUrl && (
              <img src={brandLogoUrl} alt="" className="h-7 w-auto max-w-28 sm:max-w-40 object-contain shrink-0" />
            )}
            <span className="truncate">{brandName}</span>
          </span>
          <div className="order-3 sm:order-2 w-full sm:w-auto sm:flex-1 flex gap-1 sm:gap-6 -mx-2 sm:mx-0">
            {navItems.map(({ to, label, end }) => (
              <NavLink
                key={to}
                to={to}
                end={end}
                className={({ isActive }) =>
                  `inline-flex items-center min-h-10 px-2 sm:px-0 text-sm ${isActive ? 'text-accent font-medium' : 'text-text-muted hover:text-text'}`
                }
              >
                {label}
              </NavLink>
            ))}
          </div>
          <button
            onClick={logout}
            className="order-2 sm:order-3 shrink-0 inline-flex items-center min-h-10 px-2 -mr-2 sm:mr-0 sm:px-0 text-sm text-text-muted hover:text-text cursor-pointer"
          >
            Logout
          </button>
        </div>
      </nav>
      <main className="max-w-5xl mx-auto px-4 sm:px-6 py-6 sm:py-8">
        <Outlet />
      </main>
    </div>
  )
}
