import { createFileRoute } from '@tanstack/react-router'
import { ProjectsPage } from '@/components/projects-page'

export const Route = createFileRoute('/t/$tenantId/projects/')({ component: Projects })

function Projects() {
  return <ProjectsPage tenantId={Route.useParams().tenantId} />
}
