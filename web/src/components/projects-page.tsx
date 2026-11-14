import { useQueryClient } from '@tanstack/react-query'
import { Link } from '@tanstack/react-router'
import { errorMessage } from '@/api/fetcher'
import { getListProjectsQueryKey, useCreateProject, useDeleteProject, useListProjects } from '@/api/gen/api'
import { CreateProjectBody } from '@/api/gen/api.zod'
import { NameForm } from '@/components/name-form'
import { Button } from '@/components/ui/button'

export function ProjectsPage({ tenantId }: { tenantId: string }) {
  const queryClient = useQueryClient()
  const projects = useListProjects(tenantId)
  const refresh = () => queryClient.invalidateQueries({ queryKey: getListProjectsQueryKey(tenantId) })
  const create = useCreateProject({ mutation: { onSettled: refresh } })
  const remove = useDeleteProject({ mutation: { onSettled: refresh } })

  return (
    <section className="grid gap-6">
      <h1 className="text-xl font-semibold">Projects</h1>
      <NameForm
        label="New project"
        submitLabel="Create project"
        pendingLabel="Creating…"
        schema={CreateProjectBody.shape.name}
        onSubmit={(name) => create.mutateAsync({ tenantId, data: { name } })}
      />
      {remove.isError && (
        <p role="alert" className="text-sm text-destructive">
          {errorMessage(remove.error)}
        </p>
      )}
      {projects.isPending ? (
        <p className="text-muted-foreground">Loading…</p>
      ) : projects.isError ? (
        <p className="text-destructive">Could not load the projects.</p>
      ) : projects.data.length === 0 ? (
        <p className="text-muted-foreground">No projects yet.</p>
      ) : (
        <table className="w-full text-left text-sm">
          <thead className="text-muted-foreground">
            <tr>
              <th className="py-1">Name</th>
              <th>Status</th>
              <th>Created</th>
              <th />
            </tr>
          </thead>
          <tbody>
            {projects.data.map((p) => (
              <tr key={p.id} className="border-t">
                <td className="py-2">
                  <Link to="/t/$tenantId/projects/$projectId" params={{ tenantId, projectId: p.id }} className="underline">
                    {p.name}
                  </Link>
                </td>
                <td>{p.status}</td>
                <td>{new Date(p.createdAt).toLocaleString()}</td>
                <td className="text-right">
                  <Button variant="outline" size="sm" disabled={remove.isPending} onClick={() => remove.mutate({ projectId: p.id })}>
                    Delete
                  </Button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  )
}
