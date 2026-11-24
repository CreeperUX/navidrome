import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import authProvider from '../authProvider'
import subsonic from '../subsonic'

vi.mock('../config', () => ({ default: {} }))

// a JWT-shaped token jwtDecode accepts
const session =
  'eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiJhbGljZSJ9.c2lnbmF0dXJl'

const respond = (status, body) =>
  vi.fn(() =>
    Promise.resolve({
      status,
      statusText: status === 200 ? 'OK' : 'Unauthorized',
      json: () => Promise.resolve(body),
    }),
  )

describe('Sonus: two-step verification at sign-in (authProvider)', () => {
  beforeEach(() => {
    // the page's relative /auth/login, as a browser resolves it
    vi.stubGlobal(
      'Request',
      class {
        constructor(url, init) {
          Object.assign(this, init, { url })
        }
        text() {
          return Promise.resolve(this.body)
        }
      },
    )
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('sends the code along, and tells the sign-in page when one is wanted', async () => {
    const fetch = respond(401, {
      error: 'Verification code required',
      twoFactor: 'code',
    })
    vi.stubGlobal('fetch', fetch)
    await expect(
      authProvider.login({
        username: 'alice',
        password: 'secret',
        otp: '123456',
      }),
    ).rejects.toMatchObject({ twoFactor: 'code' })
    const request = fetch.mock.calls[0][0]
    expect(JSON.parse(await request.text())).toEqual({
      username: 'alice',
      password: 'secret',
      otp: '123456',
    })
  })

  it('hands over the setup token when it must be set up first', async () => {
    vi.stubGlobal(
      'fetch',
      respond(401, { twoFactor: 'setup', setupToken: 'setup-jwt' }),
    )
    await expect(
      authProvider.login({ username: 'alice', password: 'secret' }),
    ).rejects.toMatchObject({
      twoFactor: 'setup',
      setupToken: 'setup-jwt',
      enrolmentCodeRequired: false,
    })
  })

  it('tells the sign-in page when the set-up takes an enrolment code (the Sonus server)', async () => {
    vi.stubGlobal(
      'fetch',
      respond(401, {
        twoFactor: 'setup',
        setupToken: 'setup-jwt',
        enrolmentCodeRequired: true,
      }),
    )
    await expect(
      authProvider.login({ username: 'alice', password: 'secret' }),
    ).rejects.toMatchObject({
      twoFactor: 'setup',
      enrolmentCodeRequired: true,
    })
  })

  it('keeps no password-derived Subsonic token when the server sends none', async () => {
    localStorage.setItem('subsonic-token', 'old')
    localStorage.setItem('subsonic-salt', 'old')
    vi.stubGlobal(
      'fetch',
      respond(200, {
        id: '1',
        name: 'Alice',
        username: 'alice',
        token: session,
      }),
    )
    await authProvider.login({
      username: 'alice',
      password: 'secret',
      otp: '123456',
    })
    expect(localStorage.getItem('token')).toBe(session)
    expect(localStorage.getItem('subsonic-token')).toBeNull()
    expect(localStorage.getItem('subsonic-salt')).toBeNull()
  })

  it('signs in with what a confirmed setup answered, without calling the server again', async () => {
    const fetch = vi.fn()
    vi.stubGlobal('fetch', fetch)
    await authProvider.login({
      twoFactorResult: {
        id: '1',
        name: 'Alice',
        username: 'alice',
        token: session,
      },
    })
    expect(fetch).not.toHaveBeenCalled()
    expect(localStorage.getItem('is-authenticated')).toBe('true')
  })
})

describe('Sonus: Subsonic calls signed by the session (subsonic.url)', () => {
  beforeEach(() => localStorage.clear())

  it('uses the session token when there is no password-derived one', () => {
    localStorage.setItem('username', 'alice')
    localStorage.setItem('token', session)
    const url = subsonic.url('ping')
    expect(url).toContain('u=alice')
    expect(url).toContain(`jwt=${session}`)
    expect(url).not.toContain('&t=')
  })

  it('keeps the token and salt when they are there', () => {
    localStorage.setItem('username', 'alice')
    localStorage.setItem('token', session)
    localStorage.setItem('subsonic-token', 'tok')
    localStorage.setItem('subsonic-salt', 'salt')
    const url = subsonic.url('ping')
    expect(url).toContain('t=tok')
    expect(url).not.toContain('jwt=')
  })
})
