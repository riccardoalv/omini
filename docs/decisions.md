# Decisions

What was decided, and why, area by area. Decisions are made by consensus
with the maintainer; "maintainer's rule" marks the ones the maintainer set explicitly.
How things work in detail is in the other guides; this page keeps the
choices and the reasons behind them.

- [Product](#product)
- [Stack and repositories](#stack-and-repositories)
- [Discovery](#discovery)
- [Identification](#identification)
- [Topology](#topology)
- [Collection](#collection)
- [Traffic and history](#traffic-and-history)
- [The map](#the-map)
- [Screens and panels](#screens-and-panels)
- [Alerts and notifications](#alerts-and-notifications)
- [Configuration changes](#configuration-changes)
- [VLANs](#vlans)
- [DNS and DHCP](#dns-and-dhcp)
- [Plugins](#plugins)
- [Specific devices](#specific-devices)
- [Operation](#operation)

## Product

- **Identity.** Zero-config discovery and identification, easy integrations,
  and a live map of the network. Docs never compare Omini to other products.
- **Omini manages devices** (maintainer's rule, replacing "read-only"): it
  maps the network and changes the configuration of the devices whose
  integration supports it. Changes are added one at a time, simplest
  first; see [Configuration changes](#configuration-changes).
- **Omini does not replace a device's own interface** (maintainer's rule,
  2026-10-12): a change belongs in Omini when it spans several devices at
  once (a VLAN on the router, the switches and the access points) or is
  easier centralized (it needs what only Omini knows: the topology, the
  catalog, every vendor at once). A setting of one device that its own
  interface already does well (a service restart, a port's speed, one access
  point's channel) stays there; settling a conflict between several access
  points is Omini's (see [Wi-Fi channels](#wi-fi-channels)). Features are for a **simple homelab** (a
  router, a switch or two, access points, a hypervisor): simple to use and
  to build, not niche or enterprise.
- **IPv6 is first-class** (maintainer's rule, 2026-10-18): wherever a device
  has IPv6, the model carries it as it carries IPv4 (device and interface
  addresses, neighbor tables, DNS records, leases, gateways, networks), and
  every plugin reports it where its API can. IPv4-only code is a gap to fix,
  not a choice.
- **IPv6 today** (decided 2026-10-18): plugins report IPv6 addresses, neighbors (NDP), DHCPv6
  leases, IPv6 default routes and AAAA records where their API gives them;
  presence and merging use NDP like ARP; the UI shows IPv6 next to IPv4. DNS
  names Omini sets stay A records (devices rotate their IPv6 privacy
  addresses); AAAA records others made are kept, never deleted.
- **Devices without an API** are inferred from what other devices see (ARP,
  DHCP, MAC tables), or read by an optional scraping plugin.
- **License** MIT. Nothing that would change that is shipped: nmap (NPSL) is
  used only when installed on the host; Fingerbank is not used (it sends data
  to a third party), nor nmap's databases.
- **Language.** Code, docs, commits and issues in English; the UI in `en` and
  `pt-BR`. The language is saved per user (`users.locale`); before login, the
  browser's is used.
- **Look.** Follows the OS theme by default (system / dark / light); color is
  for status (green / yellow / red), traffic and brand icons.

## Stack and repositories

- **Go core** (discovery, SNMP, topology, traffic, insights, API, the UI
  embedded); **Python plugins** for anything vendor- or software-specific;
  **Vue 3** + TypeScript with Vue Flow and a tree layout of our own (ELK.js
  was dropped: its placement looked untidy). SQLite, no external services:
  it must run on a Raspberry Pi.
- **The data contract is the JSON Schema** in `schema/`: Go types and the
  Python SDK's models are generated from it; CI fails when they are stale.
- **Repositories** on the maintainer's personal account (`riccardoalv`, no
  organization, maintainer's rule): `omini` and one `omini-plugin-<id>` each.
- **Releases** by release-please from Conventional Commits; the Docker image
  (amd64 + arm64, Debian slim with Python and uv) goes to ghcr.io and Docker
  Hub on each release. 1.0.0 was the first official release.

## Discovery

- **The network scan is built in and on from the first start**
  (`OMINI_AUTOSCAN`), and **single**: one per Omini (a second is refused,
  older duplicates are removed on start). Its methods can be turned off one
  by one: ARP, ping, open ports, reverse DNS, NetBIOS, mDNS, SSDP, web titles,
  SSH banners, DHCP fingerprints, SNMP.
- **No raw sockets needed for the basics.** ARP comes from the system's
  cache (filled by a UDP probe), ping from unprivileged ICMP sockets.
- **Ports and names are checked once per new host, then every 6 hours**, to
  keep the network quiet.
- **The routers' networks** (private subnets other integrations report, /22
  to /30, up to 16) are scanned too, routed, with "auto" (on by default).
- **SNMP is a method of the scan, not an integration**: every host is probed
  with the configured read-only communities (and an SNMP v3 user, tried
  first); vendor data comes from **YAML profiles**, which a user can add or
  replace. A profile can ask for **MAC tables kept per VLAN** (Cisco).
- **DHCP fingerprints** are read by **only watching** the requests that reach
  this server (a packet socket filtered in the kernel): Omini never opens
  port 67, so a DHCP server on the same machine is untouched.
- **What each method needs** (host network, raw sockets, multicast) is
  documented and **checked at start**; the scan shows it ("On this server")
  and a "discovery is limited" alert says what is missing.
- **nmap** is a separate, single, built-in integration that runs the host's
  nmap in the background; it can also scan one device from its panel. The
  README's and the installation guide's examples set `OMINI_NMAP=install`
  (maintainer's choice, 2026-10-12): the container installs Debian's nmap on
  start, so Omini still does not ship it.
- **Flows** (NetFlow v5/v9, IPFIX, sFlow) are a single built-in integration:
  Omini only listens; exporters are configured by the user.
- **Flows answer questions, not list conversations** (maintainer, 2026-10-13):
  a list of address pairs said nothing. They become **who uses the internet**
  (the Internet screen, replacing Flows, and the internet's node), **where a
  device goes** by name (its panel), **whom it talks to** on the network
  (highlighted on the map by dimming the rest, so no lines are added and
  nothing overlaps) and three alerts (`dns_bypass`, `upload_unusual`,
  `lan_scan`). Internet traffic is a local address talking to an outside one;
  traffic on a router's WAN address is left out (after NAT it counts twice).
- **Where traffic goes in the world** (maintainer, 2026-10-14): the Internet
  screen is a dashboard (usage over time, a world map, the networks' owners,
  the busiest devices, then the list), not a list. Countries and owners come
  from DB-IP Lite (CC BY 4.0, credited on the screen), downloaded at build
  time and embedded (about 9 MB compressed), never asked online; cities are
  left out (60 MB). Per-device destinations stay a day; per-country and
  per-owner totals, with no device, a month.
- **Destination names come from the network's own DNS server** (AdGuard Home
  and Pi-hole report the lookups they answered; Omini asks Pi-hole for the
  addresses, as it logs none). The lookups stay in memory for a day; the
  per-device destinations are stored for a day; no lookup leaves the network.
  The routers' NDP tables tie IPv6 addresses to devices.

## Identification

- `internal/classify` turns evidence into a type, OS, brand and product, and
  keeps the evidence. Users can override the type and the icon.
- **Icons:** homelab software shows its logo alone; other devices show their
  type with an OS or brand badge; an unknown type shows the brand's logo, not
  a question mark.
- **Lists embedded for offline use:** IEEE OUI (MAC vendors), Simple Icons,
  Dashboard Icons, model names (Apple identifiers, Google Play's devices).
- **A web title names an app only by a distinctive phrase**, and a title
  naming the device's own brand is its own interface, not an app.
- **DHCP fingerprints** come from public knowledge (vendor classes, a few
  parameter lists), no third-party database.

## Topology

The topology is a pure function of the devices integrations report (no I/O,
tested with fixtures). The rules that are easy to break:

- **One machine, one node** (maintainer's rule): a device seen by several
  integrations, or by MAC in one and by IP in another, is merged; a device
  reported without a MAC takes the one its address has (DHCP, scans, then
  ARP).
- **A device keeps its history when its key changes** (decided 2026-10-18):
  a managed device's node is named after its key's MAC, so a plugin that
  picks another of the device's MACs (a newer version, a stable SNMP key)
  would make it a new node. When a new device node lists, among its MACs,
  the MAC of a device the inventory knows and the map no longer has, that
  device's alias, position, history, changes, backups and areas move to the
  new node.
- **A machine with two interfaces is two nodes, each with its own address**
  (cable and Wi-Fi). Linux answers ARP for either address from either
  interface, so an ARP address leased to another MAC is ignored when the MAC
  has a lease of its own. A MAC without a lease (a static address inside the
  pool) keeps what ARP says: that is a real duplicate IP.
- **A client stays where it was last seen for sure until it is seen
  elsewhere for sure** (maintainer's rule). For sure: an access point's
  client list, alone on a switch port, behind a desk phone. Not for sure: a
  port shared with other MACs, a port towards other gear, ARP. Kept in the
  database (`attachments`) for a year.
- **The MAC table memory** keeps where each MAC was last learned for 6 hours
  (switches age idle MACs out), for clients present by other means.
- **WAN nodes are parents of their router**, one per uplink; the modem hangs
  under its WAN (seen on the WAN port, or learned in the VLAN that carries
  the uplink through the switches).
- **Proxmox guests** hang under their host when an integration says so; by
  MAC vendor alone only when the network has exactly one Proxmox host. A
  firewall running as a VM is the center of the network, not a branch of its
  host ("Runs on"). A double-NAT router is not the center.
- **End devices on LLDP** (a desk phone, a server running lldpd) are clients,
  not network gear; the computer behind a desk phone hangs from the phone.
  VRRP/CARP virtual MACs are never clients.
- **An unmanaged switch beside a device** is drawn when a switch port has
  clients the device behind it does not list as its own.
- **Port-channels** are uplinks when a member is; a **site-to-site VPN** gets
  a dashed link between the two routers, each site keeping its own internet.

## Collection

- **Rounds** (maintainer's rule, so the map never mixes data of different
  ages): one integration at a time, **from the edge of the network to its
  center** (access points and servers, then switches, then the router;
  built-in discovery last); the map is built once, at the end. One interval
  for every round (15 s to 24 h).
- **An integration that fails keeps its last good data** (its devices shown
  offline) and never stops the round.
- **"Run a round now"** on the Integrations screen (no "Run now" per
  integration, maintainer's rule) runs a whole round skipping caches; the
  map's "Refresh now" runs an ordinary one.
- **Detected devices are kept forever** (manual cleanup), hidden from the map
  after an hour offline, dimmed until then.
- **Presence**: a device that disappears gets its "left" event only after
  `max(3 × round interval, 5 min)`; a new install's first scans are not "new".

## Traffic and history

- **Rates from counters** (difference between two rounds ÷ time; resets skip
  a round; a rate above twice the port's speed is a glitch).
- **Traffic is shown on the devices** (a ↓/↑ badge), links show their speed
  and port name. The flow animation on links was removed (maintainer's rule).
- **History**: per minute for 24 hours, the hourly average and peak for a
  year; a chart opens from a link, a port, a Wi-Fi client or a WAN.

## The map

- **Left to right by default** (top down is a toggle, with leaf children
  packed in a grid); positions are saved per orientation.
- **A tidy tree of our own**: each subtree in its own band, siblings in model
  order (by port, then name). **Nothing may overlap**: nodes, link pills and
  areas they do not belong to (checked in a headless browser).
- **Every node can fold its children** into a bubble (maintainer's rule);
  clients fold automatically above a threshold (8). Expanding or folding
  keeps the node still on screen.
- **Areas are strict** (maintainer's rule): an area holds its members and
  everything below them, and nothing else; a device belongs to one area; an
  area may sit wholly inside another, never partly over it. Areas follow
  their members, can be hidden, recolored (any color) or folded.
- **Every VLAN and subnet is an area on the map** (maintainer's decision,
  replacing "a filter, not areas" and a networks view of cards he turned
  down): the networks are seen at once inside the topology. They are strict
  like any area; a device in several networks (router, trunk switch, access
  point with many VLANs) stays outside them all, a client's own address
  decides its network before the VLAN of its link, and a device in an area
  the user drew stays there. They are not stored, follow their devices and
  can be turned off in the menu's VLANs tab. The filter still highlights one
  network and zooms the map to it.
- **Reset layout simplifies** (maintainer's rule): besides forgetting the
  positions, it folds every node's clients from two of them up, until the
  threshold is changed in Settings.
- **Wi-Fi networks** are mini nodes between an access point and its clients
  (a click folds them).
- **Export** to PNG, SVG, draw.io or JSON, always of the whole map expanded,
  in the theme and orientation chosen.

## Screens and panels

- **No pinning** (maintainer's choice, 2026-10-12): pinning a device on the map
  (never folded into a bubble) was removed as not useful; servers and other
  infrastructure still stay out of bubbles.
- **Screens:** Map (home), Alerts, Integrations, Settings; Internet only while
  its integration is on. VLANs and DNS & DHCP are tabs of the map's menu, not
  screens (maintainer's rule). **The device list is a drawer on the map**, and the
  **plugin store a modal in Integrations** — not screens (maintainer's rules).
- **The map's menu** (hamburger, top left) opens that drawer in four tabs:
  Devices (with the offline and hidden switches), People, VLANs (the VLANs and
  subnets: a click on one highlights it and zooms to it; the networks' areas
  switch) and DNS & DHCP. No filters tab and no filters in the toolbar
  (maintainer's rule); a dot on the menu says the map is filtered.
- **The device panel** slides over the map: summary, health (CPU, memory,
  temperatures, disks, updates), a front view of the ports (click a port for
  its card), the device's extras (services, VPN peers, DHCP pools, firewall
  states, VLANs, SFP optics), "Talks to", alerts and presence.
- **A note per device** (decided 2026-10-18): free plain text (up to 1,000
  characters, no formatting) at the top of the device panel, saved when the
  field loses focus. It lives in the inventory, so it stays while the device
  is offline or has left the map. The map node shows a note mark, the device
  list's search matches it and the JSON export carries it; the diagnostics
  file never does (a note may hold names or passwords).
- **Settings** is a menu of sections; it has no plugin list (maintainer's
  rule). Port descriptions are stored in Omini, never written to the device.
- **People** (decided 2026-10-18): a person is a name and the devices they
  own (a device has at most one owner), set from the device's panel or the
  People tab of the map's menu (a fourth tab, next to Devices, VLANs, DNS &
  DHCP). A person is **home** while one of their phones, tablets or
  wearables is online. **Pause a person** pauses every device of theirs that
  can be paused, after one confirmation, through the same `pause.set` per
  device (each in its history). The Internet screen sums each person's
  devices. People are kept in the store (forward-only migration), never in
  the diagnostics file.
- **API tokens and MCP** (decided 2026-10-18): Settings → **API tokens**
  creates a named token, shown once, stored hashed, with its last use;
  tokens are read-only. They authenticate the HTTP API's GET routes and the
  **MCP server** at `/mcp` (streamable HTTP), whose tools only read: the
  map summary, devices (search, one device with its note, ports and alerts),
  alerts, VLANs and networks, DNS and DHCP, internet use, people. No tool
  changes anything: changes stay confirmed by the user in the UI.

## Alerts and notifications

- **Rules are pure functions of the topology** returning keys and
  parameters; **texts are translated in the UI**, never built on the server.
  An alert opens, refreshes and resolves by its key.
- **Offline after two failed rounds** (maintainer's choice, 2026-10-12): a
  device whose integration failed once shows offline on the map at once, but
  `device_offline` (and `integration_failed`) opens only when it fails a
  second round in a row: a device busy for a moment (a Mercusys unit cutting
  an answer short) is no critical alert.
- **Popups** (maintainer's rule) for open critical and warning alerts, each
  with what to do; closing one hides it until it comes back.
- **Fast Ethernet** warns about wired end devices at 100 Mbps, but not about
  devices made with such a port (cameras, UPSes, air conditioning, badge
  readers and other smart home gear, appliances, solar inverters, desk
  phones; maintainer's rule). Memory alerts are not raised for VMs.
- **IoT beside servers** (`iot_with_trusted`, info; decided 2026-10-18):
  cameras, TVs, media players, speakers, smart home gear, appliances, air
  conditioners and solar inverters on the same network (VLAN, else subnet)
  as servers, NAS, hypervisors, VMs or computers. One alert per network,
  never per device. Printers, game consoles, desk phones and phones are left
  out (they usually live on the main network). Only once the network uses a
  VLAN besides 1: without VLANs there is nowhere to move them. A type the
  user corrected counts as corrected; hidden devices do not count.
- **Alerts from data Omini already has** (decided 2026-10-18):
  `service_stopped` (warning: a service that is enabled is not running);
  `vpn_down` (warning: a VPN peer that was connected in the last 24 hours is
  not any more; peers that never connected, such as a road warrior's phone,
  are not told); `reboot_pending` (info: a device asks for a restart, even
  without a pending update); `wifi_open` (warning: a Wi-Fi network without a
  password; info when it puts its clients in a VLAN of its own, the usual
  guest network); `hot_sensor` (warning: a disk above 55 °C or a board
  above 80 °C; CPUs keep `hot_cpu`); `high_swap` (warning: swap above 50 %).
  A gateway that is down but not tied by name to a WAN interface is told as
  `wan_down` too. A VPN tunnel that is down is drawn dashed in red.
- **Exposed to the internet** (decided 2026-10-18): routers report their
  port forwards (`Device.port_forwards`: protocol, external port or range,
  internal address and port, source, description, on or off, and whether
  UPnP or NAT-PMP made it); the network scan asks the router's UPnP gateway
  (IGD) for its mappings too. `port_forward` (info) for each enabled
  forward, open while it exists; `upnp_forward` (warning) for one a device
  opened by itself through UPnP or NAT-PMP. The router's panel lists them in
  an **Exposed to the internet** section. Omini never changes them.
- **Notifications**: Telegram, e-mail, Slack, Discord, ntfy and webhooks; one
  message per channel per round, a **card per device** with its details, what
  to do and a link to it (maintainer's rule: informative, and no emoji — the
  color says the severity). A flapping alert is not sent again within 30
  minutes. Events (a restart, new devices) are never sent as resolved
  (maintainer's rule: their end is no news). "Send a test" sends an example.

## Configuration changes

The interface stays as simple as possible (maintainer's rule): a change is a
button where the thing is, not a configuration screen.

- **Always available.** No switch turns changes on: a device whose
  integration supports a change shows it.
- **Vendor-neutral changes.** The core knows a short list of changes
  (`port.enabled` first); a plugin declares the ones it supports in its
  manifest and the UI shows only those. Vendor-only features stay in the
  device's own interface.
- **A port can be enabled or disabled when its integration reports its
  state** (`enabled`): a plugin that cannot change a kind of device (a UniFi
  access point) leaves it empty, and no button shows there. Today: the network
  scan (SNMP), Horaco, MikroTik, UniFi, Omada and OpenWrt.
- **Apply, verify, save.** The plugin applies the change, reads the device
  again to confirm it took effect and the device still answers, and only then
  saves it to the startup configuration. A change that lost the device is
  never saved: a reboot undoes it.
- **The map updates at once**: the device read after the change replaces its
  data on the map. This is part of the change, not a "Run now".
- **One short confirmation** for an ordinary change ("Disable port 5
  (NixOS-Laptop)?").
- **The device panel shows little** (maintainer's rule): the alerts, the ports
  and the connected devices; the rest (details and health, identification,
  changes, activity, traffic, VLANs, services...) folds to its title and a
  summary, and the sections the user opens stay open. The port card has one
  action (enable or disable) and a pencil to rename the port; what is connected
  opens from the list of connected devices.
- **Destructive changes are blocked by default** (maintainer's rule): only
  those that hurt the network as a whole: cutting Omini off (the port where a
  device learned Omini's MAC, or that leads to Omini's node) or cutting the
  network off the internet (a device's uplink, a port where it learned the
  router's or firewall's MAC, or that leads to one). Taking a device off the
  network (an access point, a server, another switch) is an ordinary change.
  Settings has **Allow destructive changes** (off by default); with it on, a
  destructive change asks twice.
- **SNMP devices** are changed through IF-MIB `ifAdminStatus` (ports on and
  off) and Q-BRIDGE-MIB (VLANs: `dot1qVlanStaticTable` names, egress and
  untagged port lists, row status; `dot1qPvid`) with a write community (or
  the v3 user). A port joins its new VLANs before it leaves the old ones. SNMP has no standard save: the change lasts as
  the device keeps it (most switches save on their own); a vendor save through
  SNMP profiles waits until a profile needs it.
- **Where Omini runs** is detected, never asked (maintainer's rule): the node
  whose MAC or address is one of this machine's interfaces. In a container
  with a network of its own the network sees the host instead, and Omini is
  not found: then only the way to the internet is protected.
- **History.** Every change (when, what, before and after, success or the
  error) is kept for a year and shown in the device panel under **Changes**.
  A change one device makes for another (a reservation, a pause, a DNS name
  or a Wake-on-LAN on the router or DNS server) is in both histories (decided
  2026-10-18). A refusal repeated as it was is kept once, and Omini's
  automatic retries (DNS names, a paused device's new address) wait longer
  after each refusal, up to 6 hours.
- **A plan stops at the first failure**: a change applied to several devices
  (a VLAN, Wi-Fi per VLAN, Wi-Fi channels) goes one device at a time; the
  first that refuses stops it, and those already done stay.
- **A disabled port** shows as "Disabled" (not "No link") with an **Enable**
  button; the client that was on it stays there, offline.
- **Not planned** (maintainer's rule): jumbo frames, port mirroring, rate
  limits, QoS, IGMP snooping and DHCP snooping; and, decided 2026-10-11,
  **firmware updates, factory reset, device passwords, the management IP and
  HA pairs** (OPNsense/pfSense CARP sync): Omini does not change them, they
  stay in the device's own interface. Other risky changes (LAGs, global STP,
  restoring a backup) wait for a later decision.
- **Order**: enable/disable a port (done: SNMP, Horaco, MikroTik, UniFi,
  Omada, OpenWrt), then VLANs (below), the port's name, rebooting a device,
  configuration backups with history, then reading STP, LAGs and SFP modules
  into the map and alerts.

### Wi-Fi channels

Decided 2026-10-18, not built yet. One access point's channel stays in its
own interface; Omini settles a conflict between several, because only it sees
every access point of every vendor at once.

- **An alert finds the conflict**: two access points of the network on the
  same or overlapping 2.4 GHz channels, or one on a channel other than 1, 6
  or 11. Every access point of a network is taken to hear the others (a
  homelab is one house; plugins do not report what each radio hears).
- **2.4 GHz only, at first** (small first): three channels that do not
  overlap (1, 6, 11), 20 MHz. 5 GHz (widths, DFS) waits for a later decision.
- **Resolve** is a button on the alert. It shows a plan for every access
  point involved before anything changes: it keeps the ones already on a
  free channel, moves as few as it can, and moves the one with fewest clients
  first. More than three access points share the three channels evenly.
- **Never a DFS channel**, and never a width other than 20 MHz.
- **Auto channel is left alone**: an access point whose channel is automatic
  (a UniFi or Omada controller on "auto") is not moved, since its controller
  would move it back; the alert says so. If every access point involved is on
  auto, there is no button.
- **Mesh is left alone**: an access point with a wireless uplink (a mesh
  unit) is never moved, not even with destructive changes allowed: a new
  channel can drop its backhaul and the unit with it.
- **One access point at a time**: apply, check that it answers and its radio
  is up on the new channel, save, then the next. One that fails stops the
  plan; those already done stay (each is a valid state on its own).
- **Clients reconnect** for a few seconds. The confirmation says how many
  clients each access point has; when the browser asking is one of them, it
  says so. It is an ordinary change, except when Omini's own machine is on
  that access point's Wi-Fi: then it is destructive (it cuts Omini off).
- **Plugins** report each radio's band, channel, width, whether the channel
  is automatic, and declare the change `wifi.channel` in their manifest.
- **Model**: `Device.radios` (name, band, channel, width in MHz, whether the
  channel is automatic, transmit power) and `Device.wireless_uplink` (a mesh
  unit); change `wifi.channel` `{radio, channel}`.

## VLANs

VLANs are managed for the whole network in one place (maintainer's rule):
Omini keeps the network's VLANs and applies them to the devices, switch,
access point and router alike.

- **Omini is the source of truth.** The first time, it imports the VLANs the
  devices already have into the network's catalog (id, name, color, subnet).
  From then on VLANs are created, renamed and deleted in Omini, which applies
  them to each device. A device that differs from the catalog (changed by
  hand) raises an alert.
- **Ports**: access (one untagged VLAN) and trunk (tagged VLANs, with or
  without a native one), only with VLANs of the catalog. The port's VLANs are
  a vendor-neutral change (`port.vlans`).
- **The path is automatic.** Putting a port in a VLAN creates the VLAN on
  every switch on the way to the router and tags it on each uplink, from the
  map. Omini shows what it will change on each device before asking for one
  confirmation.
- **The same protection**: changing the VLANs of the port Omini comes
  through, or of the way to the internet, is destructive.
- **Changes** `vlan.set` (create or rename) and `vlan.delete` (never VLAN 1,
  nor one a port still uses), next to `port.vlans`. Adding a tagged VLAN to a
  protected port is not destructive; changing its native VLAN or removing one
  is.
- **Phases**: first the catalog and the switches (Horaco, the maintainer's,
  tested for real; then MikroTik, UniFi, Omada, OpenWrt and SNMP), then the
  router (below). Done: `port.vlans`, `vlan.set` and `vlan.delete` on SNMP
  (Q-BRIDGE-MIB), Horaco, MikroTik, UniFi, Omada and OpenWrt; only Horaco is
  tested on real hardware, the others against the devnet and the vendors'
  documented APIs.
- **A switch mode is never turned on by Omini**: when VLAN changes need the
  device's VLAN filtering (MikroTik, OpenWrt) and it is off, the change is
  refused with where to turn it on in the device's own interface.
- **Vendor shapes stay in the plugin**: VLANs that belong to a whole site
  (UniFi, Omada) change every switch of the site at once; Omada ports take
  VLANs through port profiles (an equal one is reused, else an "Omini …" one
  is created; existing profiles are never edited) and always have a native
  VLAN; UniFi and Omada networks with a gateway are deleted in the
  controller itself.

### Phase 2: a VLAN is a whole network

Omini should be an almost perfect interface to set up VLANs on switches and
routers (maintainer's goal): creating one is a single short form, and Omini
does the rest on every device.

- **A VLAN in the catalog is a network**: id, name, color and, when the
  network has a router Omini can change, its **gateway** (address and
  prefix, e.g. 192.168.30.1/24, suggested from the id), its **DHCP** (on by
  default, the range suggested as .100 to .250), its **access** and its
  **discovery** (mDNS). Without such a router a VLAN is just a VLAN on the
  switches, as in phase 1.
- **Access**, one choice per VLAN (maintainer's choice, over "always
  isolated" and free rules): **Internet only** (the default: internet, no
  other network), **Whole network** (internet and every other network) and
  **No internet** (its own network only, e.g. cameras). Omini writes the
  firewall rules for it; finer rules stay in the router's own interface.
- **The router does it all in one change** (`network.set`): the VLAN
  interface on the router's port toward the switches, its assignment and
  gateway address, the DHCP range (with the gateway as DNS) and the access
  rules. It is applied, verified and saved like any change; `network.delete`
  undoes all of it. OPNsense first, through its API: 26.7 or later (interface
  assignments came to the API in 26.7); an older one gets a note to do it by
  hand, as phase 1 does today. Then pfSense (REST API package v2), MikroTik
  (RouterOS REST) and OpenWrt (uci with rollback), with the same three access
  choices written as each one's rules, marked "Omini:". A plugin with no
  undo of its own (MikroTik) undoes its writes itself when the check fails.
- **What exists is imported and can be changed** (maintainer's rule, over
  "only what Omini created"): the router's networks (each VLAN's interface,
  gateway, DHCP range and mDNS) come into the catalog the first time, and
  any of them can be changed from Omini. The untagged LAN is VLAN 1's
  network. Access is read from the network's rules: rules that match one of
  the three choices show it; others show **Custom**, kept as they are until
  a choice is picked, which replaces that network's rules (the form says
  so). The WAN's VLANs (an operator's PPPoE VLAN) are not the network's: they
  are never imported nor shown.
- **The network Omini is in is protected**: changing its gateway or prefix,
  giving it No internet or deleting it is destructive (blocked by default,
  asked twice when allowed).
- **Switches on demand** (maintainer's choice, over carrying every VLAN on
  every uplink at once): creating a VLAN changes the router only; a switch
  gets the VLAN, and the way to the router its tag, when one of its ports is
  put in it (phase 1's plan).
- **Ports from the VLAN** (maintainer's choice): opening a VLAN shows the
  front of every switch that can change VLANs; a click on a port goes out →
  in the VLAN (access) → tagged (trunk) → out, with the plan shown before
  applying. The port's card keeps its "Change VLAN".
- **Also in this phase** (maintainer's choice): **IP reservations** (a fixed
  address for a device, from its panel: `reservation.set`/`reservation.delete`
  on the router), **moving a device to a VLAN** (its panel: the port it is on
  changes, with the plan), **discovery between VLANs** (the router's mDNS
  repeater for the VLANs that ask for it; OPNsense needs its os-mdns-repeater
  plugin, MikroTik RouterOS 7.16; pfSense and OpenWrt have none Omini can
  drive, so their networks report it unknown and the form hides the switch) and **Wi-Fi per VLAN** (a network name per VLAN on the access points
  that can: UniFi, Omada, OpenWrt; Mercusys cannot).
- **VMs and containers in VLANs**: a guest's virtual NIC is a port whose
  VLANs (Proxmox's tag and trunks) Omini changes like a switch port's; the
  hypervisor's bridge must be VLAN aware (set by hand: Omini never changes a
  host's network), and the plan goes on through it to the switch port the
  host is on. A device Omini cannot change on the way (a hypervisor, a dumb
  switch) is a note, not the end of the plan.
- **Deleting a VLAN** removes it from the router and the switches; with
  devices still in it, the confirmation says how many lose their network. It
  is not destructive in Omini's sense (Omini and the internet stay up).
- **Order**: the network in the catalog and OPNsense's `network.set` (with
  the devnet's OPNsense taking writes), the VLAN's port panel, reservations
  and moving a device, mDNS, then Wi-Fi per VLAN; the other switch plugins'
  `port.vlans` in between as they come.

## Homelab tools

Features the maintainer chose (2026-10-11 and 12) for a simple homelab, all
built on 2026-10-12: each spans several devices or is easier centralized (see
[Product](#product)). Order of work: fixing addresses, DNS names,
Wake-on-LAN, pausing the internet, STP and LAGs, then backups. Only Horaco is
tested on real hardware; the plugins' READMEs list what was taken from the
vendors' documentation. Each follows [Configuration changes](#configuration-changes):
declared by the plugin, confirmed, applied, verified, saved and kept in the
device's history. **Fix IP, Wake and Pause internet are buttons on the
device's panel** (maintainer's rule); fixing addresses also has a button
for many devices at once.

### Fix the infrastructure's addresses

- **On the device's panel**, the **Network** section's "Fix IP" (there since
  phase 2) keeps the current address and, with it, the DNS name below.
- **One button for many**, "Fix infrastructure IPs", in the map menu's **DNS & DHCP**
  tab, opens a list of the devices that still get their address by DHCP
  (their MAC is in a router's leases and has no reservation) and whose type
  is infrastructure: switch, access point, server, NAS, hypervisor, virtual
  machine, printer and UPS. Each has a checkbox (all on); other devices can
  be added from the same list. Applying makes one `reservation.set` per
  device on the router that serves it, each its own change in the history.
- **The current address is kept** (maintainer's choice, over "always outside
  the pool"): nothing changes for the device. When the router refuses a
  reservation inside its DHCP range (pfSense with ISC), Omini proposes a free
  address outside the range, shown in the list before applying, and says the
  device takes it at its next lease renewal.
- **Only a router that serves DHCP** on the device's network is asked
  (decided 2026-10-18). A device leased by a server Omini cannot reserve on
  (Pi-hole, AdGuard Home) is listed with that server, to fix there.
- **Omini's own host is never given another address** (it would move Omini
  away from its users): it is left out of the list, and from its panel a new
  address needs destructive changes allowed.
- **Alert** `infra_dynamic_ip` (info): an infrastructure device on DHCP with
  no reservation, with the same button.
- Needs nothing new from the plugins: OPNsense, pfSense, MikroTik and OpenWrt
  already have `reservation.set`.

### Local DNS names

- **A device with a fixed address gets a name** (maintainer's choice, over
  every named device): `<name>.<domain>`, the name as Omini shows it in
  lowercase with spaces as dashes (`nas`, `living-room-ap`). Fixing an
  address creates it, renaming the device changes it, freeing the address
  removes it. The device's panel shows the name next to the address, and the
  name can be edited or turned off per device.
- **The domain** is the one the router already hands out (its system or DHCP
  domain); without one, `home.arpa` (RFC 8375). Settings can change it.
- **Where the record goes**: the DNS server the device's network hands out
  by DHCP, which Omini already reads. If it is **AdGuard Home** or
  **Pi-hole** (new plugins, maintainer's requirement), the record goes there;
  if it is the router, to the router. One server per network; Omini never
  writes the same name to two.
- **Changes** `dns.set` `{name, ip}` and `dns.delete` `{name}`; integrations
  report `Device.dns_records` `[{name, ip}]`. Records set by hand are
  imported and shown, and changed only when the user edits them in Omini.
- **Integrations**: AdGuard Home (rewrites: `/control/rewrite/list|add|delete`,
  basic auth), Pi-hole v6 (`/api/config/dns/hosts`, an application
  password), OPNsense (Unbound host overrides, or dnsmasq hosts),
  pfSense (DNS resolver host overrides), MikroTik (`/ip/dns/static`), OpenWrt
  (`dhcp` `domain` sections). AdGuard Home's and Pi-hole's DHCP leases and
  query counts per client are read too, as their plugins' collection.

### Wake-on-LAN

- **A "Wake" button** on the panel of an offline device that has a MAC, and
  on the device list for one that has left the map (off for more than an
  hour; decided 2026-10-18), never for a random MAC.
- **On Omini's own networks** Omini sends the magic packet itself (UDP
  broadcast to port 9 on the interface in that network). **On another
  network** (maintainer's choice) it asks the router of that network: change
  `wol.send` `{mac, ip}` (the router picks the interface whose network holds
  `ip`) on OPNsense (its os-wol plugin), pfSense,
  MikroTik (`/tool/wol`) and OpenWrt (etherwake). Without either, there is
  no button.
- It is not a configuration change: nothing to verify or save. The history
  of the device records it, and the panel shows when the device comes back
  online.

### Pause a device's internet

- **A "Pause internet" button** on a client's panel; "Resume" undoes it. The
  device still reaches its own network (the NAS, the printer); only the
  internet is cut. Paused devices show a pause mark on the map and are listed
  in the device drawer.
- **On the router that serves the device**, over **IPv4 and IPv6**
  (2026-10-12: the maintainer's phones kept browsing over the ISP's IPv6
  while only their IPv4 was blocked): by the device's MAC where the router
  can (an OPNsense MAC alias resolved from ARP and NDP, OpenWrt `src_mac`
  rules, MikroTik's IPv6 filter), else its IPv4 and the IPv6 addresses the
  router's neighbor table knows (pfSense); local destinations (private
  IPv4, link-local, ULA, multicast and the LAN's IPv6 prefixes) stay
  reachable. Everything is marked "Omini".
  Changes `pause.set` `{mac, ips, name}` and `pause.delete` `{mac}`;
  routers report `Device.paused` `[{mac, ips, name}]`. When a paused device
  gets another address, Omini updates the list in the next round. Open
  connections are cut where the router can (firewall states).
- **Blocks set by hand are imported** (maintainer's choice, 2026-10-12): a
  simple block rule on the router (OPNsense: from hosts to anything, no ports
  or schedule) shows its devices as paused (`PausedDevice.rule`, the rule's
  name); resuming one turns the rule off when it blocks that device alone,
  and Omini never rewrites it when the device's address changes.
- **Protected**: Omini's own host, the router and network devices cannot be
  paused.
- **Controllers pause the internet only** (maintainer's choice, 2026-10-18):
  UniFi and Omada pause through their gateway's internet rules (UniFi
  traffic rules, Network 7 or later; an Omada gateway ACL towards the WAN),
  never through their client block, which also cuts the device off its own
  network. A controller too old for those rules, or a site without their
  gateway, shows no button.
- **Not on DNS servers** (maintainer's rule, 2026-10-18): AdGuard Home and
  Pi-hole could refuse a device's lookups, but a device with its own DNS
  server or DNS over HTTPS goes around it, so they never pause.

### STP and LAGs on the map

- **Read-only** (creating LAGs and changing STP stay out). SFP modules are
  already read (`sfp_low_rx`); a hot module is added: `sfp_hot` (warning)
  above the module's own high alarm, or 70 °C without one.
- **STP**: each port reports its state and role (`Interface.stp`
  `{state: forwarding|blocking|learning|disabled, role: root|designated|alternate|backup}`)
  from BRIDGE-MIB (SNMP), MikroTik, UniFi, Omada and OpenWrt. A link whose
  port is blocking is drawn as **standby** (dashed, no traffic) with its
  reason (the map is a tree: a redundant cable that is not a device's uplink
  is not drawn; its port shows "blocked" in the port's card). Alert
  `stp_root_changed` (warning, an hour): the root bridge a switch sees changed.
- **LAGs**: a member port reports its LAG (`Interface.lag`, the LAG's name);
  from IEEE8023-LAG-MIB (SNMP), MikroTik bonds, UniFi and Omada aggregates,
  OPNsense lagg and OpenWrt bonds. The cables of a LAG between two devices
  are one link whose pill shows its ports and their speed ("2 × 1G"); the
  port keeps its name, so the link's traffic is its counters. Alert
  `lag_member_down` (warning): a member is down while the LAG is up.

### Configuration backups with history

- **What**: each device's configuration as readable text, for history and
  differences, never for restoring (restoring stays out). The plugin builds
  it: the device's own text export where there is one (OPNsense
  `config.xml`, MikroTik `/export` without sensitive values, OpenWrt's uci
  files), else its settings as sorted JSON (UniFi, Omada, Proxmox guests and
  networks, Horaco). Plugin method `backup`, declared in the manifest; SNMP
  devices have none.
- **When**: once a day, and after every change Omini makes on the device. A
  version is kept only when it differs from the last one.
- **Not every integration can**: pfSense's REST API has no configuration
  export (only through its shell command endpoint, which Omini does not ask
  for: maintainer's rule, 2026-10-18, no plugin asks for a shell, nor for
  XML-RPC credentials), and SNMP devices have none; UniFi and Omada keep the site's settings
  in one device's backup (the gateway's, else the lowest MAC), so a site
  change is one change.
- **Kept**: in SQLite, compressed and encrypted like credentials (a
  configuration holds secrets); the last 50 versions per device. Only the
  admin sees and downloads them; they never reach logs.
- **Where**: a **Configuration** section in the device's panel with the
  versions (date, "by Omini" or "outside Omini"), the differences between
  any two, and a download.
- **Alert** `config_changed` (info): a version differs from the previous
  one and Omini made no change on that device, nor through its integration,
  in between (a UniFi or Omada site's settings live in one device's backup).

## DNS and DHCP

- **One screen, "DNS and DHCP"**, with a tab each: servers, pools (range and
  usage) and leases for DHCP; servers and response times for DNS; and the
  result of each check.
- **DHCP is checked every round**: Omini sends a DHCPDISCOVER and waits for
  the offer (it never sends a REQUEST: no address is taken). No offer is an
  alert. More than one server answering is an alert too (a rogue DHCP server:
  a router plugged in the wrong way), with its address and MAC. A pool almost
  full is the existing alert.
- **DNS is checked every round**: each DNS server of the network (those DHCP
  hands out and those integrations know) resolves an external and a local
  name, timed. Alerts: not answering, slow, a local name that does not
  resolve.

## Plugins

- **One repository per plugin**, installed from a GitHub release (or the
  default branch), into a **uv** environment of its own with **the SDK
  embedded in the running Omini**, so the protocol never drifts.
- **Exec per collection**: the request on stdin, devices on stdout, logs on
  stderr; secrets are decrypted only for the plugin and never logged.
- **The store** has a curated list (fetched daily, shipped as a fallback) and
  accepts any GitHub repository. Each plugin shows a **publisher**
  (official / community) and a **trust level** set by the maintainers for a
  release (plug-and-play, stable, experimental, unverified — always the level
  of a URL import). Review process: [plugin-review.md](plugin-review.md).
- **New plugins** (decided 2026-10-18, experimental until tested on real
  hardware): Docker (the Engine API over TCP, or Portainer's API: containers
  as devices under their host, with ports and state), TrueNAS (pools,
  disks, SMART health, apps) and Synology DSM (volumes, disks, SMART, Docker
  containers). Read-only, local repositories until the maintainer publishes
  them.
- **One SDK for every plugin** (decided 2026-10-18): the SDK gives an HTTP
  client (timeouts, retries on refused connections and 429, a login backoff,
  a session cached in `state_dir`), maps Forbidden and NotFound to errors
  that name the privilege or resource, and an apply → verify → undo helper
  (about 30 s to verify; undo whenever the API allows). Plugins use it;
  conventions (key, role, vendor, form fields) are in plugins.md.
- **Parity of changes** (decided 2026-10-18): `port.enabled` on OPNsense,
  pfSense (an interface) and Proxmox (a guest NIC's link); UniFi and Omada
  gateways read and change networks, reservations and DNS names, and pause
  the internet only (see Pause); MikroTik reads and changes Wi-Fi (not under
  CAPsMAN).

## Specific devices

- **OPNsense**: official REST API with a least-privilege user; reads
  interfaces (speed, media, connectors), ARP, DHCP leases, gateways, health,
  updates, services, VPN peers, firewall states and SFP optics.
- **Horaco HC-SWTGW218AS**: no SNMP on stock firmware, so the plugin reads its
  web pages; it only ever posts page navigation, and retries after the switch
  drops every connection for a few seconds.
- **Mercusys Halo**: the units' local API (TP-Link Deco protocol); each unit
  is an access point; no per-client signal in the local API (only the cloud
  app has it, and the cloud is not used); the Wi-Fi passwords that come with
  the network names are never kept.
- **Proxmox VE**: a read-only API token (PVEAuditor); clusters are read
  through any node; VMs report the memory they were given.

## Operation

- **One admin user**, created on the first visit; the password is changed in
  Settings or reset on the host (`omini reset-password`). Device credentials
  are encrypted at rest.
- **Migrations only go forward**: an older Omini refuses a database a newer
  one changed; going back means restoring a backup.
- **The devnet** (`testdata/devnet`, `make devnet`) is Acme, a realistic
  company network whose device APIs are emulated, so the real plugins run
  against it; the Go test checks every placement and the week's alerts
  against its design. No core code knows about it.
- **Testers come first** (maintainer, 2026-10-13): the project is young and
  has seen few networks, so reaching people with other gear matters more than
  new features. [Testing Omini](testing.md) shows how to try it safely (read-only
  accounts; Omini changes nothing unless a change is clicked and confirmed, so
  changes stay on without a global switch) and how to report.
- **Diagnostics file** (Settings → About, `GET /api/diagnostics`): what Omini
  collected and drew, anonymized for a public issue. MACs keep their vendor
  part (identification needs it); private IPv4 addresses stay (the networks'
  shape is what placement bugs need); names, SSIDs, serials, URLs' host names,
  public IPv4 and every IPv6 address become stable placeholders; passwords,
  integration settings, layout and configuration backups are left out. The
  user looks it over before attaching it.
