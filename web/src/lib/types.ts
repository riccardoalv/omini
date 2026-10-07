// Types mirroring the Go API (internal/api, internal/topology, schema/).

export type DeviceRole = 'router' | 'switch' | 'ap' | 'firewall' | 'server' | 'unknown'
export type NodeKind = 'device' | 'unmanaged' | 'segment' | 'client' | 'app' | 'wan' | 'ssid'
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
  /** Ports a bridge or LAG is made of. */
  members?: string[]
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

export interface Temperature {
  sensor: string
  kind?: 'cpu' | 'disk' | 'board' | 'other'
  celsius: number
}

export interface Storage {
  mount: string
  device?: string
  fs_type?: string
  total_bytes?: number
  used_bytes?: number
}

/** Version and pending updates, as of the device's last check. */
export interface Firmware {
  current?: string
  latest?: string
  update_available?: boolean
  updates?: number
  needs_reboot?: boolean
  checked_at?: string
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
  swap_pct?: number
  /** Over 1, 5 and 15 minutes. */
  load_avg?: number[]
  temperatures?: Temperature[]
  storage?: Storage[]
  firmware?: Firmware
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
  /** Wi-Fi clients: band, link rate and current traffic (rx = download) from their AP. */
  band?: '2.4ghz' | '5ghz' | '6ghz'
  link_mbps?: number
  flow?: { rx_bps: number; tx_bps: number }
  mac_count?: number
  device?: Device
  wan?: WANLink
  /** Current traffic per interface (bits/s, average of the last polling interval). */
  traffic?: Record<string, Rate>
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
  /** Open alerts. */
  alerts?: Alert[]
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

/** Traffic of an interface over the last polling interval. */
export interface Rate {
  rx_bps: number
  tx_bps: number
  /** Errors counted during the same interval. */
  rx_errors?: number
  tx_errors?: number
}

export type Severity = 'critical' | 'warning' | 'info'

/** An insight that is (or was) true: opened when found, resolved when gone. */
export interface Alert {
  id: number
  key: string
  rule: string
  severity: Severity
  node_id?: string
  params: Record<string, string | number | boolean>
  opened_at: string
  updated_at: string
  resolved_at?: string
  dismissed: boolean
}

/** A device joining or leaving the network. */
export interface PresenceEvent {
  id: number
  node_id: string
  kind: 'join' | 'leave'
  at: string
  /** The first time the device was ever seen. */
  first?: boolean
  label?: string
  mac?: string
  ip?: string
}

/** One point of a traffic chart (per minute, or per hour with its peak). */
export interface TrafficPoint {
  at: string
  rx_bps: number
  tx_bps: number
  rx_max_bps?: number
  tx_max_bps?: number
}
