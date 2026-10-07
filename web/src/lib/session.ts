import { reactive } from 'vue'

import { api, setUnauthorizedHandler } from './api'

/** Authentication state shared by the whole app. */
export const session = reactive({
  ready: false,
  setupRequired: false,
  authenticated: false,
  username: '',
})

export async function loadSession() {
  try {
    const s = await api.authStatus()
    session.setupRequired = s.setup_required
    session.authenticated = s.authenticated
    session.username = s.username ?? ''
  } finally {
    session.ready = true
  }
}

export function signedIn(username: string) {
  session.authenticated = true
  session.setupRequired = false
  session.username = username
}

export async function signOut() {
  try {
    await api.logout()
  } finally {
    session.authenticated = false
    session.username = ''
  }
}

// An expired session anywhere in the app sends the user back to the login screen.
setUnauthorizedHandler(() => {
  session.authenticated = false
})
