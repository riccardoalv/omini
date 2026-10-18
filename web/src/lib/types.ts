// Types mirroring the Go API (internal/api, internal/topology, schema/).

export type DeviceRole = 'router' | 'switch' | 'ap' | 'firewall' | 'server' | 'unknown'
export type NodeKind = 'device' | 'unmanaged' | 'segment' | 'client' | 'app' | 'wan' | 'ssid'
export type EdgeKind = 'lldp' | 'fdb' | 'wifi' | 'inferred' | 'vpn'

export interface Interface {
  name: string
  description?: string
  type?: string
  /** Physical connector: drawn as an RJ45 jack or an SFP cage. */
  connector?: 'rj45' | 'sfp' | 'qsfp'
  mac?: string
  up?: boolean
  /** false: disabled in the device's configuration (not just without a link). */
  enabled?: boolean
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
  /** VLAN id of a VLAN interface. */
  vlan?: number
  /** VLAN membership of a switch port. */
  vlans?: { untagged?: number; tagged?: number[] }
  /** Pluggable module (SFP...) and its readings. */
  transceiver?: Transceiver
  /** Spanning tree state and role, when the device reports them. */
  stp?: { state?: 'forwarding' | 'blocking' | 'listening' | 'learning' | 'disabled'; role?: string }
  /** The LAG (bond, aggregate) this port is a member of. */
  lag?: string
}

export interface Transceiver {
  vendor?: string
  part?: string
  serial?: string
  type?: string
  wavelength_nm?: number
  temperature_c?: number
  voltage_v?: number
  bias_ma?: number
  tx_power_dbm?: number
  rx_power_dbm?: number
  rx_power_low_dbm?: number
  /** The module's own high temperature alarm (sfp_hot above it, else 70 °C). */
  temperature_high_c?: number
}

export interface Vlan {
  id: number
  name?: string
  interface?: string
  subnet?: string
}

export interface Service {
  name: string
  description?: string
  running?: boolean
  enabled?: boolean
}

export interface VpnPeer {
  name: string
  protocol: 'wireguard' | 'openvpn' | 'ipsec' | 'other'
  endpoint?: string
  address?: string
  connected?: boolean
  last_handshake?: string
  rx_bytes?: number
  tx_bytes?: number
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
  /** Logical CPUs; a VM's or container's vCPUs. */
  cpu_count?: number
  mem_total_bytes?: number
  mem_used_bytes?: number
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
  fdb?: { mac: string; port: string; vlan?: number }[]
  serial?: string
  vlans?: Vlan[]
  /** The networks a router serves (26.7+ OPNsense). */
  networks?: Network[]
  /** Wi-Fi networks it broadcasts (an access point, or a controller for its site). */
  wifi_networks?: { ssid: string; vlan?: number; security?: string; enabled?: boolean }[]
  /** Fixed addresses its DHCP hands out. */
  dhcp_reservations?: { mac: string; ip: string; name?: string }[]
  services?: Service[]
  vpn_peers?: VpnPeer[]
  dhcp_pools?: { network: string; total?: number; used?: number }[]
  firewall_states?: { current?: number; limit?: number }
  /** Local DNS names it answers (a DNS server or router). */
  dns_records?: { name: string; ip: string }[]
  dns_domain?: string
  /** Devices whose internet it cuts (a router). */
  paused?: PausedDevice[]
  /** The spanning tree root bridge as it sees it. */
  stp_root?: string
  /** IPv6 neighbors (NDP), like arp. */
  ndp?: { ip: string; mac: string; interface?: string }[]
  /** LLDP, CDP or MNDP neighbors, or `other` for a declared link. */
  neighbors?: Neighbor[]
  /** Clients of an access point. */
  wireless_clients?: WirelessClient[]
  /** The network scan's DHCPDISCOVER on each interface. */
  dhcp_checks?: DhcpCheck[]
  /** The network scan's queries to each DNS server. */
  dns_checks?: DnsCheck[]
}

export type NeighborCapability =
  'ap' | 'bridge' | 'docsis' | 'other' | 'repeater' | 'router' | 'station' | 'telephone'

/** A neighbor a device announces (LLDP, CDP, MNDP) or a link it declares. */
export interface Neighbor {
  local_port: string
  protocol?: 'lldp' | 'cdp' | 'mndp' | 'other'
  remote_mac?: string
  remote_ip?: string
  remote_name?: string
  remote_port?: string
  remote_platform?: string
  capabilities?: NeighborCapability[]
}

/** A client of an access point. */
export interface WirelessClient {
  mac: string
  ssid?: string
  band?: '2.4ghz' | '5ghz' | '6ghz'
  interface?: string
  signal_dbm?: number
  rx_rate_mbps?: number
  tx_rate_mbps?: number
  rx_bps?: number
  tx_bps?: number
}

/** One DHCPDISCOVER of the network scan, on one interface. */
export interface DhcpCheck {
  interface: string
  error?: string
  offers?: Omit<DhcpOffer, 'known' | 'node_id'>[]
}

/** The network scan's test queries to one DNS server. */
export interface DnsCheck {
  server: string
  external?: DnsQuery
  local?: DnsQuery
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
  /** A router or firewall running as a VM: the node of its host. */
  runs_on?: string
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
  /** Configuration changes its integration can apply. */
  changes?: ChangeKind[]
  /** Ports whose disabling would cut Omini or other gear off. */
  protected?: Record<string, Protection>
  /** Hidden from the map by the user. */
  hidden?: boolean
  /** The user's free note about the device. */
  note?: string
  /** The person who owns the device. */
  owner_id?: number
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
  /** A LAG of that many ports (speed_mbps is their sum). */
  lag?: number
  /** Spanning tree blocks one of its ports: a redundant link on standby. */
  standby?: boolean
}

/** What the network scan can do where Omini runs (see the README). */
export interface Capabilities {
  container: boolean
  host_network: boolean
  unprivileged_ping: boolean
  raw_sockets: boolean
  multicast: boolean
}

/** The last collection round: every integration, one at a time, from the edge to the center. */
export interface CollectionRound {
  started_at: string
  duration_ms: number
  /** Integration ids in the order they were collected. */
  order: number[]
  interval_s: number
}

export interface CollectionInfo {
  interval_s: number
  /** The interval when none is set (OMINI_POLL_INTERVAL). */
  default_s: number
  round?: CollectionRound
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
  /** A preset (AREA_COLORS) or any "#rrggbb". */
  color: string
  direction: 'RIGHT' | 'DOWN'
  x: number
  y: number
  width: number
  height: number
  /** Node ids inside the area; it is drawn around them. */
  members: string[]
  /** Hidden from the map (shown again with "Show hidden"). */
  hidden?: boolean
  /** A VLAN's or subnet's area, made by Omini (not stored, not editable). */
  auto?: boolean
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
  /** The user's free note about the device. */
  note?: string
  /** The person who owns the device. */
  owner_id?: number
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
  /** firewall, router, switch, wifi, hypervisor... */
  categories?: string[]
  /** Release the trust level was given to (newer ones are not reviewed yet). */
  reviewed_version?: string
  reviewed_at?: string
  known_issues?: string
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

/** A type of notification channel and its form. */
export interface NotifierType {
  type: 'webhook' | 'telegram' | 'email'
  name: string
  fields: FormField[]
}

/** A channel alerts are sent to (secrets come back masked). */
export interface Notifier {
  id: number
  type: NotifierType['type']
  config: Config
  min_severity: Severity
  notify_resolved: boolean
  enabled: boolean
  last_sent_at?: string
  last_error?: string
}

/** Where the store's index comes from. */
export interface PluginIndexStatus {
  url?: string
  fetched_at?: string
  error?: string
  plugins: number
}

/** Traffic between two addresses over a period (from flow exports). */
export interface Conversation {
  a: string
  b: string
  /** The map nodes of the two addresses, when known. */
  a_node?: string
  b_node?: string
  bytes_ab: number
  bytes_ba: number
  packets: number
  ports: { proto: number; port: number; bytes: number }[]
}

/** A device's internet traffic over a period (bytes). */
export interface InternetUsage {
  node: string
  label: string
  on_map: boolean
  rx: number
  tx: number
}

export interface InternetResponse {
  devices: InternetUsage[]
  /** Each person's use (the sum of their devices), busiest first. */
  people?: PersonUsage[]
  total: { node: string; rx: number; tx: number }
  since: string
  seconds: number
  /** The network's internet traffic over the period (bytes per step). */
  series: { at: string; rx: number; tx: number }[]
  step_s: number
  /** The rate over the last five minutes. */
  now: { rx_bps: number; tx_bps: number }
  countries: { country: string; rx: number; tx: number }[]
  /** Who runs the places (or their name, or address), busiest first. */
  orgs: { org: string; countries: string[]; rx: number; tx: number }[]
  /** The network's own country (from its internet address). */
  home: string
  /** The geolocation database's credit; absent when places are unknown. */
  attribution?: string
  listening: boolean
  exporters: FlowsResponse['exporters']
}

export type InternetRange = '15m' | '1h' | 'today' | '7d' | '30d'

/** Where a device went on the internet: a domain name, or the address. */
export interface Destination {
  name: string
  country?: string
  org?: string
  rx: number
  tx: number
  ports: { proto: number; port: number; bytes: number }[]
}

export interface FlowsResponse {
  conversations: Conversation[]
  listening: boolean
  exporters: {
    ip: string
    kind: 'netflow' | 'sflow'
    flows: number
    last_at: string
    version?: string
  }[]
}

/** A configuration change Omini can apply to a device. */
export type ChangeKind =
  | 'port.enabled'
  | 'port.vlans'
  | 'vlan.set'
  | 'vlan.delete'
  | 'network.set'
  | 'network.delete'
  | 'reservation.set'
  | 'reservation.delete'
  | 'wifi.set'
  | 'wifi.delete'
  | 'dns.set'
  | 'dns.delete'
  | 'wol.send'
  | 'pause.set'
  | 'pause.delete'

/** What a network reaches through the router; custom: rules set by hand (reported only). */
export type NetworkAccess = 'internet' | 'all' | 'local' | 'custom'

/** A VLAN's network on the router: its gateway, DHCP and access (VLAN 1 is the LAN). */
export interface Network {
  vlan: number
  name?: string
  interface?: string
  device?: string
  parent?: string
  /** "192.168.30.1/24" */
  gateway?: string
  dhcp?: {
    enabled: boolean
    from?: string
    to?: string
    /** false: the router refuses reservations inside the range (pfSense with ISC). */
    reserve_in_range?: boolean
  }
  access?: NetworkAccess
  /** Discovery between networks; null: the router has no mDNS repeater. */
  mdns?: boolean | null
}

/**
 * Why disabling a port is destructive: Omini reaches the device through it, or
 * it is the way to the router and the internet (node: what it leads to).
 */
export interface Protection {
  reason: 'omini' | 'internet'
  node?: string
}

/** A change made from Omini, applied or refused (error). */
export interface ConfigChange {
  id: number
  node_id: string
  /** The device it was for, when another node made it (a reservation on the router). */
  target_id?: string
  kind: ChangeKind
  port?: string
  before?: unknown
  after: unknown
  error?: string
  at: string
}

/** An answer to the network scan's DHCPDISCOVER. */
export interface DhcpOffer {
  server_ip: string
  server_mac?: string
  offered_ip?: string
  routers?: string[]
  dns?: string[]
  domain?: string
  lease_s?: number
  rtt_ms?: number
  /** A router, or a device that serves DHCP: anything else is unknown. */
  known: boolean
  node_id?: string
}

/** One DNS test query. */
export interface DnsQuery {
  name: string
  ok: boolean
  rtt_ms?: number
  answers?: string[]
  /** "timeout", "refused", "not found" or the resolver's message. */
  error?: string
}

/** The "DNS and DHCP" screen. */
export interface ServicesView {
  dhcp: {
    checks: { interface: string; error?: string; offers: DhcpOffer[] }[]
    pools: { node_id: string; network: string; used?: number; total?: number }[]
    leases: {
      node_id: string
      client?: string
      ip: string
      mac: string
      hostname?: string
      expires_at?: string
    }[]
  }
  dns: { servers: { server: string; node_id?: string; external?: DnsQuery; local?: DnsQuery }[] }
  generated_at?: string
}

/** A VLAN of the network's catalog, and where it is. */
export interface NetworkVlan {
  id: number
  name: string
  color?: string
  subnet?: string
  /** Its network on the router, when one serves it. */
  network?: Network
  /** The Wi-Fi networks that put their clients in it, and the devices broadcasting each. */
  wifi?: { ssid: string; enabled: boolean; security?: string; nodes: string[] }[]
  devices: { node_id: string; name?: string; untagged: string[]; tagged: string[] }[]
}

/** A port's VLANs: access (untagged only) or trunk (tagged, with a native VLAN or none). */
export interface PortVlans {
  untagged?: number
  tagged?: number[]
}

/** What setting a port's VLANs changes, device by device. */
export interface VlanPlan {
  steps: {
    node_id: string
    kind: ChangeKind
    port?: string
    value: unknown
    protected?: Protection
  }[]
  /** Devices on the way Omini does not manage (or the router, later): by hand. */
  notes: { node_id: string; reason: 'unmanaged' | 'router' | 'unknown_port'; vlans?: number[] }[]
}

/** A device on DHCP without a reservation, on a router that can make one. */
export interface FixCandidate {
  node_id: string
  label: string
  type?: string
  mac: string
  ip: string
  router: string
  /** A switch, access point, server, NAS...: checked by default. */
  infra: boolean
  /** The address used instead of ip: the router refuses one inside its DHCP range. */
  proposed?: string
  /**
   * The DHCP server it gets its address from, when Omini cannot reserve there
   * (a Pi-hole) and no router it manages serves DHCP on its network: router is
   * empty then.
   */
  server?: string
}

/** One device's result when fixing many addresses. */
export interface FixResult {
  node_id: string
  change?: ConfigChange
  error?: string
}

/** What Omini may change, and the domain local names go under ('' automatic). */
export interface ChangeSettings {
  allow_destructive: boolean
  dns_domain: string
}

/** A device's local DNS name, as its panel shows it. */
export interface DnsName {
  /** The full name Omini keeps ('' none). */
  name?: string
  /** Its first part: the user's, or from the device's name. */
  label?: string
  custom?: boolean
  off?: boolean
  /** The DNS server that answers it. */
  server?: string
  missing?: 'no_fixed_ip' | 'no_server' | 'no_name'
  /** The names any DNS server has for the device's address. */
  others?: { name: string; ip: string }[]
}

/** What a device's panel can do: wake it (from Omini or its router), pause its internet. */
export interface HostActions {
  wake: { via?: 'omini' | 'router'; router?: string }
  can_pause: boolean
  pause_router?: string
}

/** A device whose internet a router cuts. */
export interface PausedDevice {
  mac: string
  ips?: string[]
  name?: string
  /** The router rule, set by hand, that blocks it (empty: Omini paused it). */
  rule?: string
}

/** One version of a device's configuration (its content is read apart). */
export interface Backup {
  id: number
  node_id: string
  integration_id: number
  taken_at: string
  format: 'text' | 'json' | 'xml'
  size: number
  /** Omini changed the device since the version before. */
  by_omini: boolean
}

/** Someone who owns devices on the network (GET /api/people). */
export interface Person {
  id: number
  name: string
  color?: string
  created_at: string
  /** Their devices (inventory ids). */
  devices: string[]
  /** One of their phones, tablets or wearables is online. */
  home: boolean
  /** The latest time one of their devices was seen. */
  last_seen?: string
}

/** What pausing or resuming a person did to one of their devices. */
export interface PersonDeviceResult {
  node_id: string
  status: 'done' | 'skipped' | 'failed'
  error?: string
  change?: ConfigChange
}

/** A person's internet use: the sum of their devices (bytes). */
export interface PersonUsage {
  id: number
  name: string
  rx: number
  tx: number
}

/** A read-only API token (the token itself is shown only once). */
export interface ApiToken {
  id: number
  name: string
  created_at: string
  last_used_at: string | null
}

/** A token just created: the only time the API sends it. */
export interface NewApiToken {
  id: number
  name: string
  token: string
  created_at: string
}
