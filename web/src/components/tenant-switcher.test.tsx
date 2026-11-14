// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { useListTenants } from '@/api/gen/api'
import { TenantSwitcher } from './tenant-switcher'

const navigate = vi.hoisted(() => vi.fn())
vi.mock('@tanstack/react-router', () => ({ useNavigate: () => navigate }))
vi.mock('@/api/gen/api', () => ({ useListTenants: vi.fn() }))
afterEach(cleanup)

it("lists the tenants with the caller's role and opens the one picked", async () => {
  vi.mocked(useListTenants).mockReturnValue({
    data: [
      { id: 't1', name: 'acme', role: 'owner', createdAt: '2026-11-09T10:00:00Z' },
      { id: 't2', name: 'beta', role: 'member', createdAt: '2026-11-09T11:00:00Z' },
    ],
  } as never)
  render(<TenantSwitcher tenantId="t1" />)

  screen.getByRole('option', { name: 'acme (owner)' })
  screen.getByRole('option', { name: 'beta (member)' })
  await userEvent.selectOptions(screen.getByRole('combobox'), 't2')
  expect(navigate).toHaveBeenCalledWith({ to: '/t/$tenantId/projects', params: { tenantId: 't2' } })

  await userEvent.selectOptions(screen.getByRole('combobox'), 'new')
  expect(navigate).toHaveBeenCalledWith({ to: '/tenants/new' })
})
