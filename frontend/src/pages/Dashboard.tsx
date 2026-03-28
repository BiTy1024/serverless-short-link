import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, type StatsOverview } from '../api/client'

export default function Dashboard() {
  const [stats, setStats] = useState<StatsOverview | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.getStats().then(setStats).finally(() => setLoading(false))
  }, [])

  if (loading) return <p className="text-text-muted text-sm">Loading...</p>
  if (!stats) return <p className="text-text-muted text-sm">Failed to load stats.</p>

  const topLinks = stats.stats.slice(0, 10)

  return (
    <div className="space-y-8">
      <h1 className="text-xl font-semibold tracking-tight text-text">Dashboard</h1>

      <div className="grid grid-cols-2 gap-4">
        <div className="border border-border rounded p-5">
          <p className="text-sm text-text-muted mb-1">Total Clicks</p>
          <p className="text-3xl font-semibold text-text">{stats.total_clicks}</p>
        </div>
        <div className="border border-border rounded p-5">
          <p className="text-sm text-text-muted mb-1">Active Links</p>
          <p className="text-3xl font-semibold text-text">{stats.stats.length}</p>
        </div>
      </div>

      <div>
        <h2 className="text-sm font-medium text-text-muted mb-4">Top Links by Clicks</h2>
        {topLinks.length === 0 ? (
          <p className="text-sm text-text-muted">No data yet.</p>
        ) : (
          <ResponsiveContainer width="100%" height={300}>
            <BarChart data={topLinks} layout="vertical" margin={{ left: 80 }}>
              <XAxis type="number" tick={{ fontSize: 12 }} />
              <YAxis
                dataKey="path"
                type="category"
                tick={{ fontSize: 12 }}
                width={80}
              />
              <Tooltip />
              <Bar dataKey="clicks" fill="var(--color-accent)" radius={[0, 2, 2, 0]} />
            </BarChart>
          </ResponsiveContainer>
        )}
      </div>

      <div>
        <h2 className="text-sm font-medium text-text-muted mb-3">All Links</h2>
        <div className="border border-border rounded divide-y divide-border">
          {stats.stats.map((s) => (
            <Link
              key={s.path}
              to={`/links/${s.path.replace(/^\//, '')}`}
              className="flex items-center justify-between px-4 py-3 hover:bg-gray-50 text-sm"
            >
              <span className="font-mono text-text">{s.path}</span>
              <span className="text-text-muted">{s.clicks} clicks</span>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}
