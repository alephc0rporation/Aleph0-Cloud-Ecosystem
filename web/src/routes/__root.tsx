import { Outlet, createRootRoute } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: () => (
    <main className="min-h-svh bg-background text-foreground">
      <Outlet />
    </main>
  ),
})
