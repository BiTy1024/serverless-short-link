import { useEffect, useMemo, useState } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import QRCode from 'qrcode'
import { api, shortUrl, type Link } from '../api/client'
import { useAuth } from '../auth/AuthProvider'

// NEW_DOMAIN is the select value that switches the create form to free-text domain entry.
const NEW_DOMAIN = '__new__'

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
  const [newDomainChoice, setNewDomainChoice] = useState('')
  const [newDomainText, setNewDomainText] = useState('')
  const [newSlug, setNewSlug] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [error, setError] = useState('')

  const loadLinks = () => {
    api.getLinks().then((data) => setLinks(data.links)).finally(() => setLoading(false))
  }

  useEffect(() => { loadLinks() }, [])

  // The domains offered in the filter and the create form are the ones that
  // already have links.
  const domains = useMemo(
    () => [...new Set(links.map((l) => l.domain))].sort(),
    [links]
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

  const domainChoice = newDomainChoice || domains[0] || NEW_DOMAIN
  const newDomain = domainChoice === NEW_DOMAIN ? newDomainText : domainChoice

  const handleCreate = async () => {
    setError('')
    try {
      await api.createLink(newDomain.trim(), newSlug, newUrl)
      setNewSlug('')
      setNewUrl('')
      setNewDomainText('')
      setNewDomainChoice('')
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
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-tight text-text">Links</h1>
        <button
          onClick={() => isAdmin && setShowCreate(!showCreate)}
          disabled={!isAdmin}
          className={`rounded px-3 py-1.5 text-sm font-medium ${isAdmin ? 'bg-accent text-white hover:bg-accent-light cursor-pointer' : 'bg-gray-200 text-gray-400 cursor-not-allowed'}`}
        >
          {showCreate ? 'Cancel' : 'Create'}
        </button>
      </div>

      {error && (
        <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">
          {error}
        </div>
      )}

      {showCreate && (
        <div className="border border-border rounded p-4 space-y-3">
          <div className="flex flex-wrap gap-3">
            <select
              aria-label="Domain"
              value={domainChoice}
              onChange={(e) => setNewDomainChoice(e.target.value)}
              className="border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
            >
              {domains.map((d) => (
                <option key={d} value={d}>{d}</option>
              ))}
              <option value={NEW_DOMAIN}>Other domain…</option>
            </select>
            {domainChoice === NEW_DOMAIN && (
              <input
                aria-label="New domain"
                placeholder="go.example.com"
                value={newDomainText}
                onChange={(e) => setNewDomainText(e.target.value)}
                className="flex-1 border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
              />
            )}
            <input
              aria-label="Slug"
              placeholder="slug"
              value={newSlug}
              onChange={(e) => setNewSlug(e.target.value)}
              className="flex-1 border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
            />
            <input
              aria-label="Target URL"
              placeholder="https://target-url.com"
              value={newUrl}
              onChange={(e) => setNewUrl(e.target.value)}
              className="flex-2 border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
            />
            <button
              onClick={handleCreate}
              className="bg-accent text-white rounded px-4 py-1.5 text-sm font-medium hover:bg-accent-light cursor-pointer"
            >
              Save
            </button>
          </div>
          {newDomain && newSlug && (
            <p className="text-xs text-text-muted font-mono">{shortUrl(newDomain.trim().toLowerCase(), newSlug)}</p>
          )}
        </div>
      )}

      <div className="flex gap-3">
        <select
          aria-label="Filter by domain"
          value={domainFilter}
          onChange={(e) => setDomainFilter(e.target.value)}
          className="border border-border rounded px-3 py-2 text-sm focus:outline-none focus:border-accent"
        >
          <option value="">All domains</option>
          {domains.map((d) => (
            <option key={d} value={d}>{d}</option>
          ))}
        </select>
        <input
          placeholder="Search links..."
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="flex-1 border border-border rounded px-3 py-2 text-sm focus:outline-none focus:border-accent"
        />
      </div>

      <div className="border border-border rounded divide-y divide-border">
        {filtered.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-text-muted">No links found.</p>
        ) : (
          filtered.map((link) => (
            <div key={linkKey(link)} className="px-4 py-3 flex items-center gap-4">
              <span className="text-sm text-text-muted min-w-[140px] truncate" title={link.domain}>
                {link.domain}
              </span>
              <RouterLink
                to={detailPath(link)}
                className="font-mono text-sm text-accent hover:underline min-w-[200px]"
              >
                {shortUrl(link.domain, link.slug)}
              </RouterLink>

              {editKey === linkKey(link) ? (
                <div className="flex-1 flex gap-2">
                  <input
                    value={editUrl}
                    onChange={(e) => setEditUrl(e.target.value)}
                    className="flex-1 border border-border rounded px-2 py-1 text-sm focus:outline-none focus:border-accent"
                  />
                  <button
                    onClick={() => handleUpdate(link)}
                    className="text-sm text-accent hover:underline cursor-pointer"
                  >
                    Save
                  </button>
                  <button
                    onClick={() => setEditKey(null)}
                    className="text-sm text-text-muted hover:underline cursor-pointer"
                  >
                    Cancel
                  </button>
                </div>
              ) : (
                <>
                  <span className="flex-1 text-sm text-text-muted truncate">
                    {link.target_url}
                  </span>
                  <RouterLink
                    to={detailPath(link)}
                    className="text-sm text-text-muted hover:text-text cursor-pointer"
                  >
                    Stats
                  </RouterLink>
                  <button
                    onClick={() => isAdmin && (setEditKey(linkKey(link)), setEditUrl(link.target_url))}
                    disabled={!isAdmin}
                    className={`text-sm ${isAdmin ? 'text-text-muted hover:text-text cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => isAdmin && handleDelete(link)}
                    disabled={!isAdmin}
                    className={`text-sm ${isAdmin ? 'text-red-500 hover:text-red-700 cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                  >
                    Delete
                  </button>
                  <button
                    onClick={() => downloadQR(link)}
                    className="text-sm text-text-muted hover:text-text cursor-pointer"
                  >
                    QR
                  </button>
                </>
              )}
            </div>
          ))
        )}
      </div>
    </div>
  )
}
