import type { RouterContext } from '@/routes/__root'

// beforeLoad for protected routes: without a session, go to Keycloak.
export async function requireAuth({ context }: { context: RouterContext }) {
  if (!context.auth.isAuthenticated) await context.auth.signinRedirect()
}
