import { useCallback, useEffect, useRef, useState } from 'react'
import type { DragEvent } from 'react'
import * as api from './api'
import type { ConflictPolicy } from './api'
import { formatBytes } from './format'
import { Dialog, ErrorText } from './ui'

export type UpStatus = 'queued' | 'uploading' | 'done' | 'error' | 'conflict' | 'canceled'

export interface UpItem {
  id: number
  shareId: string
  dir: string
  file: File
  status: UpStatus
  loaded: number
  total: number
  error?: string
  policy: ConflictPolicy
}

const MAX_PARALLEL = 2

export interface Uploads {
  items: UpItem[]
  /** Increments each time a batch of uploads finishes, so listings can refresh. */
  tick: number
  add: (shareId: string, dir: string, files: File[]) => void
  cancel: (id: number) => void
  retry: (id: number) => void
  dismiss: (id: number) => void
  clearFinished: () => void
  resolveConflict: (id: number, policy: ConflictPolicy | 'cancel') => void
}

export function useUploads(): Uploads {
  const [items, setItems] = useState<UpItem[]>([])
  const [tick, setTick] = useState(0)
  const handles = useRef(new Map<number, api.UploadHandle>())
  const nextId = useRef(1)
  const wasActive = useRef(false)

  const patch = useCallback((id: number, p: Partial<UpItem>) => {
    setItems((a) => a.map((i) => (i.id === id ? { ...i, ...p } : i)))
  }, [])

  const start = useCallback(
    (it: UpItem) => {
      const h = api.uploadFile(it.shareId, it.dir, it.file, it.policy, (loaded, total) => patch(it.id, { loaded, total }))
      handles.current.set(it.id, h)
      patch(it.id, { status: 'uploading', loaded: 0, error: undefined })
      h.promise
        .then(() => {
          handles.current.delete(it.id)
          patch(it.id, { status: 'done', loaded: it.file.size })
        })
        .catch((e: unknown) => {
          handles.current.delete(it.id)
          const status = e instanceof api.ApiError ? e.status : 0
          if (status === -1) patch(it.id, { status: 'canceled', error: undefined })
          else if (status === 409) patch(it.id, { status: 'conflict', error: undefined })
          else patch(it.id, { status: 'error', error: e instanceof Error ? e.message : 'Upload failed.' })
        })
    },
    [patch],
  )

  useEffect(() => {
    for (const it of items) {
      if (it.status !== 'queued' || handles.current.has(it.id)) continue
      if (handles.current.size >= MAX_PARALLEL) break
      start(it)
    }
    const active = items.some((i) => i.status === 'queued' || i.status === 'uploading')
    if (wasActive.current && !active && items.some((i) => i.status === 'done')) setTick((t) => t + 1)
    wasActive.current = active
  }, [items, start])

  // Abort anything still running when the app goes away.
  useEffect(() => {
    const hs = handles.current
    return () => hs.forEach((h) => h.abort())
  }, [])

  return {
    items,
    tick,
    add: (shareId, dir, files) =>
      setItems((a) => [
        ...a,
        ...files.map((file) => ({
          id: nextId.current++, shareId, dir, file, status: 'queued' as const, loaded: 0, total: file.size, policy: 'fail' as const,
        })),
      ]),
    cancel: (id) => {
      const h = handles.current.get(id)
      if (h) h.abort()
      else patch(id, { status: 'canceled' })
    },
    retry: (id) => patch(id, { status: 'queued', loaded: 0, error: undefined, policy: 'fail' }),
    dismiss: (id) => setItems((a) => a.filter((i) => i.id !== id)),
    clearFinished: () => setItems((a) => a.filter((i) => i.status === 'queued' || i.status === 'uploading' || i.status === 'conflict')),
    resolveConflict: (id, policy) => (policy === 'cancel' ? patch(id, { status: 'canceled' }) : patch(id, { status: 'queued', policy })),
  }
}

export function ConflictDialog({ item, onResolve }: { item: UpItem; onResolve: Uploads['resolveConflict'] }) {
  const [confirmReplace, setConfirmReplace] = useState(false)
  if (confirmReplace) {
    return (
      <Dialog
        title="Replace existing file?"
        danger
        onClose={() => setConfirmReplace(false)}
        actions={
          <>
            <button type="button" className="btn" data-autofocus onClick={() => setConfirmReplace(false)}>
              Back
            </button>
            <button type="button" className="btn btn-danger" onClick={() => onResolve(item.id, 'replace')}>
              Replace file
            </button>
          </>
        }
      >
        <p>
          The existing <strong>{item.file.name}</strong> will be overwritten with the file you selected. This cannot be undone.
        </p>
      </Dialog>
    )
  }
  return (
    <Dialog
      title="A file with this name already exists"
      onClose={() => onResolve(item.id, 'cancel')}
      actions={
        <>
          <button type="button" className="btn" onClick={() => onResolve(item.id, 'cancel')}>
            Cancel
          </button>
          <button type="button" className="btn btn-primary" data-autofocus onClick={() => onResolve(item.id, 'rename')}>
            Keep both
          </button>
          <button type="button" className="btn btn-danger" onClick={() => setConfirmReplace(true)}>
            Replace
          </button>
        </>
      }
    >
      <p>
        <strong>{item.file.name}</strong> is already in this folder. Keep both saves the new file with a number added to its name.
      </p>
    </Dialog>
  )
}

interface PanelProps {
  uploads: Uploads
  shareId: string
  dir: string
}

export function UploadPanel({ uploads, shareId, dir }: PanelProps) {
  const [over, setOver] = useState(false)
  const [note, setNote] = useState('')
  const mine = uploads.items.filter((i) => i.shareId === shareId)
  const conflict = uploads.items.find((i) => i.status === 'conflict')
  const finished = mine.some((i) => i.status === 'done' || i.status === 'error' || i.status === 'canceled')

  const pick = (files: File[]) => {
    if (files.length) uploads.add(shareId, dir, files)
  }
  const onDrop = (e: DragEvent) => {
    e.preventDefault()
    setOver(false)
    setNote('')
    const files: File[] = []
    let skipped = 0
    const dt = e.dataTransfer
    if (dt.items && dt.items.length) {
      for (const it of Array.from(dt.items)) {
        if (it.kind !== 'file') continue
        if (it.webkitGetAsEntry?.()?.isDirectory) {
          skipped++
          continue
        }
        const f = it.getAsFile()
        if (f) files.push(f)
      }
    } else {
      files.push(...Array.from(dt.files))
    }
    if (skipped) setNote('Folders cannot be uploaded by drag and drop. Select the files inside instead.')
    pick(files)
  }

  return (
    <section className="uploads" aria-label="Upload files">
      <div
        className={`dropzone${over ? ' over' : ''}`}
        onDragOver={(e) => {
          e.preventDefault()
          setOver(true)
        }}
        onDragLeave={() => setOver(false)}
        onDrop={onDrop}
        data-testid="dropzone"
      >
        <span>Drag files here or</span>
        <label className="btn btn-primary file-btn">
          Choose files
          <input
            type="file"
            multiple
            className="file-input"
            onChange={(e) => {
              pick(Array.from(e.target.files ?? []))
              e.target.value = ''
            }}
          />
        </label>
      </div>
      {note ? <p className="hint">{note}</p> : null}
      {mine.length > 0 && (
        <>
          <ul className="upload-list" aria-label="Uploads">
            {mine.map((u) => {
              const pct = u.total > 0 ? Math.min(100, Math.round((u.loaded / u.total) * 100)) : 0
              return (
                <li key={u.id} className={`upload upload-${u.status}`}>
                  <div className="upload-head">
                    <span className="upload-name" title={u.file.name}>
                      {u.file.name}
                    </span>
                    <span className="upload-size">{formatBytes(u.file.size)}</span>
                  </div>
                  {(u.status === 'uploading' || u.status === 'queued') && (
                    <progress max={100} value={u.status === 'queued' ? 0 : pct} aria-label={`Upload progress for ${u.file.name}`} />
                  )}
                  <div className="upload-foot">
                    <span className="upload-state">
                      {u.status === 'queued' && 'Waiting...'}
                      {u.status === 'uploading' && `${pct}%`}
                      {u.status === 'done' && 'Uploaded'}
                      {u.status === 'canceled' && 'Canceled'}
                      {u.status === 'conflict' && 'Waiting for your decision'}
                      {u.status === 'error' && <ErrorText>{`${u.file.name}: ${u.error ?? 'Upload failed.'}`}</ErrorText>}
                    </span>
                    <span className="row-actions">
                      {(u.status === 'queued' || u.status === 'uploading') && (
                        <button type="button" className="btn btn-sm" onClick={() => uploads.cancel(u.id)} aria-label={`Cancel upload of ${u.file.name}`}>
                          Cancel
                        </button>
                      )}
                      {(u.status === 'error' || u.status === 'canceled') && (
                        <button type="button" className="btn btn-sm" onClick={() => uploads.retry(u.id)} aria-label={`Retry upload of ${u.file.name}`}>
                          Retry
                        </button>
                      )}
                      {(u.status === 'done' || u.status === 'error' || u.status === 'canceled') && (
                        <button type="button" className="btn btn-sm btn-quiet" onClick={() => uploads.dismiss(u.id)} aria-label={`Dismiss ${u.file.name}`}>
                          Dismiss
                        </button>
                      )}
                    </span>
                  </div>
                </li>
              )
            })}
          </ul>
          {finished && (
            <button type="button" className="btn btn-sm btn-quiet" onClick={uploads.clearFinished}>
              Clear finished
            </button>
          )}
        </>
      )}
      {conflict ? <ConflictDialog key={conflict.id} item={conflict} onResolve={uploads.resolveConflict} /> : null}
    </section>
  )
}
