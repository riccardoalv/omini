import { reactive, watch } from 'vue'

/** Per-browser UI preferences, persisted in localStorage when available. */
export interface Prefs {
  theme: 'system' | 'dark' | 'light'
  locale: string
  /** Groups with more clients than this collapse into a bubble on the map. */
  collapseThreshold: number
  /** Parent node ids whose client group the user expanded. */
  expanded: string[]
  /** Parent node ids whose clients the user collapsed (context menu). */
  collapsed: string[]
  /** Map orientation: left-to-right or top-down. */
  layoutDirection: 'RIGHT' | 'DOWN'
  /** Sidebar shows labels next to icons. */
  sidebarExpanded: boolean
  /** Hide devices that are offline from the map. */
  hideOffline: boolean
}

const KEY = 'omini.prefs'

export const DEFAULT_COLLAPSE_THRESHOLD = 8

function defaultLocale(): string {
  const lang = typeof navigator !== 'undefined' ? navigator.language : 'en'
  return lang.toLowerCase().startsWith('pt') ? 'pt-BR' : 'en'
}

function load(): Prefs {
  const defaults: Prefs = {
    theme: 'system',
    locale: defaultLocale(),
    collapseThreshold: DEFAULT_COLLAPSE_THRESHOLD,
    expanded: [],
    collapsed: [],
    layoutDirection: 'RIGHT',
    sidebarExpanded: false,
    hideOffline: false,
  }
  try {
    const raw = localStorage.getItem(KEY)
    if (raw) return { ...defaults, ...JSON.parse(raw) }
  } catch {
    // storage unavailable (private mode) or corrupted: use defaults
  }
  return defaults
}

export const prefs = reactive<Prefs>(load())

watch(
  prefs,
  (value) => {
    try {
      localStorage.setItem(KEY, JSON.stringify(value))
    } catch {
      // ignore: preferences just won't persist
    }
  },
  { deep: true },
)

/** Applies the theme to <html data-theme>; "system" follows the OS (dark when unknown). */
export function applyTheme(theme: Prefs['theme']) {
  const root = document.documentElement
  if (theme === 'system') delete root.dataset.theme
  else root.dataset.theme = theme
}
