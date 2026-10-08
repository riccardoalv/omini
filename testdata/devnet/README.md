# Devnet: a simulated network for development

A large, deliberately messy network that Omini collects through ten fake
plugins, so the map, areas, VLAN and subnet areas, alerts, layout and
collection rounds can be tried on something much bigger than a home network.
It is a test fixture and only reads simulated data. It never touches a real device.

```bash
make devnet   # builds the UI and the binary, runs Omini on http://localhost:8093
```

`make devnet` starts Omini with its own data folder (`data-devnet/`, which is
gitignored), no network scan, no remote plugin index, a 30 s round and every
`devnet-*` folder loaded in place (`OMINI_PLUGIN_DIRS`). On the first run
`bootstrap.py` creates the admin user with generated test credentials and adds
one integration per plugin through the API. The credentials are in
`data-devnet/devnet-admin.json`. Delete `data-devnet/` to start over.

## Layout

- `_shared/devnet.py` defines the whole network in one place, as a pure
  function of the time. The same instant always gives the same answer.
  `devices(plugin_id, now)` returns what one plugin reports. To print every
  plugin's output, run
  `uv run --project sdk/python python testdata/devnet/_shared/devnet.py dump [--at EPOCH]`.
- `devnet-*/` holds one plugin per folder: `plugin.yaml`, a `main.py` that
  imports the shared module and an empty `requirements.txt`.
- Counters grow with time. Each one is the integral of an average rate with two
  slow sines on top, so live traffic, history and the traffic badges move.
  Devices reboot on a schedule, so their counters reset.

## What is simulated

| Plugin | Devices | Exercises |
|---|---|---|
| `devnet-fw` | `fw-edge` (OPNsense-like) | 2 WANs (fiber PPPoE over `igc0` with the ONT on the WAN port, LTE backup with a Huawei modem), LAG to the cores, 8 VLAN interfaces (IPv4 + some IPv6), ARP and DHCP for the whole HQ, gateways, WireGuard / IPsec / OpenVPN peers, services (one enabled but stopped), DHCP pools (GUEST almost full), firewall states, a pending update that needs a reboot, CPU temperatures crossing 70 °C |
| `devnet-core` | `core-sw1`, `core-sw2` (Cisco, MLAG pair) | SFP+ ports with transceivers (one dirty fiber with Rx below its alarm), port-channels, trunks carrying every VLAN, LLDP to everything managed, MAC tables of ~250 entries learned on port-channels, a loop (fw ↔ both cores ↔ each other ↔ both floor switches) |
| `devnet-access` | `acc-floor1` (Aruba), `acc-floor2` (UniFi), `acc-lab` (TP-Link) | PoE desks, phones with a PC behind them, a desk switch with 5 devices (port 12), an access point sharing a desk switch with 2 wired devices (floor 2 Port 2), cameras on VLAN 50, an IP phone and a Netgear switch nobody manages (LLDP), a port whose errors grow (floor 1 port 7), a 100 Mbps uplink (lab switch), a stale MAC on an AP port, a blocked spanning-tree uplink, a lab switch that reboots every 6 h |
| `devnet-wifi` | 5 UniFi APs (one mesh satellite) | Acme-Corp (5/6 GHz), Acme-IoT (2.4 GHz) and Acme-Guest; ~57 clients with signal, rates and live traffic; weak clients in the warehouse; randomized MACs; a phone that roams between ap-office1 and ap-office2 every 4 min; a laptop that leaves 15 min every hour |
| `devnet-pve` | 3 Proxmox hosts, 17 guests | VMs and containers hanging from their host (`other` neighbors), a VM with the apps of a Docker host (6 web apps), a VM in the users' VLAN that stops 10 min every 2 h, a host at 96 % CPU 10 min every hour, a VM at 94 % memory, a router VM |
| `devnet-lab` | `lab-router` (OPNsense VM on pve3) | double NAT: its WAN in VLAN 99, its own DHCP for VLAN 199 on the lab switch; the same device as the pve guest (merged by MAC) |
| `devnet-dnat` | `mkt-router` (TP-Link) | a consumer router on an office port (double NAT) with its own Wi-Fi clients; reboots every 4 h |
| `devnet-branch` | `br-gw`, `br-sw`, `br-cap` (MikroTik) | a branch office behind an IPsec tunnel (MNDP, its own ISP WAN); the plugin fails 3 min every 30 min while the tunnel is down |
| `devnet-storage` | `nas-01` (Synology), `bkp-01` (Dell), `ups-01` (APC) | a volume that fills from 86 % to 97 % every 6 h; a backup that saturates its 1 Gbps link for the first quarter of every hour; disk temperatures; a UPS with outdated firmware |
| `devnet-scan` | `mon-01` (Raspberry Pi) | ~130 scanned hosts: open ports, web titles, banners, mDNS; MACs only in its own VLAN (MGMT), like a routed scan; a host only the scan sees |

There are also a solar inverter (Huawei SUN2000), printers (one with a static
address that a laptop also got, which raises the duplicate IP alert), cameras,
smart TVs, Tuya and Espressif IoT devices, Raspberry Pis, Apple, Samsung and
Google devices. They use real vendor prefixes (OUIs), so identification and
icons work. The VLANs are 10 MGMT, 20 USERS (/23), 30 VOICE, 40 IOT,
50 CAMERAS (/26), 66 GUEST, 99 LAB, 100 SERVERS, plus 199 LAB-INNER behind
the lab router.

### Schedule (seconds into each period, UTC)

| Event | When |
|---|---|
| Phone roams | every 240 s |
| `LT-SALES-03` away | 600–1500 of every hour |
| LTE gateway degraded | 1200–1800 of every hour |
| Fiber gateway degraded | 2400–2700 of every hour |
| Fiber down (LTE carries the traffic) | 6000–6180 of every 4 h |
| Branch tunnel down (plugin fails) | 1500–1680 of every 30 min |
| pve2 CPU at 96 % | 1800–2400 of every hour |
| `win11-vdi` stopped | 2700–3300 of every 2 h |
| Backup saturates `bkp-01`'s link | 0–900 of every hour |

## Tests

- `sdk/python/tests/test_devnet.py` runs every plugin the way Omini does,
  validates the output against the SDK models and checks that the network is
  deterministic, that counters grow (and reset on a reboot) and that the
  schedule above happens.
- `internal/collector/devnet_test.go` builds the map from the devnet and checks
  where things hang and which alerts fire. It needs `uv`, and it is skipped
  without it or with `-short`.
