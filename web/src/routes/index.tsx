import { Navigate, createFileRoute } from '@tanstack/react-router'
import { useAuth } from 'react-oidc-context'
import { errorMessage } from '@/api/fetcher'
import { useListTenants } from '@/api/gen/api'
import { requireAuth } from '@/auth/require-auth'

export const Route = createFileRoute('/')({ beforeLoad: requireAuth, component: Home })

// Opens the first tenant, or asks for one.
function Home() {
  const auth = useAuth()
  const tenants = useListTenants({ query: { enabled: auth.isAuthenticated } })
  if (!auth.isAuthenticated || tenants.isPending) return null
  if (tenants.isError) {
    return (
      <p role="alert" className="p-4 text-destructive">
        {errorMessage(tenants.error)}
      </p>
    )
  }
  if (tenants.data.length === 0) return <Navigate to="/tenants/new" />
  return <Navigate to="/t/$tenantId/projects" params={{ tenantId: tenants.data[0].id }} />
}
