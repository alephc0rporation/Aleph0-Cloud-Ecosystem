import { useQueryClient } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { errorMessage } from '@/api/fetcher'
import { getListTenantsQueryKey, useDeleteTenant, useGetTenant } from '@/api/gen/api'
import { Button } from '@/components/ui/button'

export function TenantSettings({ tenantId }: { tenantId: string }) {
  const navigate = useNavigate()
  const queryClient = useQueryClient()
  const tenant = useGetTenant(tenantId)
  const remove = useDeleteTenant({
    mutation: {
      onSuccess: async () => {
        await queryClient.invalidateQueries({ queryKey: getListTenantsQueryKey() })
        await navigate({ to: '/' })
      },
    },
  })

  if (tenant.isPending) return <p className="text-muted-foreground">Loading…</p>
  if (tenant.isError) {
    return (
      <p role="alert" className="text-destructive">
        {errorMessage(tenant.error)}
      </p>
    )
  }
  return (
    <section className="grid gap-4">
      <h1 className="text-xl font-semibold">Settings</h1>
      {tenant.data.role !== 'owner' ? (
        <p className="text-sm text-muted-foreground">Only an owner can delete this tenant.</p>
      ) : (
        <div className="grid justify-items-start gap-2">
          <p className="text-sm">Deleting the tenant works once it has no projects.</p>
          <Button
            variant="destructive"
            disabled={remove.isPending}
            onClick={() => {
              if (window.confirm(`Delete the tenant ${tenant.data.name}?`)) remove.mutate({ tenantId })
            }}
          >
            Delete tenant
          </Button>
          {remove.isError && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage(remove.error)}
            </p>
          )}
        </div>
      )}
    </section>
  )
}
