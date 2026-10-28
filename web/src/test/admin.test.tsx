import { render, screen, waitFor, within } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import { admin, mockFetch, resetApi, session, share, writer } from './helpers'
import type { Handler } from './helpers'

beforeEach(resetApi)
afterEach(() => vi.unstubAllGlobals())

function open(hash: string, routes: Record<string, Handler>) {
  window.location.hash = hash
  const m = mockFetch({ 'GET /api/session': session(admin), ...routes })
  render(<App />)
  return m
}

describe('share management', () => {
  const s1 = share({ id: 's1', name: 'Photos', path: 'D:\\Photos', readOnly: true })
  const s2 = share({ id: 's2', name: 'Broken', path: 'E:\\Gone', status: 'offline', problem: 'Drive E: is not connected.' })

  it('adds a share, read-only by default', async () => {
    const m = open('#/admin/shares', {
      'GET /api/shares': { json: [s1] },
      'POST /api/shares': { status: 201, json: share({ id: 's9', name: 'Music' }) },
    })
    const user = userEvent.setup()
    expect(await screen.findByText('D:\\Photos')).toBeInTheDocument()
    expect(screen.getByLabelText(/^Read-only/)).toBeChecked()
    await user.type(screen.getByLabelText('Name'), 'Music')
    await user.type(screen.getByLabelText('Folder path on the server'), 'D:\\Music')
    await user.click(screen.getByRole('button', { name: 'Add share' }))
    await waitFor(() => expect(m.find('POST', '/api/shares')).toHaveLength(1))
    expect(m.find('POST', '/api/shares')[0].body).toEqual({ name: 'Music', path: 'D:\\Music', readOnly: true })
  })

  it('shows the server error when adding fails', async () => {
    open('#/admin/shares', {
      'GET /api/shares': { json: [] },
      'POST /api/shares': { status: 400, json: { error: 'folder does not exist' } },
    })
    const user = userEvent.setup()
    expect(await screen.findByText('No folders are shared yet.')).toBeInTheDocument()
    await user.type(screen.getByLabelText('Name'), 'X')
    await user.type(screen.getByLabelText('Folder path on the server'), 'Z:\\nope')
    await user.click(screen.getByRole('button', { name: 'Add share' }))
    expect(await screen.findByText('folder does not exist')).toBeInTheDocument()
  })

  it('shows path, status and problem text, and toggles enable and read-only', async () => {
    const m = open('#/admin/shares', {
      'GET /api/shares': { json: [s1, s2] },
      'PATCH /api/shares/s1': { json: s1 },
    })
    const user = userEvent.setup()
    expect(await screen.findByText('Drive E: is not connected.')).toBeInTheDocument()
    expect(screen.getByText('E:\\Gone')).toBeInTheDocument()
    await user.click(screen.getByRole('button', { name: 'Disable share Photos' }))
    await waitFor(() => expect(m.find('PATCH', '/api/shares/s1')[0]?.body).toEqual({ enabled: false }))
    await user.click(screen.getByRole('button', { name: 'Make Photos read-write' }))
    await waitFor(() => expect(m.find('PATCH', '/api/shares/s1')[1]?.body).toEqual({ readOnly: false }))
    expect(m.find('PATCH', '/api/shares/s1')[0].headers['X-CSRF-Token']).toBe('csrf-token-1')
  })

  it('renames a share', async () => {
    const m = open('#/admin/shares', { 'GET /api/shares': { json: [s1] }, 'PATCH /api/shares/s1': { json: s1 } })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Rename share Photos' }))
    const input = screen.getByLabelText('Share name')
    await user.clear(input)
    await user.type(input, 'Pictures')
    await user.click(screen.getByRole('button', { name: 'Rename' }))
    await waitFor(() => expect(m.find('PATCH', '/api/shares/s1')[0]?.body).toEqual({ name: 'Pictures' }))
  })

  it('removes a share only after confirmation and says files are not deleted', async () => {
    const m = open('#/admin/shares', { 'GET /api/shares': { json: [s1] }, 'DELETE /api/shares/s1': { json: { status: 'removed' } } })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Remove share Photos' }))
    const dlg = screen.getByRole('dialog')
    expect(dlg).toHaveTextContent(/not.*deleted/)
    await user.click(within(dlg).getByRole('button', { name: 'Cancel' }))
    expect(m.find('DELETE', '/api/shares/s1')).toHaveLength(0)
    await user.click(screen.getByRole('button', { name: 'Remove share Photos' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Remove share' }))
    await waitFor(() => expect(m.find('DELETE', '/api/shares/s1')).toHaveLength(1))
  })
})

describe('user management', () => {
  const users = [admin, writer]
  it('lists users, adds a user with a role, changes role, disables and deletes', async () => {
    const m = open('#/admin/users', {
      'GET /api/admin/users': { json: users },
      'POST /api/admin/users': { status: 201, json: { ...writer, id: 9 } },
      'PATCH /api/admin/users/2': { json: writer },
      'DELETE /api/admin/users/2': { json: { status: 'deleted' } },
      'POST /api/admin/users/2/password': { json: { status: 'ok' } },
    })
    const user = userEvent.setup()
    expect(await screen.findByText('bob')).toBeInTheDocument()
    // The signed-in admin cannot demote, disable or delete themselves from the UI.
    expect(screen.queryByRole('button', { name: 'Delete alice' })).not.toBeInTheDocument()
    expect(screen.getByLabelText('Role for alice')).toBeDisabled()

    await user.type(screen.getByLabelText('Username'), 'dave')
    await user.type(screen.getByLabelText('Password'), 'long-password')
    await user.selectOptions(screen.getByLabelText('Role'), 'writer')
    await user.click(screen.getByRole('button', { name: 'Add user' }))
    await waitFor(() => expect(m.find('POST', '/api/admin/users')[0]?.body).toEqual({ username: 'dave', password: 'long-password', role: 'writer' }))

    await user.selectOptions(screen.getByLabelText('Role for bob'), 'reader')
    await waitFor(() => expect(m.find('PATCH', '/api/admin/users/2')[0]?.body).toEqual({ role: 'reader' }))
    await user.click(screen.getByRole('button', { name: 'Disable bob' }))
    await waitFor(() => expect(m.find('PATCH', '/api/admin/users/2')[1]?.body).toEqual({ disabled: true }))

    await user.click(screen.getByRole('button', { name: 'Reset password for bob' }))
    await user.type(screen.getByLabelText('New password'), 'another-password')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Reset password' }))
    await waitFor(() => expect(m.find('POST', '/api/admin/users/2/password')[0]?.body).toEqual({ password: 'another-password' }))
    expect(await screen.findByText('Password reset for bob.')).toBeInTheDocument()

    await user.click(screen.getByRole('button', { name: 'Delete bob' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Delete user' }))
    await waitFor(() => expect(m.find('DELETE', '/api/admin/users/2')).toHaveLength(1))
  })

  it('shows the server error when the last admin is protected', async () => {
    open('#/admin/users', {
      'GET /api/admin/users': { json: [admin, writer] },
      'PATCH /api/admin/users/2': { status: 409, json: { error: 'cannot remove the last administrator' } },
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Disable bob' }))
    expect(await screen.findByText('cannot remove the last administrator')).toBeInTheDocument()
  })
})

describe('dashboard', () => {
  const status = {
    version: '1.2.3', uptimeSeconds: 3725, pid: 1, goroutines: 12, heapBytes: 5 * 1024 * 1024, sysBytes: 20 * 1024 * 1024,
    activeTransfers: 1, maxTransfers: 16, listening: '[::]:8686', tls: false, urls: [], shares: 2, sharesUnavailable: 1, os: 'windows/amd64',
  }
  const routes = (net: unknown): Record<string, Handler> => ({
    'GET /api/admin/status': { json: status },
    'GET /api/network': { json: net },
    'GET /api/admin/transfers': { json: { active: 1, recent: [{ ts: 1700000000, user: 'bob', share: 'Photos', path: 'a.jpg', direction: 'upload', bytes: 2048, status: 'ok', ip: '192.168.1.5' }] } },
  })

  it('shows status, offline-share warning, URLs with copy buttons, QR and the HTTP note', async () => {
    open('#/admin/dashboard', routes({ urls: ['http://192.168.1.10:8686', 'http://10.0.0.4:8686'], addresses: [{ interface: 'Wi-Fi', ip: '192.168.1.10', private: true }], port: 8686, tls: false, listening: '[::]:8686' }))
    const user = userEvent.setup()
    expect(await screen.findByText('1h 2m')).toBeInTheDocument()
    expect(screen.getByText('1 of 16')).toBeInTheDocument()
    expect(screen.getByText(/1 shared folder is offline/)).toBeInTheDocument()
    expect(await screen.findByText('http://192.168.1.10:8686')).toBeInTheDocument()
    expect(screen.getAllByRole('button', { name: /^Copy http/ })).toHaveLength(2)
    expect(screen.getByRole('img', { name: /QR code for http:\/\/192.168.1.10:8686/ })).toHaveAttribute('src', '/api/qr.png?i=0')
    await user.click(screen.getByRole('radio', { name: 'http://10.0.0.4:8686' }))
    expect(screen.getByRole('img', { name: /10.0.0.4/ })).toHaveAttribute('src', '/api/qr.png?i=1')
    expect(screen.getByText(/plain HTTP, which is not encrypted/)).toBeInTheDocument()
    expect(await screen.findByText('Photos/a.jpg')).toBeInTheDocument()
  })

  it('copies a URL to the clipboard', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    open('#/admin/dashboard', routes({ urls: ['http://192.168.1.10:8686'], addresses: [{ interface: 'Wi-Fi', ip: '192.168.1.10', private: true }], port: 8686, tls: false, listening: '' }))
    const user = userEvent.setup()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    await user.click(await screen.findByRole('button', { name: /^Copy http/ }))
    expect(writeText).toHaveBeenCalledWith('http://192.168.1.10:8686')
    expect((await screen.findAllByText(/Copied/)).length).toBeGreaterThan(0)
  })

  it('warns when there is no network connection', async () => {
    open('#/admin/dashboard', routes({ urls: ['http://localhost:8686'], addresses: null, port: 8686, tls: false, listening: '' }))
    expect(await screen.findByText(/No active network connection/)).toBeInTheDocument()
  })

  it('asks for confirmation before restarting', async () => {
    const m = open('#/admin/dashboard', { ...routes({ urls: [], addresses: [], port: 1, tls: false, listening: '' }), 'POST /api/admin/restart': { status: 202, json: { status: 'restarting' } } })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Restart server' }))
    const dlg = screen.getByRole('dialog')
    await user.click(within(dlg).getByRole('button', { name: 'Cancel' }))
    expect(m.find('POST', '/api/admin/restart')).toHaveLength(0)
  })
})

describe('settings', () => {
  const settings = { port: 8686, bindAddr: '0.0.0.0', maxUploadBytes: 2 * 1024 ** 3, maxTransfers: 16, sessionHours: 24, tlsMode: 'off', tlsCert: '', tlsKey: '' }

  it('uses friendly units, saves, and shows restart required', async () => {
    const m = open('#/admin/settings', {
      'GET /api/admin/settings': { json: settings },
      'PUT /api/admin/settings': (c) => ({ json: { settings: c.body, restartRequired: true } }),
    })
    const user = userEvent.setup()
    const max = await screen.findByLabelText('Maximum upload size')
    expect(max).toHaveValue(2)
    expect(screen.getByLabelText('Upload size unit')).toHaveValue('GB')
    await user.selectOptions(screen.getByLabelText('Upload size unit'), 'MB')
    await user.clear(max)
    await user.type(max, '500')
    const port = screen.getByLabelText('Port')
    await user.clear(port)
    await user.type(port, '9000')
    await user.selectOptions(screen.getByLabelText('Encryption (HTTPS)'), 'custom')
    await user.type(screen.getByLabelText('Certificate file path'), 'C:\\c.pem')
    await user.type(screen.getByLabelText('Private key file path'), 'C:\\k.pem')
    await user.click(screen.getByRole('button', { name: 'Save settings' }))
    expect(await screen.findByText(/Restart required/)).toBeInTheDocument()
    expect(m.find('PUT', '/api/admin/settings')[0].body).toEqual({ ...settings, port: 9000, maxUploadBytes: 500 * 1024 ** 2, tlsMode: 'custom', tlsCert: 'C:\\c.pem', tlsKey: 'C:\\k.pem' })
    expect(screen.getByText(/self-signed certificate gives encryption/)).toBeInTheDocument()
  })

  it('does not claim a restart is needed when the server says it is not', async () => {
    open('#/admin/settings', {
      'GET /api/admin/settings': { json: settings },
      'PUT /api/admin/settings': (c) => ({ json: { settings: c.body, restartRequired: false } }),
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Save settings' }))
    expect(await screen.findByText('Settings saved.')).toBeInTheDocument()
    expect(screen.queryByText(/Restart required/)).not.toBeInTheDocument()
  })

  it('shows a validation error from the server', async () => {
    open('#/admin/settings', {
      'GET /api/admin/settings': { json: settings },
      'PUT /api/admin/settings': { status: 400, json: { error: 'port must be 1-65535' } },
    })
    const user = userEvent.setup()
    await user.click(await screen.findByRole('button', { name: 'Save settings' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('port must be 1-65535')
  })
})

describe('diagnostics', () => {
  it('shows disk space with a low-disk warning, a copyable firewall command, logs, audit and backup link', async () => {
    open('#/admin/diagnostics', {
      'GET /api/admin/diagnose': {
        json: {
          database: true, listening: '[::]:8686', port: 8686, tls: false, urls: [], dataDir: 'C:\\ProgramData\\HomeServer',
          shares: [
            { name: 'Photos', status: 'online', freeBytes: 500 * 1024 ** 2, totalBytes: 100 * 1024 ** 3, lowDisk: true },
            { name: 'Gone', status: 'offline', problem: 'Drive E: is not connected.' },
          ],
          hints: ['If other devices cannot connect:', 'netsh advfirewall firewall add rule name="HomeServer" dir=in action=allow protocol=TCP localport=8686 profile=private', 'Router isolation off.'],
        },
      },
      'GET /api/admin/logs': { json: { lines: ['line one', 'line two'] } },
      'GET /api/admin/audit': { json: [{ ts: 1700000000, actor: 'alice', ip: '127.0.0.1', event: 'login.ok', detail: '' }] },
    })
    expect(await screen.findByText(/Low disk space/)).toBeInTheDocument()
    expect(screen.getByText(/500 MB of 100 GB/)).toBeInTheDocument()
    expect(screen.getByLabelText('Firewall command')).toHaveTextContent('netsh advfirewall')
    expect(screen.getByRole('button', { name: /^Copy command/ })).toBeInTheDocument()
    expect(await screen.findByLabelText('Server log')).toHaveTextContent('line one')
    expect(await screen.findByText('login.ok')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Download backup' })).toHaveAttribute('href', '/api/admin/backup')
  })

  it('shows empty states for log and audit', async () => {
    open('#/admin/diagnostics', {
      'GET /api/admin/diagnose': { json: { database: true, listening: '', port: 1, tls: false, urls: [], dataDir: 'x', shares: [], hints: [] } },
      'GET /api/admin/logs': { json: { lines: [] } },
      'GET /api/admin/audit': { json: [] },
    })
    expect(await screen.findByText('The log is empty.')).toBeInTheDocument()
    expect(await screen.findByText('No events recorded yet.')).toBeInTheDocument()
    expect(screen.getByText('No shares configured.')).toBeInTheDocument()
  })
})
