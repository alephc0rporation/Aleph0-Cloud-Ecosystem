import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { AuthProvider, useAuth } from 'react-oidc-context'
import { ApiError } from '@/api/fetcher'
import { onSigninCallback, userManager } from '@/auth/oidc'
import { routeTree } from './routeTree.gen'
import './index.css'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // A 4xx answer does not change on retry; only network errors and 5xx are retried.
      retry: (failures, err) => !(err instanceof ApiError && err.status < 500) && failures < 3,
    },
  },
})
const router = createRouter({ routeTree, context: { auth: undefined! } })

declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

function App() {
  const auth = useAuth()
  // Wait for the stored session or the sign-in callback before any route decides.
  if (auth.isLoading) return null
  // A failed callback would otherwise redirect to Keycloak and back in a loop; a failed silent renewal is left to the fetcher's re-login.
  if (auth.error && auth.error.source !== 'renewSilent') {
    return (
      <p role="alert" className="p-4 text-destructive">
        Sign-in failed: {auth.error.message}
      </p>
    )
  }
  return <RouterProvider router={router} context={{ auth }} />
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <AuthProvider userManager={userManager} onSigninCallback={onSigninCallback}>
      <QueryClientProvider client={queryClient}>
        <App />
      </QueryClientProvider>
    </AuthProvider>
  </StrictMode>,
)
