/**
 * IPv6 addresses as the UI shows them: compact (RFC 5952: lowercase, no
 * leading zeros, the longest run of zero groups as "::"), global and ULA
 * ones before link-local ones (fe80::/10), which name no machine.
 */

/** Whether an address is IPv6 link-local (fe80::/10). */
export const isLinkLocal6 = (ip: string) => /^fe[89ab][0-9a-f]:/i.test(ip)

/** The addresses that are not link-local: global and ULA. */
export const globalIPv6 = (ips?: string[]) => (ips ?? []).filter((ip) => !isLinkLocal6(ip))

/** The compact form of an IPv6 address; anything else is returned as is. */
export function compactIPv6(ip: string): string {
  const [addr = '', zone] = ip.trim().split('%')
  if (!addr.includes(':') || addr.includes('.') || (addr.match(/::/g) ?? []).length > 1) return ip
  const [head = '', tail] = addr.split('::')
  const left = head ? head.split(':') : []
  const right = tail ? tail.split(':') : []
  const missing = 8 - left.length - right.length
  if (tail === undefined ? missing !== 0 : missing < 1) return ip
  const groups = [...left, ...Array<string>(tail === undefined ? 0 : missing).fill('0'), ...right]
  if (!groups.every((g) => /^[0-9a-f]{1,4}$/i.test(g))) return ip
  const hex = groups.map((g) => g.toLowerCase().replace(/^0+(?=.)/, ''))
  // The longest run of zero groups (two or more; the first of equal ones).
  let best = -1
  let bestLen = 1
  for (let i = 0; i < 8;) {
    let j = i
    while (j < 8 && hex[j] === '0') j++
    if (j - i > bestLen) [best, bestLen] = [i, j - i]
    i = j === i ? i + 1 : j
  }
  const out =
    best < 0
      ? hex.join(':')
      : `${hex.slice(0, best).join(':')}::${hex.slice(best + bestLen).join(':')}`
  return zone ? `${out}%${zone}` : out
}
