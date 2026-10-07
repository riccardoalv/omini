import { reactive } from 'vue'

import { api, setUnauthorizedHandler } from './api'
import { prefs } from './prefs'
import type { AuthStatus } from './types'

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
    applyUserLocale(s)
  } finally {
    session.ready = true
  }
}

export function signedIn(status: AuthStatus) {
  session.authenticated = true
  session.setupRequired = false
  session.username = status.username ?? ''
  applyUserLocale(status)
}

/** The language saved for the user wins over the browser's. */
function applyUserLocale(status: AuthStatus) {
  if (status.authenticated && status.locale) prefs.locale = status.locale
}

/** Changes the UI language and saves it for the user. */
export async function setLocale(locale: string) {
  prefs.locale = locale
  if (session.authenticated) await api.updateMe({ locale })
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
