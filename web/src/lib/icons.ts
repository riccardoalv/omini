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
  badge: boolean
}

/** Lucide icon for each device type. */
export const typeIcons: Record<string, string> = {
  firewall: 'shield',
  router: 'router',
  switch: 'ethernet-port',
  ap: 'wifi',
  server: 'server',
  nas: 'hard-drive',
  hypervisor: 'layers',
  virtual_machine: 'container',
  computer: 'monitor',
  phone: 'smartphone',
  tablet: 'tablet',
  tv: 'tv',
  media_player: 'cast',
  speaker: 'speaker',
  printer: 'printer',
  camera: 'cctv',
  smart_home: 'house',
  appliance: 'air-vent',
  game_console: 'gamepad-2',
  wearable: 'watch',
  segment: 'network',
  unknown: 'circle-question-mark',
}

/**
 * Decides what to draw for a device: homelab products get only their logo;
 * other devices get their type icon with the OS (or else brand) as a badge.
 */
export function iconChoice(d: Classified): IconChoice {
  const custom = d.icon && logos[d.icon] ? d.icon : undefined
  const product = d.product && logos[d.product] ? d.product : undefined
  const type =
    typeIcons[d.type ?? ''] ?? (d.kind === 'segment' ? 'network' : 'circle-question-mark')
  if (custom && productSlugs.has(custom)) return { logo: custom, badge: false }
  if (!custom && product) return { logo: product, badge: false }
  const badge = custom ?? [d.os, d.brand].find((s) => s && logos[s])
  return { type, logo: badge, badge: !!badge }
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
export const deviceTypes = Object.keys(typeIcons).filter((t) => t !== 'segment')
