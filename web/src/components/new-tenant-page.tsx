import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { getListTenantsQueryKey, useCreateTenant } from '@/api/gen/api'
import { CreateTenantBody } from '@/api/gen/api.zod'
import { NameForm } from '@/components/name-form'

export function NewTenantPage() {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const create = useCreateTenant()

  return (
    <div className="mx-auto grid max-w-md gap-4 p-4 pt-16">
      <h1 className="text-xl font-semibold">Create your tenant</h1>
      <p className="text-sm text-muted-foreground">
        A tenant holds your projects and the people who work on them. You can create more later.
      </p>
      <NameForm
        label="Tenant name"
        submitLabel="Create tenant"
        pendingLabel="Creating…"
        schema={CreateTenantBody.shape.name}
        onSubmit={async (name) => {
          const tenant = await create.mutateAsync({ data: { name } })
          await queryClient.invalidateQueries({ queryKey: getListTenantsQueryKey() })
          await navigate({ to: '/t/$tenantId/projects', params: { tenantId: tenant.id } })
        }}
      />
    </div>
  )
}
