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
  short_path: string
  target_url: string
  created_at: string
  updated_at: string
}

export interface StatsOverview {
  stats: Array<{
    path: string
    clicks: number
    target_url: string | null
    first_click: string | null
    last_click: string | null
  }>
  total_clicks: number
}

export interface StatsDetail {
  path: string
  clicks: number
  target_url: string | null
  recent_clicks: Array<{ timestamp: string }>
}

export const api = {
  getLinks: (): Promise<{ links: Link[]; count: number }> =>
    request('/api/links'),

  createLink: (short_path: string, target_url: string): Promise<Link> =>
    request('/api/links', {
      method: 'POST',
      body: JSON.stringify({ short_path, target_url }),
    }),

  updateLink: (path: string, target_url: string): Promise<Link> =>
    request(`/api/links/${path}`, {
      method: 'PUT',
      body: JSON.stringify({ target_url }),
    }),

  deleteLink: (path: string): Promise<null> =>
    request(`/api/links/${path}`, { method: 'DELETE' }),

  getStats: (): Promise<StatsOverview> =>
    request('/api/stats?linked_only=true'),

  getStatsByPath: (path: string, params?: { days?: number; from?: string; to?: string }): Promise<StatsDetail> => {
    const query = new URLSearchParams()
    if (params?.days) query.set('days', String(params.days))
    if (params?.from) query.set('from', params.from)
    if (params?.to) query.set('to', params.to)
    const qs = query.toString()
    return request(`/api/stats/${path}${qs ? `?${qs}` : ''}`)
  },
}
