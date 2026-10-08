import {
  siAdguard,
  siAlpinelinux,
  siAndroid,
  siApple,
  siArchlinux,
  siAsus,
  siBitwarden,
  siCentos,
  siDebian,
  siDell,
  siEmby,
  siEpson,
  siEspressif,
  siEsphome,
  siFedora,
  siFreebsd,
  siFrigate,
  siGoogle,
  siGrafana,
  siHomeassistant,
  siHonor,
  siHp,
  siHuawei,
  siImmich,
  siIntel,
  siIos,
  siJellyfin,
  siLenovo,
  siLg,
  siLinux,
  siLinuxmint,
  siMacos,
  siManjaro,
  siMikrotik,
  siMotorola,
  siMsi,
  siNetgear,
  siNextcloud,
  siNixos,
  siNvidia,
  siOneplus,
  siOpenbsd,
  siOpensuse,
  siOpenwrt,
  siOpnsense,
  siOppo,
  siPaperlessngx,
  siPfsense,
  siPihole,
  siPlaystation,
  siPlex,
  siPopos,
  siPortainer,
  siProxmox,
  siQnap,
  siRaspberrypi,
  siRedhat,
  siRoku,
  siSamsung,
  siShelly,
  siSonos,
  siSony,
  siSynology,
  siTasmota,
  siTplink,
  siTruenas,
  siUbiquiti,
  siUbuntu,
  siUnraid,
  siUptimekuma,
  siVaultwarden,
  siVivo,
  siVmware,
  siXiaomi,
  siPhilipshue,
  type SimpleIcon,
} from 'simple-icons'

import { catalog } from './catalog'

/** A logo: an SVG path on a 24x24 grid and its brand color. */
export interface Logo {
  title: string
  path: string
  hex: string
}

// Not in Simple Icons (Microsoft asked for removal): a neutral four-pane glyph.
const windows: Logo = {
  title: 'Windows',
  path: 'M2 3.5 10.5 2.3V11H2V3.5Zm0 9.5h8.5v8.7L2 20.5V13Zm9.5-10.8L22 .8V11H11.5V2.2ZM11.5 13H22v10.2l-10.5-1.4V13Z',
  hex: '4A90D9',
}

// Not in Simple Icons: the "M" of the Mercusys wordmark (the bundled app icon is
// the whole wordmark, unreadable in a square).
const mercusys: Logo = {
  title: 'Mercusys',
  path: 'M 1.0,4.57 C 2.38,4.57 3.77,4.60 5.16,4.56 C 6.28,4.53 7.11,5.01 7.76,5.85 C 8.09,6.28 8.36,6.75 8.61,7.23 C 9.76,9.35 10.88,11.48 12.02,13.60 C 12.12,13.79 12.30,13.92 12.44,14.08 C 12.58,13.92 12.75,13.78 12.85,13.59 C 14.34,10.68 15.82,7.78 17.29,4.86 C 17.47,4.51 17.65,4.36 18.05,4.37 C 19.56,4.40 21.07,4.38 22.57,4.38 L 23.0,4.38 L 23.0,19.52 L 17.24,19.52 L 17.24,11.09 L 17.15,11.06 C 17.09,11.17 17.01,11.28 16.95,11.40 C 15.54,14.01 14.13,16.63 12.73,19.25 C 12.58,19.54 12.42,19.63 12.11,19.62 A 32.05,32.05 0.0 0,0 10.03,19.62 C 9.53,19.63 9.10,19.47 8.80,19.11 C 8.48,18.73 8.18,18.31 7.95,17.86 C 6.68,15.39 5.43,12.90 4.17,10.42 C 4.09,10.26 3.91,10.15 3.78,10.01 C 3.72,10.19 3.61,10.37 3.61,10.54 C 3.58,13.43 3.57,16.31 3.55,19.19 C 3.55,19.31 3.55,19.42 3.55,19.61 L 1.0,19.61 L 1.0,4.57',
  hex: 'C72127',
}

const fromSimpleIcons = (icon: SimpleIcon): Logo => ({
  title: icon.title,
  path: icon.path,
  hex: icon.hex,
})

/** Logos by slug (the values produced by the classifier and chosen by users). */
export const logos: Record<string, Logo> = {
  // Homelab software and appliances: shown alone.
  opnsense: fromSimpleIcons(siOpnsense),
  pfsense: fromSimpleIcons(siPfsense),
  proxmox: fromSimpleIcons(siProxmox),
  truenas: fromSimpleIcons(siTruenas),
  unraid: fromSimpleIcons(siUnraid),
  homeassistant: fromSimpleIcons(siHomeassistant),
  jellyfin: fromSimpleIcons(siJellyfin),
  plex: fromSimpleIcons(siPlex),
  emby: fromSimpleIcons(siEmby),
  pihole: fromSimpleIcons(siPihole),
  adguard: fromSimpleIcons(siAdguard),
  nextcloud: fromSimpleIcons(siNextcloud),
  portainer: fromSimpleIcons(siPortainer),
  grafana: fromSimpleIcons(siGrafana),
  frigate: fromSimpleIcons(siFrigate),
  immich: fromSimpleIcons(siImmich),
  paperlessngx: fromSimpleIcons(siPaperlessngx),
  uptimekuma: fromSimpleIcons(siUptimekuma),
  vaultwarden: fromSimpleIcons(siVaultwarden),
  bitwarden: fromSimpleIcons(siBitwarden),
  esphome: fromSimpleIcons(siEsphome),
  tasmota: fromSimpleIcons(siTasmota),
  openwrt: fromSimpleIcons(siOpenwrt),
  mikrotik: fromSimpleIcons(siMikrotik),
  synology: fromSimpleIcons(siSynology),
  qnap: fromSimpleIcons(siQnap),
  // Operating systems: badge on the device type icon.
  nixos: fromSimpleIcons(siNixos),
  debian: fromSimpleIcons(siDebian),
  ubuntu: fromSimpleIcons(siUbuntu),
  fedora: fromSimpleIcons(siFedora),
  archlinux: fromSimpleIcons(siArchlinux),
  raspberrypi: fromSimpleIcons(siRaspberrypi),
  alpinelinux: fromSimpleIcons(siAlpinelinux),
  centos: fromSimpleIcons(siCentos),
  redhat: fromSimpleIcons(siRedhat),
  opensuse: fromSimpleIcons(siOpensuse),
  linuxmint: fromSimpleIcons(siLinuxmint),
  popos: fromSimpleIcons(siPopos),
  manjaro: fromSimpleIcons(siManjaro),
  freebsd: fromSimpleIcons(siFreebsd),
  openbsd: fromSimpleIcons(siOpenbsd),
  linux: fromSimpleIcons(siLinux),
  android: fromSimpleIcons(siAndroid),
  ios: fromSimpleIcons(siIos),
  macos: fromSimpleIcons(siMacos),
  tvos: fromSimpleIcons(siApple),
  apple: fromSimpleIcons(siApple),
  windows,
  // Brands: badge when the OS is unknown.
  samsung: fromSimpleIcons(siSamsung),
  huawei: fromSimpleIcons(siHuawei),
  honor: fromSimpleIcons(siHonor),
  xiaomi: fromSimpleIcons(siXiaomi),
  oneplus: fromSimpleIcons(siOneplus),
  motorola: fromSimpleIcons(siMotorola),
  oppo: fromSimpleIcons(siOppo),
  vivo: fromSimpleIcons(siVivo),
  google: fromSimpleIcons(siGoogle),
  lg: fromSimpleIcons(siLg),
  sony: fromSimpleIcons(siSony),
  playstation: fromSimpleIcons(siPlaystation),
  espressif: fromSimpleIcons(siEspressif),
  tplink: fromSimpleIcons(siTplink),
  mercusys,
  ubiquiti: fromSimpleIcons(siUbiquiti),
  netgear: fromSimpleIcons(siNetgear),
  asus: fromSimpleIcons(siAsus),
  intel: fromSimpleIcons(siIntel),
  dell: fromSimpleIcons(siDell),
  lenovo: fromSimpleIcons(siLenovo),
  hp: fromSimpleIcons(siHp),
  msi: fromSimpleIcons(siMsi),
  nvidia: fromSimpleIcons(siNvidia),
  sonos: fromSimpleIcons(siSonos),
  philipshue: fromSimpleIcons(siPhilipshue),
  roku: fromSimpleIcons(siRoku),
  vmware: fromSimpleIcons(siVmware),
  shelly: fromSimpleIcons(siShelly),
  epson: fromSimpleIcons(siEpson),
}

/** Slugs that are products (homelab software/appliances): their logo replaces the type icon. */
export const productSlugs = new Set([
  'opnsense',
  'pfsense',
  'proxmox',
  'truenas',
  'unraid',
  'homeassistant',
  'jellyfin',
  'plex',
  'emby',
  'pihole',
  'adguard',
  'nextcloud',
  'portainer',
  'grafana',
  'frigate',
  'immich',
  'paperlessngx',
  'uptimekuma',
  'vaultwarden',
  'bitwarden',
  'esphome',
  'tasmota',
  'openwrt',
  'mikrotik',
  'synology',
  'qnap',
])

export interface Classified {
  kind?: string
  type?: string
  os?: string
  brand?: string
  product?: string
  /** Logo chosen by the user. */
  icon?: string
  random_mac?: boolean
}

export interface IconChoice {
  /** Lucide icon name for the device type, unless a product logo is shown alone. */
  type?: string
  /** Logo shown alone (product) or as a badge (OS or brand). */
  logo?: string
  /** App catalog icon (served by /api/icons) when Simple Icons has no logo. */
  remote?: string
  badge: boolean
}

/** Classifier slugs whose app catalog name differs. */
const catalogNames: Record<string, string> = {
  homeassistant: 'home-assistant',
  pihole: 'pi-hole',
  adguard: 'adguard-home',
  uptimekuma: 'uptime-kuma',
  paperlessngx: 'paperless-ngx',
  tplink: 'tp-link',
  raspberrypi: 'raspberry-pi',
}

const catalogName = (slug: string) => catalogNames[slug] ?? slug

/** URL of an app catalog icon. */
export const iconURL = (slug: string) => `/api/icons/${catalogName(slug)}.svg`

/** Whether the app catalog has an icon for a slug. */
export const inCatalog = (slug: string) => catalog.names.has(catalogName(slug))

/** Display names of brands without a logo in Simple Icons. */
export const brandNames: Record<string, string> = {
  midea: 'Midea',
  horaco: 'Horaco',
  realtek: 'Realtek',
  amazon: 'Amazon',
  tuya: 'Tuya',
  hikvision: 'Hikvision',
  dahua: 'Dahua',
  reolink: 'Reolink',
  brother: 'Brother',
  canon: 'Canon',
  gigabyte: 'Gigabyte',
  tenda: 'Tenda',
  dlink: 'D-Link',
  zte: 'ZTE',
  realme: 'realme',
  nintendo: 'Nintendo',
}

/** Human name of a logo or brand slug. */
export const slugName = (slug?: string) =>
  slug ? (logos[slug]?.title ?? brandNames[slug] ?? slug) : ''

/** Lucide icon for each device type. */
export const typeIcons: Record<string, string> = {
  firewall: 'shield',
  router: 'router',
  switch: 'ethernet-port',
  ap: 'wifi',
  server: 'server',
  nas: 'hard-drive',
  hypervisor: 'layers',
  virtual_machine: 'server',
  wan: 'globe',
  computer: 'monitor',
  phone: 'smartphone',
  tablet: 'tablet',
  tv: 'tv',
  media_player: 'cast',
  speaker: 'speaker',
  printer: 'printer',
  camera: 'cctv',
  smart_home: 'house',
  air_conditioner: 'air-vent',
  solar_inverter: 'solar-panel',
  appliance: 'refrigerator',
  game_console: 'gamepad-2',
  wearable: 'watch',
  segment: 'network',
  app: 'app-window',
  unknown: 'circle-question-mark',
}

/**
 * Decides what to draw for a device: homelab products get only their logo;
 * other devices get their type icon with the OS (or else brand) as a badge;
 * a device of unknown type gets its brand's (or OS's) logo alone.
 */
export function iconChoice(d: Classified): IconChoice {
  const custom = d.icon && logos[d.icon] ? d.icon : undefined
  const product = d.product && logos[d.product] ? d.product : undefined
  const type =
    typeIcons[d.type ?? ''] ?? (d.kind === 'segment' ? 'network' : 'circle-question-mark')
  if (custom && productSlugs.has(custom)) return { logo: custom, badge: false }
  if (!custom && product) return { logo: product, badge: false }
  // Apps and products without a Simple Icons logo: icon from the app catalog.
  if (!custom && d.product && inCatalog(d.product)) {
    return { type, remote: d.product, badge: false }
  }
  if (d.icon && !custom && inCatalog(d.icon)) return { type, remote: d.icon, badge: true }
  // Type unknown but the brand (or OS) known: its logo alone instead of a question mark.
  if (!custom && !d.icon && type === 'circle-question-mark') {
    const known = [d.brand, d.os].find((s) => s && logos[s])
    if (known) return { logo: known, badge: false }
    if (d.brand && inCatalog(d.brand)) return { type, remote: d.brand, badge: false }
  }
  const badge = custom ?? [d.os, d.brand].find((s) => s && logos[s])
  if (badge) return { type, logo: badge, badge: true }
  // Brands without a Simple Icons logo may have one in the app catalog; if not, no badge.
  if (d.brand && inCatalog(d.brand)) return { type, remote: d.brand, badge: true }
  return { type, badge: false }
}

/** Brand color usable on the current background, or undefined to use the text color. */
export function logoColor(hex: string, dark: boolean): string | undefined {
  const n = parseInt(hex, 16)
  const r = (n >> 16) & 255
  const g = (n >> 8) & 255
  const b = n & 255
  const luminance = (0.2126 * r + 0.7152 * g + 0.0722 * b) / 255
  if (dark && luminance < 0.25) return undefined // e.g. black logos on a dark theme
  if (!dark && luminance > 0.85) return undefined // e.g. white logos on a light theme
  return `#${hex}`
}

/** Device types users can choose from, in display order. */
export const deviceTypes = Object.keys(typeIcons).filter((t) => t !== 'segment' && t !== 'app')
