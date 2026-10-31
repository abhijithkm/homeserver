import { useEffect, useRef, useState } from 'react'
import type { ReactNode } from 'react'
import * as api from './api'
import type { AuditEvent, Diagnose, NetworkInfo, Status, TransfersResponse, UpdateResult, User } from './types'
import { formatBytes, formatDate, formatUnix, formatUptime } from './format'
import { ConfirmDialog, CopyButton, Empty, ErrorText, Led, Notice, Spinner } from './ui'
import { useLoad } from './useLoad'

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

type LampState = 'ok' | 'warn' | 'bad' | 'info' | 'off'
interface Lamp {
  key: string
  label: string
  state: LampState
  text: string
}

function Meter({ pct, label, tone: forced }: { pct: number; label: string; tone?: 'ok' | 'warn' | 'bad' }) {
  const p = Math.max(0, Math.min(100, Math.round(pct)))
  const tone = forced ?? (p >= 90 ? 'bad' : p >= 75 ? 'warn' : 'ok')
  return (
    <div className={`meter meter-${tone}`} role="img" aria-label={`${label}: ${p}%`}>
      <span style={{ width: `${p}%` }} />
    </div>
  )
}

function Tile({ label, value, sub, meter }: { label: string; value: string; sub?: string; meter?: ReactNode }) {
  return (
    <div className="tile">
      <p className="tile-label">{label}</p>
      <p className="tile-value">{value}</p>
      {meter}
      {sub ? <p className="tile-sub">{sub}</p> : null}
    </div>
  )
}

const REFRESH_MS = 10_000

export function Dashboard() {
  const status = useLoad<Status>(api.getStatus)
  const net = useLoad<NetworkInfo>(api.getNetwork)
  const transfers = useLoad<TransfersResponse>(api.getTransfers)
  const update = useLoad<UpdateResult>(api.getUpdate)
  const diag = useLoad<Diagnose>(api.getDiagnose)
  const users = useLoad<User[]>(api.listUsers)
  const audit = useLoad<AuditEvent[]>(api.getAudit)
  const [sel, setSel] = useState(0)
  const [stamp, setStamp] = useState('')
  const tick = useRef(0)

  // Live view: status and transfers every 10 s, slower data every 30 s, only while the tab is visible.
  const reloadStatus = status.reload
  const reloadTransfers = transfers.reload
  const reloadDiag = diag.reload
  const reloadAudit = audit.reload
  const reloadNet = net.reload
  useEffect(() => {
    const id = window.setInterval(() => {
      if (document.visibilityState !== 'visible') return
      tick.current++
      reloadStatus()
      reloadTransfers()
      if (tick.current % 3 === 0) {
        reloadDiag()
        reloadAudit()
        reloadNet()
      }
    }, REFRESH_MS)
    return () => window.clearInterval(id)
  }, [reloadStatus, reloadTransfers, reloadDiag, reloadAudit, reloadNet])

  useEffect(() => {
    if (status.data) setStamp(new Date().toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' }))
  }, [status.data])

  const refreshAll = () => {
    status.reload()
    net.reload()
    transfers.reload()
    update.reload()
    diag.reload()
    users.reload()
    audit.reload()
  }

  const s = status.data
  const d = diag.data
  const urls = net.data?.urls ?? []
  const noNetwork = !!net.data && ((net.data.addresses ?? []).length === 0 || urls.length === 0)
  const up = update.data?.enabled ? update.data.info : undefined
  const recent = transfers.data?.recent ?? []
  const accounts = users.data ?? []
  const measured = (d?.shares ?? []).filter((x) => (x.totalBytes ?? 0) > 0)
  const lowDisk = (d?.shares ?? []).filter((x) => x.lowDisk)

  const lamps: Lamp[] = [
    status.error
      ? { key: 'server', label: 'Server', state: 'bad', text: 'Not responding' }
      : { key: 'server', label: 'Server', state: s ? 'ok' : 'off', text: s ? 'Running' : 'Checking' },
    d ? { key: 'db', label: 'Database', state: d.database ? 'ok' : 'bad', text: d.database ? 'Working' : 'Not responding' } : { key: 'db', label: 'Database', state: 'off', text: diag.error ? 'Unknown' : 'Checking' },
    net.data
      ? noNetwork
        ? { key: 'net', label: 'Network', state: 'bad', text: 'No connection' }
        : { key: 'net', label: 'Network', state: 'ok', text: `${urls.length} ${urls.length === 1 ? 'address' : 'addresses'}` }
      : { key: 'net', label: 'Network', state: 'off', text: net.error ? 'Unknown' : 'Checking' },
    s
      ? s.shares === 0
        ? { key: 'shares', label: 'Shares', state: 'info', text: 'None yet' }
        : s.sharesUnavailable > 0
          ? { key: 'shares', label: 'Shares', state: 'warn', text: `${s.shares - s.sharesUnavailable} of ${s.shares} online` }
          : { key: 'shares', label: 'Shares', state: 'ok', text: `${s.shares} online` }
      : { key: 'shares', label: 'Shares', state: 'off', text: 'Checking' },
    d
      ? lowDisk.length > 0
        ? { key: 'disk', label: 'Disk space', state: 'warn', text: `Low on ${lowDisk[0].name}` }
        : measured.length > 0
          ? { key: 'disk', label: 'Disk space', state: 'ok', text: 'Enough free' }
          : { key: 'disk', label: 'Disk space', state: 'off', text: 'Not measured' }
      : { key: 'disk', label: 'Disk space', state: 'off', text: diag.error ? 'Unknown' : 'Checking' },
    net.data
      ? net.data.tls
        ? { key: 'tls', label: 'Security', state: 'ok', text: 'HTTPS on' }
        : { key: 'tls', label: 'Security', state: 'info', text: 'HTTP, not encrypted' }
      : { key: 'tls', label: 'Security', state: 'off', text: 'Checking' },
    update.data
      ? update.data.enabled
        ? up?.available
          ? { key: 'upd', label: 'Updates', state: 'info', text: `${up.latest} available` }
          : { key: 'upd', label: 'Updates', state: 'ok', text: 'Up to date' }
        : { key: 'upd', label: 'Updates', state: 'off', text: 'Checks turned off' }
      : { key: 'upd', label: 'Updates', state: 'off', text: update.error ? 'Unknown' : 'Checking' },
  ]
  const bad = lamps.filter((l) => l.state === 'bad').length
  const warn = lamps.filter((l) => l.state === 'warn').length
  const attention = bad + warn
  const tone: LampState = status.error || bad > 0 ? 'bad' : warn > 0 ? 'warn' : s ? 'ok' : 'off'
  const headline = status.error
    ? 'HomeServer is not responding'
    : !s
      ? 'Checking the server...'
      : attention === 0
        ? 'Everything is running'
        : `${attention} ${attention === 1 ? 'thing needs' : 'things need'} attention`

  const ups = recent.filter((t) => t.direction === 'upload').length
  const downs = recent.length - ups
  const moved = recent.reduce((n, t) => n + t.bytes, 0)
  const failed = recent.filter((t) => t.status !== 'ok').length
  const events = (audit.data ?? []).slice(0, 6)

  return (
    <div className="dash">
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

      <section className={`hero hero-${tone}`} aria-labelledby="dash-h">
        <div className="hero-top">
          <div className="hero-main">
            <span className={`led led-lg led-${tone}`} aria-hidden="true" />
            <div>
              <h2 id="dash-h">{headline}</h2>
              <p className="hero-sub">{s ? `HomeServer ${s.version} on ${s.os}` : status.error || ' '}</p>
            </div>
          </div>
          <div className="hero-actions">
            {stamp ? <span className="hint">Updated {stamp}</span> : null}
            <button type="button" className="btn btn-sm" onClick={refreshAll}>
              Refresh
            </button>
          </div>
        </div>
        <ErrorText>{status.error}</ErrorText>
        <ul className="lamps" aria-label="System checks">
          {lamps.map((l) => (
            <li key={l.key} className="lamp">
              <Led status={l.state} />
              <span className="lamp-label">{l.label}</span>
              <span className="lamp-text">{l.text}</span>
            </li>
          ))}
        </ul>
      </section>

      {status.loading && !s ? <Spinner label="Reading server status..." /> : null}
      {s ? (
        <div className="tiles">
          <Tile label="Uptime" value={formatUptime(s.uptimeSeconds)} sub={`Since ${formatDate(Date.now() - s.uptimeSeconds * 1000)}`} />
          <Tile
            label="Transfers now"
            value={`${s.activeTransfers} of ${s.maxTransfers}`}
            sub="download and upload slots in use"
            meter={<Meter pct={s.maxTransfers > 0 ? (s.activeTransfers / s.maxTransfers) * 100 : 0} label="Transfer slots in use" />}
          />
          <Tile
            label="Memory"
            value={formatBytes(s.heapBytes)}
            sub={`of ${formatBytes(s.sysBytes)} reserved`}
            meter={<Meter pct={s.sysBytes > 0 ? (s.heapBytes / s.sysBytes) * 100 : 0} label="Memory in use" />}
          />
          <Tile label="Shared folders" value={String(s.shares)} sub="being served" />
          {accounts.length > 0 ? (
            <Tile
              label="Accounts"
              value={String(accounts.length)}
              sub={`${accounts.filter((a) => a.role === 'admin').length} admin, ${accounts.filter((a) => a.disabled).length} turned off`}
            />
          ) : null}
        </div>
      ) : null}

      <div className="cols">
        <div className="card">
          <h2>Storage</h2>
          {diag.loading && !d ? <Spinner label="Measuring disks..." /> : null}
          {d && measured.length === 0 ? <Empty>No disk space readings yet. Add a share to see free space.</Empty> : null}
          {measured.length > 0 ? (
            <ul className="disks" aria-label="Disk space by share">
              {measured.map((x) => {
                const total = x.totalBytes ?? 0
                const free = x.freeBytes ?? 0
                return (
                  <li key={x.name} className="disk">
                    <div className="disk-head">
                      <span className="disk-name">
                        <Led status={x.status === 'online' ? 'ok' : 'bad'} /> {x.name}
                      </span>
                      <span className="disk-free">
                        {formatBytes(free)} free of {formatBytes(total)}
                      </span>
                    </div>
                    <Meter pct={((total - free) / total) * 100} label={`${x.name} disk used`} tone={x.lowDisk ? 'bad' : (total - free) / total >= 0.9 ? 'warn' : 'ok'} />
                    {x.lowDisk ? (
                      <p className="problem" role="status">
                        Low disk space: uploads may fail soon.
                      </p>
                    ) : null}
                  </li>
                )
              })}
            </ul>
          ) : null}
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
        {transfers.data && recent.length === 0 ? <Empty>No transfers yet.</Empty> : null}
        {recent.length > 0 ? (
          <>
            <ul className="chips" aria-label="Transfer summary">
              <li>{ups === 1 ? '1 upload' : `${ups} uploads`}</li>
              <li>{downs === 1 ? '1 download' : `${downs} downloads`}</li>
              <li>{formatBytes(moved)} moved</li>
              {failed > 0 ? <li className="chip-warn">{failed} did not finish</li> : null}
            </ul>
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
                  {recent.map((t, i) => (
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
          </>
        ) : null}
      </div>

      <div className="cols">
        <div className="card">
          <div className="card-head">
            <h2>Recent events</h2>
            <a className="btn btn-sm" href="#/admin/diagnostics">
              All events
            </a>
          </div>
          {audit.loading && !audit.data ? <Spinner /> : null}
          {audit.error ? <p className="hint">Events could not be loaded.</p> : null}
          {audit.data && events.length === 0 ? <Empty>No events recorded yet.</Empty> : null}
          {events.length > 0 ? (
            <ul className="feed" aria-label="Recent events">
              {events.map((e, i) => (
                <li key={i}>
                  <time className="feed-time">{formatUnix(e.ts)}</time>
                  <span className="feed-what">
                    <strong>{e.actor || 'system'}</strong> {e.event}
                    {e.detail ? <span className="feed-detail"> {e.detail}</span> : null}
                  </span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>

        <div className="card">
          <h2>Server</h2>
          {s ? (
            <dl className="kv">
              <dt>Version</dt>
              <dd>{s.version}</dd>
              <dt>System</dt>
              <dd>{s.os}</dd>
              <dt>Process</dt>
              <dd>{s.pid}</dd>
              <dt>Goroutines</dt>
              <dd>{s.goroutines}</dd>
              <dt>Listening on</dt>
              <dd>
                {s.listening || '-'} {s.tls ? '(HTTPS)' : '(HTTP)'}
              </dd>
              {d ? (
                <>
                  <dt>Data folder</dt>
                  <dd>
                    <code>{d.dataDir}</code>
                  </dd>
                </>
              ) : null}
            </dl>
          ) : null}
          <div className="server-actions">
            <RestartControl onBack={status.reload} />
            <a className="btn" href={api.backupUrl} download>
              Download backup
            </a>
          </div>
        </div>
      </div>
    </div>
  )
}
