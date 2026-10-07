# CLAUDE.md

Guide for agents (and humans) working on Omini. Read the [README](README.md) first for the product vision and MVP scope.

## The project in one sentence

A self-hosted tool that reads data from network devices and software of many vendors, normalizes it into a common model, and renders a live topology + traffic flow map, client inventory and health insights in a web UI.

## Status

**v0.2 released.** Network scan (with SNMP), identification (types, OS, brands, products, model names), app nodes, nmap, topology map (areas, WANs, port front view, live traffic), plugin runtime + SDK + store, OPNsense plugin, Docker image, auth, en + pt-BR. v0.2 so far: per-device nmap scan, panel redesign, port names on links, system health. Next: insights, 24h traffic history, presence timeline, animated traffic flow (full list: README → Roadmap).

Product and architecture decisions are made by consensus with the maintainer: raise questions and trade-offs instead of deciding unilaterally, then record agreed decisions here and in the README. Every change ships with tests that run in CI.

## Decisions log

| Topic | Decision |
|---|---|
| Positioning | Omini has its own identity: zero-config discovery and identification, easy integrations and a traffic flow map. Docs never compare Omini to other products or define it by them |
| MVP core | Automatic discovery + integrations screen + topology map with per-link traffic |
| "Flow" in the MVP | Per-link bandwidth utilization from interface counters. "Who talks to whom" (NetFlow) is post-MVP |
| Writes to devices | Read-only in the MVP; write actions later, behind explicit permissions |
| Devices without SNMP/API | Inferred from other devices' data (ARP/DHCP/FDB) **and** optional scraping plugins |
| License | MIT |
| Language | Everything in English (code, comments, docs, commits, issues). UI is translatable (i18n): `en` + `pt-BR` |
| Core | **Go**: discovery, generic SNMP + YAML profiles, LLDP, topology engine, traffic rates, insights, API, serving the UI |
| Integrations | **Go** for standard protocols (SNMP/LLDP/ARP/ICMP); **Python plugins** for anything vendor/software specific (OPNsense, Mercusys, UniFi, MikroTik...) |
| Plugin runtime | uv-managed venv per plugin, SDK embedded in the core and installed into every venv, install from GitHub release tarballs, `OMINI_PLUGIN_DIRS` for development — see "Plugin runtime (core)" |
| Docker image | `ghcr.io/riccardoalv/omini` (amd64 + arm64), built and pushed by the release workflow when release-please creates a release: Debian slim with Python 3 and uv (plugins), app icons bundled; data in `/data`; `network_mode: host` recommended; `OMINI_NMAP=install` installs nmap on start |
| Python runtime | Always bundled in the official image, so every plugin works out of the box |
| Plugin protocol | Exec per collection: core runs the plugin, sends config as JSON on stdin, reads devices as JSON on stdout |
| Frontend | **Vue 3** + TypeScript (Vite); map with **Vue Flow** + **ELK.js** auto-layout (left-to-right: firewall on the left, clients stacked on the right — top-down made wide networks unreadable); vue-i18n; built assets embedded in the Go binary. Tooling: Vitest, ESLint + oxlint, Prettier, vue-tsc. Node.js 24 |
| Authentication | Single admin user created on first run; device credentials encrypted at rest in SQLite |
| Traffic history | Short history (~24h) per link/port in SQLite, shown as a chart when a link is clicked |
| Plugin distribution | Each plugin is its own Git repository. Built-in **plugin store** with a default curated list (one-click install) + install from any GitHub URL. Reference model: Home Assistant's HACS. Installs pinned to a release |
| Plugin trust | Publisher badge (`official` / `community`) + trust level assigned by maintainers: `plug-and-play` (fully tested, works out of the box), `stable` (tested, known issues documented), `experimental` (partially tested), `unverified` (not reviewed; always the level for URL imports) |
| Firewall | **OPNsense** via its official REST API (key/secret, dedicated least-privilege user, HTTPS) as a Python plugin in its own repo (`omini-plugin-opnsense`). v0.1 reads: interfaces with status, IPs, **link speed/media** and traffic counters; ARP; DHCP leases (ISC, Kea or dnsmasq, whichever is in use); CPU, memory, uptime, version; **gateway status** (up/down, latency, loss). pfSense is post-MVP |
| Releases | Incremental: v0.1 core + SNMP + network scan + identification + OPNsense + topology map + login; v0.2 traffic flow + 24h traffic history + insights + presence timeline; v0.3 plugin store + trust levels + YAML profiles; v0.4 Mercusys + pt-BR + refined discovery (public launch) |
| Test network | OPNsense (router/firewall, DHCP), managed Horaco **HC-SWTGW218AS** switch, Mercusys routers in **AP mode** (clients visible in OPNsense ARP/DHCP) |
| Horaco HC-SWTGW218AS | No SNMP on stock firmware: Python plugin [`omini-plugin-horaco`](https://github.com/riccardoalv/omini-plugin-horaco) reads its web interface — `/info.cgi` (model, firmware V200.x, MAC, uptime, port status), `/port.cgi?page=stats` (64-bit packet and byte counters as `hi-lo`), `/panel.cgi` (RJ45/SFP from each port's `div` class) and `/mac.cgi?page=fwd_tbl` (paged with its form, `cmd=goto`). The session cookie is `md5(user+password)`: reused, login posted only when asked (the switch keeps one session). Only page navigation is ever posted |
| Mercusys Halo | Python plugin [`omini-plugin-mercusys`](https://github.com/riccardoalv/omini-plugin-mercusys): the units' local web interface (TP-Link Deco protocol: RSA password, AES requests signed with `h=md5(admin+password)&s=seq+len`, the login's signature also carrying `k`/`i`; the password at the top level of the login payload, unlike Deco). Each unit is an AP; clients read unit by unit (`access_host` is always "1"); no per-client signal or link rate in the local API (checked every model, controller and view of its web interface: only the Mercusys cloud app has them). Wi-Fi clients with band, network name (`/admin/wireless?form=wlan`: only the SSIDs are read — that answer also carries the Wi-Fi passwords, never kept) and **current traffic** (`rx_bps`/`tx_bps` — the units report no link rate), wired ones in the FDB (`LAN`), names as hosts; requests carry `Content-Type: application/json` like the web interface (else "no such callback"). Session kept in the state folder, 15 min pause after a refused login |
| Wi-Fi clients on the map | A client node carries its band, link rate (`tx/rx_rate_mbps`, when the AP reports it) and current traffic (`rx_bps`/`tx_bps` from the AP): the traffic is the ↓/↑ badge on the device (none while idle; on top of every node — compact, on its border, for clients), each Wi-Fi network of an AP is a **mini node** between it and its clients (AP → "IOT · 2.4 GHz" → clients; icon colored by band; no panel or menu, not in the inventory, but draggable like any node; collapsing the AP gathers its networks and their clients in one bubble, a network alone collapses its own clients — `withWifiNetworks` in `lib/graph.ts`), so Wi-Fi links have no pill; the link rate is in the client's panel. App links have no pill either (the app shows its port) |
| Plugin order | OPNsense (v0.1), then Horaco and Mercusys (v0.3) |
| Plugin store icons | Catalog `icon` is a logo slug (Simple Icons, or the bundled Dashboard Icons, e.g. `mercusys`) or a device type (`switch`) when the brand has no logo (Horaco) |
| Unmanaged switch beside a device | A client seen only on the switch port towards a device is placed behind that device — unless the device's integration lists its clients (Wi-Fi or wired) and this one is not among them: then an unmanaged switch is on that port, drawn as a segment with the device and those clients under it (e.g. switch Port 5 → unmanaged switch → {Halo AP, laptop by cable}). A MAC that only the switch's table vouches for there (no ARP, no scan) is not shown at all: a stale entry, or something even the AP does not see |
| MAC table memory | Switches age idle MACs out after ~5 min (a phone asleep), which made clients jump to wherever ARP saw them (the firewall's LAN). The collector remembers where each MAC was last learned (6 h, `collector.LastSeenTTL`) and the topology uses it (`topology.Options.LastSeen`) for clients present by other means (ARP, DHCP, Wi-Fi) — never to make one that left look present — and to keep a switch's uplink where the router was last learned (else the uplink came and went, and what is behind the router looked like a segment) |
| Detected devices | Kept **forever** (no automatic purge), with `first_seen` / `last_seen`; manual cleanup in the UI (bulk delete, filter by randomized MAC). Hidden from the map after **1h offline** (dimmed until then), always kept in the inventory |
| Presence timeline | Full join/leave timeline per device, plus a "new device" insight when a never-seen MAC appears. A device is only marked offline after missing several consecutive polls (debounce, avoids flapping events). Ships in v0.2 |
| UI screens | Map (home, full screen), Devices (inventory), Integrations, Settings in v0.1; Insights (alerts + timeline) in v0.2; Store in v0.3 |
| Map clients | All clients shown as nodes; a group (AP / switch port) with **more than 8** clients collapses into a "N clients" bubble that expands on click. Threshold configurable; expanded/collapsed state persisted. Pinned devices and servers always visible |
| Node details | Click opens a **slide-over side panel** (summary, ports with status/speed/traffic, connected clients, alerts) without leaving the map; "open full page" link |
| Repositories | Personal GitHub account for now (organization only at public launch, v0.4). Main monorepo `omini` + one repo per plugin from day one (`omini-plugin-opnsense`) |
| Data contract | **JSON Schema** in `schema/` is the single source of truth; Go types and Python (pydantic) models are generated from it; CI fails if generated code is stale |
| Plugin format | `plugin.yaml` manifest (id, name, version, protocol version, entrypoint, form fields) + `requirements.txt`; Python SDK `omini-sdk` handles stdin/stdout, validation and errors so authors only write `collect()` and `test()` |
| Dev environment | `make dev` runs the Go backend and the Vite dev server together; `make run` builds and runs the production-like binary. Optional Nix flake. (Docker Compose + SNMP simulator: later) |
| Map areas | Named rectangles drawn on the map ("New area" button, then drag). An area **remembers its member nodes** (those inside when it is drawn or resized, plus devices dropped into it; dragging a device out removes it) **and everything below them** (apps, clients, VMs) and is always drawn around them, so it follows them when the map is laid out again (reset, expand, new devices). The automatic layout treats each area as a box (ELK compound node), so its devices stay together and no other device lands inside it. Moving an area moves its members. Name, 6 preset colors, resize from the corner (re-picks the members), right-click menu on the title (rename, color, delete). Stored in the `areas` table; shown in both orientations (drawn around the same devices). "Reset layout" keeps areas. Collapsing an area into a bubble: later |
| UI language | Saved per user in the database (`users.locale`) and applied on login; the language picked on the setup screen becomes the user's. Before login, the browser language is used |
| Expand/collapse | The expanded/collapsed node stays still on screen; the map is laid out again around it (no overlaps). The viewport only refits on first load, orientation change, layout reset and sidebar toggle |
| Sibling order | The layout keeps model order: nodes are given in a walk from the roots where each node's children are grouped by the port or Wi-Fi network they hang from ("IOT · 2.4 GHz" ones together), then by name (`modelOrder` in `lib/layout.ts`), so a link's pill sits over the devices that use it |
| Map orientation | Left-to-right by default, toggle to top-down in the map toolbar (per browser). Top-down packs the leaf children of a node (4 or more, nothing below them: clients, apps) into a compact grid under it, so wide networks do not become one very long row. Saved node positions are kept per orientation (`DOWN:` prefix in the layout table) |
| Device web UI | Side panel offers "Open web interface" (new tab) when one is detected: `internal/webui` checks common admin ports (443, 80, 8443, 8080, 8006, 5000/5001, 8123, 8096, 9443) over HTTP/HTTPS and reads the page `<title>`. Only IPs of nodes on the map; cached 10 min; no credentials sent |
| Sidebar | Compact (icons) or expanded (icons + labels), remembered per browser |
| Zero-config discovery | Core **network scan** integration (`internal/netscan`), created automatically on first start (`OMINI_AUTOSCAN`). Methods: UDP probe to fill the OS ARP cache + read `/proc/net/arp` (no raw sockets needed), unprivileged ICMP, TCP liveness for routed subnets, common-port scan, reverse DNS (system, then the gateway's DNS), NetBIOS NBSTAT, mDNS (legacy unicast + group listener on 5353) and SSDP/UPnP descriptions. Ports/names once per new host, then every 6h. Results are `Host` records (schema) under the gateway device |
| MAC vendors | IEEE MA-L registry embedded gzipped (`internal/oui`, refresh with `make oui`) |
| Device identification | `internal/classify` (rules + evidence) → type, OS, brand, product. Icons: homelab software shows its logo alone; other devices show the type icon with an OS/brand badge. Users can override type and icon |
| Solar inverters | Type `solar_inverter` (solar panel icon): Huawei SUN2000/SUN5000/SDongle, Fronius, SolarEdge, SMA, Growatt, GoodWe, Enphase, Sungrow, Deye/Solarman, Solis — by name (hostname, page title, model) or by a MAC vendor that only makes solar equipment |
| Icon/device lists | IEEE OUI and Simple Icons; **Dashboard Icons** (Apache-2.0, homelab apps, bundled for offline use); **model names** (`internal/models`, `make models`): Apple identifiers (community list + `extra.tsv` for Apple TV, HomePod, Macs) and Google Play's official list of Android devices — "iPhone14,2" → iPhone 13 Pro, "SM-S911B" → Samsung Galaxy S23, applied to models and to DHCP names that are model codes. Not used: Fingerbank (sends data to a third party), nmap databases (NPSL) |
| Proxmox guests | Any device with a Proxmox MAC (OUI) — VM or container, whatever it runs (Ubuntu, TrueNAS...) — is drawn under the Proxmox host **only when the network has exactly one Proxmox host**; with several hosts no inference is made (a Proxmox integration can place them later) |
| Several apps on one IP | One node per app (e.g. Jellyfin + qBittorrent on a VM), attached to the device and collapsed above the threshold like clients |
| SNMP | Not a separate integration: an option (method) of the network scan, on by default. Every host found is probed with the configured read-only communities (default `public`, secret field, comma-separated) once per deep scan; hosts that answer are read in full (interfaces, traffic, LLDP, FDB, ARP) on every run. Older standalone SNMP integrations are converted on start (their community is added to the scan) |
| Device panel | CPU and memory as bars (green < 60%, yellow < 85%, red). **Front view of the ports** (2D, like a switch): one jack per physical port colored by link speed (10G purple, 5G blue, 2.5G teal, 1G green, ≤100M amber, down empty), all in one row in order (they shrink to fit), hover for details, **click opens the port's card** (details, name it, open the connected device). No port table |
| WAN nodes | Each internet uplink of a router/firewall (an interface with gateways, flagged `wan` by the integration) is a **WAN node, parent of the firewall** — several WANs, several parents. It shows the uplink's name, speed (of the physical port, following PPPoE → VLAN → port), gateway latency and a status dot (green / yellow if a gateway is degraded / red when down). Devices seen on the WAN port (the ISP modem) hang under it. Not stored in the inventory |
| Port connector | Integrations report `connector` (rj45 / sfp / qsfp). OPNsense: from the current media (`1000baseT` = RJ45; `SR/LR/SX/LX/CX/CR/Twinax` = SFP), or from the supported media when the port is down and they all agree; virtual NICs (virtio...) have none and are not drawn. The front view draws SFP cages and names the generation by speed (SFP, SFP+, SFP28, QSFP+) |
| System health | Vendor-neutral fields on `Device`, ready for insights: `swap_pct`, `load_avg` (1/5/15 min), `temperatures[]` (sensor, kind cpu/disk/board/other, °C), `storage[]` (mount, fs, total/used bytes) and `firmware` (current, latest, update_available, updates, needs_reboot, checked_at — what the device knows from **its own last check**: Omini never starts a check, read-only). Device panel: tiles for updates (yellow when pending, "Not checked" when the device never checked), hottest CPU temperature (yellow ≥ 70 °C, red ≥ 85 °C) and load; bars for swap and each disk (disks yellow ≥ 80 %, red ≥ 90 %). OPNsense reads them from the dashboard endpoints and `core/firmware/status` (privilege *System: Firmware*); plugins built for an older SDK still run without them |
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
| Visual style | **Dark by default**, light theme available, follows the OS setting. Clean, minimal look; color reserved for status (green/yellow/red), traffic and brand icons |

## Open questions

- SNMP profile format (YAML schema)
- Plugin store: where the curated index lives, review process for promoting a plugin between trust levels
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
- **Catalog (store, minimal):** a curated list embedded in the binary (`internal/plugins/catalog.json`: id, name, description, URL, publisher, trust). The **plugin store** is a modal (Integrations → "Plugin store", Settings → Plugins → "Open the store", and the last card of "Add integration"): search bar, All / Installed / Available tabs, cards with logo, publisher and trust badges, repository and actions (install; installed: add integration, update, remove), and a "+" button to add any GitHub repository. Plugins installed from any other URL are `community` / `unverified`. A remote index and the full store screen come in v0.3.
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
├── web/               # Vue 3 + Vue Flow + ELK.js (built assets embedded into the Go binary)
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

Each rule is a function `(devices, topology) -> insights[]` with `severity` (`critical|warning|info`), `title`, `detail`, optional `node_id`. MVP rules: offline, duplicate IP, update pending, disk almost full, hot CPU, uplink < 1 Gbps, interface errors, Wi-Fi signal < -75 dBm, CPU > 80%, unmanaged segment, unknown LLDP neighbor, saturated link (> 80% utilization).

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

Flow analysis (NetFlow/sFlow), write actions, SNMP v3, historical metrics, notifications, multi-tenancy.
