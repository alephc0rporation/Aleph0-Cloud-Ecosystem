import { createFileRoute } from '@tanstack/react-router'
import { TenantSettings } from '@/components/tenant-settings'

export const Route = createFileRoute('/t/$tenantId/settings')({ component: Settings })

function Settings() {
  return <TenantSettings tenantId={Route.useParams().tenantId} />
}
