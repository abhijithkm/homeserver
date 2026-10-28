import { useCallback, useEffect, useRef, useState } from 'react'
import type { FormEvent } from 'react'
import * as api from './api'
import type { Entry, Share, SortKey, User } from './types'
import type { Route } from './router'
import { filesHash } from './router'
import { canPreview, formatBytes, formatDate, joinPath, parentPath } from './format'
import { ConfirmDialog, Dialog, Empty, ErrorText, Notice, PromptDialog, Spinner } from './ui'
import { UploadPanel, useUploads } from './Uploads'

const PAGE = 100

export function shareProblemText(s: Share): string {
  if (s.problem) return s.problem
  if (s.status === 'offline') return 'This share is offline: its folder is missing or the drive is disconnected.'
  if (s.status === 'denied') return 'The server cannot read this share\'s folder.'
  if (s.status === 'disabled') return 'This share is turned off.'
  return ''
}

export function ShareBadges({ share }: { share: Share }) {
  return (
    <>
      <span className={`badge ${share.readOnly ? 'badge-ro' : 'badge-rw'}`}>{share.readOnly ? 'Read-only' : 'Read-write'}</span>
      <span className={`badge badge-${share.status}`}>{share.status}</span>
    </>
  )
}

export function FilesPage({ user, route }: { user: User; route: Route }) {
  const uploads = useUploads()
  const [sort, setSort] = useState<SortKey>('name')
  const [desc, setDesc] = useState(false)
  if (!route.shareId) return <SharesList user={user} />
  return (
    <Browser
      key={`${route.shareId}/${route.path ?? ''}`}
      user={user}
      shareId={route.shareId}
      path={route.path ?? ''}
      sort={sort}
      desc={desc}
      setSort={(k, d) => {
        setSort(k)
        setDesc(d)
      }}
      uploads={uploads}
    />
  )
}

function SharesList({ user }: { user: User }) {
  const [shares, setShares] = useState<Share[] | null>(null)
  const [error, setError] = useState('')
  const load = useCallback(() => {
    setError('')
    api.listShares().then(setShares, (e: Error) => setError(e.message))
  }, [])
  useEffect(load, [load])

  return (
    <section aria-labelledby="shares-h">
      <h1 id="shares-h">Shared folders</h1>
      {error ? (
        <>
          <ErrorText>{error}</ErrorText>
          <button type="button" className="btn" onClick={load}>
            Try again
          </button>
        </>
      ) : shares === null ? (
        <Spinner label="Loading shares..." />
      ) : shares.length === 0 ? (
        <Empty>
          {user.role === 'admin' ? (
            <>
              Nothing is shared yet. <a href="#/admin/shares">Add a shared folder</a> to get started.
            </>
          ) : (
            'No folders are shared with you yet. Ask the administrator to add one.'
          )}
        </Empty>
      ) : (
        <ul className="share-list">
          {shares.map((s) => {
            const open = s.status === 'online'
            const problem = open ? '' : shareProblemText(s)
            return (
              <li key={s.id} className={`card share share-${s.status}`}>
                <div className="share-main">
                  {open ? (
                    <a className="share-name" href={filesHash(s.id)}>
                      {s.name}
                    </a>
                  ) : (
                    <span className="share-name muted">{s.name}</span>
                  )}
                  <span className="badges">
                    <ShareBadges share={s} />
                  </span>
                </div>
                {problem ? <p className="problem">{problem}</p> : null}
              </li>
            )
          })}
        </ul>
      )}
    </section>
  )
}

type Dlg =
  | { kind: 'mkdir' }
  | { kind: 'rename'; entry: Entry }
  | { kind: 'move'; entry: Entry }
  | { kind: 'delete'; entry: Entry }

interface BrowserProps {
  user: User
  shareId: string
  path: string
  sort: SortKey
  desc: boolean
  setSort: (k: SortKey, desc: boolean) => void
  uploads: ReturnType<typeof useUploads>
}

function Browser({ user, shareId, path, sort, desc, setSort, uploads }: BrowserProps) {
  const [share, setShare] = useState<Share | null>(null)
  const [entries, setEntries] = useState<Entry[]>([])
  const [total, setTotal] = useState(0)
  const [canWrite, setCanWrite] = useState(false)
  const [loading, setLoading] = useState(true)
  const [loadingMore, setLoadingMore] = useState(false)
  const [error, setError] = useState('')
  const [qInput, setQInput] = useState('')
  const [query, setQuery] = useState('')
  const [dlg, setDlg] = useState<Dlg | null>(null)
  const [busy, setBusy] = useState(false)
  const [dlgError, setDlgError] = useState('')
  const [reload, setReload] = useState(0)
  const reqId = useRef(0)

  useEffect(() => {
    api.listShares().then(
      (all) => setShare(all.find((s) => s.id === shareId) ?? null),
      () => undefined,
    )
  }, [shareId])

  const fetchPage = useCallback(
    async (offset: number) => {
      const id = ++reqId.current
      if (offset === 0) setLoading(true)
      else setLoadingMore(true)
      setError('')
      try {
        const r = await api.listDir(shareId, { path, sort, desc, q: query, offset, limit: PAGE })
        if (id !== reqId.current) return
        setEntries((prev) => (offset === 0 ? r.entries ?? [] : [...prev, ...(r.entries ?? [])]))
        setTotal(r.total)
        setCanWrite(r.canWrite)
      } catch (e) {
        if (id !== reqId.current) return
        setError(e instanceof Error ? e.message : 'Could not load this folder.')
      }
      if (id === reqId.current) {
        setLoading(false)
        setLoadingMore(false)
      }
    },
    [shareId, path, sort, desc, query],
  )

  useEffect(() => {
    void fetchPage(0)
  }, [fetchPage, reload, uploads.tick])

  const refresh = () => setReload((n) => n + 1)
  const closeDlg = () => {
    setDlg(null)
    setDlgError('')
    setBusy(false)
  }
  const run = async (op: () => Promise<unknown>) => {
    setBusy(true)
    setDlgError('')
    try {
      await op()
      closeDlg()
      refresh()
    } catch (e) {
      setDlgError(e instanceof Error ? e.message : 'The action failed.')
      setBusy(false)
    }
  }

  const segs = path ? path.split('/') : []
  const shareName = share?.name ?? 'Folder'
  const effectiveCanWrite = canWrite && share?.status !== 'offline'

  const toggleSort = (k: SortKey) => {
    if (k === sort) setSort(k, !desc)
    else setSort(k, false)
  }
  const searchSubmit = (e: FormEvent) => {
    e.preventDefault()
    setQuery(qInput.trim())
  }
  const why = !canWrite
    ? share?.readOnly
      ? 'This share is read-only, so files cannot be added or changed here.'
      : user.role === 'reader'
        ? 'Your account has read-only access, so files cannot be added or changed.'
        : 'Changes are not available for this share right now.'
    : ''

  return (
    <section aria-labelledby="browse-h">
      <h1 id="browse-h" className="sr-only">
        {shareName}
      </h1>
      <nav aria-label="Breadcrumb" className="crumbs">
        <ol>
          <li>
            <a href="#/files">Shares</a>
          </li>
          <li>
            {segs.length === 0 ? (
              <span aria-current="page">{shareName}</span>
            ) : (
              <a href={filesHash(shareId)}>{shareName}</a>
            )}
          </li>
          {segs.map((s, i) => {
            const p = segs.slice(0, i + 1).join('/')
            return (
              <li key={p}>
                {i === segs.length - 1 ? (
                  <span aria-current="page">{s}</span>
                ) : (
                  <a href={filesHash(shareId, p)}>{s}</a>
                )}
              </li>
            )
          })}
        </ol>
      </nav>

      <div className="toolbar">
        <form role="search" className="search" onSubmit={searchSubmit}>
          <label htmlFor="q" className="sr-only">
            Search in this folder
          </label>
          <input id="q" type="search" placeholder="Search in this folder" value={qInput} onChange={(e) => setQInput(e.target.value)} />
          <button type="submit" className="btn">
            Search
          </button>
          {query ? (
            <button
              type="button"
              className="btn btn-quiet"
              onClick={() => {
                setQInput('')
                setQuery('')
              }}
            >
              Clear search
            </button>
          ) : null}
        </form>
        <div className="sorter">
          <label htmlFor="sort">Sort by</label>
          <select id="sort" value={sort} onChange={(e) => setSort(e.target.value as SortKey, desc)}>
            <option value="name">Name</option>
            <option value="size">Size</option>
            <option value="type">Type</option>
            <option value="modified">Modified</option>
          </select>
          <button type="button" className="btn" onClick={() => setSort(sort, !desc)} aria-label={`Order: ${desc ? 'descending' : 'ascending'}. Switch order`}>
            {desc ? 'Z-A' : 'A-Z'}
          </button>
        </div>
        {effectiveCanWrite ? (
          <button type="button" className="btn" onClick={() => setDlg({ kind: 'mkdir' })}>
            New folder
          </button>
        ) : null}
      </div>

      {!loading && why ? <Notice kind="info">{why}</Notice> : null}
      {effectiveCanWrite ? <UploadPanel uploads={uploads} shareId={shareId} dir={path} /> : null}

      {path ? (
        <p className="up">
          <a href={filesHash(shareId, parentPath(path))}>Parent folder</a>
        </p>
      ) : null}

      {error ? (
        <div>
          <ErrorText>{error}</ErrorText>
          <button type="button" className="btn" onClick={refresh}>
            Try again
          </button>
        </div>
      ) : loading ? (
        <Spinner label="Loading files..." />
      ) : entries.length === 0 ? (
        <Empty>{query ? `No files or folders match "${query}".` : 'This folder is empty.'}</Empty>
      ) : (
        <div className="table-wrap">
          <table className="files">
            <caption className="sr-only">
              Files in {shareName}
              {path ? ` / ${path}` : ''}
            </caption>
            <thead>
              <tr>
                {(['name', 'size', 'type', 'modified'] as SortKey[]).map((k) => (
                  <th
                    key={k}
                    scope="col"
                    className={`col-${k}`}
                    aria-sort={sort === k ? (desc ? 'descending' : 'ascending') : 'none'}
                  >
                    <button type="button" className="th-btn" onClick={() => toggleSort(k)}>
                      {k[0].toUpperCase() + k.slice(1)}
                      <span aria-hidden="true">{sort === k ? (desc ? ' ▼' : ' ▲') : ''}</span>
                    </button>
                  </th>
                ))}
                <th scope="col" className="col-actions">
                  Actions
                </th>
              </tr>
            </thead>
            <tbody>
              {entries.map((e) => {
                const full = joinPath(path, e.name)
                return (
                  <tr key={e.name}>
                    <td className="col-name">
                      {e.isDir ? (
                        <a className="entry dir" href={filesHash(shareId, full)}>
                          <span aria-hidden="true">{'📁 '}</span>
                          {e.name}
                        </a>
                      ) : (
                        <span className="entry">
                          <span aria-hidden="true">{'📄 '}</span>
                          {e.name}
                        </span>
                      )}
                      <span className="meta-mobile" aria-hidden="true">
                        {e.isDir ? '' : formatBytes(e.size) + ' · '}
                        {formatDate(e.modTime)}
                      </span>
                    </td>
                    <td className="col-size">{e.isDir ? '-' : formatBytes(e.size)}</td>
                    <td className="col-type">{e.isDir ? 'Folder' : e.type || 'File'}</td>
                    <td className="col-modified">{formatDate(e.modTime)}</td>
                    <td className="col-actions">
                      <span className="row-actions">
                        {!e.isDir && (
                          <a className="btn btn-sm" href={api.downloadUrl(shareId, full)} aria-label={`Download ${e.name}`}>
                            Download
                          </a>
                        )}
                        {!e.isDir && canPreview(e.type) && (
                          <a className="btn btn-sm" href={api.downloadUrl(shareId, full, true)} target="_blank" rel="noopener" aria-label={`Preview ${e.name}`}>
                            Preview
                          </a>
                        )}
                        {effectiveCanWrite && (
                          <>
                            <button type="button" className="btn btn-sm" aria-label={`Rename ${e.name}`} onClick={() => setDlg({ kind: 'rename', entry: e })}>
                              Rename
                            </button>
                            <button type="button" className="btn btn-sm" aria-label={`Move ${e.name}`} onClick={() => setDlg({ kind: 'move', entry: e })}>
                              Move
                            </button>
                            <button type="button" className="btn btn-sm btn-danger-quiet" aria-label={`Delete ${e.name}`} onClick={() => setDlg({ kind: 'delete', entry: e })}>
                              Delete
                            </button>
                          </>
                        )}
                      </span>
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>
      )}

      {!loading && !error && entries.length > 0 ? (
        <div className="pager" aria-live="polite">
          <span>
            Showing {entries.length} of {total}
          </span>
          {entries.length < total ? (
            <button type="button" className="btn" disabled={loadingMore} onClick={() => void fetchPage(entries.length)}>
              {loadingMore ? 'Loading...' : 'Load more'}
            </button>
          ) : null}
        </div>
      ) : null}

      {dlg?.kind === 'mkdir' && (
        <PromptDialog title="New folder" label="Folder name" submitLabel="Create" busy={busy} error={dlgError} onCancel={closeDlg} onSubmit={(name) => void run(() => api.mkdir(shareId, path, name))} />
      )}
      {dlg?.kind === 'rename' && (
        <PromptDialog
          title={`Rename ${dlg.entry.name}`}
          label="New name"
          initial={dlg.entry.name}
          submitLabel="Rename"
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onSubmit={(name) => void run(() => api.renameItem(shareId, joinPath(path, dlg.entry.name), name))}
        />
      )}
      {dlg?.kind === 'delete' && (
        <ConfirmDialog
          title={dlg.entry.isDir ? 'Delete folder?' : 'Delete file?'}
          danger
          confirmLabel="Delete"
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onConfirm={() => void run(() => api.deleteItem(shareId, joinPath(path, dlg.entry.name)))}
          message={
            dlg.entry.isDir ? (
              <p>
                Delete the folder <strong>{dlg.entry.name}</strong> and everything inside it? This permanently deletes all of its files and subfolders and cannot be undone.
              </p>
            ) : (
              <p>
                Delete <strong>{dlg.entry.name}</strong>? This permanently deletes the file from the server and cannot be undone.
              </p>
            )
          }
        />
      )}
      {dlg?.kind === 'move' && (
        <FolderPicker
          shareId={shareId}
          item={dlg.entry}
          itemPath={joinPath(path, dlg.entry.name)}
          currentDir={path}
          busy={busy}
          error={dlgError}
          onCancel={closeDlg}
          onPick={(dest) => void run(() => api.moveItem(shareId, joinPath(path, dlg.entry.name), dest))}
        />
      )}
    </section>
  )
}

interface PickerProps {
  shareId: string
  item: Entry
  itemPath: string
  currentDir: string
  busy: boolean
  error: string
  onCancel: () => void
  onPick: (dest: string) => void
}

/** Simple folder picker: shows the subfolders of one directory at a time. */
function FolderPicker({ shareId, item, itemPath, currentDir, busy, error, onCancel, onPick }: PickerProps) {
  const [dir, setDir] = useState('')
  const [folders, setFolders] = useState<Entry[] | null>(null)
  const [err, setErr] = useState('')

  useEffect(() => {
    let live = true
    setFolders(null)
    setErr('')
    api.listDir(shareId, { path: dir, sort: 'name', desc: false, q: '', offset: 0, limit: 1000 }).then(
      (r) => live && setFolders((r.entries ?? []).filter((e) => e.isDir && joinPath(dir, e.name) !== itemPath)),
      (e: Error) => live && setErr(e.message),
    )
    return () => {
      live = false
    }
  }, [shareId, dir, itemPath])

  const same = dir === currentDir
  return (
    <Dialog
      title={`Move ${item.name}`}
      onClose={onCancel}
      actions={
        <>
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" disabled={busy || same} onClick={() => onPick(dir)}>
            {busy ? 'Moving...' : 'Move here'}
          </button>
        </>
      }
    >
      <p>
        Destination: <strong>/{dir}</strong>
      </p>
      {dir ? (
        <button type="button" className="btn btn-sm" onClick={() => setDir(parentPath(dir))}>
          Up one level
        </button>
      ) : null}
      {err ? (
        <ErrorText>{err}</ErrorText>
      ) : folders === null ? (
        <Spinner label="Loading folders..." />
      ) : folders.length === 0 ? (
        <Empty>No subfolders here.</Empty>
      ) : (
        <ul className="picker" aria-label="Folders">
          {folders.map((f) => (
            <li key={f.name}>
              <button type="button" className="btn btn-quiet" onClick={() => setDir(joinPath(dir, f.name))}>
                {'📁 '}
                {f.name}
              </button>
            </li>
          ))}
        </ul>
      )}
      {same ? <p className="hint">The item is already in this folder. Pick a different one.</p> : null}
      <ErrorText>{error}</ErrorText>
    </Dialog>
  )
}
