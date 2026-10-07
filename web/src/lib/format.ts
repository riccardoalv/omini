/** Formats a link speed: 100 → "100M", 2500 → "2.5G", 10000 → "10G". */
export function formatSpeed(mbps?: number): string {
  if (!mbps) return ''
  if (mbps >= 1000) {
    const g = mbps / 1000
    return `${Number.isInteger(g) ? g : g.toFixed(1)}G`
  }
  return `${mbps}M`
}

/** Formats bytes with binary units: 1536 → "1.5 KiB". */
export function formatBytes(bytes?: number): string {
  if (bytes === undefined) return ''
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB', 'PiB']
  let value = bytes
  let i = 0
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024
    i++
  }
  return `${i === 0 ? value : value.toFixed(1)} ${units[i]}`
}

/** Formats a duration in seconds as "41d 7h", "3h 12m" or "45m". */
export function formatUptime(seconds?: number): string {
  if (seconds === undefined) return ''
  const d = Math.floor(seconds / 86400)
  const h = Math.floor((seconds % 86400) / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (d > 0) return `${d}d ${h}h`
  if (h > 0) return `${h}h ${m}m`
  return `${m}m`
}

/** Formats how long ago a timestamp was, in the given locale ("5 minutes ago"). */
export function formatAgo(iso: string | undefined, locale: string, now = Date.now()): string {
  if (!iso) return ''
  const diff = (new Date(iso).getTime() - now) / 1000
  const rtf = new Intl.RelativeTimeFormat(locale, { numeric: 'auto' })
  const abs = Math.abs(diff)
  if (abs < 60) return rtf.format(Math.round(diff), 'second')
  if (abs < 3600) return rtf.format(Math.round(diff / 60), 'minute')
  if (abs < 86400) return rtf.format(Math.round(diff / 3600), 'hour')
  return rtf.format(Math.round(diff / 86400), 'day')
}

/** Formats a timestamp as a short local date/time. */
export function formatDateTime(iso: string | undefined, locale: string): string {
  if (!iso) return ''
  return new Intl.DateTimeFormat(locale, { dateStyle: 'short', timeStyle: 'short' }).format(
    new Date(iso),
  )
}
