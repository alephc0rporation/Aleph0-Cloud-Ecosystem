import { UserManager } from 'oidc-client-ts'

// One instance shared by React (AuthProvider) and the fetch mutator. Tokens live in sessionStorage.
export const userManager = new UserManager({
  authority: import.meta.env.VITE_OIDC_AUTHORITY,
  client_id: import.meta.env.VITE_OIDC_CLIENT_ID,
  redirect_uri: `${window.location.origin}/`,
  post_logout_redirect_uri: `${window.location.origin}/`,
  scope: 'openid profile email',
})

// Removes ?code=&state= from the address bar after Keycloak redirects back.
export function onSigninCallback() {
  window.history.replaceState({}, document.title, window.location.pathname)
}
