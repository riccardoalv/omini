import { slugName } from './icons'

export interface Nameable {
  label?: string
  ip?: string
  mac?: string
  hostname?: string
  type?: string
  brand?: string
  product?: string
  kind?: string
  os?: string
}

type Translate = (key: string, params?: Record<string, unknown>) => string

const isAddress = (n: Nameable, label: string) =>
  label === n.ip || label === n.mac || /^\d{1,3}(\.\d{1,3}){3}$/.test(label)

/**
 * The name to show for a device: its own name when it has one, otherwise
 * what it is ("Home Assistant", "Mercusys access point", "Android phone"...).
 */
export function displayName(n: Nameable, t: Translate): string {
  const label = n.label ?? ''
  if (label && !isAddress(n, label)) return label
  if (n.product) return slugName(n.product)
  // Machines named by their system: "Ubuntu", "Debian"...
  if (n.os && ['server', 'virtual_machine', 'computer'].includes(n.type ?? '')) {
    return slugName(n.os)
  }
  const hasType = n.type && n.type !== 'unknown' && n.type !== 'segment'
  if (hasType && n.brand)
    return t('names.typeBrand', { type: t(`types.${n.type}`), brand: slugName(n.brand) })
  if (hasType) return t(`types.${n.type}`)
  return label || n.ip || n.mac || ''
}
