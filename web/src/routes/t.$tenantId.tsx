import { Link, Outlet, createFileRoute } from '@tanstack/react-router'
import { useAuth } from 'react-oidc-context'
import { requireAuth } from '@/auth/require-auth'
import { Button } from '@/components/ui/button'

export const Route = createFileRoute('/t/$tenantId')({
  beforeLoad: requireAuth,
  component: TenantLayout,
})

function TenantLayout() {
  const auth = useAuth()
  const { tenantId } = Route.useParams()
  // The browser is already on its way to Keycloak.
  if (!auth.isAuthenticated) return null
  return (
    <div className="mx-auto grid max-w-4xl gap-6 p-4">
      <header className="flex flex-wrap items-center gap-4 border-b pb-3">
        <span className="font-semibold">Aleph0 Cardinal</span>
        <nav className="flex gap-3 text-sm">
          <Link to="/t/$tenantId/projects" params={{ tenantId }} className="[&.active]:font-semibold">
            Projects
          </Link>
        </nav>
        <span className="ml-auto text-sm text-muted-foreground">{auth.user?.profile.email}</span>
        <Button variant="outline" size="sm" onClick={() => void auth.signoutRedirect()}>
          Log out
        </Button>
      </header>
      <Outlet />
    </div>
  )
}
