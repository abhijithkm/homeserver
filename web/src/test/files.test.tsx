import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import { admin, file, folder, listing, mockFetch, reader, resetApi, session, share, writer } from './helpers'
import type { Handler } from './helpers'
import type { User } from '../types'

beforeEach(resetApi)
afterEach(() => vi.unstubAllGlobals())

function setup(user: User, routes: Record<string, Handler> = {}, hash = '#/files') {
  window.location.hash = hash
  const m = mockFetch({
    'GET /api/session': session(user),
    'GET /api/shares': { json: [share()] },
    'GET /api/shares/s1/list': listing([folder('docs'), file('a.txt', 2048), file('pic.png', 4096, 'png')]),
    ...routes,
  })
  render(<App />)
  return m
}

describe('share list', () => {
  it('shows name, access badge and status, with problem text', async () => {
    setup(admin, {
      'GET /api/shares': {
        json: [
          share({ id: 's1', name: 'Photos', readOnly: true }),
          share({ id: 's2', name: 'Backup', status: 'offline', problem: 'Drive E: is not connected.', canWrite: false }),
          share({ id: 's3', name: 'Old', enabled: false, status: 'disabled' }),
        ],
      },
    })
    expect(await screen.findByRole('link', { name: 'Photos' })).toHaveAttribute('href', '#/files/s1')
    expect(screen.getByText('Read-only')).toBeInTheDocument()
    expect(screen.getByText('Drive E: is not connected.')).toBeInTheDocument()
    expect(screen.getByText('offline')).toBeInTheDocument()
    expect(screen.getByText('disabled')).toBeInTheDocument()
    expect(screen.queryByRole('link', { name: 'Backup' })).not.toBeInTheDocument()
  })

  it('shows an empty state for admins and for other users', async () => {
    setup(admin, { 'GET /api/shares': { json: [] } })
    expect(await screen.findByRole('link', { name: 'Add a shared folder' })).toBeInTheDocument()
  })

  it('shows an empty state for readers', async () => {
    setup(reader, { 'GET /api/shares': { json: [] } })
    expect(await screen.findByText(/No folders are shared with you/)).toBeInTheDocument()
  })

  it('shows an error with retry', async () => {
    let fail = true
    setup(admin, { 'GET /api/shares': () => (fail ? { status: 500, json: { error: 'database error' } } : { json: [share()] }) })
    expect(await screen.findByRole('alert')).toHaveTextContent('database error')
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(await screen.findByRole('link', { name: 'Photos' })).toBeInTheDocument()
  })
})

describe('file browser navigation', () => {
  it('lists entries, opens a folder, uses breadcrumbs and the parent link', async () => {
    const m = setup(writer, {
      'GET /api/shares/s1/list': (c) =>
        c.query.get('path') === 'docs' ? listing([file('inner.md', 10, 'md')], { path: 'docs' }) : listing([folder('docs'), file('a.txt', 2048)]),
    }, '#/files/s1')
    const user = userEvent.setup()
    expect(await screen.findByRole('link', { name: /docs/ })).toBeInTheDocument()
    expect(screen.getByText('a.txt')).toBeInTheDocument()

    await user.click(screen.getByRole('link', { name: /docs/ }))
    expect(await screen.findByText('inner.md')).toBeInTheDocument()
    expect(window.location.hash).toBe('#/files/s1/docs')
    const crumbs = screen.getByRole('navigation', { name: 'Breadcrumb' })
    expect(within(crumbs).getByText('docs')).toHaveAttribute('aria-current', 'page')

    await user.click(screen.getByRole('link', { name: 'Parent folder' }))
    expect(await screen.findByText('a.txt')).toBeInTheDocument()
    expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('path')).toBeNull()

    await user.click(screen.getByRole('link', { name: /docs/ }))
    await screen.findByText('inner.md')
    await user.click(within(crumbs).getByRole('link', { name: 'Photos' }))
    expect(await screen.findByText('a.txt')).toBeInTheDocument()
  })

  it('sorts by column and by the sort control, asc and desc', async () => {
    const m = setup(writer, {}, '#/files/s1')
    const user = userEvent.setup()
    await screen.findByText('a.txt')
    await user.click(screen.getByRole('button', { name: /^Size/ }))
    await waitFor(() => expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('sort')).toBe('size'))
    expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('desc')).toBeNull()
    await user.click(screen.getByRole('button', { name: /^Size/ }))
    await waitFor(() => expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('desc')).toBe('1'))
    expect(screen.getByRole('columnheader', { name: /Size/ })).toHaveAttribute('aria-sort', 'descending')
    await user.selectOptions(screen.getByLabelText('Sort by'), 'modified')
    await waitFor(() => expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('sort')).toBe('modified'))
  })

  it('searches the folder and clears the search', async () => {
    const m = setup(writer, {
      'GET /api/shares/s1/list': (c) => (c.query.get('q') === 'zzz' ? listing([]) : listing([file('a.txt')])),
    }, '#/files/s1')
    const user = userEvent.setup()
    await screen.findByText('a.txt')
    await user.type(screen.getByLabelText('Search in this folder'), 'zzz')
    await user.click(screen.getByRole('button', { name: 'Search' }))
    expect(await screen.findByText(/No files or folders match "zzz"/)).toBeInTheDocument()
    expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('q')).toBe('zzz')
    await user.click(screen.getByRole('button', { name: 'Clear search' }))
    expect(await screen.findByText('a.txt')).toBeInTheDocument()
  })

  it('loads more with offset and limit', async () => {
    const all = Array.from({ length: 150 }, (_, i) => file(`f${String(i).padStart(3, '0')}.txt`))
    const m = setup(writer, {
      'GET /api/shares/s1/list': (c) => {
        const off = Number(c.query.get('offset') ?? 0)
        const lim = Number(c.query.get('limit'))
        return listing(all.slice(off, off + lim), { total: 150, offset: off, limit: lim })
      },
    }, '#/files/s1')
    const user = userEvent.setup()
    expect(await screen.findByText('Showing 100 of 150')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Load more' }))
    expect(await screen.findByText('Showing 150 of 150')).toBeInTheDocument()
    expect(m.find('GET', '/api/shares/s1/list').at(-1)!.query.get('offset')).toBe('100')
    expect(screen.getByText('f149.txt')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Load more' })).not.toBeInTheDocument()
  })

  it('shows empty folder and error states', async () => {
    setup(writer, { 'GET /api/shares/s1/list': listing([]) }, '#/files/s1')
    expect(await screen.findByText('This folder is empty.')).toBeInTheDocument()
  })

  it('shows the server error when a folder cannot be loaded', async () => {
    setup(writer, { 'GET /api/shares/s1/list': { status: 503, json: { error: "this share's folder is currently unavailable" } } }, '#/files/s1')
    expect(await screen.findByRole('alert')).toHaveTextContent('currently unavailable')
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })

  it('offers download and preview links', async () => {
    setup(writer, {}, '#/files/s1')
    const dl = await screen.findByRole('link', { name: 'Download a.txt' })
    expect(dl).toHaveAttribute('href', '/api/shares/s1/download?path=a.txt')
    expect(screen.getByRole('link', { name: 'Preview pic.png' })).toHaveAttribute('href', '/api/shares/s1/download?path=pic.png&inline=1')
    expect(screen.queryByRole('link', { name: 'Preview a.txt' })).not.toBeInTheDocument()
  })
})

describe('file actions', () => {
  it('creates a folder and sends the CSRF header', async () => {
    const m = setup(writer, { 'POST /api/shares/s1/mkdir': { json: { status: 'ok' } } }, '#/files/s1')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'New folder' }))
    await user.type(screen.getByLabelText('Folder name'), 'Trips')
    await user.click(screen.getByRole('button', { name: 'Create' }))
    await waitFor(() => expect(m.find('POST', '/api/shares/s1/mkdir')).toHaveLength(1))
    const c = m.find('POST', '/api/shares/s1/mkdir')[0]
    expect(c.body).toEqual({ path: '', name: 'Trips' })
    expect(c.headers['X-CSRF-Token']).toBe('csrf-token-1')
    await waitFor(() => expect(screen.queryByRole('dialog')).not.toBeInTheDocument())
  })

  it('shows the server error in the dialog when rename conflicts', async () => {
    setup(writer, { 'POST /api/shares/s1/rename': { status: 409, json: { error: 'an item with that name already exists' } } }, '#/files/s1')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Rename a.txt' }))
    const input = screen.getByLabelText('New name')
    await user.clear(input)
    await user.type(input, 'b.txt')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('already exists')
  })

  it('confirms before deleting; folders warn about their contents', async () => {
    const m = setup(writer, { 'POST /api/shares/s1/delete': { json: { status: 'ok' } } }, '#/files/s1')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Delete docs' }))
    const dlg = screen.getByRole('dialog')
    expect(dlg).toHaveTextContent(/everything inside/)
    await user.click(within(dlg).getByRole('button', { name: 'Cancel' }))
    expect(m.find('POST', '/api/shares/s1/delete')).toHaveLength(0)
    await user.click(screen.getByRole('button', { name: 'Delete docs' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete' }))
    await waitFor(() => expect(m.find('POST', '/api/shares/s1/delete')[0]?.body).toEqual({ path: 'docs' }))
  })

  it('moves an item using the folder picker', async () => {
    const m = setup(writer, {
      'GET /api/shares/s1/list': (c) => (c.query.get('limit') === '1000' ? listing([folder('docs'), folder('archive')]) : listing([folder('docs'), file('a.txt')])),
      'POST /api/shares/s1/move': { json: { status: 'ok' } },
    }, '#/files/s1')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Move a.txt' }))
    const dlg = await screen.findByRole('dialog')
    expect(await within(dlg).findByRole('button', { name: /archive/ })).toBeInTheDocument()
    expect(within(dlg).getByRole('button', { name: 'Move here' })).toBeDisabled()
    await user.click(within(dlg).getByRole('button', { name: /archive/ }))
    await user.click(within(dlg).getByRole('button', { name: 'Move here' }))
    await waitFor(() => expect(m.find('POST', '/api/shares/s1/move')[0]?.body).toEqual({ path: 'a.txt', dest: 'archive' }))
  })

  it('closes dialogs with Escape and keeps focus management accessible', async () => {
    setup(writer, {}, '#/files/s1')
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'New folder' }))
    const dlg = screen.getByRole('dialog')
    expect(dlg).toHaveAttribute('aria-modal', 'true')
    expect(dlg).toHaveAccessibleName('New folder')
    expect(screen.getByLabelText('Folder name')).toHaveFocus()
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  })
})

describe('permission-aware UI', () => {
  it('hides write actions and explains why on a read-only share', async () => {
    setup(writer, {
      'GET /api/shares': { json: [share({ readOnly: true, canWrite: false })] },
      'GET /api/shares/s1/list': listing([file('a.txt')], { canWrite: false }),
    }, '#/files/s1')
    expect(await screen.findByText('a.txt')).toBeInTheDocument()
    expect(screen.getByText(/This share is read-only/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'New folder' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Rename a.txt' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete a.txt' })).not.toBeInTheDocument()
    expect(screen.queryByText('Choose files')).not.toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download a.txt' })).toBeInTheDocument()
  })

  it('treats a reader account as read-only even on a read-write share', async () => {
    setup(reader, { 'GET /api/shares/s1/list': listing([file('a.txt')], { canWrite: false }) }, '#/files/s1')
    expect(await screen.findByText('a.txt')).toBeInTheDocument()
    expect(screen.getByText(/Your account has read-only access/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Delete a.txt' })).not.toBeInTheDocument()
    expect(screen.queryByText('Choose files')).not.toBeInTheDocument()
  })

  it('shows write actions to writers on read-write shares', async () => {
    setup(writer, {}, '#/files/s1')
    expect(await screen.findByRole('button', { name: 'Delete a.txt' })).toBeInTheDocument()
    expect(screen.getByText('Choose files')).toBeInTheDocument()
  })

  it('hides the admin navigation from non-admins and blocks the admin route', async () => {
    setup(writer, {}, '#/admin/users')
    const nav = await screen.findByRole('navigation', { name: 'Main' })
    for (const name of ['Dashboard', 'Shares', 'Users', 'Settings', 'Diagnostics', 'Manage']) {
      expect(within(nav).queryByRole('link', { name })).not.toBeInTheDocument()
    }
    expect(within(nav).getByRole('link', { name: 'Files' })).toBeInTheDocument()
    expect(within(nav).getByRole('link', { name: 'Account' })).toBeInTheDocument()
    expect(await screen.findByRole('heading', { name: 'Shared folders' })).toBeInTheDocument()
  })

  it('shows the Dashboard and Manage navigation to administrators', async () => {
    setup(admin)
    const nav = await screen.findByRole('navigation', { name: 'Main' })
    expect(within(nav).getByRole('link', { name: 'Dashboard' })).toHaveAttribute('href', '#/admin/dashboard')
    expect(within(nav).getByRole('link', { name: 'Shares' })).toHaveAttribute('href', '#/admin/shares')
    expect(within(nav).getByRole('link', { name: 'Users' })).toHaveAttribute('href', '#/admin/users')
    expect(within(nav).getByRole('link', { name: 'Settings' })).toHaveAttribute('href', '#/admin/settings')
    expect(within(nav).getByRole('link', { name: 'Diagnostics' })).toHaveAttribute('href', '#/admin/diagnostics')
    expect(within(nav).getByRole('link', { name: 'Files' })).toHaveAttribute('aria-current', 'page')
  })

  it('opens the dashboard first for administrators', async () => {
    setup(admin, {}, '')
    await waitFor(() => expect(window.location.hash).toBe('#/admin/dashboard'))
    expect(await screen.findByRole('heading', { name: 'Dashboard' })).toBeInTheDocument()
    expect(within(screen.getByRole('navigation', { name: 'Main' })).getByRole('link', { name: 'Dashboard' })).toHaveAttribute('aria-current', 'page')
  })

  it('opens Files first for everyone else and keeps an explicit page', async () => {
    setup(writer, {}, '')
    expect(await screen.findByRole('heading', { name: 'Shared folders' })).toBeInTheDocument()
    expect(window.location.hash).toBe('')
  })
})

describe('responsive and accessibility structure', () => {
  it('has landmarks, a skip link, a captioned table and aria-sort', async () => {
    setup(writer, {}, '#/files/s1')
    await screen.findByText('a.txt')
    expect(screen.getByRole('link', { name: 'Skip to content' })).toHaveAttribute('href', '#main')
    expect(screen.getByRole('main')).toBeInTheDocument()
    expect(screen.getByRole('search')).toBeInTheDocument()
    expect(screen.getByRole('table', { name: /Files in Photos/ })).toBeInTheDocument()
    expect(screen.getByRole('columnheader', { name: /Name/ })).toHaveAttribute('aria-sort', 'ascending')
    // Secondary columns carry classes that the stylesheet hides on narrow screens.
    expect(document.querySelector('th.col-type')).not.toBeNull()
    expect(document.querySelector('th.col-modified')).not.toBeNull()
    expect(document.querySelector('.meta-mobile')).toHaveAttribute('aria-hidden', 'true')
  })

  it('exposes a live region for loading status', async () => {
    let release: (v: unknown) => void = () => undefined
    const gate = new Promise((r) => (release = r))
    setup(writer, { 'GET /api/shares/s1/list': async () => { await gate; return listing([]) } }, '#/files/s1')
    expect(await screen.findByText('Loading files...')).toBeInTheDocument()
    release(null)
    expect(await screen.findByText('This folder is empty.')).toBeInTheDocument()
  })
})
