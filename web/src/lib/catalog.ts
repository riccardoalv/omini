import { reactive } from 'vue'

/** Names of the app icon catalog (Dashboard Icons) served by /api/icons. */
export const catalog = reactive({ names: new Set<string>() })

export async function loadCatalog() {
  try {
    const res = await fetch('/api/icons')
    if (res.ok) catalog.names = new Set((await res.json()) as string[])
  } catch {
    // icons from the catalog are optional
  }
}
