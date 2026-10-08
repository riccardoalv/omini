# Architecture

Omini is one process: a Go server with an embedded SQLite database and the
web UI built in. Plugins run as short-lived Python processes. There are no
external services (no message queue, no separate database), so it runs on a
Raspberry Pi.

```
 ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
 │ Network scan │  │ nmap / flows │  │ Plugin (Py)  │  │ Plugin (Py)  │  integrations
 └──────┬───────┘  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘
        └─────────────────┴──── devices in the common model ──┘
                                   ▼
 ┌─────────────────────────────────────────────────────────────────────┐
 │ Collector: rounds, edge to center; traffic rates; MAC table memory  │
 └──────────────────────────────────┬──────────────────────────────────┘
                                    ▼
 ┌─────────────────────────────────────────────────────────────────────┐
 │ Topology engine → identification → inventory → insights → alerts   │
 └──────────────────────────────────┬──────────────────────────────────┘
                                    ▼
          SQLite (store)  ←→  HTTP API  →  Web UI (Vue)  ·  Notifications
```

- [Components](#components)
- [Data model](#data-model)
- [How the topology is built](#how-the-topology-is-built)
- [Traffic](#traffic)
- [What is stored](#what-is-stored)
- [Repository layout](#repository-layout)

## Components

| Component | Package | Role |
|---|---|---|
| Entry point | `cmd/omini` | Reads the environment, opens the store, loads the secret key, SNMP profiles and plugins, runs the collector and the HTTP server |
| Integrations | `internal/integration` | The contract every source implements: `Info()` (type, name, form fields), `Collect()` and `Test()`, plus a registry |
| Network scan | `internal/netscan` | ARP, ping, TCP, ports, DNS, NetBIOS, mDNS, SSDP, web titles, SSH banners, SNMP; checks what the system allows (`DetectCapabilities`) |
| SNMP | `internal/snmp` | v2c and v3 client, standard MIBs, YAML vendor profiles |
| nmap | `internal/nmapscan` | Runs the host's nmap in the background, and on one device on demand |
| Flows | `internal/flows` | NetFlow v5/v9, IPFIX and sFlow v5 listeners; conversations per minute |
| Plugins | `internal/plugins` | Install from GitHub, uv environments, run with the JSON protocol, store index |
| Collector | `internal/collector` | Collection rounds, snapshots, traffic rates, MAC table memory, presence, traffic history, alerts |
| Topology | `internal/topology` | A pure function from devices to a map |
| Identification | `internal/classify`, `internal/oui`, `internal/models`, `internal/appicons` | Type, OS, brand, product; MAC vendors; model names; app icons |
| Insights | `internal/insights` | Alert rules: pure functions of the map |
| Notifications | `internal/notify` | Webhook, Telegram and e-mail senders; grouping and cooldown |
| Store | `internal/store` | SQLite (pure Go driver, WAL), migrations |
| Secrets | `internal/secret` | Encrypts credentials at rest with the secret key |
| Auth | `internal/auth` | The single admin, bcrypt passwords, session tokens (stored hashed) |
| Web probe | `internal/webui` | Finds a device's admin page for "Open web interface" |
| API | `internal/api` | HTTP routes, CSRF check, serves the embedded UI |
| UI | `web/` | Vue 3 + TypeScript + Vue Flow, its own tree layout, vue-i18n (English, Brazilian Portuguese) |
| Data contract | `schema/omini.schema.json` | JSON Schema; Go types (`internal/model`) and the Python SDK models are generated from it |

### A collection round

1. The collector picks the enabled integrations and orders them from the edge
   of the network to its center (by their devices' depth on the last map,
   else by role; built-in discovery last).
2. It runs them one at a time. Each gets its config with secrets decrypted, a
   time limit (45 s, or the plugin's own), the subnets other integrations
   reported, and its instance id (for plugin state). A panicking or failing
   integration keeps its last snapshot and is marked offline.
3. Each result (a list of devices) is saved as the integration's **snapshot**
   and fed to the traffic meter.
4. The map is **rebuilt once**, from every snapshot:
   topology → MAC table memory → inventory (first/last seen) →
   identification → apps and VMs → user overrides and port names → traffic →
   presence events and traffic history → insights → alerts.
5. Alerts opened or resolved in the round are handed to the notification
   dispatcher, which delivers them in the background.

Snapshots are stored, so after a restart the last map is available at once.

## Data model

Every integration, built-in or plugin, returns devices in one vendor-neutral
shape, defined in `schema/omini.schema.json`:

- **Device**: `key`, `name`, `host`, `role`, `vendor`, `model`, `serial`,
  `os_version`, `uptime_s`, `macs`, `ips`, health (`cpu_pct`, `mem_pct`,
  `swap_pct`, `load_avg`, `temperatures`, `storage`, `firmware`), and:
  - `interfaces`: name, description, MAC, up, speed, duplex, media,
    connector, counters, errors, IPs, WAN flag, members, VLANs, transceiver,
  - `neighbors`: LLDP / CDP / MNDP, or `other` for a declared link,
  - `fdb`: MAC table entries (MAC, port, VLAN),
  - `arp` and `dhcp_leases`,
  - `wireless_clients`: MAC, SSID, band, signal, link rates, traffic,
  - `gateways`: WAN gateways with status, latency and loss,
  - extras: `vlans`, `services`, `vpn_peers`, `dhcp_pools`, `firewall_states`,
  - `hosts`: end devices seen by a scanner or controller.

Rules: MACs are lowercase `aa:bb:cc:dd:ee:ff`; ports are referenced by name,
never by index; unknown values are left out, never invented. See
[What to return](plugins.md#what-to-return).

The map is made of **nodes** (`device`, `unmanaged`, `segment`, `client`,
`app`, `wan`) and **edges** (`lldp`, `fdb`, `wifi`, `inferred`, `vpn`), each
edge with its two ports and speed.

## How the topology is built

`topology.BuildWith` is a pure function: the same devices always give the
same map, with no I/O, so it is tested with JSON fixtures of real networks.

1. **Identity.** Every device is indexed by all its MACs, IPs and name. The
   same machine reported by two integrations (SNMP and a plugin, the scan and
   a controller) is merged into one node by any shared MAC, keeping the richer
   data. A device without MACs (the Proxmox API exposes none) takes the MAC its
   address has in ARP tables, leases or scans; a device without MACs at an
   address already on the map is that device.
2. **Center.** Routers and firewalls with a WAN are the center. A router whose
   WAN sits in another managed device's LAN (double NAT) is not.
3. **Device links.** LLDP/CDP/MNDP neighbors are matched to known devices
   (chassis MAC, management IP or name). A link seen from both ends becomes
   one edge with both ports. Unknown neighbors become **unmanaged** nodes.
   Links declared with protocol `other` place only the side that reported
   them. A firewall running as a VM is never linked to its host (its panel
   says **Runs on**).
4. **Uplinks.** Ports with an infrastructure neighbor, and LAGs, are uplinks
   and ignored when locating clients.
5. **MAC tables.** Where each MAC is learned is indexed. Switches forget idle
   MACs after about 5 minutes, so the collector remembers where each MAC was
   last learned for 6 hours. That memory is used for devices present by other
   means (ARP, DHCP, Wi-Fi), never to make a device that left look present,
   and to keep a switch's uplink where the router was last learned.
6. **Devices without neighbors** are placed through the MAC tables, like
   clients (edge `inferred`).
7. **WANs.** Each interface flagged WAN becomes a WAN node, parent of its
   router; devices seen on the WAN port (the modem) hang under it.
8. **VPN links** between two routers of the same network (site-to-site) are
   drawn as `vpn` edges.
9. **Clients.**
   - Access points' client lists come first: the client hangs under the AP.
   - Wired clients go to the **edge-most** port that learned them: not an
     uplink, the fewest MACs.
   - A client seen only on the switch port towards a device whose integration
     lists its clients, and not among them, means an unmanaged switch on that
     port: a segment is drawn with the device and those clients under it.
   - A MAC only a switch table vouches for there (no ARP, no scan) is not
     shown: a stale entry.
   - Hosts found only by the scan hang under the gateway of their network.
10. **Segments.** A non-uplink port with three or more client MACs becomes an
    **unmanaged segment** (a small switch, a consumer AP, a hypervisor).
11. **Shared ports and speeds.** A port shared by several devices gets one
    speed at the port; a device port with a single link gives that link its
    speed. A bridge runs at its fastest physical member's speed.
12. **Cleanup.** A placeholder for a scanned network whose hosts were all
    placed elsewhere is dropped.

After the build, the collector adds what is not topology:

- **Identification** (`internal/classify`): type, OS, brand and product from
  the evidence (titles, ports, mDNS, UPnP, vendor, names, banners, TTL,
  model, integration).
- **Apps**: one node per self-hosted app found on a device.
- **VMs**: with exactly one Proxmox host on the network and no Proxmox
  integration, devices with a Proxmox MAC hang under it.
- **User data**: names, pinned, hidden, type and icon overrides, port names.
- Devices offline for more than an hour leave the map (they stay in the
  inventory).

## Traffic

Integrations report byte counters. The collector turns them into rates: the
difference between two collections divided by the time between them. A
counter that went back (a reboot, a reset) skips a round; a failed collection
clears the rates. Rates per interface ride on the nodes (`traffic`), errors
grown since the last collection feed the `interface_errors` alert, and
up/down changes feed `link_flapping`.

Every rebuild records each interface's rate (and a Wi-Fi client's own): one
point per minute kept 24 hours, and an hourly average and peak kept a year,
computed from the minutes.

## What is stored

SQLite at `<data dir>/omini.db`, in WAL mode, one writer. Migrations run on
start (`PRAGMA user_version`).

| Table | Contents | Kept |
|---|---|---|
| `integrations` | Type, config (secrets encrypted), enabled | until deleted |
| `snapshots` | The last result of each integration | replaced every collection |
| `inventory` | Every device ever seen, first/last seen, user fields | until deleted |
| `layout`, `areas`, `port_labels` | Positions, areas, port names | until changed |
| `mac_ports` | Where each MAC was last learned | 6 hours |
| `traffic_minutes`, `traffic_hours` | Traffic history | 24 hours, 1 year |
| `presence_events` | Joined / left | 1 year |
| `alerts` | Open and resolved alerts | 90 days after resolution |
| `flow_minutes` | Top conversations per minute | 24 hours |
| `notifiers` | Notification channels (secrets encrypted) | until deleted |
| `users`, `sessions` | The admin and its sessions | sessions 30 days |
| `settings` | Round interval, when presence tracking started | |

Secrets are sealed with the 32-byte key in `secret.key` (or
`OMINI_SECRET_KEY`); passwords of the admin are bcrypt hashes; session tokens
are stored as hashes.

## Repository layout

```
omini/
├── cmd/omini/        # entry point
├── internal/         # collector, topology, insights, store, api, plugins, ...
├── web/              # Vue 3 UI, embedded into the binary
├── schema/           # JSON Schema: the data contract
├── sdk/python/       # omini-sdk, installed into every plugin environment
├── testdata/         # anonymized real data, and the simulated network (devnet)
├── docs/             # this documentation
├── docker/           # container entrypoint
├── Dockerfile
├── docker-compose.yml
└── Makefile
```
