import { createFileRoute } from '@tanstack/react-router'
import { requireAuth } from '@/auth/require-auth'
import { NewTenantPage } from '@/components/new-tenant-page'

export const Route = createFileRoute('/tenants/new')({ beforeLoad: requireAuth, component: NewTenantPage })
