import { useId, useState, type FormEvent } from 'react'
import type { ZodType } from 'zod'
import { errorMessage } from '@/api/fetcher'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'

interface NameFormProps {
  label: string
  submitLabel: string
  pendingLabel: string
  // The name field from the generated Zod schemas, so the form checks what the API checks.
  schema: ZodType<string>
  onSubmit: (name: string) => Promise<unknown>
}

export function NameForm({ label, submitLabel, pendingLabel, schema, onSubmit }: NameFormProps) {
  const id = useId()
  const [name, setName] = useState('')
  const [error, setError] = useState<string>()
  const [pending, setPending] = useState(false)

  async function submit(e: FormEvent) {
    e.preventDefault()
    const parsed = schema.safeParse(name.trim())
    if (!parsed.success) {
      setError('Use 1 to 64 characters.')
      return
    }
    setError(undefined)
    setPending(true)
    try {
      await onSubmit(parsed.data)
      setName('')
    } catch (err) {
      setError(errorMessage(err))
    } finally {
      setPending(false)
    }
  }

  return (
    <form onSubmit={submit} noValidate className="flex flex-wrap items-end gap-2">
      <div className="grid gap-1">
        <Label htmlFor={id}>{label}</Label>
        <Input id={id} value={name} onChange={(e) => setName(e.target.value)} aria-invalid={error ? true : undefined} />
      </div>
      <Button type="submit" disabled={pending}>
        {pending ? pendingLabel : submitLabel}
      </Button>
      {error && (
        <p role="alert" className="w-full text-sm text-destructive">
          {error}
        </p>
      )}
    </form>
  )
}
