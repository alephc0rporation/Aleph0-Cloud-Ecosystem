// @vitest-environment jsdom
import { cleanup, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { useCreateTenant } from '@/api/gen/api'
import { renderWithClient } from '@/test/render'
import { NewTenantPage } from './new-tenant-page'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@/auth/oidc', () => ({ userManager: {} }))
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
vi.mock('@/api/gen/api', () => ({ useCreateTenant: vi.fn(), getListTenantsQueryKey: () => ['/v1/tenants'] }))
afterEach(cleanup)

it('creates the tenant and opens it', async () => {
  const mutateAsync = vi.fn().mockResolvedValue({ id: 't1', name: 'acme', role: 'owner', createdAt: '2026-11-09T10:00:00Z' })
  vi.mocked(useCreateTenant).mockReturnValue({ mutateAsync } as never)
  renderWithClient(<NewTenantPage />)

  await userEvent.type(screen.getByLabelText('Tenant name'), 'acme')
  await userEvent.click(screen.getByRole('button', { name: 'Create tenant' }))

  expect(mutateAsync).toHaveBeenCalledWith({ data: { name: 'acme' } })
  expect(navigate).toHaveBeenCalledWith({ to: '/t/$tenantId/projects', params: { tenantId: 't1' } })
})
