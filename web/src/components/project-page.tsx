import { errorMessage } from '@/api/fetcher'
import { useGetProject, useListRegions } from '@/api/gen/api'

export function ProjectPage({ projectId }: { projectId: string }) {
  const project = useGetProject(projectId)
  const regions = useListRegions()
  if (project.isPending) return <p className="text-muted-foreground">Loading…</p>
  if (project.isError) {
    return (
      <p role="alert" className="text-destructive">
        {errorMessage(project.error)}
      </p>
    )
  }
  return (
    <section className="grid gap-4">
      <h1 className="text-xl font-semibold">{project.data.name}</h1>
      <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1 text-sm">
        <dt className="text-muted-foreground">Status</dt>
        <dd>{project.data.status}</dd>
        <dt className="text-muted-foreground">Regions</dt>
        <dd>{regions.data?.map((r) => r.name).join(', ') ?? '…'}</dd>
        <dt className="text-muted-foreground">Created</dt>
        <dd>{new Date(project.data.createdAt).toLocaleString()}</dd>
      </dl>
    </section>
  )
}
