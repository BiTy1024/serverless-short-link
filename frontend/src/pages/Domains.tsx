import { useEffect, useState } from 'react'
import { api, type DNSMode, type DNSRecord, type DomainSetting, type DomainStatus } from '../api/client'
import { useAuth } from '../auth/AuthProvider'

const statusLabel: Record<DomainStatus, string> = {
  pending_validation: 'Waiting for certificate',
  pending_dns: 'Waiting for DNS',
  active: 'Active',
  failed: 'Failed',
}

const statusStyle: Record<DomainStatus, string> = {
  pending_validation: 'bg-amber-50 text-amber-800 border-amber-200',
  pending_dns: 'bg-amber-50 text-amber-800 border-amber-200',
  active: 'bg-green-50 text-green-800 border-green-200',
  failed: 'bg-red-50 text-red-700 border-red-200',
}

const inputClass =
  'border border-border rounded px-3 py-1.5 text-sm focus:outline-none focus:border-accent'

function StatusBadge({ status }: { status: DomainStatus }) {
  return (
    <span className={`text-xs border rounded px-2 py-0.5 ${statusStyle[status]}`}>
      {statusLabel[status]}
    </span>
  )
}

function CopyButton({ value }: { value: string }) {
  const [copied, setCopied] = useState(false)
  const copy = async () => {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    setTimeout(() => setCopied(false), 1500)
  }
  return (
    <button onClick={copy} className="text-xs text-text-muted hover:text-text cursor-pointer">
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

function RecordTable({ title, hint, records }: { title: string; hint: string; records: DNSRecord[] }) {
  if (records.length === 0) return null
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-text">{title}</p>
      <p className="text-xs text-text-muted">{hint}</p>
      <div className="overflow-x-auto">
        <table className="w-full text-xs font-mono">
          <thead>
            <tr className="text-left text-text-muted">
              <th className="py-1 pr-3 font-normal">Type</th>
              <th className="py-1 pr-3 font-normal">Name</th>
              <th className="py-1 pr-3 font-normal">Value</th>
            </tr>
          </thead>
          <tbody>
            {records.map((r) => (
              <tr key={`${r.type} ${r.name}`} className="border-t border-border align-top">
                <td className="py-1 pr-3">{r.type}</td>
                <td className="py-1 pr-3 break-all">
                  {r.name} <CopyButton value={r.name} />
                </td>
                <td className="py-1 pr-3 break-all">
                  {r.value} <CopyButton value={r.value} />
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// DomainRecords shows the records a person still has to set. In route53 mode
// the service writes them itself.
function DomainRecords({ d }: { d: DomainSetting }) {
  if (d.dns_mode === 'route53') {
    return <p className="text-xs text-text-muted">DNS records are written to Route 53 hosted zone {d.hosted_zone_id}.</p>
  }
  return (
    <div className="space-y-3">
      {d.status === 'pending_validation' && d.validation_records.length === 0 && (
        <p className="text-xs text-text-muted">The certificate's validation record is not available yet. Check again in a few seconds.</p>
      )}
      {(d.status === 'pending_validation' || d.role === 'primary') && (
        <RecordTable
          title="Certificate validation"
          hint="Set these CNAME records at your DNS host and keep them, so the certificate can renew."
          records={d.validation_records}
        />
      )}
      <RecordTable
        title={d.role === 'primary' ? 'Service records' : 'Domain record'}
        hint="Point the domain at the service with these CNAME records. A domain apex cannot be a CNAME."
        records={d.dns_records}
      />
    </div>
  )
}

function DomainRow({ d, isAdmin, onChange }: { d: DomainSetting; isAdmin: boolean; onChange: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [fallback, setFallback] = useState(d.fallback_url)
  const secondary = d.role === 'secondary'

  const run = async (action: () => Promise<unknown>) => {
    setBusy(true)
    setError('')
    try {
      await action()
      onChange()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Request failed')
    } finally {
      setBusy(false)
    }
  }

  const check = () => run(() => api.checkDomain(d.domain))
  const saveFallback = () => run(async () => {
    await api.updateDomainFallback(d.domain, fallback)
    setEditing(false)
  })
  const remove = () => {
    if (!confirm(`Delete ${d.domain}? Its certificate and API Gateway custom domain are removed as well.`)) return
    run(() => api.deleteDomain(d.domain))
  }

  return (
    <div className="px-4 py-4 space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <span className="font-mono text-sm text-text">{d.domain}</span>
        <StatusBadge status={d.status} />
        <span className="text-xs text-text-muted">
          {d.role === 'primary' ? 'Primary' : 'Secondary'} · {d.dns_mode === 'route53' ? 'Route 53' : 'External DNS'}
        </span>
        {isAdmin && secondary && (
          <div className="ml-auto flex gap-4">
            {d.status !== 'active' && d.status !== 'failed' && (
              <button onClick={check} disabled={busy} className="text-sm text-accent hover:underline cursor-pointer disabled:opacity-50">
                {busy ? 'Checking…' : 'Check now'}
              </button>
            )}
            <button onClick={remove} disabled={busy} className="text-sm text-red-500 hover:text-red-700 cursor-pointer disabled:opacity-50">
              Delete
            </button>
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-text-muted">Fallback:</span>
        {editing ? (
          <>
            <input aria-label="Fallback URL" value={fallback} onChange={(e) => setFallback(e.target.value)} className={`flex-1 ${inputClass}`} />
            <button onClick={saveFallback} disabled={busy} className="text-sm text-accent hover:underline cursor-pointer">Save</button>
            <button onClick={() => { setEditing(false); setFallback(d.fallback_url) }} className="text-sm text-text-muted hover:underline cursor-pointer">Cancel</button>
          </>
        ) : (
          <>
            <span className="truncate">{d.fallback_url}</span>
            {isAdmin && secondary && (
              <button onClick={() => setEditing(true)} className="text-sm text-text-muted hover:text-text cursor-pointer">Edit</button>
            )}
            {!secondary && <span className="text-xs text-text-muted">(Terraform variable default_redirect_url)</span>}
          </>
        )}
      </div>

      {(d.error || error) && (
        <p className="text-xs text-red-600">{error || d.error}</p>
      )}
      {d.status !== 'failed' && <DomainRecords d={d} />}
    </div>
  )
}

function AddDomainForm({ zones, onAdded }: { zones: string[]; onAdded: () => void }) {
  const [domain, setDomain] = useState('')
  const [mode, setMode] = useState<DNSMode>('external')
  const [zone, setZone] = useState('')
  const [fallback, setFallback] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)

  const submit = async () => {
    setBusy(true)
    setError('')
    try {
      await api.createDomain({
        domain: domain.trim(),
        dns_mode: mode,
        hosted_zone_id: mode === 'route53' ? zone || zones[0] : undefined,
        fallback_url: fallback.trim(),
      })
      setDomain('')
      setFallback('')
      onAdded()
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Adding the domain failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="border border-border rounded p-4 space-y-3">
      <div className="flex flex-wrap gap-3">
        <input aria-label="Domain" placeholder="go.example.com" value={domain} onChange={(e) => setDomain(e.target.value)} className={`flex-1 ${inputClass}`} />
        <select aria-label="DNS mode" value={mode} onChange={(e) => setMode(e.target.value as DNSMode)} className={inputClass}>
          <option value="external">External DNS</option>
          <option value="route53" disabled={zones.length === 0}>
            Route 53{zones.length === 0 ? ' (no hosted zones configured)' : ''}
          </option>
        </select>
        {mode === 'route53' && (
          <select aria-label="Hosted zone" value={zone || zones[0]} onChange={(e) => setZone(e.target.value)} className={inputClass}>
            {zones.map((z) => (
              <option key={z} value={z}>{z}</option>
            ))}
          </select>
        )}
        <input aria-label="Fallback URL" placeholder="https://fallback.example.com" value={fallback} onChange={(e) => setFallback(e.target.value)} className={`flex-2 ${inputClass}`} />
        <button onClick={submit} disabled={busy} className="bg-accent text-white rounded px-4 py-1.5 text-sm font-medium hover:bg-accent-light cursor-pointer disabled:opacity-50">
          {busy ? 'Adding…' : 'Add'}
        </button>
      </div>
      <p className="text-xs text-text-muted">
        The fallback URL is where the domain's root and unknown slugs redirect to. After adding, set the
        certificate validation record shown below; the domain is checked every five minutes, or use "Check now".
      </p>
      {error && <p className="text-xs text-red-600">{error}</p>}
    </div>
  )
}

export default function Domains() {
  const { isAdmin } = useAuth()
  const [domains, setDomains] = useState<DomainSetting[]>([])
  const [zones, setZones] = useState<string[]>([])
  const [loading, setLoading] = useState(true)
  const [showAdd, setShowAdd] = useState(false)
  const [error, setError] = useState('')

  const load = () => {
    api.getDomains()
      .then((data) => {
        setDomains(data.domains)
        setZones(data.route53_zone_ids)
        setError('')
      })
      .catch((err: unknown) => setError(err instanceof Error ? err.message : 'Loading failed'))
      .finally(() => setLoading(false))
  }

  useEffect(() => { load() }, [])

  if (loading) return <p className="text-text-muted text-sm">Loading...</p>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <h1 className="text-xl font-semibold tracking-tight text-text">Domains</h1>
        <button
          onClick={() => isAdmin && setShowAdd(!showAdd)}
          disabled={!isAdmin}
          className={`rounded px-3 py-1.5 text-sm font-medium ${isAdmin ? 'bg-accent text-white hover:bg-accent-light cursor-pointer' : 'bg-gray-200 text-gray-400 cursor-not-allowed'}`}
        >
          {showAdd ? 'Cancel' : 'Add domain'}
        </button>
      </div>

      {error && (
        <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2">{error}</div>
      )}

      {showAdd && isAdmin && <AddDomainForm zones={zones} onAdded={() => { setShowAdd(false); load() }} />}

      <div className="border border-border rounded divide-y divide-border">
        {domains.length === 0 ? (
          <p className="px-4 py-8 text-center text-sm text-text-muted">No domains configured.</p>
        ) : (
          domains.map((d) => <DomainRow key={d.domain} d={d} isAdmin={isAdmin} onChange={load} />)
        )}
      </div>
    </div>
  )
}
