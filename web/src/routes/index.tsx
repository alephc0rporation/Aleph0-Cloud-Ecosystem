import { createFileRoute } from '@tanstack/react-router'
import { Card, CardHeader, CardTitle } from '@/components/ui/card'

export const Route = createFileRoute('/')({
  component: Home,
})

function Home() {
  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Aleph0 Cloud</CardTitle>
        </CardHeader>
      </Card>
    </div>
  )
}
