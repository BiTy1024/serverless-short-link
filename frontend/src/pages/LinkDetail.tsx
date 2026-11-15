import { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { LineChart, Line, XAxis, YAxis, Tooltip, ResponsiveContainer } from 'recharts'
import { api, shortUrl, type StatsDetail } from '../api/client'

const RANGES = [
  { label: '7d', days: 7 },
  { label: '30d', days: 30 },
  { label: '90d', days: 90 },
  { label: 'All', days: undefined },
]

function aggregateByDate(clicks: Array<{ timestamp: string }>) {
  const counts: Record<string, number> = {}
  for (const click of clicks) {
    const date = click.timestamp.split('T')[0]
    counts[date] = (counts[date] || 0) + 1
  }
  return Object.entries(counts)
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([date, count]) => ({ date, clicks: count }))
}

export default function LinkDetail() {
  const { domain, slug } = useParams<{ domain: string; slug: string }>()
  const [stats, setStats] = useState<StatsDetail | null>(null)
  const [loading, setLoading] = useState(true)
  const [range, setRange] = useState<number | undefined>(30)

  useEffect(() => {
    if (!domain || !slug) return
    setLoading(true)
    api
      .getLinkStats(domain, slug, range ? { days: range } : undefined)
      .then(setStats)
      .finally(() => setLoading(false))
  }, [domain, slug, range])

  if (loading) return <p className="text-text-muted text-sm">Loading...</p>
  if (!stats) return <p className="text-text-muted text-sm">Failed to load stats.</p>

  const chartData = aggregateByDate(stats.recent_clicks)

  return (
    <div className="space-y-8">
      <div>
        <Link to="/links" className="inline-flex items-center min-h-10 text-sm text-text-muted hover:text-text">
          &larr; Back to links
        </Link>
      </div>

      <div>
        <h1 className="text-lg sm:text-xl font-semibold tracking-tight text-text font-mono break-all">{shortUrl(stats.domain, stats.slug)}</h1>
        {stats.target_url && (
          <p className="text-sm text-text-muted mt-1 break-all sm:truncate" title={stats.target_url}>{stats.target_url}</p>
        )}
      </div>

      <div className="border border-border rounded p-5">
        <p className="text-sm text-text-muted mb-1">Total Clicks</p>
        <p className="text-3xl font-semibold text-text">{stats.clicks}</p>
      </div>

      <div>
        <div className="flex flex-wrap items-center justify-between gap-2 mb-4">
          <h2 className="text-sm font-medium text-text-muted">Click Trend</h2>
          <div className="flex gap-1">
            {RANGES.map((r) => (
              <button
                key={r.label}
                onClick={() => setRange(r.days)}
                className={`min-h-10 min-w-10 px-3 text-xs rounded cursor-pointer ${
                  range === r.days
                    ? 'bg-accent text-white'
                    : 'bg-gray-100 text-text-muted hover:bg-gray-200'
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>
        </div>

        {chartData.length === 0 ? (
          <p className="text-sm text-text-muted py-8 text-center">No clicks in this period.</p>
        ) : (
          <ResponsiveContainer width="100%" height={250}>
            <LineChart data={chartData}>
              <XAxis dataKey="date" tick={{ fontSize: 11 }} />
              <YAxis tick={{ fontSize: 11 }} allowDecimals={false} />
              <Tooltip />
              <Line
                type="monotone"
                dataKey="clicks"
                stroke="var(--color-accent)"
                strokeWidth={2}
                dot={false}
              />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>

      <div>
        <h2 className="text-sm font-medium text-text-muted mb-3">Recent Clicks</h2>
        <div className="border border-border rounded divide-y divide-border">
          {stats.recent_clicks.length === 0 ? (
            <p className="px-4 py-6 text-center text-sm text-text-muted">No clicks yet.</p>
          ) : (
            stats.recent_clicks.map((click, i) => (
              <div key={i} className="px-4 py-2 text-sm font-mono text-text-muted">
                {new Date(click.timestamp).toLocaleString()}
              </div>
            ))
          )}
        </div>
      </div>
    </div>
  )
}
