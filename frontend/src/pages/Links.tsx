import { useEffect, useMemo, useState } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import QRCode from 'qrcode'
import { api, shortUrl, type Link } from '../api/client'
import { useAuth } from '../auth/AuthProvider'

// Form fields are full width on phones; 16px text there keeps iOS from
// zooming in on focus.
const fieldClass =
  'min-h-10 border border-border rounded px-3 py-2 text-base sm:text-sm focus:outline-none focus:border-accent'

// Row actions are text buttons with a 40px tap target.
const actionClass = 'inline-flex items-center min-h-10 px-2 text-sm'

function linkKey(link: Pick<Link, 'domain' | 'slug'>) {
  return `${link.domain}/${link.slug}`
}

function detailPath(link: Pick<Link, 'domain' | 'slug'>) {
  return `/links/${encodeURIComponent(link.domain)}/${encodeURIComponent(link.slug)}`
}

function downloadQR(link: Link) {
  QRCode.toString(shortUrl(link.domain, link.slug), { type: 'svg', margin: 1 }, (err, svg) => {
    if (err) return
    const blob = new Blob([svg], { type: 'image/svg+xml' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `qr-${link.domain}-${link.slug}.svg`
    a.click()
    URL.revokeObjectURL(a.href)
  })
}

export default function Links() {
  const { isAdmin } = useAuth()
  const [links, setLinks] = useState<Link[]>([])
  const [search, setSearch] = useState('')
  const [domainFilter, setDomainFilter] = useState('')
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [editKey, setEditKey] = useState<string | null>(null)
  const [editUrl, setEditUrl] = useState('')
  const [activeDomains, setActiveDomains] = useState<string[]>([])
  const [newDomainChoice, setNewDomainChoice] = useState('')
  const [newSlug, setNewSlug] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [error, setError] = useState('')

  const loadLinks = () => {
    api.getLinks().then((data) => setLinks(data.links)).finally(() => setLoading(false))
  }

  useEffect(() => {
    loadLinks()
    api.getDomains()
      .then((data) => setActiveDomains(data.domains.filter((d) => d.status === 'active').map((d) => d.domain)))
      .catch((err: unknown) => setError(err instanceof Error ? err.message : 'Loading domains failed'))
  }, [])

  // The filter offers every domain that has links or is active; the create
  // form offers the active domains, in the order the API lists them.
  const domains = useMemo(
    () => [...new Set([...links.map((l) => l.domain), ...activeDomains])].sort(),
    [links, activeDomains]
  )

  const query = search.toLowerCase()
  const filtered = links
    .filter((l) => !domainFilter || l.domain === domainFilter)
    .filter(
      (l) =>
        l.slug.includes(query) ||
        l.domain.includes(query) ||
        l.target_url.toLowerCase().includes(query)
    )
    .sort((a, b) => linkKey(a).localeCompare(linkKey(b)))

  const newDomain = activeDomains.includes(newDomainChoice) ? newDomainChoice : activeDomains[0] ?? ''

  const handleCreate = async () => {
    setError('')
    try {
      await api.createLink(newDomain, newSlug, newUrl)
      setNewSlug('')
      setNewUrl('')
      setShowCreate(false)
      loadLinks()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Create failed')
    }
  }

  const handleUpdate = async (link: Link) => {
    setError('')
    try {
      await api.updateLink(link.domain, link.slug, editUrl)
      setEditKey(null)
      loadLinks()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Update failed')
    }
  }

  const handleDelete = async (link: Link) => {
    if (!confirm(`Delete ${shortUrl(link.domain, link.slug)}?`)) return
    try {
      await api.deleteLink(link.domain, link.slug)
      loadLinks()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Delete failed')
    }
  }

  if (loading) return <p className="text-text-muted text-sm">Loading...</p>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold tracking-tight text-text">Links</h1>
        <button
          onClick={() => isAdmin && setShowCreate(!showCreate)}
          disabled={!isAdmin}
          className={`min-h-10 rounded px-4 text-sm font-medium ${isAdmin ? 'bg-accent text-white hover:bg-accent-light cursor-pointer' : 'bg-gray-200 text-gray-400 cursor-not-allowed'}`}
        >
          {showCreate ? 'Cancel' : 'Create'}
        </button>
      </div>

      {error && (
        <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2 break-words">
          {error}
        </div>
      )}

      {showCreate && (
        <div className="border border-border rounded p-4 space-y-3">
          <div className="flex flex-col sm:flex-row sm:flex-wrap gap-3">
            <select
              aria-label="Domain"
              value={newDomain}
              onChange={(e) => setNewDomainChoice(e.target.value)}
              className={`w-full sm:w-auto min-w-0 ${fieldClass}`}
            >
              {activeDomains.length === 0 && <option value="">No active domain</option>}
              {activeDomains.map((d) => (
                <option key={d} value={d}>{d}</option>
              ))}
            </select>
            <input
              aria-label="Slug"
              placeholder="slug"
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value)}
              className={`w-full sm:w-auto sm:flex-1 sm:min-w-32 ${fieldClass}`}
            />
            <input
              aria-label="Target URL"
              placeholder="https://target-url.com"
              value={newUrl}
              onChange={(e) => setNewUrl(e.target.value)}
              className={`w-full sm:w-auto sm:flex-2 sm:min-w-48 ${fieldClass}`}
            />
            <button
              onClick={handleCreate}
              className="min-h-10 bg-accent text-white rounded px-4 text-sm font-medium hover:bg-accent-light cursor-pointer"
            >
              Save
            </button>
          </div>
          {newDomain && newSlug && (
            <p className="text-xs text-text-muted font-mono break-all">{shortUrl(newDomain, newSlug)}</p>
          )}
        </div>
      )}

      <div className="flex flex-col sm:flex-row gap-3">
        <select
          aria-label="Filter by domain"
          value={domainFilter}
          onChange={(e) => setDomainFilter(e.target.value)}
          className={`w-full sm:w-auto min-w-0 ${fieldClass}`}
        >
          <option value="">All domains</option>
          {domains.map((d) => (
            <option key={d} value={d}>{d}</option>
          ))}
        </select>
        <input
          aria-label="Search links"
          placeholder="Search links..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className={`w-full sm:w-auto sm:flex-1 min-w-0 ${fieldClass}`}
        />
      </div>

      {/* Below md each link is a card: short URL, target URL, then the
          actions on their own row. From md on it is one row whose text
          column shrinks and truncates, so it never runs under the actions.
          The short URL already names the domain, so there is no domain
          column. */}
      <div className="border border-border rounded divide-y divide-border">
        {filtered.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-text-muted">No links found.</p>
        ) : (
          filtered.map((link) => {
            const url = shortUrl(link.domain, link.slug)
            const editing = editKey === linkKey(link)
            return (
              <div key={linkKey(link)} className="px-4 py-3 flex flex-col gap-2 md:flex-row md:items-center md:gap-4">
                <div className="min-w-0 flex-1 space-y-0.5">
                  <RouterLink
                    to={detailPath(link)}
                    title={url}
                    className="block font-mono text-sm text-accent hover:underline break-all md:truncate"
                  >
                    {url}
                  </RouterLink>
                  {editing ? (
                    <div className="flex flex-col sm:flex-row gap-2 pt-1">
                      <input
                        aria-label="Target URL"
                        value={editUrl}
                        onChange={(e) => setEditUrl(e.target.value)}
                        className={`w-full sm:flex-1 min-w-0 ${fieldClass}`}
                      />
                      <div className="flex gap-2 shrink-0">
                        <button
                          onClick={() => handleUpdate(link)}
                          className={`${actionClass} text-accent hover:underline cursor-pointer`}
                        >
                          Save
                        </button>
                        <button
                          onClick={() => setEditKey(null)}
                          className={`${actionClass} text-text-muted hover:underline cursor-pointer`}
                        >
                          Cancel
                        </button>
                      </div>
                    </div>
                  ) : (
                    <p className="text-sm text-text-muted truncate" title={link.target_url}>
                      {link.target_url}
                    </p>
                  )}
                </div>

                {!editing && (
                  <div className="flex flex-wrap items-center gap-1 -ml-2 md:ml-0 md:shrink-0">
                    <RouterLink
                      to={detailPath(link)}
                      className={`${actionClass} text-text-muted hover:text-text cursor-pointer`}
                    >
                      Stats
                    </RouterLink>
                    <button
                      onClick={() => isAdmin && (setEditKey(linkKey(link)), setEditUrl(link.target_url))}
                      disabled={!isAdmin}
                      className={`${actionClass} ${isAdmin ? 'text-text-muted hover:text-text cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                    >
                      Edit
                    </button>
                    <button
                      onClick={() => isAdmin && handleDelete(link)}
                      disabled={!isAdmin}
                      className={`${actionClass} ${isAdmin ? 'text-red-500 hover:text-red-700 cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                    >
                      Delete
                    </button>
                    <button
                      onClick={() => downloadQR(link)}
                      className={`${actionClass} text-text-muted hover:text-text cursor-pointer`}
                    >
                      QR
                    </button>
                  </div>
                )}
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}
