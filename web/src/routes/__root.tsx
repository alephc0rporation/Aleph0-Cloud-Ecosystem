import { Outlet, createRootRouteWithContext } from '@tanstack/react-router'
import type { AuthContextProps } from 'react-oidc-context'

export interface RouterContext {
  auth: AuthContextProps
}

export const Route = createRootRouteWithContext<RouterContext>()({
  component: () => (
    <main className="min-h-svh bg-background text-foreground">
      <Outlet />
    </main>
  ),
})
