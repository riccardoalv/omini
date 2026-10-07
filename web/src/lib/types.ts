// Types mirroring the Go API (internal/api, internal/topology, schema/).

export type DeviceRole = 'router' | 'switch' | 'ap' | 'firewall' | 'server' | 'unknown'
export type NodeKind = 'device' | 'unmanaged' | 'segment' | 'client' | 'app'
export type EdgeKind = 'lldp' | 'fdb' | 'wifi' | 'inferred'

export interface Interface {
  name: string
  description?: string
  type?: string
  mac?: string
  up?: boolean
  speed_mbps?: number
  rx_bytes?: number
  tx_bytes?: number
  rx_errors?: number
  tx_errors?: number
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
  hosts?: { ip: string; mac?: string; sources?: string[] }[]
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
  pinned?: boolean
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
  fields: FormField[]
}

export type Config = Record<string, string | number | boolean>

export interface Integration {
  id: number
  name: string
  type: string
  config: Config
  enabled: boolean
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

export interface AuthStatus {
  setup_required: boolean
  authenticated: boolean
  username?: string
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
