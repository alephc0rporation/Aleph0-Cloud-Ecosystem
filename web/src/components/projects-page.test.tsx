// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import type { ReactNode } from 'react'
import { afterEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/fetcher'
import { useCreateProject, useDeleteProject, useListProjects } from '@/api/gen/api'
import { renderWithClient } from '@/test/render'
import { ProjectsPage } from './projects-page'

vi.mock('@/auth/oidc', () => ({ userManager: {} }))
vi.mock('@tanstack/react-router', () => ({ Link: ({ children }: { children: ReactNode }) => <a>{children}</a> }))
vi.mock('@/api/gen/api', () => ({
  useListProjects: vi.fn(),
  useCreateProject: vi.fn(),
  useDeleteProject: vi.fn(),
  getListProjectsQueryKey: (tenantId: string) => [`/v1/tenants/${tenantId}/projects`],
}))
afterEach(cleanup)

const project = (name: string, status: string) => ({
  id: `id-${name}`, tenantId: 't1', name, status, createdAt: '2026-11-09T10:00:00Z',
})

function mockHooks(remove: Record<string, unknown> = {}) {
  vi.mocked(useListProjects).mockReturnValue({
    isPending: false, isError: false,
    data: [project('web', 'ready'), project('db', 'failed'), project('new', 'provisioning')],
  } as never)
  vi.mocked(useCreateProject).mockReturnValue({ mutateAsync: vi.fn() } as never)
  vi.mocked(useDeleteProject).mockReturnValue({ mutate: vi.fn(), isPending: false, isError: false, ...remove } as never)
}

it('lists every project with its status and a delete button', () => {
  mockHooks()
  renderWithClient(<ProjectsPage tenantId="t1" />)
  for (const status of ['ready', 'failed', 'provisioning']) screen.getByText(status)
  expect(screen.getAllByRole('button', { name: 'Delete' })).toHaveLength(3)
})

it('deletes the project the button belongs to', async () => {
  const mutate = vi.fn()
  mockHooks({ mutate })
  renderWithClient(<ProjectsPage tenantId="t1" />)
  await userEvent.click(screen.getAllByRole('button', { name: 'Delete' })[1])
  expect(mutate).toHaveBeenCalledWith({ projectId: 'id-db' })
})

it("shows the API's message when a delete is refused", () => {
  mockHooks({
    isError: true,
    error: new ApiError(409, { type: 'about:blank', title: 'Conflict', status: 409, detail: "delete the project's VMs first" }),
  })
  renderWithClient(<ProjectsPage tenantId="t1" />)
  expect(screen.getByRole('alert').textContent).toBe("delete the project's VMs first")
})
