import { useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import type { NetworkInfo, Settings, Status, TransfersResponse, UpdateResult } from './types'
import { formatBytes, formatUnix, formatUptime, joinBytes, splitBytes } from './format'
import type { ByteUnit } from './format'
import { ConfirmDialog, CopyButton, Empty, ErrorText, Notice, Spinner } from './ui'
import { useLoad } from './useLoad'
import { SharesAdmin, UsersAdmin } from './AdminManage'
import { DiagnosticsAdmin } from './AdminDiag'

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
      <h1 id="admin-h">Administration</h1>
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

/** Restart the server and wait until /healthz answers again. */
export function RestartControl({ onBack }: { onBack?: () => void }) {
  const [confirm, setConfirm] = useState(false)
  const [phase, setPhase] = useState<'idle' | 'sending' | 'waiting' | 'back' | 'timeout'>('idle')
  const [error, setError] = useState('')
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])

  const go = async () => {
    setError('')
    setPhase('sending')
    try {
      await api.restartServer()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Could not restart.')
      setPhase('idle')
      return
    }
    setConfirm(false)
    setPhase('waiting')
    await new Promise((r) => setTimeout(r, 1500))
    for (let i = 0; i < 60 && alive.current; i++) {
      if (await api.healthz()) {
        if (alive.current) {
          setPhase('back')
          onBack?.()
        }
        return
      }
      await new Promise((r) => setTimeout(r, 1000))
    }
    if (alive.current) setPhase('timeout')
  }

  return (
    <div className="restart">
      <button type="button" className="btn" onClick={() => setConfirm(true)} disabled={phase === 'waiting'}>
        Restart server
      </button>
      {phase === 'waiting' && <Spinner label="Restarting. Waiting for the server to answer..." />}
      {phase === 'back' && <Notice kind="ok">The server is back.</Notice>}
      {phase === 'timeout' && <Notice kind="error">The server did not answer within a minute. Check the server computer.</Notice>}
      {confirm && (
        <ConfirmDialog
          title="Restart HomeServer?"
          confirmLabel="Restart"
          busy={phase === 'sending'}
          error={error}
          onCancel={() => setConfirm(false)}
          onConfirm={() => void go()}
          message={<p>Transfers in progress will be interrupted (uploads must be retried). Everyone stays signed in. This page waits until the server is back.</p>}
        />
      )}
    </div>
  )
}

function Dashboard() {
  const status = useLoad<Status>(api.getStatus)
  const net = useLoad<NetworkInfo>(api.getNetwork)
  const transfers = useLoad<TransfersResponse>(api.getTransfers)
  const update = useLoad<UpdateResult>(api.getUpdate)
  const [sel, setSel] = useState(0)
  const s = status.data
  const urls = net.data?.urls ?? []
  const noNetwork = !!net.data && (net.data.addresses ?? []).length === 0
  const up = update.data?.enabled ? update.data.info : undefined

  return (
    <div>
      {up?.available ? (
        <Notice kind="info">
          <strong>HomeServer {up.latest}</strong> {up.prerelease ? '(pre-release) ' : ''}is available; this server runs {up.current}. To install it, open an
          administrator terminal on the server computer and run <code>homeserver update</code>, or use the tray icon and choose &quot;Check for updates...&quot;.
          The download is verified against the release checksum before it runs.{' '}
          {up.pageUrl ? (
            <a href={up.pageUrl} target="_blank" rel="noreferrer noopener">
              Release notes
            </a>
          ) : null}
        </Notice>
      ) : null}
      {s && s.sharesUnavailable > 0 ? (
        <Notice kind="warn">
          {s.sharesUnavailable} shared {s.sharesUnavailable === 1 ? 'folder is' : 'folders are'} offline or not accessible. <a href="#/admin/shares">Check shares</a>
        </Notice>
      ) : null}

      <div className="card">
        <div className="card-head">
          <h2>Server status</h2>
          <button type="button" className="btn btn-sm" onClick={status.reload}>
            Refresh
          </button>
        </div>
        <ErrorText>{status.error}</ErrorText>
        {status.loading && !s ? <Spinner /> : null}
        {s ? (
          <dl className="kv">
            <dt>Version</dt>
            <dd>{s.version}</dd>
            <dt>Uptime</dt>
            <dd>{formatUptime(s.uptimeSeconds)}</dd>
            <dt>Memory</dt>
            <dd>
              {formatBytes(s.heapBytes)} in use ({formatBytes(s.sysBytes)} reserved)
            </dd>
            <dt>Goroutines</dt>
            <dd>{s.goroutines}</dd>
            <dt>Active transfers</dt>
            <dd>
              {s.activeTransfers} of {s.maxTransfers}
            </dd>
            <dt>Listening on</dt>
            <dd>
              {s.listening || '-'} {s.tls ? '(HTTPS)' : '(HTTP)'}
            </dd>
          </dl>
        ) : null}
        <RestartControl onBack={status.reload} />
      </div>

      <div className="card">
        <h2>Open HomeServer from other devices</h2>
        <ErrorText>{net.error}</ErrorText>
        {net.loading && !net.data ? <Spinner /> : null}
        {noNetwork || (net.data && urls.length === 0) ? (
          <Notice kind="warn">No active network connection was found. Connect this computer to Wi-Fi or Ethernet so other devices can reach it.</Notice>
        ) : null}
        {urls.length > 0 && (
          <div className="net">
            <ul className="urls" aria-label="Server addresses">
              {urls.map((u, i) => (
                <li key={u}>
                  <label className="radio">
                    <input type="radio" name="qr-url" checked={sel === i} onChange={() => setSel(i)} />
                    <code>{u}</code>
                  </label>
                  <CopyButton text={u} />
                </li>
              ))}
            </ul>
            <figure className="qr">
              <img src={api.qrUrl(sel)} width={200} height={200} alt={`QR code for ${urls[sel] ?? ''}`} />
              <figcaption>Scan with a phone on the same Wi-Fi.</figcaption>
            </figure>
          </div>
        )}
        {net.data && !net.data.tls ? (
          <Notice kind="info">
            This address uses plain HTTP, which is not encrypted. Anyone with access to your Wi-Fi network could read passwords and files in transit. That is acceptable on a trusted home
            network; see Settings to enable HTTPS.
          </Notice>
        ) : null}
      </div>

      <div className="card">
        <div className="card-head">
          <h2>Recent transfers</h2>
          <button type="button" className="btn btn-sm" onClick={transfers.reload}>
            Refresh
          </button>
        </div>
        <ErrorText>{transfers.error}</ErrorText>
        {transfers.loading && !transfers.data ? <Spinner /> : null}
        {transfers.data && (transfers.data.recent ?? []).length === 0 ? <Empty>No transfers yet.</Empty> : null}
        {transfers.data && (transfers.data.recent ?? []).length > 0 ? (
          <div className="table-wrap">
            <table className="data">
              <caption className="sr-only">Recent transfers</caption>
              <thead>
                <tr>
                  <th scope="col">When</th>
                  <th scope="col">User</th>
                  <th scope="col">Direction</th>
                  <th scope="col">File</th>
                  <th scope="col">Size</th>
                  <th scope="col">Result</th>
                </tr>
              </thead>
              <tbody>
                {(transfers.data.recent ?? []).map((t, i) => (
                  <tr key={i}>
                    <td>{formatUnix(t.ts)}</td>
                    <td>{t.user}</td>
                    <td>{t.direction}</td>
                    <td className="wrap">
                      {t.share}/{t.path}
                    </td>
                    <td>{formatBytes(t.bytes)}</td>
                    <td>
                      <span className={`badge badge-${t.status === 'ok' ? 'online' : t.status === 'error' ? 'offline' : 'disabled'}`}>{t.status}</span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        ) : null}
      </div>
    </div>
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
