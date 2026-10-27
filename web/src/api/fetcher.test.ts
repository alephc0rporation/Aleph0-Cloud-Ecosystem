import { beforeEach, expect, it, vi } from 'vitest'
import { userManager } from '@/auth/oidc'
import { ApiError, apiFetch } from './fetcher'

vi.mock('@/auth/oidc', () => ({
  userManager: { getUser: vi.fn(), signinRedirect: vi.fn() },
}))

const fetchMock = vi.fn()

beforeEach(() => {
  vi.resetAllMocks()
  vi.stubGlobal('fetch', fetchMock)
})

it('sends the access token and returns the JSON body', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue({ access_token: 'tok' } as never)
  fetchMock.mockResolvedValue(new Response(JSON.stringify({ id: 'user-1' }), { status: 200 }))

  await expect(apiFetch('/v1/me', { method: 'GET' })).resolves.toEqual({ id: 'user-1' })

  const [url, init] = fetchMock.mock.calls[0]
  expect(url).toBe('/v1/me')
  expect(init.method).toBe('GET')
  expect(new Headers(init.headers).get('Authorization')).toBe('Bearer tok')
})

it('sends no Authorization header without a session', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue(null)
  fetchMock.mockResolvedValue(new Response('{}', { status: 200 }))

  await apiFetch('/v1/me')

  expect(new Headers(fetchMock.mock.calls[0][1].headers).has('Authorization')).toBe(false)
})

it.each([
  ['no session', null],
  ['an expired session', { access_token: 'old', expired: true }],
])('starts the login redirect on 401 with %s', async (_, user) => {
  vi.mocked(userManager.getUser).mockResolvedValue(user as never)
  fetchMock.mockResolvedValue(new Response(null, { status: 401 }))

  await expect(apiFetch('/v1/me')).rejects.toBeInstanceOf(ApiError)
  expect(userManager.signinRedirect).toHaveBeenCalledOnce()
})

// Keycloak still has a session, so a redirect would come straight back and loop forever.
it('does not redirect when the API rejects a fresh token', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue({ access_token: 'tok', expired: false } as never)
  fetchMock.mockResolvedValue(new Response(null, { status: 401 }))

  const err = await apiFetch('/v1/me').catch((e: unknown) => e)
  expect((err as ApiError).status).toBe(401)
  expect(userManager.signinRedirect).not.toHaveBeenCalled()
})

it('throws the problem details on other errors', async () => {
  vi.mocked(userManager.getUser).mockResolvedValue(null)
  const body = { type: 'about:blank', title: 'Internal Server Error', status: 500 }
  fetchMock.mockResolvedValue(new Response(JSON.stringify(body), { status: 500 }))

  const err = await apiFetch('/v1/me').catch((e: unknown) => e)
  expect(err).toBeInstanceOf(ApiError)
  expect((err as ApiError).status).toBe(500)
  expect((err as ApiError).problem).toEqual(body)
  expect(userManager.signinRedirect).not.toHaveBeenCalled()
})
