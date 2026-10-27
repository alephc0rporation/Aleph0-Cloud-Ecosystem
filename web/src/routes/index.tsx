import { createFileRoute } from '@tanstack/react-router'
import { useAuth } from 'react-oidc-context'
import { useGetMe } from '@/api/gen/api'
import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'

export const Route = createFileRoute('/')({
  beforeLoad: async ({ context }) => {
    if (!context.auth.isAuthenticated) await context.auth.signinRedirect()
  },
  component: Home,
})

function Home() {
  const auth = useAuth()
  // The browser is already on its way to Keycloak.
  if (!auth.isAuthenticated) return null

  return (
    <div className="flex min-h-svh items-center justify-center p-4">
      <Card className="w-full max-w-sm">
        <CardHeader>
          <CardTitle>Aleph0 Cloud</CardTitle>
          <CardDescription>Signed in</CardDescription>
        </CardHeader>
        <CardContent>
          <Profile />
        </CardContent>
        <CardFooter>
          <Button variant="outline" onClick={() => void auth.signoutRedirect()}>
            Log out
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}

function Profile() {
  const me = useGetMe()
  if (me.isPending) return <p className="text-muted-foreground">Loading…</p>
  if (me.isError) {
    return (
      <p role="alert" className="text-destructive">
        Could not load your profile.
      </p>
    )
  }
  return (
    <dl className="grid gap-1">
      <dt className="text-sm text-muted-foreground">Name</dt>
      <dd>{me.data.name}</dd>
      <dt className="text-sm text-muted-foreground">Email</dt>
      <dd>{me.data.email}</dd>
    </dl>
  )
}
