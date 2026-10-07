# Omini

> **Multi-vendor**, **self-hosted**, **open source** network topology and traffic map for homelabs and small networks.

Omini connects to your routers, switches, access points and network software — from different vendors — reads what each one knows about the network, and automatically builds a **live topology map** showing **where traffic is flowing**, **which devices are connected** and **how healthy the network is**. All in a clean web UI running on your own hardware.

> ⚠️ **Status:** v0.1 in progress — the core (SNMP, topology, collector, API) and the web UI work; the OPNsense plugin and the Docker image are next. Not ready for production use yet.

---

## Why

A homelab grows one device at a time: a firewall here, a cheap 2.5G switch there, a couple of Wi-Fi access points, a hypervisor full of VMs, smart plugs, TVs and phones. Each piece speaks a different language, and nothing shows the whole picture.

Omini is that picture. Start it, and in a few seconds it finds every device on your network, recognizes what each one is — the firewall, the NAS, the Proxmox host, the Ubuntu VM and the apps running on it, the Android phone, the air conditioner — and draws it all on one map. No agents, no setup on your devices, no vendor lock-in.

What makes Omini, Omini:

1. **Zero configuration** — it discovers the network by itself and identifies devices, operating systems, brands and self-hosted apps, showing each with its own icon.
2. **Integrations that are easy** — when you want more detail, connecting a device or a piece of software (SNMP switches, firewalls, vendor APIs, community plugins) takes a few clicks, including devices that have no SNMP or API at all.
3. **Traffic flow map** — every link shows live bandwidth usage (animated, with thickness/color by load), so you see at a glance where traffic flows and where the bottlenecks are.

## MVP scope

- **Works with zero configuration** — on first start Omini scans the networks it is connected to and finds every device, combining ARP, ping, open ports, reverse DNS (including the router's DNS), NetBIOS, mDNS/Bonjour and SSDP/UPnP. Vendors come from an embedded MAC (OUI) database. Ports and names are checked once per new device and then every few hours, to keep the network quiet.
- **Device identification** — type (firewall, switch, access point, NAS, hypervisor, VM, phone, TV, air conditioner...), operating system, brand and software, with the evidence behind each conclusion. Self-hosted apps (Jellyfin, qBittorrent, Home Assistant and 2,600+ others) appear as their own nodes under the machine that runs them. You can correct anything by hand.
- **Icons** — product logos for homelab software and appliances; device-type icons with an OS or brand badge for everything else.
- **Integrations screen** — pick an integration, enter host and credentials, test the connection, save. Forms are generated from each integration's definition.
- **Automatic topology** — cross-reference LLDP/CDP neighbors, MAC tables (FDB), ARP, DHCP leases and Wi-Fi client tables to work out *what is plugged into what, and on which port*.
- **Traffic flow map** — per-link utilization computed from interface counters, refreshed periodically and rendered on the map.
- **Connected clients** — every device on the network with IP, MAC, hostname, where it is attached (switch/port or AP/SSID) and Wi-Fi signal.
- **Devices without API** — devices that cannot be integrated (e.g. consumer routers in AP mode, unmanaged switches) are **inferred** from what other devices see, and appear as "unmanaged" nodes you can name and position.
- **Insights** — simple, useful alerts: device offline, duplicate IP, uplink negotiated below 1 Gbps, interface errors, weak Wi-Fi signal, high CPU, likely unmanaged switch, unknown LLDP neighbor, saturated link.
- **Interactive map** — drag nodes (positions are saved), click a node to see its ports, traffic and clients.
- **Plugin store** — install integrations with one click from a curated list, or from any GitHub repository URL.
- **Short traffic history** — click a link to see its traffic over the last ~24h.
- **Device inventory and timeline** — every device ever seen is kept with first/last seen and a full join/leave timeline; devices offline for more than 1h are hidden from the map but stay in the inventory.
- **Read-only** — the MVP **never changes device configuration**. Write actions (e.g. disable a port, change a VLAN) may come later behind explicit permissions.
- **Translatable UI** — English and Brazilian Portuguese from day one.

## Integrations

Integrations are layered so that most contributions require little or no core code:

| Layer | What it is | Who writes it |
|---|---|---|
| **SNMP profiles** | Declarative files (OIDs and mappings) describing a vendor/model | Anyone — no programming |
| **Python plugins** | A Python script that receives its config as JSON and prints devices as JSON | Anyone who knows some Python |
| **Core (Go)** | Standard protocols: generic SNMP, LLDP, ARP, ICMP discovery | Core contributors |

### First targets (based on hardware available for real testing)

| Integration | Type | Data |
|---|---|---|
| Network scan | Core (Go), on by default | Every device: IP, MAC, vendor, hostnames, open ports, mDNS/UPnP services and models |
| Generic SNMP v2c | Core (Go) | `SNMPv2-MIB`, `IF-MIB`, `LLDP-MIB`, `BRIDGE-MIB`/`Q-BRIDGE-MIB`, ARP |
| OPNsense | Python plugin (official REST API, key/secret) | Interfaces, traffic, ARP, DHCP leases, CPU/memory |
| Horaco HC-SWTGW218AS | Python plugin (web UI scraping, later — no SNMP on stock firmware) | Ports, traffic, MAC table |
| Mercusys | Python plugin (web UI scraping, optional) | Wi-Fi clients |

Later: MikroTik (REST API), UniFi controller, TP-Link Omada, OpenWrt, Proxmox, pfSense.

> Web scraping integrations are inherently fragile (they break with firmware updates), so they ship as optional plugins, never as part of the core.

### Plugin store and trust levels

Every plugin lives in its own Git repository. The built-in store lists a curated set of plugins (one-click install), and any GitHub repository can be imported by URL. Installs are always pinned to a release.

Each plugin shows two things:

**Publisher badge** — who maintains it:
- **Official** — maintained by the Omini project
- **Community** — maintained by a third party

**Trust level** — how well it is known to work:

| Level | Meaning |
|---|---|
| 🟢 **Plug & Play** | Fully tested on real hardware; works out of the box |
| 🔵 **Stable** | Tested; known issues or instabilities are documented |
| 🟠 **Experimental** | Partially tested; expect rough edges |
| ⚪ **Unverified** | Not reviewed by the Omini project — install at your own risk |

Trust levels are assigned by the Omini maintainers when a plugin is reviewed for the curated list — never self-declared by the plugin author. Plugins imported by URL are always **Unverified**.

## User interface

Omini opens straight into a **full-screen map** with a summary bar on top (devices, clients, alerts).

```
┌─────────────────────────────────────────────────────────────┐
│ ◉ Omini     12 devices · 47 clients · ⚠ 2                   │
├────┬───────────────────────────────────┬────────────────────┤
│ 🗺 │                                   │ Horaco SW1      ✕  │
│ 💻 │          [OPNsense]               │ 192.168.1.2 · ●    │
│ 🔌 │              │                    │ ────────────────── │
│ ⚠  │        ▶[Horaco SW1]◀             │ Ports              │
│ ⚙  │        /     |      \             │ ● 1 2.5G  OPNsense │
│    │    [AP1]   [AP2]   [NAS]          │ ● 2 1G    AP1      │
│    │      │     ┌─┼─┬─┐               │ ○ 3 —              │
│    │   ( 20 )   📱 💻 📺               │ Clients (23)    ›  │
└────┴───────────────────────────────────┴────────────────────┘
```

- **Screens:** Map, Devices (inventory), Integrations, Settings — plus Insights (v0.2) and Store (v0.3).
- **Clients on the map:** every client is shown; when an AP or switch port has more than 8 clients, they collapse into a "N clients" bubble that expands on click (threshold configurable).
- **Details:** clicking a node opens a side panel with its ports, traffic, clients and alerts — without leaving the map.
- **Open the device's web interface:** when a device has an admin page (router, NAS, Proxmox, Home Assistant...), the side panel offers to open it in a new tab. Omini detects it by checking common web ports on that device — only devices on the map, and no credentials are ever sent.
- **Orientation:** the map is laid out left to right by default; one click switches to top down (dragged positions are kept separately for each orientation).
- **Areas:** draw named, colored rectangles on the map ("Rack", "Living room") to group devices. An area remembers its devices and follows them when the map is reorganized; moving it moves them. Drag a device in or out to change the group; resize from the corner; rename, recolor or delete with a right click on the title. Areas are kept per orientation.
- **Expand and collapse:** the node you expand or collapse stays where it is on screen; the rest of the map makes room around it.
- **Sidebar:** compact (icons) or expanded (icons and names).
- **Theme:** dark by default, light available, follows your OS setting.

## How it works

```
 ┌──────────────┐   ┌──────────────┐   ┌──────────────┐
 │ Switch (SNMP)│   │   OPNsense   │   │ Plugin script│   ← integrations
 └──────┬───────┘   └──────┬───────┘   └──────┬───────┘
        │   translate to the common data model│
        ▼                  ▼                  ▼
 ┌─────────────────────────────────────────────────────┐
 │  Collector (periodic, concurrent polling)           │
 │  interfaces + counters, neighbors, FDB, ARP, DHCP,  │
 │  Wi-Fi clients, CPU/memory                          │
 └──────────────────────────┬──────────────────────────┘
                            ▼
 ┌─────────────────────────────────────────────────────┐
 │  Topology engine + traffic rates + insights         │
 └──────────────────────────┬──────────────────────────┘
                            ▼
                  API  →  Web UI (map)
```

### How the topology is built

1. **Identity** — every managed device is indexed by all its MACs, IPs and name.
2. **Device-to-device links** — LLDP/CDP/MNDP neighbors are matched to known devices (chassis MAC, management IP or name). The same link seen from both ends becomes a single edge with both ports. Unknown neighbors become "unmanaged" nodes.
3. **Uplink ports** — ports with an infrastructure neighbor are marked as uplinks and ignored when locating clients.
4. **Wi-Fi clients** — AP registration tables take priority: the client is attached to the AP/SSID.
5. **Wired clients** — for each MAC in the FDB tables, pick the *edge-most* port (non-uplink, fewest learned MACs).
6. **Unmanaged segments** — a non-uplink port with several MACs becomes a virtual "unmanaged switch/host" node (a dumb switch, a consumer AP, or a hypervisor with VMs).
7. **Devices without LLDP** — managed devices with no neighbors are located through the FDB like clients (edge marked as *inferred*).
8. **Enrichment** — IP from ARP, hostname from DHCP; randomized MACs (locally administered bit) are flagged.

### How traffic flow is computed

Each poll reads interface byte counters; the rate is `Δbytes / Δtime` between two polls (handling counter wraps and resets). The rate of a link is taken from whichever end has an integration; utilization is the rate divided by the negotiated link speed.

## Stack

| Part | Technology |
|---|---|
| Core (discovery, SNMP, topology, traffic, API) | **Go** |
| Vendor/software integrations | **Python** plugins |
| Web UI | **Vue 3** + **Vue Flow** (map) + **ELK.js** (auto-layout) |
| Database | SQLite (embedded) |
| Deployment | One Docker image (Go binary + bundled Python runtime) |

Why: Go keeps the always-on core fast and light (like Caddy, Traefik, AdGuard Home, Beszel); Python makes writing integrations easy for the community (like Home Assistant). No external services — runs on a Raspberry Pi, a small VM or a NAS.

## Installation (planned)

```yaml
services:
  omini:
    image: ghcr.io/<org>/omini:latest
    container_name: omini
    restart: unless-stopped
    network_mode: host   # recommended for discovery (ARP/ICMP)
    volumes:
      - ./data:/data
```

```bash
docker compose up -d
# open http://<your-server>:8080 — the first visit asks you to create the admin user
```

### Configuration

| Variable | Default | Description |
|---|---|---|
| `OMINI_ADDR` | `:8080` | Address the web server listens on |
| `OMINI_DATA_DIR` | `./data` | Where the SQLite database and the secret key live |
| `OMINI_POLL_INTERVAL` | `60` | Collection interval, in seconds or as a duration (`1m30s`); minimum 10s |
| `OMINI_SECRET_KEY` | — | Base64 32-byte key to encrypt device credentials. If unset, one is generated in `<data dir>/secret.key` — back it up together with the database |
| `OMINI_LOG_LEVEL` | `info` | `debug`, `info`, `warn` or `error` |
| `OMINI_AUTOSCAN` | `true` | Create the network scan integration on first start |

## Development

Requirements: Go (see `go.mod`), Node.js 24, [uv](https://docs.astral.sh/uv/) and `make` — or just `nix develop`.

```bash
make run    # build the UI and run everything on http://localhost:8080 (scans your network)
make dev    # backend + UI with hot reload on http://localhost:5173 (API on :8080)
make test   # all tests (Go, Python SDK, web)
make ci     # everything CI runs, locally
```

On first access, Omini asks you to create the admin user. Data (database and secret key) goes to `./data`.

The data contract between the Go core and plugins is a JSON Schema in `schema/`; Go types and the Python SDK models are generated from it (`make generate`).

### Writing a plugin

A plugin is a Git repository with a `plugin.yaml` manifest, a `requirements.txt` and a Python entrypoint. With the `omini-sdk` package you only write two functions:

```python
from omini_sdk import plugin, Device

@plugin.collect
def collect(cfg) -> list[Device]:
    ...  # talk to the device/API, return devices in Omini's model

@plugin.test
def test(cfg) -> str:
    ...  # check connectivity/credentials, return a short message
```

See [`CLAUDE.md`](CLAUDE.md) for the manifest format and protocol.

## Preparing your devices

- **Network scan:** run Omini with **host networking** (`network_mode: host` in Docker) so it sees your LAN directly. If the host has a firewall, allow inbound **UDP 5353** (mDNS) and **UDP 1900** (SSDP) — otherwise names and models announced by devices are not received. To get hostnames from your router, enable registering DHCP leases in its DNS (OPNsense: *Services → Unbound DNS → General → Register DHCP leases*).
- **SNMP devices:** enable SNMP v2c (read-only community) and, if available, **LLDP**. Without LLDP Omini still works, but links between switches become *inferred*.
- **OPNsense:** create a dedicated user with only the privileges Omini needs (diagnostics, DHCP leases), generate an API key/secret for it, and keep the API on HTTPS. One key per application, as recommended by the [OPNsense docs](https://docs.opnsense.org/development/how-tos/api.html).

## Security

- The MVP is **read-only**: no integration sends write commands.
- The UI requires login (an admin user is created on first run).
- Device credentials are encrypted at rest.
- Detected devices are kept until you delete them (no automatic purge). Phones with randomized MACs can create duplicate entries; the UI offers filters and bulk cleanup.
- Plugins run code on your server and receive the credentials you configure for them: only install plugins you trust.

## Roadmap

The MVP scope above ships in incremental releases, each one usable on its own:

| Release | Delivers | You can... |
|---|---|---|
| **v0.1** | Go core, generic SNMP, OPNsense plugin (manual install), network scan, device identification, topology engine, map (Vue Flow), login | See a map of your real network and its clients |
| **v0.2** | Per-link traffic (animated flow), 24h traffic history, insights, device presence timeline | See where traffic flows, what is wrong, and who joined or left |
| **v0.3** | Plugin store, trust levels, install from URL, YAML SNMP profiles | Install integrations with one click |
| **v0.4** | Mercusys plugin (scraping), `pt-BR` UI, refined subnet discovery | Cover a full mixed homelab — public launch |

**Later:**
- Long-term traffic history
- "Who talks to whom" flow analysis (NetFlow/sFlow/IPFIX, e.g. from OPNsense NetFlow)
- Write actions behind explicit permissions
- SNMP v3
- OUI vendor database (identify "Apple", "Raspberry Pi"...)
- More integrations: MikroTik, UniFi, Omada, OpenWrt, Proxmox, pfSense
- VLAN view
- Notifications (Telegram, e-mail, webhook)
- Topology export (PNG, SVG, draw.io, JSON)

## Contributing

Contributions are very welcome — especially **SNMP profiles**, **plugins** and **real device data** (anonymized SNMP walks help a lot with testing).
See [`CLAUDE.md`](CLAUDE.md) for project conventions.

## Credits

- Device and software logos: [Simple Icons](https://simpleicons.org) (CC0-1.0).
- App icons and catalog: [Dashboard Icons](https://github.com/homarr-labs/dashboard-icons) by homarr-labs (Apache-2.0).
- MAC vendors: [IEEE registration authority](https://standards-oui.ieee.org/) (MA-L registry).
- Logos and trademarks belong to their respective owners; Omini is not affiliated with them.

## License

[MIT](LICENSE)
