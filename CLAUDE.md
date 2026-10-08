# CLAUDE.md

Guide for agents (and humans) working on Omini. Read the [README](README.md) first for the product vision and MVP scope.

## The project in one sentence

A self-hosted tool that reads data from network devices and software of many vendors, normalizes it into a common model, and renders a live topology + traffic flow map, client inventory and health insights in a web UI.

## Status

**v0.2 released.** v0.2 complete: insights and alerts, 24 h traffic history (and hourly for a year), presence timeline, Alerts screen. Network scan (with SNMP), identification (types, OS, brands, products, model names), app nodes, nmap, topology map (areas, WANs, port front view, live traffic), plugin runtime + SDK + store, OPNsense plugin, Docker image, auth, en + pt-BR. v0.2 so far: per-device nmap scan, panel redesign, port names on links, system health. Next: insights, 24h traffic history, presence timeline (full list: README → Roadmap).

Product and architecture decisions are made by consensus with the maintainer: raise questions and trade-offs instead of deciding unilaterally, then record agreed decisions here and in the README. Every change ships with tests that run in CI.

## Decisions log

| Topic | Decision |
|---|---|
| Positioning | Omini has its own identity: zero-config discovery and identification, easy integrations and a traffic flow map. Docs never compare Omini to other products or define it by them |
| MVP core | Automatic discovery + integrations screen + topology map with per-link traffic |
| "Flow" in the MVP | Per-link bandwidth utilization from interface counters; "who talks to whom" from NetFlow/IPFIX/sFlow came after (see Flows) |
| Writes to devices | Read-only in the MVP; write actions later, behind explicit permissions |
| Devices without SNMP/API | Inferred from other devices' data (ARP/DHCP/FDB) **and** optional scraping plugins |
| License | MIT |
| Language | Everything in English (code, comments, docs, commits, issues). UI is translatable (i18n): `en` + `pt-BR` |
| Core | **Go**: discovery, generic SNMP + YAML profiles, LLDP, topology engine, traffic rates, insights, API, serving the UI |
| Integrations | **Go** for standard protocols (SNMP/LLDP/ARP/ICMP); **Python plugins** for anything vendor/software specific (OPNsense, Mercusys, UniFi, MikroTik...) |
| Plugin runtime | uv-managed venv per plugin, SDK embedded in the core and installed into every venv, install from GitHub release tarballs, `OMINI_PLUGIN_DIRS` for development — see "Plugin runtime (core)" |
| Docker image | `ghcr.io/riccardoalv/omini` and Docker Hub (`<DOCKERHUB_USERNAME>/omini`, when the repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` are set) (amd64 + arm64), built and pushed by the release workflow when release-please creates a release: Debian slim with Python 3 and uv (plugins), app icons bundled; data in `/data`; `network_mode: host` recommended; `OMINI_NMAP=install` installs nmap on start |
| Python runtime | Always bundled in the official image, so every plugin works out of the box |
| Plugin protocol | Exec per collection: core runs the plugin, sends config as JSON on stdin, reads devices as JSON on stdout |
| Frontend | **Vue 3** + TypeScript (Vite); map with **Vue Flow** and its own tree layout (`lib/layout.ts`, see "Map layout"; ELK.js was dropped: its placement looked untidy) (left-to-right: firewall on the left, clients stacked on the right — top-down made wide networks unreadable); vue-i18n; built assets embedded in the Go binary. Tooling: Vitest, ESLint + oxlint, Prettier, vue-tsc. Node.js 24 |
| Authentication | Single admin user created on first run; device credentials encrypted at rest in SQLite |
| Traffic history | Every rebuild records each interface's rate (and a Wi-Fi client's own, interface `""`): one point per minute kept **24 h** (`traffic_minutes`) and the hourly average + peak kept **a year** (`traffic_hours`, rebuilt from the minutes so a replaced minute never counts twice). `GET /api/history?node&iface&hours` (per minute up to a day, per hour beyond). Chart (`TrafficChart`, plain SVG, ranges 1 h / 24 h / 7 d / 30 d / 1 y, average and peak, gaps where data is missing; opens on 1 h while Omini has only minutes of data): click a **link** (the upstream port's history, else the device's port, else the Wi-Fi client's), a **port's card**, a Wi-Fi client or a WAN in the panel |
| Plugin distribution | Each plugin is its own Git repository. Built-in **plugin store** with a default curated list (one-click install) + install from any GitHub URL. Reference model: Home Assistant's HACS. Installs pinned to a release |
| Plugin trust | Publisher badge (`official` / `community`) + trust level assigned by maintainers: `plug-and-play` (fully tested, works out of the box), `stable` (tested, known issues documented), `experimental` (partially tested), `unverified` (not reviewed; always the level for URL imports). Levels belong to a release (`reviewed_version`, `reviewed_at`, `known_issues` in the index); the store says when the installed release is newer than the one reviewed. Process and criteria in `docs/plugin-review.md`; requests through the "Plugin review" issue form |
| Firewall | **OPNsense** via its official REST API (key/secret, dedicated least-privilege user, HTTPS) as a Python plugin in its own repo (`omini-plugin-opnsense`). v0.1 reads: interfaces with status, IPs, **link speed/media** and traffic counters; ARP; DHCP leases (ISC, Kea or dnsmasq, whichever is in use); CPU, memory, uptime, version; **gateway status** (up/down, latency, loss). pfSense is post-MVP |
| Releases | Incremental: v0.1 core + SNMP + network scan + identification + OPNsense + topology map + login; v0.2 traffic flow + 24h traffic history + insights + presence timeline; v0.3 plugin store + trust levels + YAML profiles; v0.4 Mercusys + pt-BR + refined discovery (public launch) |
| Test network | OPNsense (router/firewall, DHCP), managed Horaco **HC-SWTGW218AS** switch, Mercusys routers in **AP mode** (clients visible in OPNsense ARP/DHCP) |
| Horaco HC-SWTGW218AS | No SNMP on stock firmware: Python plugin [`omini-plugin-horaco`](https://github.com/riccardoalv/omini-plugin-horaco) reads its web interface — `/info.cgi` (model, firmware V200.x, MAC, uptime, port status), `/port.cgi?page=stats` (64-bit packet and byte counters as `hi-lo`), `/panel.cgi` (RJ45/SFP from each port's `div` class) and `/mac.cgi?page=fwd_tbl` (paged with its form, `cmd=goto`). The session cookie is `md5(user+password)`: reused, login posted only when asked (the switch keeps one session). Only page navigation is ever posted. The switch now and then closes every connection at once for a few seconds: a page is tried again after a 6 s pause, and a page that still fails never fails the collection (the MAC table memory covers it) |
| Mercusys Halo | Python plugin [`omini-plugin-mercusys`](https://github.com/riccardoalv/omini-plugin-mercusys): the units' local web interface (TP-Link Deco protocol: RSA password, AES requests signed with `h=md5(admin+password)&s=seq+len`, the login's signature also carrying `k`/`i`; the password at the top level of the login payload, unlike Deco). Each unit is an AP; clients read unit by unit (`access_host` is always "1"); no per-client signal or link rate in the local API (checked every model, controller and view of its web interface: only the Mercusys cloud app has them). Wi-Fi clients with band, network name (`/admin/wireless?form=wlan`: only the SSIDs are read — that answer also carries the Wi-Fi passwords, never kept) and **current traffic** (`rx_bps`/`tx_bps` — the units report no link rate), wired ones in the FDB (`LAN`), names as hosts; requests carry `Content-Type: application/json` like the web interface (else "no such callback"). Session kept in the state folder, 15 min pause after a refused login |
| Wi-Fi clients on the map | A client node carries its band, link rate (`tx/rx_rate_mbps`, when the AP reports it) and current traffic (`rx_bps`/`tx_bps` from the AP): the traffic is the ↓/↑ badge on the device (none while idle; on top of every node — compact for clients, in the gap above them), each Wi-Fi network of an AP is a **mini node** between it and its clients (AP → "IOT · 2.4 GHz" → clients; icon colored by band; no panel, not in the inventory, draggable like any node; **a click on it folds or unfolds its clients** (as do a middle click and its context menu, which has no "details"); collapsing the AP gathers its networks and their clients in one bubble — `withWifiNetworks` in `lib/graph.ts`), so Wi-Fi links have no pill; the link rate is in the client's panel. App links have no pill either (the app shows its port) |
| Plugin order | OPNsense (v0.1), then Horaco and Mercusys (v0.3) |
| Plugin store icons | Catalog `icon` is a logo slug (Simple Icons, or the bundled Dashboard Icons, e.g. `mercusys`) or a device type (`switch`) when the brand has no logo (Horaco) |
| Unmanaged switch beside a device | A client seen only on the switch port towards a device is placed behind that device — unless the device's integration lists its clients (Wi-Fi or wired) and this one is not among them: then an unmanaged switch is on that port, drawn as a segment with the device and those clients under it (e.g. switch Port 5 → unmanaged switch → {Halo AP, laptop by cable}). A MAC that only the switch's table vouches for there (no ARP, no scan) is not shown at all: a stale entry, or something even the AP does not see |
| MAC table memory | Switches age idle MACs out after ~5 min (a phone asleep), which made clients jump to wherever ARP saw them (the firewall's LAN). The collector remembers where each MAC was last learned (6 h, `collector.LastSeenTTL`; kept in the `mac_ports` table, so it survives restarts) and the topology uses it (`topology.Options.LastSeen`) for clients present by other means (ARP, DHCP, Wi-Fi) — never to make one that left look present — and to keep a switch's uplink where the router was last learned (else the uplink came and went, and what is behind the router looked like a segment) |
| Detected devices | Kept **forever** (no automatic purge), with `first_seen` / `last_seen`; manual cleanup in the UI (bulk delete, filter by randomized MAC). Hidden from the map after **1h offline** (dimmed until then), always kept in the inventory |
| Presence timeline | `presence_events` (join/leave, `first` for the first sighting ever; kept a year) for clients, devices and LLDP neighbors. A device that disappears gets its leave event (at the time it was last seen) only after missing for `max(3 × poll interval, 5 min)` — no flapping. "New device" insight (info, 24 h) for devices first seen after tracking started (`presence_since` setting) + 15 min, so the first scans of a new install are not "new". Timeline in the Alerts screen (by day, "new devices only", paging) and the last events in the device panel |
| UI screens | Map (home, full screen), Devices (inventory), Integrations, Settings in v0.1; Alerts (alerts + timeline; called Insights until it was renamed by the maintainer — `/insights` redirects to `/alerts`) in v0.2; Flows only in the menu while the flows integration is on (`lib/features.ts`, fed by every `api.integrations()` call). The store is **not a screen** (maintainer's decision): a modal in Integrations (`/store` redirects there and opens it) |
| Map clients | All clients shown as nodes; a group (AP / switch port) with **more than 8** clients collapses into a "N clients" bubble that expands on click. Threshold configurable; expanded/collapsed state persisted. Pinned devices and servers always visible |
| Node details | Click opens a **slide-over side panel** (summary, ports with status/speed/traffic, connected clients, alerts) without leaving the map; "open full page" link |
| Repositories | Personal GitHub account (`riccardoalv`): no organization (decided by the maintainer). Main monorepo `omini` + one repo per plugin (`omini-plugin-<id>`) |
| Data contract | **JSON Schema** in `schema/` is the single source of truth; Go types and Python (pydantic) models are generated from it; CI fails if generated code is stale |
| Plugin format | `plugin.yaml` manifest (id, name, version, protocol version, entrypoint, form fields) + `requirements.txt`; Python SDK `omini-sdk` handles stdin/stdout, validation and errors so authors only write `collect()` and `test()` |
| Dev environment | `make dev` runs the Go backend and the Vite dev server together; `make run` builds and runs the production-like binary. Optional Nix flake. (Docker Compose + SNMP simulator: later) |
| Map areas | Named rectangles drawn on the map ("New area" button, then drag). An area **remembers its member nodes** (those inside when it is drawn or resized, plus devices dropped into it; dragging a device out removes it) **and everything below them** (apps, clients, VMs) and is always drawn around them, so it follows them when the map is laid out again (reset, expand, new devices). The automatic layout gives each area a band of its own (see "Map layout"), so its devices stay together and no other device lands inside it. Moving an area moves its members. **Hide** (menu: the frame leaves the map and stops grouping the layout, its devices stay; `areas.hidden`; "Show hidden (N)" in the toolbar counts hidden devices and areas and shows hidden areas dimmed, to show them again from the menu). Name, color (6 presets or any `#rrggbb` from the color picker in the menu: saturation square and hue bar from **vanilla-colorful** (MIT, web components), hex and R/G/B fields, shown live on the area and saved with "Apply"), resize from the corner (re-picks the members), right-click menu on the title (rename, color, delete). Stored in the `areas` table; shown in both orientations (drawn around the same devices). "Reset layout" keeps areas. Collapsing an area into a bubble: later |
| UI language | Saved per user in the database (`users.locale`) and applied on login; the language picked on the setup screen becomes the user's. Before login, the browser language is used |
| Expand/collapse | The expanded/collapsed node stays still on screen; the map is laid out again around it (no overlaps). The viewport only refits on first load, orientation change, layout reset and sidebar toggle |
| Map layout | A **tidy tree** of our own (`treeLayout` in `lib/layout.ts`, no ELK): every level in one column (left to right) or row (top down), as wide as its widest node; each subtree in a band of its own across; a parent centered on its children (a parent wider than them: they are centered under it); a node with several parents hangs from the first, and parents with nothing else below them (a second WAN) stand next to that one. Gaps: 150 px between columns / 100 between rows (room for the pill and, top down, the traffic badge), 22 px between siblings (+6 above devices, for their badge) / 24 across top down. **Areas:** siblings entering the same area are put next to each other (where the first was); their run gets the area's padding across (and its title, left to right), and a level where an area starts gets its border (and title, top down) before it — so an area's box never takes in another node. Top-down leaf grids (packLeaves) are nodes of the tree. A node without links in an area stands as a sibling of the area's first linked member (else it would stretch the box over everything). Saved (dragged) positions still win. **Link pills** sit at the end of the wire, just before the device (left to right: 10 px left of it; top down: above its traffic badge), moved out of the border and title of an area the link enters (`inset`); a shared port's single pill sits where the wire leaves the port. Checked in a headless browser: no node, badge, pill or area title overlaps, both orientations |
| Sibling order | The layout keeps model order: nodes are given in a walk from the roots where each node's children are grouped by the port or Wi-Fi network they hang from ("IOT · 2.4 GHz" ones together), then by name (`modelOrder` in `lib/layout.ts`), so a link's pill sits over the devices that use it. Top-down leaf grids and area boxes take the place of their first node in that order (they used to go last, which scrambled "Reset layout"). Nothing may overlap: nodes, link pills and areas they do not belong to (checked in a headless browser on a real network's data) |
| Map orientation | Left-to-right by default, toggle to top-down in the map toolbar (per browser). Top-down packs the leaf children of a node (4 or more, nothing below them: clients, apps) into a compact grid under it, so wide networks do not become one very long row. Saved node positions are kept per orientation (`DOWN:` prefix in the layout table) |
| Device web UI | Side panel offers "Open web interface" (new tab) when one is detected: `internal/webui` checks common admin ports (443, 80, 8443, 8080, 8006, 5000/5001, 8123, 8096, 9443) over HTTP/HTTPS and reads the page `<title>`. Only IPs of nodes on the map; cached 10 min; no credentials sent |
| Sidebar | Compact (icons) or expanded (icons + labels), remembered per browser |
| Zero-config discovery | Core **network scan** integration (`internal/netscan`), created automatically on first start (`OMINI_AUTOSCAN`). Methods: UDP probe to fill the OS ARP cache + read `/proc/net/arp` (no raw sockets needed), unprivileged ICMP, TCP liveness for routed subnets, common-port scan, reverse DNS (system, then the gateway's DNS), NetBIOS NBSTAT, mDNS (legacy unicast + group listener on 5353) and SSDP/UPnP descriptions. Ports/names once per new host, then every 6h. Results are `Host` records (schema) under the gateway device. A routed network without a gateway is a "Network <cidr>" device holding its hosts; when none is left under it (all placed elsewhere, e.g. the modem under its WAN) it is not drawn **Routers' networks:** with "auto", the private IPv4 subnets other integrations report (non-WAN interface addresses, VLAN subnets; /22 to /30) are scanned too, up to 16, routed (ping + TCP, no MACs) — "Also scan the routers' networks", on by default; the collector hands them to every integration (`integration.KnownSubnets`) |
| MAC vendors | IEEE MA-L registry embedded gzipped (`internal/oui`, refresh with `make oui`) |
| Device identification | `internal/classify` (rules + evidence) → type, OS, brand, product. Icons: homelab software shows its logo alone; other devices show the type icon with an OS/brand badge; a device whose type is unknown shows its brand's (else OS's) logo alone instead of a question mark. Users can override type and icon |
| Solar inverters | Type `solar_inverter` (solar panel icon): Huawei SUN2000/SUN5000/SDongle, Fronius, SolarEdge, SMA, Growatt, GoodWe, Enphase, Sungrow, Deye/Solarman, Solis — by name (hostname, page title, model) or by a MAC vendor that only makes solar equipment |
| Icon/device lists | IEEE OUI and Simple Icons; **Dashboard Icons** (Apache-2.0, homelab apps, bundled for offline use); **model names** (`internal/models`, `make models`): Apple identifiers (community list + `extra.tsv` for Apple TV, HomePod, Macs) and Google Play's official list of Android devices — "iPhone14,2" → iPhone 13 Pro, "SM-S911B" → Samsung Galaxy S23, applied to models and to DHCP names that are model codes. Not used: Fingerbank (sends data to a third party), nmap databases (NPSL) |
| Proxmox guests | Any device with a Proxmox MAC (OUI) — VM or container, whatever it runs (Ubuntu, TrueNAS...) — is drawn under the Proxmox host **only when the network has exactly one Proxmox host**; with several hosts no inference is made. The **Proxmox plugin** places them for real: each running guest is a device with a neighbor of protocol `other` towards its host (`net0` → `vmbr0`); then no inference is made at all, and devices reported by any integration (a firewall running as a VM) are never moved. A link declared from one side (`other` neighbor: a VM, a mesh satellite) only places the side that reported it — the host is still placed on its switch port. A **cluster** works the same: every node is read through one address, each guest names its node (name, address and, once known, MAC) and hangs under it, each node on its own switch port (tested end to end in `TestProxmoxClusterGuestsUnderTheirNode`); clicking a node or a guest shows its CPU, memory, disks and uptime. A Proxmox integration that sees no guests (its token lacks VM.Audit: Proxmox returns empty lists) does not stop the inference; the plugin says so in its test. A device reported by an integration but placed by nothing now stays on the switch port its MAC was last learned on (the MAC table memory), else hangs from the router or firewall interface whose network has its address (`subnetRouter`), never floating apart. A device reported **without any MAC** (the Proxmox API exposes none) takes the MAC its address has in ARP tables, DHCP leases or scans (`adoptMACs`), so it is the machine already seen on a switch port, not a second node |
| Several apps on one IP | One node per app (e.g. Jellyfin + qBittorrent on a VM), attached to the device and collapsed above the threshold like clients |
| SNMP | Not a separate integration: an option (method) of the network scan, on by default. Every host found is probed with the configured read-only communities (default `public`, secret field, comma-separated) once per deep scan; hosts that answer are read in full (interfaces, traffic, LLDP, FDB, ARP) on every run. Older standalone SNMP integrations are converted on start (their community is added to the scan). **SNMP v3** (one user in the scan's "SNMP v3" settings: auth none/MD5/SHA-1/224/256/384/512, privacy none/DES/AES-128/192/256) is tried before the communities; the probe remembers which credential each host answered to. **SNMP profiles** (YAML, `internal/snmp/profiles/*.yaml` shipped, `<data>/profiles/*.yaml` added or replacing by `id`; format in `docs/snmp-profiles.md`): `match` by sysObjectID prefix, sysDescr regex or an OID that exists; set vendor, role, model, OS version, serial, CPU / memory / swap % (scalar or walked column with avg/max/min/sum, scale, invert, used/free/total), load average, temperatures and firmware (current, latest, update available with a value map); every matching profile applies, by ascending `priority`. Shipped: Net-SNMP (UCD-SNMP-MIB, lm-sensors), MikroTik, Cisco IOS, Juniper, FortiGate, Synology, QNAP, HPE Aruba |
| Device panel | CPU and memory as bars (green < 60%, yellow < 85%, red). **Front view of the ports** (2D, like a switch): one jack per physical port colored by link speed (10G purple, 5G blue, 2.5G teal, 1G green, ≤100M amber, down empty), all in one row in order (they shrink to fit), hover for details, **click opens the port's card** (details, name it, open the connected device). No port table |
| WAN nodes | Each internet uplink of a router/firewall (an interface with gateways, flagged `wan` by the integration) is a **WAN node, parent of the firewall** — several WANs, several parents. It shows the uplink's name, speed (of the physical port, following PPPoE → VLAN → port), gateway latency and a status dot (green / yellow if a gateway is degraded / red when down). Devices seen on the WAN port (the ISP modem) hang under it. Not stored in the inventory |
| Port connector | Integrations report `connector` (rj45 / sfp / qsfp). OPNsense: from the current media (`1000baseT` = RJ45; `SR/LR/SX/LX/CX/CR/Twinax` = SFP), or from the supported media when the port is down and they all agree; virtual NICs (virtio...) have none and are not drawn. The front view draws SFP cages and names the generation by speed (SFP, SFP+, SFP28, QSFP+) |
| System health | Vendor-neutral fields on `Device`, ready for insights: `cpu_count` (logical CPUs; a guest's vCPUs), `mem_total_bytes` / `mem_used_bytes` (shown next to the CPU and memory bars: "4 CPUs", "3.0 GiB of 8.0 GiB"; memory % worked out from them when only they are known), `swap_pct`, `load_avg` (1/5/15 min), `temperatures[]` (sensor, kind cpu/disk/board/other, °C), `storage[]` (mount, fs, total/used bytes) and `firmware` (current, latest, update_available, updates, needs_reboot, checked_at — what the device knows from **its own last check**: Omini never starts a check, read-only). Device panel: tiles for updates (yellow when pending, "Not checked" when the device never checked), hottest CPU temperature (yellow ≥ 70 °C, red ≥ 85 °C) and load; bars for swap and each disk (disks yellow ≥ 80 %, red ≥ 90 %). OPNsense reads them from the dashboard endpoints and `core/firmware/status` (privilege *System: Firmware*); plugins built for an older SDK still run without them |
| Port descriptions | The user can describe any port ("Uplink to the rack"): pencil next to the port in the device panel; stored in Omini (`port_labels`, never written to the device), shown instead of the device's description; empty restores it |
| Live traffic (early v0.2) | The collector turns interface byte counters into rates: difference between two collections ÷ time (average of the polling interval; counter resets skip a round; a failed collection clears them). Nodes carry `traffic` per interface. Map: links show only their **maximum speed**; **traffic is shown on the devices** as a ↓/↑ badge: routers/firewalls and WAN nodes show internet traffic, a device reached by a link of its own shows that link's traffic. A port shared by several devices (e.g. a LAN bridge with a switch behind it) gets one speed pill at the port (its traffic stays in the port view). A bridge runs at its fastest physical member's speed |
| Link speed on the map | Shown as a colored pill at the end of each link whose speed is known: LLDP/switch-port links, and a device port with a single link (e.g. WAN → modem). A port shared through ARP (switch behind it) has no per-device speed. The pill also names the port: the name the user gave it, else the interface's own name (`mlxen0`, `Port 1`) — never the device's description; a bridge shows the physical port behind it (its fastest member up, from `members`; without them, the only physical port up at the bridge's speed): "Porta LAN | 10G" |
| Hide / delete devices | The device panel has **Hide** (a per-device `hidden` flag in the inventory: the device and everything below it leave the map; "Show hidden (N)" in the map toolbar brings them back temporarily; Devices lists them with a badge and an unhide action) and **Delete** (removes it from the inventory after a second click; a device still on the network returns on the next scan). App nodes have neither (they are not in the inventory) |
| Adding integrations | "Add integration" opens the **store**: built-in integrations (Network scan, marked "Built in"), installed plugins and the catalog; picking one opens its form. Types can be **single** (`Info.Single`): the **network scan is single** — the API refuses a second one (409), the store shows "Already added", and duplicates left by older versions are removed on start (the oldest is kept) |
| Collection interval | Per integration ("Collect every" in its details: default = `OMINI_POLL_INTERVAL`, 15 s … 24 h; `interval_s` in the API, 0 = default). The collector checks every 10 s and collects what is due; "Run now" and saving settings collect everything |
| Integration names | Not editable: an integration is named after its type ("Network scan", "OPNsense") |
| Integration settings | Clicking an integration expands it inline with its settings and status. The network scan exposes each method (ARP, ping, ports, DNS, NetBIOS, mDNS, SSDP, web titles, SSH banners), ports and intervals |
| Demo network | Removed from the product; `internal/demo` is only a test fixture. Leftover demo integrations are deleted on start |
| nmap | Built-in, **single** integration (`internal/nmapscan`) that runs the nmap installed on the host (NPSL: Omini never ships nmap, to stay MIT). Runs **in the background** (Collect returns the last results at once; a /24 takes about a minute) every `every_hours` (default 24 h) or on "Run now": `-sV --version-light --top-ports 100`, plus `-O` when Omini runs as root (Docker). Hosts merge with the network scan's under the gateway (same MAC; without root nmap sees no MACs, so they come from the system ARP cache, and a device without MACs merges by IP); versions become banners and OS guesses (≥ 90 % accuracy) the OS. **Scan one device:** with nmap added and enabled, the device panel has "Scan (nmap)" (`POST /api/nodes/{id}/scan`, only nodes on the map): a scan of that IP set in the integration's "Scan one device" group — ports (`--top-ports`, default 100), service versions (off / quick = `--version-intensity 0` (default) / light / full), default scripts (`-sC`, off) and OS + route (`-O --traceroute`, on) with raw-socket permission (root, or `OMINI_NMAP_PRIVILEGED=true` with nmap's capabilities set: then `--privileged`); quick by default (~15 s on a firewall; full versions took ~100 s), up to `-A`-like depth — the result shown in the panel and kept until the next full scan, the map updated right away. Not installed → a message saying how to install it. The Docker image installs it on start when `OMINI_NMAP=install` |
| Traffic flow animation | **Removed** (maintainer's decision): links no longer animate; traffic stays on the devices (↓/↑ badge) and in the link's chart |
| Flows (who talks to whom) | Built-in **single** integration "Traffic flows" (`internal/flows`): while enabled, Omini listens on UDP 2055 (NetFlow v5/v9, IPFIX: templates per exporter and observation domain, sampling applied) and 6343 (sFlow v5: raw packet headers, Ethernet/802.1Q/IPv4/IPv6/TCP/UDP, scaled by the sampling rate); it reports no devices. Flows are summed per minute into **conversations** (the two IPs, protocol and service port = the lower port; bytes each way), the busiest 300 per minute kept 24 h (`flow_minutes`). Listeners close 10 min after the integration stops asking (disabled or deleted). `GET /api/flows?minutes&node|ip&limit` (addresses mapped to map nodes). UI: **Flows** screen (range 15 min–24 h, search, services named, exporters, "how to turn it on") and "Talks to" (top 5, last hour) in the device panel. Omini only listens: exporters are configured by the user |
| Notifications | Channels in Settings → Notifications (`notifiers` table, secrets sealed like integrations'): **webhook** (format json = Omini's events, slack, discord, ntfy; optional HMAC signature `X-Omini-Signature: sha256=…`), **Telegram** bot (token + chat id) and **e-mail** (SMTP: STARTTLS / TLS / none, optional login). Each has a minimum severity (default warning), "tell when resolved" and an on/off switch; "Send a test". The collector hands each round's opened/resolved alerts to `notify.Dispatcher` (in the background): one message per channel per round, most severe first, in the admin's language (texts in `internal/notify/messages.go`, kept in step with the UI). A flapping alert (opened again within 30 min of its last notice) is not sent again, nor its resolution. Delivery outcome kept per channel (last sent, last error) |
| VLAN view | Schema: `Interface.vlan` (a VLAN interface's id), `Interface.vlans` (switch port: untagged + tagged), `Device.vlans` (id, name, interface, subnet), FDB `vlan`. VLANs are shown as **automatic areas** (see that row; the toolbar VLAN picker that dimmed the rest was replaced by them). Port cards show the port's VLANs |
| Automatic areas | When the network has **more than one VLAN or subnet**, each gets an area (`lib/autoAreas.ts`), created by the UI as it appears (`areas.auto` = `vlan:<id>` or `subnet:<cidr>`, unique; colors from a palette): "VLAN 20 · IOT" (VLAN name, else its subnet's interface label) and "LAN · 192.168.1.0/24" (private IPv4 /16–/30 on non-WAN interfaces; a subnet on a listed VLAN is that VLAN's area). A node belongs to one only when it is in **exactly one** VLAN or subnet — by its own VLAN interfaces and port membership, by the upstream port carrying the VLAN (or the upstream switch's MAC table), and by its addresses; unnamed VLAN 1 has no area but still counts (a trunk with 1 + 20 is in two), and on a node with a subnet it is that subnet. So the firewall, trunk switches and multi-VLAN APs stay between areas; what hangs below a member comes along, and a node without addresses or VLANs (a scanned network, an unmanaged switch, a Wi-Fi network) joins the area all its children are in; nodes in an area the user drew stay there; a collapsed bubble joins when all its clients are in. Members are worked out on every render, never stored (no resize, drag in/out does not change them); the map is laid out again when they change. Rename, recolor and collapse like any area; deleting one only **dismisses** it (`dismissed`, never created again) |
| Device extras | Schema fields any integration can fill, shown in the device panel (`DeviceExtras`): `services` (stopped first, count of stopped), `vpn_peers` (WireGuard / OpenVPN / IPsec: connected, endpoint, last handshake, bytes), `dhcp_pools` and `firewall_states` (usage bars, yellow ≥ 75 %, red ≥ 90 %), `vlans`; `Interface.transceiver` (SFP vendor, part, type, temperature, voltage, bias, Tx/Rx power — Rx below the module's alarm threshold, or -20 dBm without one, in red) in the port card. OPNsense 0.3 reads them all |
| Map export | "Export" in the map toolbar opens a dialog: format, theme (dark / light) and orientation (left-to-right / top-down) — the image is laid out in the chosen orientation and theme whatever the screen shows, then the screen is put back as it was (pan and zoom included). PNG (2× pixel ratio) or SVG of the **whole map with every group and area expanded** (laid out expanded for the capture, then folded back; every node framed with a margin, measured as drawn, scaled down past 8192 px; the framing goes on Vue Flow's transformation pane, which holds the screen's pan and zoom; `html-to-image`, MIT) on the theme's background, a **draw.io** diagram (`lib/drawio.ts`: nodes where they are drawn — laid out expanded in the chosen orientation —, areas as rectangles behind them, links with "port | speed", editable in draw.io), or the data as JSON (topology, areas, layout) |
| Visual style | **Dark by default**, light theme available, follows the OS setting. Clean, minimal look; color reserved for status (green/yellow/red), traffic and brand icons |

## Open questions

- Discovery methods and required privileges (ICMP/ARP need raw sockets / host networking)

## Principles

1. **Read-only (MVP).** No integration may send commands that change device configuration.
2. **Lightweight.** One deployable unit, embedded database (SQLite), no external services (no Redis, Postgres, queues). Must run on a Raspberry Pi.
3. **Easy to contribute.** Most new device support should be a declarative profile or an external plugin, not core code.
4. **Vendor logic stays in integrations.** Nothing outside the integrations layer may know about specific vendors.
5. **One device failing never breaks the rest.** An integration error becomes an "offline" status + insight; other collections continue.
6. **MVP first.** When in doubt, leave it out and add it to the README roadmap.

## Common data model (draft)

Every integration returns a list of devices in this vendor-neutral shape:

- **Device**: `key` (stable id within the integration), `name`, `host`, `role` (`router|switch|ap|firewall|server|unknown`), `vendor`, `model`, `os_version`, `uptime`, `cpu_pct`, `mem_pct`, `macs[]`, `ips[]`, plus:
  - `interfaces[]` — name, description, MAC, up/down, speed, rx/tx bytes (counters), rx/tx errors
  - `neighbors[]` — local port, remote name/port/MAC/IP/platform, protocol (`lldp|cdp|mndp`)
  - `fdb[]` — MAC, port, VLAN
  - `arp[]` — IP, MAC, interface
  - `dhcp_leases[]` — IP, MAC, hostname
  - `wireless_clients[]` — MAC, interface, SSID, signal (dBm)
  - health: `swap_pct`, `load_avg[]`, `temperatures[]`, `storage[]`, `firmware` (pending updates)

Rules:
- **MACs are always normalized** to `aa:bb:cc:dd:ee:ff`.
- **Ports are always referenced by readable name** (`ifName` / interface name), never by numeric index. Index → name translation is the integration's job.
- Unknown fields are null — never invent values.
- Integrations may return **multiple devices** (e.g. a controller returns all the APs it manages).

## Integration contract (draft)

Each integration declares:
- `name`, `label`, and the **form fields** it needs (host, community, username, password, verify TLS...). The UI renders the "add integration" form from these fields, so a new integration needs no frontend changes.
- `collect(config) -> devices[]`
- `test(config) -> short success message | error`

Guidelines:
- Optional endpoints/tables (e.g. a Wi-Fi table missing on some firmware) are tried and skipped on error — never fail the whole collection because of them.
- Short timeouts (~5s per request); the collector applies a global per-device timeout.
- Credentials never appear in logs or API responses (password fields are masked).

### Python plugins

Each plugin is its own repository:

```
omini-plugin-opnsense/
├── plugin.yaml        # manifest
├── requirements.txt   # dependencies (installed into a per-plugin venv)
├── main.py            # entrypoint
└── README.md
```

```yaml
# plugin.yaml
id: opnsense
name: OPNsense
version: 0.1.0
protocol: 1            # plugin protocol version
entrypoint: main.py
fields:                # rendered as the "add integration" form
  - {key: url, type: url, required: true}
  - {key: api_key, type: string, required: true}
  - {key: api_secret, type: secret, required: true}
  - {key: verify_tls, type: bool, default: true}
```

```python
# main.py
from omini_sdk import plugin, Device

@plugin.collect
def collect(cfg) -> list[Device]: ...

@plugin.test
def test(cfg) -> str: ...

if __name__ == "__main__":
    plugin.run()
```

Protocol: the core executes the plugin once per collection (or per connection test), writes a request (`{protocol, action: "collect" | "test", config, state_dir}`) as JSON to stdin and reads the response (`{devices: [...]}`, `{message}` or `{error}`) as JSON from stdout. Logs go to stderr. Fields of type `secret` are decrypted only when passed to the plugin and never logged. The SDK hides all of this: a plugin raises `PluginError("message")` for errors the user should read (wrong key, host unreachable); any other exception becomes "unexpected error" with the traceback in the logs.

### Plugin runtime (core)

- **Install:** from a GitHub repository URL. The version is resolved to a commit through the GitHub API: the one asked for (tag, branch or commit), else the latest release, else the newest commit of the default branch (repositories without releases work, shown as `main@1a2b3c4`). The core downloads that commit's tarball (no `git` needed) into `<data>/plugins/<id>/src`, next to `install.json` (URL, version, commit, date). "Update" installs again from the same URL.
- **Index (store):** `internal/plugins/catalog.json` (id, name, description, URL, icon, publisher, trust, categories, reviewed version/date, known issues) is embedded in the binary **and** fetched from this repository's main branch once a day (`OMINI_PLUGIN_INDEX`: another URL, or `off`), cached in `<data>/plugins/index.json` for offline starts; entries of the shipped list missing from the remote one are kept. An invalid index (unknown trust, non-GitHub URL, duplicates) is ignored whole. The **plugin store** is a modal (Integrations → "Plugin store", Settings → Plugins → "Open the store", and the last card of "Add integration"): search bar, All / Installed / Available tabs, cards with logo, publisher and trust badges, repository and actions (install; installed: add integration, update, remove), and a "+" button to add any GitHub repository. Plugins installed from any other URL are `community` / `unverified`. The modal also has categories, a trust-level filter, the review of each plugin and where the index came from ("Update now"); there is no Store screen (`/store` opens the modal in Integrations).
- **Python environment:** created with **uv** (`<data>/plugins/<id>/.venv`), installing the plugin's `requirements.txt` plus **the SDK embedded in the Omini binary** — a plugin always gets the SDK of the core running it, so the protocol never drifts. uv must be on PATH (`OMINI_UV` to point to it); it downloads a Python when the system has none. The official image ships uv and Python.
- **Run:** `<venv>/bin/python <entrypoint>` with the request on stdin, a timeout (manifest `timeout_s`, default 60s, max 300s) and stdout limited to 32 MB; stderr goes to Omini's log (debug).
- **State:** `state_dir` is `<data>/plugins/<id>/state/<integration id>`, kept between runs (e.g. a session cookie).
- **Protocol version:** the manifest declares `protocol`; the core runs plugins whose protocol it supports (today: 1) and refuses others with a clear message.
- **Development:** `OMINI_PLUGIN_DIRS` (comma-separated folders) loads plugins in place, without installing them — edit the code and the next collection uses it.

## Repository layout (planned)

```
omini/
├── cmd/omini/         # Go entrypoint
├── internal/          # collector, snmp, discovery, topology, insights, store, api, plugins, auth
├── web/               # Vue 3 + Vue Flow (built assets embedded into the Go binary)
├── schema/            # JSON Schema — source of truth for the data contract
├── sdk/python/        # omini-sdk (published to PyPI)
├── profiles/          # YAML SNMP profiles (v0.3)
├── testdata/          # anonymized real device data + SNMP simulator records
├── docs/
├── Makefile
├── docker-compose.dev.yml
└── Dockerfile
```

## Commands

```bash
make run         # build the UI and run everything on :8080 (scans your network)
make icons       # refresh the app icon catalog and download the icon bundle
make dev         # backend on :8080 + Vite with hot reload on :5173
make generate    # regenerate Go types and Python models from schema/
make test        # Go + SDK + web tests
make ci          # everything CI runs, locally (run before pushing)
make lint        # golangci-lint + ruff + eslint/oxlint/prettier/vue-tsc
make fmt         # format Go, Python and web
make hooks       # install git hooks (lefthook)
make image       # build the Docker image locally (omini:dev)
make models      # refresh the device model names
```

## Topology engine

A pure function: `devices[] -> topology` (nodes, edges, clients). No I/O — testable with fixtures. Algorithm steps are described in the README ("How the topology is built").

Node types: `device`, `unmanaged`, `segment`, `client`. Edge types: `lldp`, `fdb`, `wifi`, `inferred`.

Traffic: rate = Δbytes / Δtime between consecutive polls, handling counter wraps (32/64-bit) and resets (uptime decreased). Utilization = rate / link speed.

## Insights

`internal/insights`: each rule is a pure function of the topology (plus integration statuses and new devices) returning insights with a stable `key` (rule + subject), `rule`, `severity` (`critical|warning|info`), optional `node_id` and `params` — **texts are translated in the UI** (`insights.rules.<rule>`), never built on the server. Rules: device offline (with its integration's error), integration failing with nothing on the map, WAN gateway down / degraded, duplicate IP (online nodes, different MACs), update pending, disk ≥ 80 % / ≥ 90 %, hottest CPU sensor ≥ 70 / ≥ 85 °C, CPU > 80 % / ≥ 95 %, memory ≥ 90 %, uplink < 1 Gbps between network devices, interface errors grown since the last poll (the traffic meter keeps the deltas), Wi-Fi signal < -75 dBm, physical port > 80 % used, likely unmanaged switch (info), unknown LLDP neighbor (info), new device (info).

The collector turns them into **alerts** (`alerts` table) on every rebuild: a new key opens one, a known key refreshes it, a missing key resolves it (kept 90 days). Users can **dismiss** an open alert (hidden until it resolves and comes back). `GET /api/alerts[?resolved_hours]`, `POST /api/alerts/{id}/dismiss`; open alerts also ride along `/api/topology`. UI: **Alerts** screen (`/alerts`; alerts: severity filter, dismissed, resolved in 7 days, "show on the map" = `/?node=<id>` opens and centers it; Timeline), a badge on the menu (critical + warning not dismissed), an "N alerts" chip on the map, a mark on the node (critical / warning) and the node's alerts in its panel.

## Conventions

- **Everything in English**: code, identifiers, comments, docs, commit messages, issues, PRs. User-facing UI strings go through i18n (`en`, `pt-BR`).
- **Conventional Commits** for every commit and PR title (`feat(snmp): ...`, `fix(topology): ...`); scopes and rules in [CONTRIBUTING.md](CONTRIBUTING.md). PRs are squash-merged; release-please builds the changelog and versions from them.
- Formatting/linting: Go with gofumpt + goimports + golangci-lint v2 (`.golangci.yml`); Python with ruff; web with Prettier + ESLint + oxlint + vue-tsc. Run `make fmt lint test` before committing; lefthook runs them as git hooks (`make hooks`).
- Never edit generated files (`internal/model/model_gen.go`, `sdk/python/src/omini_sdk/models.py`): change `schema/omini.schema.json` and run `make generate`.
- **Never push** to GitHub unless the maintainer asks; verify with `make ci` locally instead.
- All network I/O is async/concurrent.
- Structured logging, no ad-hoc prints.
- Topology and insights tests use JSON fixtures in `testdata/` (anonymized real device data); `internal/demo` (a fictional network, not available in the product) also serves as a fixture.

## Out of MVP scope

Write actions, multi-tenancy.
