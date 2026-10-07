// Types mirroring the Go API (internal/api, internal/topology, schema/).

export type DeviceRole = 'router' | 'switch' | 'ap' | 'firewall' | 'server' | 'unknown'
export type NodeKind = 'device' | 'unmanaged' | 'segment' | 'client' | 'app' | 'wan'
export type EdgeKind = 'lldp' | 'fdb' | 'wifi' | 'inferred'

export interface Interface {
  name: string
  description?: string
  type?: string
  /** Physical connector: drawn as an RJ45 jack or an SFP cage. */
  connector?: 'rj45' | 'sfp' | 'qsfp'
  mac?: string
  up?: boolean
  speed_mbps?: number
  duplex?: 'full' | 'half'
  /** As reported by the device, e.g. "1000baseT <full-duplex>". */
  media?: string
  ips?: string[]
  /** Internet uplink of a router or firewall. */
  wan?: boolean
  /** Interface it runs on (a VLAN's port, the port carrying PPPoE). */
  parent?: string
  rx_bytes?: number
  tx_bytes?: number
  rx_errors?: number
  tx_errors?: number
}

export interface Gateway {
  name: string
  interface?: string
  address?: string
  status: 'up' | 'degraded' | 'down' | 'unknown'
  rtt_ms?: number
  loss_pct?: number
}

/** WAN nodes: an internet uplink of a router or firewall. */
export interface WANLink {
  interface: string
  /** Physical port carrying it (e.g. re0 for PPPoE over a VLAN). */
  port?: string
  speed_mbps?: number
  ips?: string[]
  gateways?: Gateway[]
}

export interface Device {
  key: string
  name: string
  host?: string
  role?: DeviceRole
  vendor?: string
  model?: string
  os_version?: string
  uptime_s?: number
  cpu_pct?: number
  mem_pct?: number
  macs?: string[]
  ips?: string[]
  interfaces?: Interface[]
  gateways?: Gateway[]
  hosts?: { ip: string; mac?: string; sources?: string[] }[]
  dhcp_leases?: { ip: string; mac: string; hostname?: string }[]
  arp?: { ip: string; mac: string; interface?: string }[]
  serial?: string
}

export interface TopoNode {
  id: string
  kind: NodeKind
  label: string
  role?: string
  online: boolean
  integration_id?: number
  vendor?: string
  model?: string
  ip?: string
  mac?: string
  hostname?: string
  random_mac?: boolean
  parent_id?: string
  port?: string
  ssid?: string
  signal_dbm?: number
  mac_count?: number
  device?: Device
  wan?: WANLink
  /** Current traffic per interface (bits/s, average of the last polling interval). */
  traffic?: Record<string, { rx_bps: number; tx_bps: number }>
  /** The user's port descriptions, by port name. */
  port_labels?: Record<string, string>
  pinned?: boolean
  /** Hidden from the map by the user. */
  hidden?: boolean
  last_seen?: string
  os?: string
  open_ports?: number[]
  services?: string[]
  titles?: string[]
  banners?: string[]
  ttl?: number
  web?: { port: number; url: string; title?: string; app?: string }[]
  type?: string
  brand?: string
  product?: string
  reasons?: string[]
  icon?: string
}

export interface TopoEdge {
  id: string
  source: string
  target: string
  source_port?: string
  target_port?: string
  kind: EdgeKind
  speed_mbps?: number
}

export interface CollectionStatus {
  integration_id: number
  ok: boolean
  error?: string
  collected_at: string
  duration_ms: number
  devices: number
}

export interface Point {
  x: number
  y: number
}

export interface TopologyResponse {
  topology: { nodes: TopoNode[]; edges: TopoEdge[] }
  statuses: CollectionStatus[]
  generated_at: string
  layout: Record<string, Point>
  areas?: MapArea[]
}

export const AREA_COLORS = ['gray', 'blue', 'green', 'yellow', 'red', 'purple'] as const
export type AreaColor = (typeof AREA_COLORS)[number]

/** A named rectangle drawn on the map; the nodes inside it move with it. */
export interface MapArea {
  id: number
  name: string
  color: AreaColor
  direction: 'RIGHT' | 'DOWN'
  x: number
  y: number
  width: number
  height: number
  /** Node ids inside the area; it is drawn around them. */
  members: string[]
}

export type FieldType = 'string' | 'secret' | 'host' | 'url' | 'int' | 'bool' | 'select'

export interface FormField {
  key: string
  type: FieldType
  group?: string
  label?: string
  help?: string
  required?: boolean
  default?: string | number | boolean
  options?: string[]
}

export interface IntegrationType {
  type: string
  name: string
  description?: string
  kind: 'core' | 'plugin'
  /** Can be added only once (the network scan). */
  single?: boolean
  fields: FormField[]
}

export type Config = Record<string, string | number | boolean>

export interface Integration {
  id: number
  name: string
  type: string
  config: Config
  enabled: boolean
  /** Seconds between collections; 0 = the default. */
  interval_s?: number
  created_at: string
  updated_at: string
  status?: CollectionStatus
}

export interface InventoryEntry {
  id: string
  kind: NodeKind
  label: string
  mac?: string
  ip?: string
  hostname?: string
  vendor?: string
  parent_id?: string
  port?: string
  alias?: string
  pinned: boolean
  hidden: boolean
  first_seen: string
  last_seen: string
  online: boolean
  role?: string
  type?: string
  os?: string
  brand?: string
  product?: string
  device_type?: string
  icon?: string
}

/** An installed plugin (GET /api/plugins). */
export type PluginTrust = 'plug-and-play' | 'stable' | 'experimental' | 'unverified'

export interface PluginInfo {
  manifest: { id: string; name: string; version: string; description?: string; homepage?: string }
  /** Where it was installed from: a tag, or branch@commit without releases. */
  source?: { url: string; version: string; commit?: string; installed_at: string }
  /** Loaded in place from OMINI_PLUGIN_DIRS (development). */
  dev: boolean
  publisher: 'official' | 'community'
  trust: PluginTrust
}

/** A plugin of the curated list shipped with Omini. */
export interface CatalogEntry {
  id: string
  name: string
  description: string
  url: string
  icon?: string
  publisher: 'official' | 'community'
  trust: PluginTrust
  installed: boolean
  version?: string
}

export interface AuthStatus {
  setup_required: boolean
  authenticated: boolean
  username?: string
  /** The user's UI language; empty means the browser default. */
  locale?: string
}

export interface TestResult {
  ok: boolean
  message?: string
  error?: string
}

export interface WebService {
  url: string
  port: number
  title?: string
}
