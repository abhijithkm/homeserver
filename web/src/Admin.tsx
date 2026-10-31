import { useEffect, useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import type { Settings } from './types'
import { joinBytes, splitBytes } from './format'
import type { ByteUnit } from './format'
import { ErrorText, Notice, Spinner } from './ui'
import { useLoad } from './useLoad'
import { SharesAdmin, UsersAdmin } from './AdminManage'
import { DiagnosticsAdmin } from './AdminDiag'
import { Dashboard, RestartControl } from './AdminDashboard'

const TABS: [string, string][] = [
  ['dashboard', 'Dashboard'],
  ['shares', 'Shares'],
  ['users', 'Users'],
  ['settings', 'Settings'],
  ['diagnostics', 'Diagnostics'],
]

export function AdminPage({ tab, tls, currentUserId }: { tab: string; tls: boolean; currentUserId: number }) {
  const current = TABS.some(([k]) => k === tab) ? tab : 'dashboard'
  return (
    <section aria-labelledby="admin-h">
      <h1 id="admin-h">{TABS.find(([k]) => k === current)?.[1] ?? 'Administration'}</h1>
      <nav aria-label="Admin sections" className="tabs">
        {TABS.map(([k, label]) => (
          <a key={k} href={`#/admin/${k}`} aria-current={current === k ? 'page' : undefined}>
            {label}
          </a>
        ))}
      </nav>
      {current === 'dashboard' && <Dashboard />}
      {current === 'shares' && <SharesAdmin />}
      {current === 'users' && <UsersAdmin currentUserId={currentUserId} />}
      {current === 'settings' && <SettingsAdmin tls={tls} />}
      {current === 'diagnostics' && <DiagnosticsAdmin />}
    </section>
  )
}

function SettingsAdmin({ tls }: { tls: boolean }) {
  const load = useLoad<Settings>(api.getSettings)
  const [form, setForm] = useState<Settings | null>(null)
  const [maxVal, setMaxVal] = useState('0')
  const [maxUnit, setMaxUnit] = useState<ByteUnit>('GB')
  const [error, setError] = useState('')
  const [saved, setSaved] = useState(false)
  const [restartNeeded, setRestartNeeded] = useState(false)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (load.data) {
      setForm(load.data)
      const b = splitBytes(load.data.maxUploadBytes)
      setMaxVal(b.value)
      setMaxUnit(b.unit)
    }
  }, [load.data])

  if (load.error) return <ErrorText>{load.error}</ErrorText>
  if (!form) return <Spinner />

  const set = <K extends keyof Settings>(k: K, v: Settings[K]) => {
    setSaved(false)
    setForm({ ...form, [k]: v })
  }
  const num = (k: 'port' | 'maxTransfers' | 'sessionHours', id: string) => (
    <input
      id={id}
      type="number"
      min={1}
      value={Number.isNaN(form[k]) ? '' : form[k]}
      onChange={(e) => set(k, e.target.value === '' ? NaN : Number(e.target.value))}
    />
  )

  const submit = async (e: FormEvent) => {
    e.preventDefault()
    const bytes = joinBytes(maxVal, maxUnit)
    if (Number.isNaN(bytes)) {
      setError('Maximum upload size must be a number, 0 or more.')
      return
    }
    setBusy(true)
    setError('')
    try {
      const r = await api.saveSettings({ ...form, maxUploadBytes: bytes })
      setForm(r.settings)
      setSaved(true)
      setRestartNeeded(r.restartRequired)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Could not save settings.')
    }
    setBusy(false)
  }

  return (
    <form className="card" onSubmit={submit} aria-labelledby="set-h">
      <h2 id="set-h">Settings</h2>
      <div className="grid">
        <div>
          <label htmlFor="s-port">Port</label>
          {num('port', 's-port')}
          <p className="hint">The number after the colon in the address. Default 8686.</p>
        </div>
        <div>
          <label htmlFor="s-bind">Listen address</label>
          <input id="s-bind" value={form.bindAddr} onChange={(e) => set('bindAddr', e.target.value)} />
          <p className="hint">0.0.0.0 lets other devices connect. 127.0.0.1 allows this computer only.</p>
        </div>
        <div>
          <label htmlFor="s-max">Maximum upload size</label>
          <div className="inline">
            <input id="s-max" type="number" min={0} step="any" value={maxVal} onChange={(e) => { setSaved(false); setMaxVal(e.target.value) }} />
            <select aria-label="Upload size unit" value={maxUnit} onChange={(e) => { setSaved(false); setMaxUnit(e.target.value as ByteUnit) }}>
              <option>KB</option>
              <option>MB</option>
              <option>GB</option>
            </select>
          </div>
          <p className="hint">Per file. 0 means no limit. Applies immediately.</p>
        </div>
        <div>
          <label htmlFor="s-tr">Simultaneous transfers</label>
          {num('maxTransfers', 's-tr')}
          <p className="hint">1 to 256. Needs a restart.</p>
        </div>
        <div>
          <label htmlFor="s-sess">Stay signed in for (hours)</label>
          {num('sessionHours', 's-sess')}
          <p className="hint">Applies to new sign-ins.</p>
        </div>
        <div>
          <label htmlFor="s-tls">Encryption (HTTPS)</label>
          <select id="s-tls" value={form.tlsMode} onChange={(e) => set('tlsMode', e.target.value as Settings['tlsMode'])}>
            <option value="off">Off (plain HTTP)</option>
            <option value="selfsigned">Self-signed certificate</option>
            <option value="custom">My own certificate</option>
          </select>
        </div>
        <div>
          <label htmlFor="s-upd">Software updates</label>
          <label className="check">
            <input id="s-upd" type="checkbox" checked={form.updateCheck} onChange={(e) => set('updateCheck', e.target.checked)} />
            Check GitHub for new versions
          </label>
          <label className="check">
            <input type="checkbox" checked={form.updatePrerelease} disabled={!form.updateCheck} onChange={(e) => set('updatePrerelease', e.target.checked)} />
            Include pre-releases
          </label>
          <p className="hint">Only looks; it never installs by itself. This sends a request to github.com from the server computer.</p>
        </div>
      </div>
      {form.tlsMode === 'custom' && (
        <div className="grid">
          <div>
            <label htmlFor="s-cert">Certificate file path</label>
            <input id="s-cert" value={form.tlsCert} onChange={(e) => set('tlsCert', e.target.value)} />
          </div>
          <div>
            <label htmlFor="s-key">Private key file path</label>
            <input id="s-key" value={form.tlsKey} onChange={(e) => set('tlsKey', e.target.value)} />
          </div>
        </div>
      )}
      <Notice kind="info">
        <strong>HTTP vs HTTPS.</strong> Plain HTTP (the default) sends passwords and files unencrypted, so anyone on your Wi-Fi could read them. On a trusted home network this is a common trade-off.
        HTTPS encrypts traffic. A self-signed certificate gives encryption, but every browser will show a warning you must accept once per device, and it does not prove the server&apos;s identity. A
        certificate from a real authority avoids the warning but needs a domain name. Currently the server is running with {tls ? 'HTTPS' : 'plain HTTP'}.
      </Notice>
      <ErrorText>{error}</ErrorText>
      {saved ? <Notice kind="ok">Settings saved.</Notice> : null}
      {saved && restartNeeded ? (
        <Notice kind="warn">
          Restart required: some changes (port, listen address, encryption or simultaneous transfers) only take effect after the server restarts. After a port change, open the new address.
        </Notice>
      ) : null}
      <div className="form-actions">
        <button type="submit" className="btn btn-primary" disabled={busy}>
          {busy ? 'Saving...' : 'Save settings'}
        </button>
      </div>
      {saved && restartNeeded ? <RestartControl /> : null}
    </form>
  )
}
