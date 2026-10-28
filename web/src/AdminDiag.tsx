import * as api from './api'
import type { AuditEvent, Diagnose } from './types'
import { formatBytes, formatUnix } from './format'
import { CopyButton, Empty, ErrorText, Notice, Spinner } from './ui'
import { useLoad } from './useLoad'

const isCommand = (h: string) => /^netsh\s/i.test(h)

export function DiagnosticsAdmin() {
  const diag = useLoad<Diagnose>(api.getDiagnose)
  const logs = useLoad(() => api.getLogs(200))
  const audit = useLoad<AuditEvent[]>(api.getAudit)
  const d = diag.data

  return (
    <div>
      <div className="card">
        <div className="card-head">
          <h2>Diagnostics</h2>
          <button type="button" className="btn btn-sm" onClick={diag.reload}>
            Run again
          </button>
        </div>
        <ErrorText>{diag.error}</ErrorText>
        {diag.loading && !d ? <Spinner label="Running checks..." /> : null}
        {d ? (
          <>
            <dl className="kv">
              <dt>Database</dt>
              <dd>{d.database ? 'OK' : 'Problem: the configuration database did not respond'}</dd>
              <dt>Listening on</dt>
              <dd>
                {d.listening || '-'} {d.tls ? '(HTTPS)' : '(HTTP)'}
              </dd>
              <dt>Data folder</dt>
              <dd>
                <code>{d.dataDir}</code>
              </dd>
            </dl>
            {!d.database ? <Notice kind="error">The configuration database is not responding. Check the log below.</Notice> : null}
            <h3>Shares</h3>
            {d.shares.length === 0 ? (
              <Empty>No shares configured.</Empty>
            ) : (
              <div className="table-wrap">
                <table className="data">
                  <caption className="sr-only">Share health and disk space</caption>
                  <thead>
                    <tr>
                      <th scope="col">Share</th>
                      <th scope="col">Status</th>
                      <th scope="col">Disk free</th>
                    </tr>
                  </thead>
                  <tbody>
                    {d.shares.map((s) => (
                      <tr key={s.name}>
                        <td>{s.name}</td>
                        <td>
                          <span className={`badge badge-${s.status}`}>{s.status}</span>
                          {s.problem ? <div className="problem">{s.problem}</div> : null}
                        </td>
                        <td>
                          {s.totalBytes ? `${formatBytes(s.freeBytes ?? 0)} of ${formatBytes(s.totalBytes)}` : '-'}
                          {s.lowDisk ? (
                            <div className="problem" role="status">
                              Low disk space: uploads may fail soon.
                            </div>
                          ) : null}
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
            <h3>If other devices cannot connect</h3>
            <ul className="hints">
              {d.hints.map((h, i) =>
                isCommand(h) ? (
                  <li key={i} className="cmd">
                    <pre aria-label="Firewall command">{h}</pre>
                    <CopyButton text={h} label="Copy command" />
                  </li>
                ) : (
                  <li key={i}>{h}</li>
                ),
              )}
            </ul>
          </>
        ) : null}
      </div>

      <div className="card">
        <h2>Backup</h2>
        <p>Download a copy of HomeServer&apos;s settings, users and share list. It contains password hashes, so keep it private. It does not include your shared files.</p>
        <a className="btn btn-primary" href={api.backupUrl} download>
          Download backup
        </a>
      </div>

      <div className="card">
        <div className="card-head">
          <h2>Server log (last 200 lines)</h2>
          <button type="button" className="btn btn-sm" onClick={logs.reload}>
            Refresh
          </button>
        </div>
        <ErrorText>{logs.error}</ErrorText>
        {logs.loading && !logs.data ? <Spinner /> : null}
        {logs.data && (logs.data.lines ?? []).length === 0 ? <Empty>The log is empty.</Empty> : null}
        {logs.data && (logs.data.lines ?? []).length > 0 ? (
          <pre className="log" tabIndex={0} aria-label="Server log">
            {(logs.data.lines ?? []).join('\n')}
          </pre>
        ) : null}
      </div>

      <div className="card">
        <div className="card-head">
          <h2>Audit log</h2>
          <button type="button" className="btn btn-sm" onClick={audit.reload}>
            Refresh
          </button>
        </div>
        <ErrorText>{audit.error}</ErrorText>
        {audit.loading && !audit.data ? <Spinner /> : null}
        {audit.data && audit.data.length === 0 ? <Empty>No events recorded yet.</Empty> : null}
        {audit.data && audit.data.length > 0 ? (
          <div className="table-wrap">
            <table className="data">
              <caption className="sr-only">Audit events, newest first</caption>
              <thead>
                <tr>
                  <th scope="col">When</th>
                  <th scope="col">Who</th>
                  <th scope="col">Address</th>
                  <th scope="col">Event</th>
                  <th scope="col">Detail</th>
                </tr>
              </thead>
              <tbody>
                {audit.data.map((e, i) => (
                  <tr key={i}>
                    <td>{formatUnix(e.ts)}</td>
                    <td>{e.actor}</td>
                    <td>{e.ip}</td>
                    <td>{e.event}</td>
                    <td className="wrap">{e.detail}</td>
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
