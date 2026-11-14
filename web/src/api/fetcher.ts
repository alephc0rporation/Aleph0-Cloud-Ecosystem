import { userManager } from '@/auth/oidc'
import type { Problem } from './gen/api'

export class ApiError extends Error {
  readonly status: number
  readonly problem?: Problem

  constructor(status: number, problem?: Problem) {
    super(problem?.detail || problem?.title || `HTTP ${status}`)
    this.status = status
    this.problem = problem
  }
}

// Mutator used by the orval-generated client: adds the access token and turns
// non-2xx responses into ApiError.
export async function apiFetch<T>(url: string, init: RequestInit = {}): Promise<T> {
  const user = await userManager.getUser()
  const headers = new Headers(init.headers)
  if (user?.access_token) headers.set('Authorization', `Bearer ${user.access_token}`)

  const res = await fetch(url, { ...init, headers })
  // Log in again only without a usable session. A 401 for a fresh token means the API rejects
  // it (wrong aud or iss), and a redirect would bounce through Keycloak forever.
  if (res.status === 401 && (!user || user.expired)) {
    await userManager.signinRedirect()
    throw new ApiError(401)
  }
  if (!res.ok) {
    throw new ApiError(res.status, await res.json().catch(() => undefined))
  }
  return (res.status === 204 ? undefined : await res.json()) as T
}

// The message to show for a failed request: the API's detail when it gave one.
export function errorMessage(err: unknown): string {
  return err instanceof ApiError ? err.message : 'Something went wrong. Try again.'
}
