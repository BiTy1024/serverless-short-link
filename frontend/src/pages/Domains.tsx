import { useEffect, useEffectEvent, useId, useState } from 'react'
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

// While ACM has not yet filled in a new certificate's validation record, the
// page asks for it every RECORD_POLL_MS for at most RECORD_POLL_LIMIT_MS.
// Pending domains are reloaded every STATUS_POLL_MS to pick up the scheduled
// check's progress.
const RECORD_POLL_MS = 5_000
const RECORD_POLL_LIMIT_MS = 120_000
const STATUS_POLL_MS = 60_000

// 16px text below sm keeps iOS from zooming in on focus.
const inputClass =
  'min-h-10 min-w-0 border border-border rounded px-3 py-2 text-base sm:text-sm focus:outline-none focus:border-accent'

// Text buttons with a 40px tap target.
const actionClass = 'inline-flex items-center min-h-10 px-2 text-sm'

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
    <button
      onClick={copy}
      className="shrink-0 inline-flex items-center min-h-10 sm:min-h-0 px-2 sm:py-0.5 font-sans text-xs text-text-muted hover:text-text cursor-pointer"
    >
      {copied ? 'Copied' : 'Copy'}
    </button>
  )
}

// CopyableValue keeps the copy button beside a value that breaks anywhere,
// so a long record never pushes the button out of reach.
function CopyableValue({ value }: { value: string }) {
  return (
    <div className="flex items-start gap-1">
      <span className="min-w-0 flex-1 break-all py-1 sm:py-0">{value}</span>
      <CopyButton value={value} />
    </div>
  )
}

// RecordTable is a table from sm on and a stack of records below it.
function RecordTable({ title, hint, records }: { title: string; hint: string; records: DNSRecord[] }) {
  if (records.length === 0) return null
  return (
    <div className="space-y-1">
      <p className="text-xs font-medium text-text">{title}</p>
      <p className="text-xs text-text-muted">{hint}</p>
      <table className="hidden sm:table w-full table-fixed text-xs font-mono">
        <thead>
          <tr className="text-left text-text-muted">
            <th className="w-16 py-1 pr-3 font-normal">Type</th>
            <th className="py-1 pr-3 font-normal">Name</th>
            <th className="py-1 font-normal">Value</th>
          </tr>
        </thead>
        <tbody>
          {records.map((r) => (
            <tr key={`${r.type} ${r.name}`} className="border-t border-border align-top">
              <td className="py-1 pr-3">{r.type}</td>
              <td className="py-1 pr-3"><CopyableValue value={r.name} /></td>
              <td className="py-1"><CopyableValue value={r.value} /></td>
            </tr>
          ))}
        </tbody>
      </table>
      <ul className="sm:hidden divide-y divide-border border-t border-border text-xs font-mono">
        {records.map((r) => (
          <li key={`${r.type} ${r.name}`} className="py-2 space-y-1">
            <p className="text-text-muted">{r.type}</p>
            <div>
              <p className="font-sans text-text-muted">Name</p>
              <CopyableValue value={r.name} />
            </div>
            <div>
              <p className="font-sans text-text-muted">Value</p>
              <CopyableValue value={r.value} />
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}

// DomainRecords shows the records a person still has to set. In route53 mode
// the service writes them itself. The records are collapsed by default; while
// a secondary domain is pending, the toggle says how many are still to set.
function DomainRecords({ d, polling }: { d: DomainSetting; polling: boolean }) {
  const [open, setOpen] = useState(false)
  const panelId = useId()
  if (d.dns_mode === 'route53') {
    return <p className="text-xs text-text-muted break-words">DNS records are written to Route 53 hosted zone {d.hosted_zone_id}.</p>
  }
  const awaitingRecord = d.status === 'pending_validation' && d.validation_records.length === 0
  const validationRecords = d.status === 'pending_validation' || d.role === 'primary' ? d.validation_records : []
  const recordCount = validationRecords.length + d.dns_records.length
  const pending = d.role === 'secondary' && (d.status === 'pending_validation' || d.status === 'pending_dns')
  return (
    <div className="space-y-3">
      {awaitingRecord && polling && (
        <p className="flex items-center gap-2 text-sm text-text">
          <span className="inline-block size-3.5 shrink-0 rounded-full border-2 border-accent border-t-transparent animate-spin" aria-hidden="true" />
          Requesting the certificate's DNS record…
        </p>
      )}
      {awaitingRecord && !polling && (
        <p className="text-sm text-text">The certificate's DNS record is not available yet. Use "Check now" to ask again.</p>
      )}
      {recordCount > 0 && (
        <>
          <button
            onClick={() => setOpen(!open)}
            aria-expanded={open}
            aria-controls={panelId}
            className="-ml-2 inline-flex flex-wrap items-center gap-x-2 min-h-10 px-2 text-sm text-text-muted hover:text-text cursor-pointer"
          >
            <span aria-hidden="true" className={`inline-block transition-transform ${open ? 'rotate-90' : ''}`}>&#9656;</span>
            <span>DNS records ({recordCount})</span>
            {pending && !open && (
              <span className="text-xs text-amber-800">
                {recordCount === 1 ? '1 record to set' : `${recordCount} records to set`}
              </span>
            )}
          </button>
          {open && (
            <div id={panelId} className="space-y-3">
              <RecordTable
                title="Certificate validation"
                hint="Set these CNAME records at your DNS host and keep them, so the certificate can renew."
                records={validationRecords}
              />
              <RecordTable
                title={d.role === 'primary' ? 'Service records' : 'Domain record'}
                hint="Point the domain at the service with these CNAME records. A domain apex cannot be a CNAME."
                records={d.dns_records}
              />
              <p className="text-xs text-text-muted">
                At Cloudflare and similar hosts, set each CNAME to "DNS only", not proxied.
              </p>
            </div>
          )}
        </>
      )}
    </div>
  )
}

function DomainRow({ d, isAdmin, onChange }: { d: DomainSetting; isAdmin: boolean; onChange: () => void }) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState('')
  const [editing, setEditing] = useState(false)
  const [fallback, setFallback] = useState(d.fallback_url)
  const secondary = d.role === 'secondary'
  const awaitingRecord = secondary && d.dns_mode === 'external' &&
    d.status === 'pending_validation' && d.validation_records.length === 0
  const [pollTimedOut, setPollTimedOut] = useState(false)

  // Only admins may run a check; everyone else reloads the list.
  const refreshRecord = useEffectEvent(() => {
    const action = isAdmin ? api.checkDomain(d.domain) : Promise.resolve()
    action.then(onChange, () => {})
  })

  useEffect(() => {
    if (!awaitingRecord) return
    const poll = setInterval(refreshRecord, RECORD_POLL_MS)
    const limit = setTimeout(() => {
      clearInterval(poll)
      setPollTimedOut(true)
    }, RECORD_POLL_LIMIT_MS)
    return () => {
      clearInterval(poll)
      clearTimeout(limit)
    }
  }, [awaitingRecord])

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
        <span className="min-w-0 font-mono text-sm text-text break-all">{d.domain}</span>
        <StatusBadge status={d.status} />
        <span className="text-xs text-text-muted">
          {d.role === 'primary' ? 'Primary' : 'Secondary'} · {d.dns_mode === 'route53' ? 'Route 53' : 'External DNS'}
        </span>
        {isAdmin && secondary && (
          <div className="ml-auto -mr-2 flex gap-1">
            {d.status !== 'active' && d.status !== 'failed' && (
              <button onClick={check} disabled={busy} className={`${actionClass} text-accent hover:underline cursor-pointer disabled:opacity-50`}>
                {busy ? 'Checking…' : 'Check now'}
              </button>
            )}
            <button onClick={remove} disabled={busy} className={`${actionClass} text-red-500 hover:text-red-700 cursor-pointer disabled:opacity-50`}>
              Delete
            </button>
          </div>
        )}
      </div>

      <div className="flex flex-wrap items-center gap-2 text-sm">
        <span className="text-text-muted">Fallback:</span>
        {editing ? (
          <>
            <input aria-label="Fallback URL" value={fallback} onChange={(e) => setFallback(e.target.value)} className={`w-full sm:w-auto sm:flex-1 ${inputClass}`} />
            <button onClick={saveFallback} disabled={busy} className={`${actionClass} text-accent hover:underline cursor-pointer`}>Save</button>
            <button onClick={() => { setEditing(false); setFallback(d.fallback_url) }} className={`${actionClass} text-text-muted hover:underline cursor-pointer`}>Cancel</button>
          </>
        ) : (
          <>
            <span className="min-w-0 break-all">{d.fallback_url}</span>
            {isAdmin && secondary && (
              <button onClick={() => setEditing(true)} className={`${actionClass} text-text-muted hover:text-text cursor-pointer`}>Edit</button>
            )}
            {!secondary && <span className="text-xs text-text-muted">(Terraform variable default_redirect_url)</span>}
          </>
        )}
      </div>

      {(d.error || error) && (
        <p className="text-xs text-red-600 break-words">{error || d.error}</p>
      )}
      {d.status !== 'failed' && <DomainRecords d={d} polling={!pollTimedOut} />}
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
      <div className="flex flex-col sm:flex-row sm:flex-wrap gap-3">
        <input aria-label="Domain" placeholder="go.example.com" value={domain} onChange={(e) => setDomain(e.target.value)} className={`w-full sm:w-auto sm:flex-1 sm:min-w-40 ${inputClass}`} />
        <select aria-label="DNS mode" value={mode} onChange={(e) => setMode(e.target.value as DNSMode)} className={`w-full sm:w-auto ${inputClass}`}>
          <option value="external">External DNS</option>
          <option value="route53" disabled={zones.length === 0}>
            Route 53{zones.length === 0 ? ' (no hosted zones configured)' : ''}
          </option>
        </select>
        {mode === 'route53' && (
          <select aria-label="Hosted zone" value={zone || zones[0]} onChange={(e) => setZone(e.target.value)} className={`w-full sm:w-auto ${inputClass}`}>
            {zones.map((z) => (
              <option key={z} value={z}>{z}</option>
            ))}
          </select>
        )}
        <input aria-label="Fallback URL" placeholder="https://fallback.example.com" value={fallback} onChange={(e) => setFallback(e.target.value)} className={`w-full sm:w-auto sm:flex-2 sm:min-w-48 ${inputClass}`} />
        <button onClick={submit} disabled={busy} className="min-h-10 bg-accent text-white rounded px-4 text-sm font-medium hover:bg-accent-light cursor-pointer disabled:opacity-50">
          {busy ? 'Adding…' : 'Add'}
        </button>
      </div>
      <p className="text-xs text-text-muted">
        The fallback URL is where the domain's root and unknown slugs redirect to. After adding, set the
        certificate validation record shown below; the domain is checked every five minutes, or use "Check now".
      </p>
      {error && <p className="text-xs text-red-600 break-words">{error}</p>}
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

  const anyPending = domains.some((d) =>
    d.role === 'secondary' && (d.status === 'pending_validation' || d.status === 'pending_dns'))
  const reload = useEffectEvent(load)
  useEffect(() => {
    if (!anyPending) return
    const poll = setInterval(reload, STATUS_POLL_MS)
    return () => clearInterval(poll)
  }, [anyPending])

  if (loading) return <p className="text-text-muted text-sm">Loading...</p>

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-3">
        <h1 className="text-xl font-semibold tracking-tight text-text">Domains</h1>
        <button
          onClick={() => isAdmin && setShowAdd(!showAdd)}
          disabled={!isAdmin}
          className={`min-h-10 shrink-0 rounded px-4 text-sm font-medium ${isAdmin ? 'bg-accent text-white hover:bg-accent-light cursor-pointer' : 'bg-gray-200 text-gray-400 cursor-not-allowed'}`}
        >
          {showAdd ? 'Cancel' : 'Add domain'}
        </button>
      </div>

      {error && (
        <div className="text-sm text-red-600 bg-red-50 border border-red-200 rounded px-3 py-2 break-words">{error}</div>
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
