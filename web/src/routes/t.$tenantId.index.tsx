import { createFileRoute, redirect } from '@tanstack/react-router'

export const Route = createFileRoute('/t/$tenantId/')({
  beforeLoad: ({ params }) => {
    throw redirect({ to: '/t/$tenantId/projects', params })
  },
})
