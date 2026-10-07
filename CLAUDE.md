# CLAUDE.md

Guide for agents (and humans) working on Omini. Read the [README](README.md) first for the product vision and MVP scope.

## The project in one sentence

A self-hosted tool that reads data from network devices and software of many vendors, normalizes it into a common model, and renders a live topology + traffic flow map, client inventory and health insights in a web UI.

## Status

**v0.1 in progress.** Done: data contract (JSON Schema + codegen), generic SNMP integration, SNMP discovery, demo network, SQLite store, secret encryption, topology engine, collector, auth and HTTP API. Next: Vue UI, Python plugin runtime + SDK, OPNsense plugin, Docker image.

Product and architecture decisions are made by consensus with the maintainer: raise questions and trade-offs instead of deciding unilaterally, then record agreed decisions here and in the README. Every change ships with tests that run in CI.

## Decisions log

| Topic | Decision |
|---|---|
| Positioning | Own project (not a Scanopy fork). Differentiators: easy integrations + traffic flow map |
| MVP core | Automatic discovery + integrations screen + topology map with per-link traffic |
| "Flow" in the MVP | Per-link bandwidth utilization from interface counters. "Who talks to whom" (NetFlow) is post-MVP |
| Writes to devices | Read-only in the MVP; write actions later, behind explicit permissions |
| Devices without SNMP/API | Inferred from other devices' data (ARP/DHCP/FDB) **and** optional scraping plugins |
| License | MIT |
| Language | Everything in English (code, comments, docs, commits, issues). UI is translatable (i18n): `en` + `pt-BR` |
| Core | **Go**: discovery, generic SNMP + YAML profiles, LLDP, topology engine, traffic rates, insights, API, serving the UI |
| Integrations | **Go** for standard protocols (SNMP/LLDP/ARP/ICMP); **Python plugins** for anything vendor/software specific (OPNsense, Mercusys, UniFi, MikroTik...) |
| Python runtime | Always bundled in the official image, so every plugin works out of the box |
| Plugin protocol | Exec per collection: core runs the plugin, sends config as JSON on stdin, reads devices as JSON on stdout |
| Frontend | **Vue 3**; map with **Vue Flow** + **ELK.js** auto-layout; built assets embedded in the Go binary |
| Authentication | Single admin user created on first run; device credentials encrypted at rest in SQLite |
| Traffic history | Short history (~24h) per link/port in SQLite, shown as a chart when a link is clicked |
| Plugin distribution | Each plugin is its own Git repository. Built-in **plugin store** with a default curated list (one-click install) + install from any GitHub URL. Reference model: Home Assistant's HACS. Installs pinned to a release |
| Plugin trust | Publisher badge (`official` / `community`) + trust level assigned by maintainers: `plug-and-play` (fully tested, works out of the box), `stable` (tested, known issues documented), `experimental` (partially tested), `unverified` (not reviewed; always the level for URL imports) |
| Firewall | **OPNsense** via its official REST API (key/secret, dedicated least-privilege user, HTTPS) as a Python plugin. pfSense is post-MVP |
| Releases | Incremental: v0.1 core + SNMP + OPNsense + topology map + login + demo; v0.2 traffic flow + 24h traffic history + insights + presence timeline; v0.3 plugin store + trust levels + YAML profiles; v0.4 Mercusys + pt-BR + refined discovery (public launch) |
| Test network | OPNsense (router/firewall, DHCP), managed Horaco switches (SNMP), Mercusys routers in **AP mode** (clients visible in OPNsense ARP/DHCP; AP attachment inferred from switch FDB until the Mercusys plugin exists) |
| Detected devices | Kept **forever** (no automatic purge), with `first_seen` / `last_seen`; manual cleanup in the UI (bulk delete, filter by randomized MAC). Hidden from the map after **1h offline** (dimmed until then), always kept in the inventory |
| Presence timeline | Full join/leave timeline per device, plus a "new device" insight when a never-seen MAC appears. A device is only marked offline after missing several consecutive polls (debounce, avoids flapping events). Ships in v0.2 |
| UI screens | Map (home, full screen), Devices (inventory), Integrations, Settings in v0.1; Insights (alerts + timeline) in v0.2; Store in v0.3 |
| Map clients | All clients shown as nodes; a group (AP / switch port) with **more than 8** clients collapses into a "N clients" bubble that expands on click. Threshold configurable; expanded/collapsed state persisted. Pinned devices and servers always visible |
| Node details | Click opens a **slide-over side panel** (summary, ports with status/speed/traffic, connected clients, alerts) without leaving the map; "open full page" link |
| Repositories | Personal GitHub account for now (organization only at public launch, v0.4). Main monorepo `omini` + one repo per plugin from day one (`omini-plugin-opnsense`) |
| Data contract | **JSON Schema** in `schema/` is the single source of truth; Go types and Python (pydantic) models are generated from it; CI fails if generated code is stale |
| Plugin format | `plugin.yaml` manifest (id, name, version, protocol version, entrypoint, form fields) + `requirements.txt`; Python SDK `omini-sdk` handles stdin/stdout, validation and errors so authors only write `collect()` and `test()` |
| Dev environment | `make dev` via Docker Compose (Go with live reload, Vite dev server, SNMP simulator with sample data). Optional Nix flake |
| Visual style | **Dark by default**, light theme available, follows the OS setting. Clean UniFi/Linear-like look; color reserved for status (green/yellow/red) and traffic |

## Open questions

- SNMP profile format (YAML schema)
- Plugin runtime details: per-plugin venv location, state dir (to cache sessions between runs), timeouts, how protocol versions are negotiated
- Plugin store: where the curated index lives, review process for promoting a plugin between trust levels
- Horaco: exact model, whether SNMP and LLDP are available and enabled
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
```

Protocol: the core executes the plugin once per collection (or per connection test), writes a request (`{action: "collect" | "test", config}`) as JSON to stdin and reads the response (`{devices: [...]}`, `{message}` or `{error}`) as JSON from stdout. Logs go to stderr. Fields of type `secret` are decrypted only when passed to the plugin and never logged. The SDK hides all of this.

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
make dev         # (planned) start the full dev stack (Go live reload, Vite, SNMP simulator)
make generate    # regenerate Go types and Python models from schema/
make test        # Go + SDK tests
make lint        # golangci-lint + ruff
make fmt         # format Go and Python
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
- Formatting/linting: Go with gofumpt + goimports + golangci-lint v2 (`.golangci.yml`); Python with ruff. Run `make fmt lint test` before committing; lefthook runs them as git hooks (`make hooks`).
- Never edit generated files (`internal/model/model_gen.go`, `sdk/python/src/omini_sdk/models.py`): change `schema/omini.schema.json` and run `make generate`.
- All network I/O is async/concurrent.
- Structured logging, no ad-hoc prints.
- Topology and insights tests use JSON fixtures in `testdata/` (anonymized real device data); the demo integration also serves as a fixture.

## Out of MVP scope

Flow analysis (NetFlow/sFlow), write actions, SNMP v3, historical metrics, notifications, multi-tenancy.
