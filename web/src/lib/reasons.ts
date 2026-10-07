/** A piece of classification evidence, ready to display. */
export interface Reason {
  key: string // i18n key under panel.*
  value?: string
}

const prefixes: Record<string, string> = {
  title: 'reasonTitle',
  port: 'reasonPort',
  mdns: 'reasonMdns',
  vendor: 'reasonVendor',
  hostname: 'reasonHostname',
  banner: 'reasonBanner',
  ttl: 'reasonTtl',
  model: 'reasonModel',
  upnp: 'reasonUpnp',
  integration: 'reasonIntegration',
  'os-release': 'reasonOs',
}

/** Turns classifier reasons ("title:TrueNAS", "private-mac") into displayable evidence. */
export function parseReasons(reasons: string[] = []): Reason[] {
  const out: Reason[] = []
  for (const r of reasons) {
    if (r === 'user') out.push({ key: 'reasonUser' })
    else if (r === 'private-mac') out.push({ key: 'reasonPrivate' })
    else if (r === 'this-server') out.push({ key: 'reasonOs' })
    else {
      const i = r.indexOf(':')
      const key = i > 0 ? prefixes[r.slice(0, i)] : undefined
      if (key) out.push({ key, value: r.slice(i + 1) })
    }
  }
  return out
}
