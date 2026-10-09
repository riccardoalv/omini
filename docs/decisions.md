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
- [Plugins](#plugins)
- [Specific devices](#specific-devices)
- [Operation](#operation)

## Product

- **Identity.** Zero-config discovery and identification, easy integrations,
  and a live map of the network. Docs never compare Omini to other products.
- **Read-only.** Omini never changes a device's configuration. Write actions
  come later, behind explicit permissions (first candidate: Wake-on-LAN).
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
  nmap in the background; it can also scan one device from its panel.
- **Flows** (NetFlow v5/v9, IPFIX, sFlow) are a single built-in integration:
  Omini only listens; exporters are configured by the user.

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
  reported without a MAC takes the one its address has (ARP, DHCP, scans).
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
- **VLANs and subnets are a filter, not areas** (maintainer's decision after
  comparing other tools): the picker highlights a network's devices and draws
  a temporary frame around each group of them.
- **Wi-Fi networks** are mini nodes between an access point and its clients
  (a click folds them).
- **Export** to PNG, SVG, draw.io or JSON, always of the whole map expanded,
  in the theme and orientation chosen.

## Screens and panels

- **Screens:** Map (home), Alerts, Integrations, Settings; Flows only while
  its integration is on. **The device list is a drawer on the map**, and the
  **plugin store a modal in Integrations** — not screens (maintainer's rules).
- **The device panel** slides over the map: summary, health (CPU, memory,
  temperatures, disks, updates), a front view of the ports (click a port for
  its card), the device's extras (services, VPN peers, DHCP pools, firewall
  states, VLANs, SFP optics), "Talks to", alerts and presence.
- **Settings** is a menu of sections; it has no plugin list (maintainer's
  rule). Port descriptions are stored in Omini, never written to the device.

## Alerts and notifications

- **Rules are pure functions of the topology** returning keys and
  parameters; **texts are translated in the UI**, never built on the server.
  An alert opens, refreshes and resolves by its key.
- **Popups** (maintainer's rule) for open critical and warning alerts, each
  with what to do; closing one hides it until it comes back.
- **Fast Ethernet** warns about wired end devices at 100 Mbps, but not about
  devices made with such a port (cameras, UPSes, air conditioning, badge
  readers and other smart home gear, appliances, solar inverters, desk
  phones; maintainer's rule). Memory alerts are not raised for VMs.
- **Notifications**: Telegram, e-mail, Slack, Discord, ntfy and webhooks; one
  message per channel per round, a **card per device** with its details, what
  to do and a link to it (maintainer's rule: informative, and no emoji — the
  color says the severity). A flapping alert is not sent again within 30
  minutes. "Send a test" sends an example.

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
