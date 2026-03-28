import { useEffect, useState } from 'react'
import { Link as RouterLink } from 'react-router-dom'
import QRCode from 'qrcode'
import { api, type Link } from '../api/client'
import { useAuth } from '../auth/AuthProvider'

const DOMAIN = import.meta.env.VITE_API_BASE

function downloadQR(shortPath: string) {
  const url = `${DOMAIN}/${shortPath}`
  QRCode.toString(url, { type: 'svg', margin: 1 }, (err, svg) => {
    if (err) return
    const blob = new Blob([svg], { type: 'image/svg+xml' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = `qr-${shortPath}.svg`
    a.click()
    URL.revokeObjectURL(a.href)
  })
}

export default function Links() {
  const { isAdmin } = useAuth()
  const [links, setLinks] = useState<Link[]>([])
  const [search, setSearch] = useState('')
  const [loading, setLoading] = useState(true)
  const [showCreate, setShowCreate] = useState(false)
  const [editPath, setEditPath] = useState<string | null>(null)
  const [editUrl, setEditUrl] = useState('')
  const [newPath, setNewPath] = useState('')
  const [newUrl, setNewUrl] = useState('')
  const [error, setError] = useState('')

  const loadLinks = () => {
    api.getLinks().then((data) => setLinks(data.links)).finally(() => setLoading(false))
  }

  useEffect(() => { loadLinks() }, [])

  const filtered = links.filter(
    (l) =>
      l.short_path.includes(search.toLowerCase()) ||
      l.target_url.toLowerCase().includes(search.toLowerCase())
  )

  const handleCreate = async () => {
    setError('')
    try {
      await api.createLink(newPath, newUrl)
      setNewPath('')
      setNewUrl('')
      setShowCreate(false)
      loadLinks()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Create failed')
    }
  }

  const handleUpdate = async (path: string) => {
    setError('')
    try {
      await api.updateLink(path, editUrl)
      setEditPath(null)
      loadLinks()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Update failed')
    }
  }

  const handleDelete = async (path: string) => {
    if (!confirm(`Delete /${path}?`)) return
    try {
      await api.deleteLink(path)
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
          <div className="flex gap-3">
            <input
              placeholder="path"
              value={newPath}
              onChange={(e) => setNewPath(e.target.value)}
              className="flex-1 border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent"
            />
            <input
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
        </div>
      )}

      <input
        placeholder="Search links..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="w-full border border-border rounded px-3 py-2 text-sm focus:outline-none focus:border-accent"
      />

      <div className="border border-border rounded divide-y divide-border">
        {filtered.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-text-muted">No links found.</p>
        ) : (
          filtered.map((link) => (
            <div key={link.short_path} className="px-4 py-3 flex items-center gap-4">
              <RouterLink
                to={`/links/${link.short_path}`}
                className="font-mono text-sm text-accent hover:underline min-w-[120px]"
              >
                /{link.short_path}
              </RouterLink>

              {editPath === link.short_path ? (
                <div className="flex-1 flex gap-2">
                  <input
                    value={editUrl}
                    onChange={(e) => setEditUrl(e.target.value)}
                    className="flex-1 border border-border rounded px-2 py-1 text-sm focus:outline-none focus:border-accent"
                  />
                  <button
                    onClick={() => handleUpdate(link.short_path)}
                    className="text-sm text-accent hover:underline cursor-pointer"
                  >
                    Save
                  </button>
                  <button
                    onClick={() => setEditPath(null)}
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
                    to={`/links/${link.short_path}`}
                    className="text-sm text-text-muted hover:text-text cursor-pointer"
                  >
                    Stats
                  </RouterLink>
                  <button
                    onClick={() => isAdmin && (setEditPath(link.short_path), setEditUrl(link.target_url))}
                    disabled={!isAdmin}
                    className={`text-sm ${isAdmin ? 'text-text-muted hover:text-text cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                  >
                    Edit
                  </button>
                  <button
                    onClick={() => isAdmin && handleDelete(link.short_path)}
                    disabled={!isAdmin}
                    className={`text-sm ${isAdmin ? 'text-red-500 hover:text-red-700 cursor-pointer' : 'text-gray-300 cursor-not-allowed'}`}
                  >
                    Delete
                  </button>
                  <button
                    onClick={() => downloadQR(link.short_path)}
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
