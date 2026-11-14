import { useNavigate } from '@tanstack/react-router'
import { useListTenants } from '@/api/gen/api'

const NEW = 'new'

export function TenantSwitcher({ tenantId }: { tenantId: string }) {
  const navigate = useNavigate()
  const tenants = useListTenants()
  if (!tenants.data) return null
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="sr-only">Tenant</span>
      <select
        className="rounded-md border bg-background px-2 py-1"
        value={tenantId}
        onChange={(e) =>
          void (e.target.value === NEW
            ? navigate({ to: '/tenants/new' })
            : navigate({ to: '/t/$tenantId/projects', params: { tenantId: e.target.value } }))
        }
      >
        {tenants.data.map((t) => (
          <option key={t.id} value={t.id}>
            {t.name} ({t.role})
          </option>
        ))}
        <option value={NEW}>Create tenant…</option>
      </select>
    </label>
  )
}
