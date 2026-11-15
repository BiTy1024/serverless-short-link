import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, shortUrl, type StatsOverview } from '../api/client'

export default function Dashboard() {
  const [stats, setStats] = useState<StatsOverview | null>(null)
  const [domains, setDomains] = useState<string[]>([])
  const [domain, setDomain] = useState('')
  const [loading, setLoading] = useState(true)

  // The domain filter offers every domain that has links.
  useEffect(() => {
    api
      .getLinks()
      .then((data) => setDomains([...new Set(data.links.map((l) => l.domain))].sort()))
      .catch(() => setDomains([]))
  }, [])

  useEffect(() => {
    setLoading(true)
    api
      .getStats(domain ? { domain } : undefined)
      .then(setStats)
      .catch(() => setStats(null))
      .finally(() => setLoading(false))
  }, [domain])

  const header = (
    <div className="flex items-center justify-between">
      <h1 className="text-xl font-semibold tracking-tight text-text">Dashboard</h1>
      <select
        aria-label="Filter by domain"
        value={domain}
        onChange={(e) => setDomain(e.target.value)}
        className="border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
      >
        <option value="">All domains</option>
        {domains.map((d) => (
          <option key={d} value={d}>{d}</option>
        ))}
      </select>
    </div>
  )

  if (loading) return <div className="space-y-8">{header}<p className="text-text-muted text-sm">Loading...</p></div>
  if (!stats) return <div className="space-y-8">{header}<p className="text-text-muted text-sm">Failed to load stats.</p></div>

  // With all domains shown, a bar is labelled domain/slug so equal slugs on
  // different domains stay apart.
  const topLinks = stats.stats.slice(0, 10).map((s) => ({
    ...s,
    label: domain ? `/${s.slug}` : `${s.domain}/${s.slug}`,
  }))
  const axisWidth = domain ? 80 : 180

  return (
    <div className="space-y-8">
      {header}

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
            <BarChart data={topLinks} layout="vertical" margin={{ left: axisWidth }}>
              <XAxis type="number" tick={{ fontSize: 12 }} />
              <YAxis
                dataKey="label"
                type="category"
                tick={{ fontSize: 12 }}
                width={axisWidth}
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
              key={`${s.domain}/${s.slug}`}
              to={`/links/${encodeURIComponent(s.domain)}/${encodeURIComponent(s.slug)}`}
              className="flex items-center justify-between px-4 py-3 hover:bg-gray-50 text-sm"
            >
              <span className="font-mono text-text">{shortUrl(s.domain, s.slug)}</span>
              <span className="text-text-muted">{s.clicks} clicks</span>
            </Link>
          ))}
        </div>
      </div>
    </div>
  )
}
