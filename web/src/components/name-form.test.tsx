// @vitest-environment jsdom
import { cleanup, render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { afterEach, expect, it, vi } from 'vitest'
import { ApiError } from '@/api/fetcher'
import { CreateTenantBody } from '@/api/gen/api.zod'
import { NameForm } from './name-form'

vi.mock('@/auth/oidc', () => ({ userManager: {} }))
afterEach(cleanup)

function renderForm(onSubmit: (name: string) => Promise<unknown>) {
  render(
    <NameForm label="Tenant name" submitLabel="Create" pendingLabel="Creating…"
      schema={CreateTenantBody.shape.name} onSubmit={onSubmit} />,
  )
}

it('trims the name and submits it', async () => {
  const onSubmit = vi.fn().mockResolvedValue(undefined)
  renderForm(onSubmit)
  await userEvent.type(screen.getByLabelText('Tenant name'), '  acme  ')
  await userEvent.click(screen.getByRole('button', { name: 'Create' }))
  expect(onSubmit).toHaveBeenCalledWith('acme')
})

it('refuses an empty name without calling the API', async () => {
  const onSubmit = vi.fn()
  renderForm(onSubmit)
  await userEvent.click(screen.getByRole('button', { name: 'Create' }))
  expect(onSubmit).not.toHaveBeenCalled()
  expect(screen.getByRole('alert').textContent).toBe('Use 1 to 64 characters.')
})

it("shows the API's message when the request fails", async () => {
  const onSubmit = vi.fn().mockRejectedValue(
    new ApiError(403, { type: 'about:blank', title: 'Forbidden', status: 403, detail: 'you can create at most 3 tenants' }),
  )
  renderForm(onSubmit)
  await userEvent.type(screen.getByLabelText('Tenant name'), 'acme')
  await userEvent.click(screen.getByRole('button', { name: 'Create' }))
  expect((await screen.findByRole('alert')).textContent).toBe('you can create at most 3 tenants')
})
