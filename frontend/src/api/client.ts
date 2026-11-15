import { fetchAuthSession } from 'aws-amplify/auth'

const API_BASE = import.meta.env.VITE_API_BASE

async function getToken(): Promise<string> {
  const session = await fetchAuthSession()
  const token = session.tokens?.accessToken?.toString() ?? ''
  if (!token) console.warn('No access token found in session')
  return token
}

async function request(path: string, options: RequestInit = {}) {
  const token = await getToken()
  const res = await fetch(`${API_BASE}${path}`, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      Authorization: `Bearer ${token}`,
      ...options.headers,
    },
  })
  if (res.status === 401 || res.status === 403) {
    window.location.href = '/login'
    throw new Error('Session expired')
  }
  if (!res.ok) {
    const body = await res.json().catch(() => ({}))
    throw new Error(body.error || `HTTP ${res.status}`)
  }
  if (res.status === 204) return null
  return res.json()
}

export interface Link {
  domain: string
  slug: string
  target_url: string
  created_at: string
  updated_at: string
}

export interface StatsOverview {
  stats: Array<{
    domain: string
    slug: string
    clicks: number
    target_url: string | null
    first_click: string | null
    last_click: string | null
  }>
  total_clicks: number
}

export interface StatsDetail {
  domain: string
  slug: string
  clicks: number
  target_url: string | null
  recent_clicks: Array<{ timestamp: string }>
}

export interface TimeFilter {
  days?: number
  from?: string
  to?: string
}

// linkPath addresses one link as /<domain>/<slug>, each segment escaped.
function linkPath(domain: string, slug: string) {
  return `/${encodeURIComponent(domain)}/${encodeURIComponent(slug)}`
}

function queryString(params: Record<string, string | number | undefined>) {
  const query = new URLSearchParams()
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== '') query.set(key, String(value))
  }
  const qs = query.toString()
  return qs ? `?${qs}` : ''
}

// shortUrl is the public URL a link is served at.
export function shortUrl(domain: string, slug: string) {
  return `https://${domain}/${slug}`
}

export const api = {
  getLinks: (domain?: string): Promise<{ links: Link[]; count: number }> =>
    request(`/api/links${queryString({ domain })}`),

  createLink: (domain: string, slug: string, target_url: string): Promise<Link> =>
    request('/api/links', {
      method: 'POST',
      body: JSON.stringify({ domain, slug, target_url }),
    }),

  updateLink: (domain: string, slug: string, target_url: string): Promise<Link> =>
    request(`/api/links${linkPath(domain, slug)}`, {
      method: 'PUT',
      body: JSON.stringify({ target_url }),
    }),

  deleteLink: (domain: string, slug: string): Promise<null> =>
    request(`/api/links${linkPath(domain, slug)}`, { method: 'DELETE' }),

  getStats: (params?: { domain?: string } & TimeFilter): Promise<StatsOverview> =>
    request(`/api/stats${queryString({ linked_only: 'true', ...params })}`),

  getLinkStats: (domain: string, slug: string, params?: TimeFilter): Promise<StatsDetail> =>
    request(`/api/stats${linkPath(domain, slug)}${queryString({ ...params })}`),
}
