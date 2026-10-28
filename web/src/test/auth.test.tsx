import { render, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from '../App'
import { admin, mockFetch, resetApi, session } from './helpers'

beforeEach(resetApi)
afterEach(() => vi.unstubAllGlobals())

describe('login', () => {
  it('signs in and shows the app', async () => {
    let loggedIn = false
    const m = mockFetch({
      'GET /api/session': () => (loggedIn ? session(admin) : { status: 401, json: { error: 'login required' } }),
      'GET /api/setup-status': { json: { needsSetup: false, local: true } },
      'POST /api/login': () => {
        loggedIn = true
        return session(admin)
      },
      'GET /api/shares': { json: [] },
    })
    render(<App />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'secret-pass')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('navigation', { name: 'Main' })).toBeInTheDocument()
    expect(m.find('POST', '/api/login')[0].body).toEqual({ username: 'alice', password: 'secret-pass' })
  })

  it('shows the server error for a wrong password and stays on the form', async () => {
    mockFetch({
      'GET /api/session': { status: 401, json: { error: 'login required' } },
      'GET /api/setup-status': { json: { needsSetup: false, local: false } },
      'POST /api/login': { status: 401, json: { error: 'invalid username or password' } },
    })
    render(<App />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'nope')
    await user.click(screen.getByRole('button', { name: 'Sign in' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('invalid username or password')
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeEnabled()
  })

  it('goes back to login with a notice when a call returns 401', async () => {
    window.location.hash = '#/files'
    mockFetch({
      'GET /api/session': session(admin),
      'GET /api/shares': { status: 401, json: { error: 'login required' } },
      'GET /api/setup-status': { json: { needsSetup: false, local: true } },
    })
    render(<App />)
    expect(await screen.findByText(/Your session ended/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument()
  })

  it('shows a retry when the server cannot be reached', async () => {
    vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')))
    render(<App />)
    expect(await screen.findByRole('alert')).toHaveTextContent(/Cannot reach the server/)
    expect(screen.getByRole('button', { name: 'Try again' })).toBeInTheDocument()
  })
})

describe('first-run setup', () => {
  it('creates the administrator on the server computer', async () => {
    let done = false
    const m = mockFetch({
      'GET /api/session': () => (done ? session(admin) : { status: 401, json: { error: 'login required' } }),
      'GET /api/setup-status': { json: { needsSetup: true, local: true } },
      'POST /api/setup': () => {
        done = true
        return session(admin)
      },
      'GET /api/shares': { json: [] },
    })
    render(<App />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Administrator username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'long-password-1')
    await user.type(screen.getByLabelText('Repeat password'), 'different')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('do not match')
    expect(m.find('POST', '/api/setup')).toHaveLength(0)

    await user.clear(screen.getByLabelText('Repeat password'))
    await user.type(screen.getByLabelText('Repeat password'), 'long-password-1')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))
    await waitFor(() => expect(m.find('POST', '/api/setup')).toHaveLength(1))
    expect(m.find('POST', '/api/setup')[0].body).toEqual({ username: 'alice', password: 'long-password-1' })
    expect(await screen.findByRole('navigation', { name: 'Main' })).toBeInTheDocument()
  })

  it('tells remote visitors to open localhost on the server', async () => {
    mockFetch({
      'GET /api/session': { status: 401, json: { error: 'login required' } },
      'GET /api/setup-status': { json: { needsSetup: true, local: false } },
    })
    render(<App />)
    expect(await screen.findByText(/http:\/\/localhost/)).toBeInTheDocument()
    expect(screen.queryByLabelText('Administrator username')).not.toBeInTheDocument()
  })

  it('surfaces a server error during setup', async () => {
    mockFetch({
      'GET /api/session': { status: 401, json: { error: 'login required' } },
      'GET /api/setup-status': { json: { needsSetup: true, local: true } },
      'POST /api/setup': { status: 400, json: { error: 'password must be at least 8 characters' } },
    })
    render(<App />)
    const user = userEvent.setup()
    await user.type(await screen.findByLabelText('Administrator username'), 'alice')
    await user.type(screen.getByLabelText('Password'), 'short')
    await user.type(screen.getByLabelText('Repeat password'), 'short')
    await user.click(screen.getByRole('button', { name: 'Create administrator' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('at least 8 characters')
  })
})

describe('account', () => {
  it('shows the user, changes the password and logs out', async () => {
    window.location.hash = '#/account'
    let out = false
    const m = mockFetch({
      'GET /api/session': () => (out ? { status: 401, json: { error: 'login required' } } : session(admin)),
      'POST /api/password': session(admin),
      'POST /api/logout': () => {
        out = true
        return { json: { status: 'ok' } }
      },
      'GET /api/setup-status': { json: { needsSetup: false, local: true } },
    })
    render(<App />)
    const user = userEvent.setup()
    expect(await screen.findByRole('heading', { name: 'Account' })).toBeInTheDocument()
    await user.type(screen.getByLabelText('Current password'), 'old-password')
    await user.type(screen.getByLabelText('New password'), 'new-password-1')
    await user.type(screen.getByLabelText('Repeat new password'), 'new-password-1')
    await user.click(screen.getByRole('button', { name: 'Change password' }))
    expect(await screen.findByText(/Password changed/)).toBeInTheDocument()
    const call = m.find('POST', '/api/password')[0]
    expect(call.body).toEqual({ current: 'old-password', new: 'new-password-1' })
    expect(call.headers['X-CSRF-Token']).toBe('csrf-token-1')

    await user.click(screen.getByRole('button', { name: 'Log out' }))
    expect(await screen.findByRole('button', { name: 'Sign in' })).toBeInTheDocument()
    expect(m.find('POST', '/api/logout')).toHaveLength(1)
  })
})
