import { createFileRoute } from '@tanstack/react-router'
import { ProjectPage } from '@/components/project-page'

export const Route = createFileRoute('/t/$tenantId/projects/$projectId')({ component: Project })

function Project() {
  return <ProjectPage projectId={Route.useParams().projectId} />
}
