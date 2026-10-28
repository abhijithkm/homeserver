import { useEffect, useId, useRef, useState } from 'react'
import type { FormEvent, ReactNode } from 'react'

/** Error text announced to screen readers. */
export function ErrorText({ children }: { children?: ReactNode }) {
  return (
    <div role="alert" className="error-slot">
      {children ? <p className="error">{children}</p> : null}
    </div>
  )
}

export function Notice({ kind = 'info', children }: { kind?: 'info' | 'warn' | 'error' | 'ok'; children: ReactNode }) {
  return (
    <div className={`notice notice-${kind}`} role={kind === 'error' ? 'alert' : 'status'}>
      {children}
    </div>
  )
}

export function Spinner({ label = 'Loading...' }: { label?: string }) {
  return (
    <p className="loading" role="status" aria-live="polite">
      <span className="spinner" aria-hidden="true" /> {label}
    </p>
  )
}

export function Empty({ children }: { children: ReactNode }) {
  return <p className="empty">{children}</p>
}

const FOCUSABLE = 'a[href],button:not([disabled]),input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])'

interface DialogProps {
  title: string
  onClose: () => void
  children: ReactNode
  actions?: ReactNode
  /** Danger styling for destructive confirmations. */
  danger?: boolean
}

/** Accessible modal: focus moves in, Tab is trapped, Escape closes, focus returns on close. */
export function Dialog({ title, onClose, children, actions, danger }: DialogProps) {
  const ref = useRef<HTMLDivElement>(null)
  const titleId = useId()
  const closeRef = useRef(onClose)
  closeRef.current = onClose
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null
    const el = ref.current
    const first = el?.querySelector<HTMLElement>('[data-autofocus],input,select,textarea') ?? el?.querySelector<HTMLElement>(FOCUSABLE)
    first?.focus()
    return () => prev?.focus?.()
  }, [])
  const onKey = (e: React.KeyboardEvent) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      closeRef.current()
    } else if (e.key === 'Tab' && ref.current) {
      const f = Array.from(ref.current.querySelectorAll<HTMLElement>(FOCUSABLE))
      if (f.length === 0) return
      const a = f[0]
      const z = f[f.length - 1]
      if (e.shiftKey && document.activeElement === a) {
        e.preventDefault()
        z.focus()
      } else if (!e.shiftKey && document.activeElement === z) {
        e.preventDefault()
        a.focus()
      }
    }
  }
  return (
    <div className="backdrop" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div ref={ref} className={`dialog${danger ? ' dialog-danger' : ''}`} role="dialog" aria-modal="true" aria-labelledby={titleId} onKeyDown={onKey}>
        <h2 id={titleId}>{title}</h2>
        <div className="dialog-body">{children}</div>
        {actions ? <div className="dialog-actions">{actions}</div> : null}
      </div>
    </div>
  )
}

interface ConfirmProps {
  title: string
  message: ReactNode
  confirmLabel: string
  danger?: boolean
  busy?: boolean
  error?: string
  onConfirm: () => void
  onCancel: () => void
}

export function ConfirmDialog({ title, message, confirmLabel, danger, busy, error, onConfirm, onCancel }: ConfirmProps) {
  return (
    <Dialog
      title={title}
      onClose={onCancel}
      danger={danger}
      actions={
        <>
          <button type="button" className="btn" onClick={onCancel} data-autofocus>
            Cancel
          </button>
          <button type="button" className={`btn ${danger ? 'btn-danger' : 'btn-primary'}`} disabled={busy} onClick={onConfirm}>
            {busy ? 'Working...' : confirmLabel}
          </button>
        </>
      }
    >
      <div>{message}</div>
      <ErrorText>{error}</ErrorText>
    </Dialog>
  )
}

interface PromptProps {
  title: string
  label: string
  initial?: string
  submitLabel: string
  busy?: boolean
  error?: string
  hint?: string
  type?: 'text' | 'password'
  onSubmit: (value: string) => void
  onCancel: () => void
}

export function PromptDialog({ title, label, initial = '', submitLabel, busy, error, hint, type = 'text', onSubmit, onCancel }: PromptProps) {
  const [value, setValue] = useState(initial)
  const id = useId()
  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (value.trim()) onSubmit(type === 'password' ? value : value.trim())
  }
  return (
    <Dialog title={title} onClose={onCancel}>
      <form onSubmit={submit}>
        <label htmlFor={id}>{label}</label>
        <input id={id} type={type} value={value} onChange={(e) => setValue(e.target.value)} autoComplete={type === 'password' ? 'new-password' : 'off'} spellCheck={false} />
        {hint ? <p className="hint">{hint}</p> : null}
        <ErrorText>{error}</ErrorText>
        <div className="dialog-actions">
          <button type="button" className="btn" onClick={onCancel}>
            Cancel
          </button>
          <button type="submit" className="btn btn-primary" disabled={busy || !value.trim()}>
            {busy ? 'Working...' : submitLabel}
          </button>
        </div>
      </form>
    </Dialog>
  )
}

export function CopyButton({ text, label = 'Copy' }: { text: string; label?: string }) {
  const [state, setState] = useState<'idle' | 'ok' | 'fail'>('idle')
  const copy = async () => {
    try {
      if (navigator.clipboard?.writeText) await navigator.clipboard.writeText(text)
      else throw new Error('no clipboard')
      setState('ok')
    } catch {
      // Clipboard API needs a secure context; fall back to selecting a hidden textarea.
      try {
        const ta = document.createElement('textarea')
        ta.value = text
        ta.style.position = 'fixed'
        ta.style.opacity = '0'
        document.body.appendChild(ta)
        ta.select()
        const ok = document.execCommand('copy')
        ta.remove()
        setState(ok ? 'ok' : 'fail')
      } catch {
        setState('fail')
      }
    }
    setTimeout(() => setState('idle'), 2000)
  }
  return (
    <button type="button" className="btn btn-sm" onClick={copy} aria-label={`${label} ${text}`}>
      {state === 'ok' ? 'Copied' : state === 'fail' ? 'Copy failed' : label}
      <span className="sr-only" role="status" aria-live="polite">
        {state === 'ok' ? 'Copied to clipboard' : state === 'fail' ? 'Copy failed, select the text manually' : ''}
      </span>
    </button>
  )
}

export function StatusBadge({ status }: { status: string }) {
  return <span className={`badge badge-${status}`}>{status}</span>
}
