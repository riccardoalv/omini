# CLAUDE.md

Guide for agents (and humans) working on Omini. Read the [README](README.md) first for the product vision and MVP scope.

## The project in one sentence

A self-hosted tool that reads data from network devices and software of many vendors, normalizes it into a common model, and renders a live topology + traffic flow map, client inventory and health insights in a web UI.

## Status

**v0.1 in progress.** Done: data contract (JSON Schema + codegen), zero-config network scan (with SNMP), device identification (types, OS, brands, products) and app nodes with icons, map areas, SQLite store, secret encryption, topology engine, collector, auth, HTTP API, the web UI (map, devices, integrations, settings; en + pt-BR), the Python plugin runtime + SDK and the OPNsense plugin (`omini-plugin-opnsense`, to be checked against a real firewall). Next: nmap integration, model names, port panel, Docker image.

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
| Horaco HC-SWTGW218AS | Stock firmware very likely has **no SNMP** (community projects read it through its web CGI: `/login.cgi`, `/info.cgi`, `/port.cgi?page=stats`, `/mac.cgi?page=fwd_tbl`). A web-scraping Python plugin is **post-v0.1**; until then it shows as an unmanaged segment |
| Plugin order | OPNsense plugin first (v0.1); Horaco web plugin later |
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
| Map orientation | Left-to-right by default, toggle to top-down in the map toolbar (per browser). Top-down packs the leaf children of a node (4 or more, nothing below them: clients, apps) into a compact grid under it, so wide networks do not become one very long row. Saved node positions are kept per orientation (`DOWN:` prefix in the layout table) |
| Device web UI | Side panel offers "Open web interface" (new tab) when one is detected: `internal/webui` checks common admin ports (443, 80, 8443, 8080, 8006, 5000/5001, 8123, 8096, 9443) over HTTP/HTTPS and reads the page `<title>`. Only IPs of nodes on the map; cached 10 min; no credentials sent |
| Sidebar | Compact (icons) or expanded (icons + labels), remembered per browser |
| Zero-config discovery | Core **network scan** integration (`internal/netscan`), created automatically on first start (`OMINI_AUTOSCAN`). Methods: UDP probe to fill the OS ARP cache + read `/proc/net/arp` (no raw sockets needed), unprivileged ICMP, TCP liveness for routed subnets, common-port scan, reverse DNS (system, then the gateway's DNS), NetBIOS NBSTAT, mDNS (legacy unicast + group listener on 5353) and SSDP/UPnP descriptions. Ports/names once per new host, then every 6h. Results are `Host` records (schema) under the gateway device |
| MAC vendors | IEEE MA-L registry embedded gzipped (`internal/oui`, refresh with `make oui`) |
| Device identification | `internal/classify` (rules + evidence) → type, OS, brand, product. Icons: homelab software shows its logo alone; other devices show the type icon with an OS/brand badge. Users can override type and icon |
| Icon/device lists | IEEE OUI and Simple Icons (in use); **Dashboard Icons** (Apache-2.0, homelab apps, bundled for offline use); Apple/Google model-name lists; **nmap optional** (OS detection when the binary is installed). Not used: Fingerbank (sends data to a third party), nmap databases (NPSL, incompatible with MIT) |
| Proxmox guests | Any device with a Proxmox MAC (OUI) — VM or container, whatever it runs (Ubuntu, TrueNAS...) — is drawn under the Proxmox host **only when the network has exactly one Proxmox host**; with several hosts no inference is made (a Proxmox integration can place them later) |
| Several apps on one IP | One node per app (e.g. Jellyfin + qBittorrent on a VM), attached to the device and collapsed above the threshold like clients |
| SNMP | Not a separate integration: an option (method) of the network scan, on by default. Every host found is probed with the configured read-only communities (default `public`, secret field, comma-separated) once per deep scan; hosts that answer are read in full (interfaces, traffic, LLDP, FDB, ARP) on every run. Older standalone SNMP integrations are converted on start (their community is added to the scan) |
| Device panel | CPU and memory as bars (green < 60%, yellow < 85%, red). **Front view of the ports** (2D, like a switch): one jack per physical port colored by link speed (10G purple, 5G blue, 2.5G teal, 1G green, ≤100M amber, down empty), two rows above 8 ports (odd on top), hover/focus for details (speed, duplex, media, IPs, MAC, traffic, errors, connected device), click opens the connected device. A table lists every port, VLANs included |
| WAN nodes | Each internet uplink of a router/firewall (an interface with gateways, flagged `wan` by the integration) is a **WAN node, parent of the firewall** — several WANs, several parents. It shows the uplink's name, speed (of the physical port, following PPPoE → VLAN → port), gateway latency and a status dot (green / yellow if a gateway is degraded / red when down). Devices seen on the WAN port (the ISP modem) hang under it. Not stored in the inventory |
| Port connector | Integrations report `connector` (rj45 / sfp / qsfp). OPNsense: from the current media (`1000baseT` = RJ45; `SR/LR/SX/LX/CX/CR/Twinax` = SFP), or from the supported media when the port is down and they all agree; virtual NICs (virtio...) have none and are not drawn. The front view draws SFP cages and names the generation by speed (SFP, SFP+, SFP28, QSFP+) |
| Port descriptions | The user can describe any port ("Uplink to the rack"): pencil next to the port in the device panel; stored in Omini (`port_labels`, never written to the device), shown instead of the device's description; empty restores it |
| Link speed on the map | Shown as a colored pill at the end of each link whose speed is known: LLDP/switch-port links, and a device port with a single link (e.g. WAN → modem). A port shared through ARP (switch behind it) has no per-device speed |
| Hide / delete devices | The device panel has **Hide** (a per-device `hidden` flag in the inventory: the device and everything below it leave the map; "Show hidden (N)" in the map toolbar brings them back temporarily; Devices lists them with a badge and an unhide action) and **Delete** (removes it from the inventory after a second click; a device still on the network returns on the next scan). App nodes have neither (they are not in the inventory) |
| Integration names | Not editable: an integration is named after its type ("Network scan", "OPNsense") |
| Integration settings | Clicking an integration expands it inline with its settings and status. The network scan exposes each method (ARP, ping, ports, DNS, NetBIOS, mDNS, SSDP, web titles, SSH banners), ports and intervals |
| Demo network | Removed from the product; `internal/demo` is only a test fixture. Leftover demo integrations are deleted on start |
| nmap | Built-in integration that runs the nmap installed on the host (NPSL: Omini must not ship nmap itself to stay MIT). Docker image installs it on first start only when asked (`OMINI_NMAP=install`) |
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
- **Catalog (store, minimal):** a curated list embedded in the binary (`internal/plugins/catalog.json`: id, name, description, URL, publisher, trust). Settings → Plugins lists it under "Available" (one-click install), and "Add integration" offers its plugins next to the installed types (install, then the form opens). Plugins installed from any other URL are `community` / `unverified`. A remote index and the full store screen come in v0.3.
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
make build       # (planned) production Docker image
```

## Topology engine

A pure function: `devices[] -> topology` (nodes, edges, clients). No I/O — testable with fixtures. Algorithm steps are described in the README ("How the topology is built").

Node types: `device`, `unmanaged`, `segment`, `client`. Edge types: `lldp`, `fdb`, `wifi`, `inferred`.

Traffic: rate = Δbytes / Δtime between consecutive polls, handling counter wraps (32/64-bit) and resets (uptime decreased). Utilization = rate / link speed.

## Insights

Each rule is a function `(devices, topology) -> insights[]` with `severity` (`critical|warning|info`), `title`, `detail`, optional `node_id`. MVP rules: offline, duplicate IP, uplink < 1 Gbps, interface errors, Wi-Fi signal < -75 dBm, CPU > 80%, unmanaged segment, unknown LLDP neighbor, saturated link (> 80% utilization).

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
