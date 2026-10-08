# Devnet: Acme's emulated network

A mid-size company's network — HQ with an OPNsense HA pair, a Cisco core, Aruba/UniFi/Juniper/MikroTik/Horaco
switches, UniFi and Mercusys Wi-Fi, a 3-node Proxmox cluster with Ceph, NAS, cameras, phones and ~80 people; a
branch office with MikroTik + Omada behind WireGuard; a store with OpenWrt behind IPsec; a pfSense lab — whose
devices' **APIs are emulated**, so Omini's **real integrations** (the plugins from their own repositories, and the
built-in network scan with SNMP) run against it unmodified. [DESIGN.md](DESIGN.md) is the design (addressing,
VLANs, equipment, scenario) and the source of truth the emulator follows.

It only reads simulated data and never touches a real device.

```bash
make devnet   # builds the UI and the binary, runs Omini on http://localhost:8093 against the lab
```

## How it runs

`make devnet` (`run.sh` → `python -m emulator lab`) needs Linux with unprivileged user namespaces, `uv`, and the
plugin repositories next to this one (`../omini-plugin-*`, or `OMINI_DEVNET_PLUGINS=<folder>`). It:

1. builds each plugin's Python environment once (this fills uv's cache; the lab itself is offline);
2. starts a network namespace (`unshare --user --map-root-user --net`, no root needed) where Omini's server has
   Acme's address 10.10.10.250/24 in the MGMT VLAN and every emulated host is a /32 on `lo` while it is on the
   network (absent laptops disappear, like on a real network); the MGMT VLAN has ARP entries, other subnets are
   routed; everything outside Acme is unreachable;
3. serves every emulated API on the device's real address and port (OPNsense on https://10.10.10.2, Proxmox on
   https://10.10.10.11:8006, SNMP agents on udp/161, …), plus what the network scan probes (open ports, web page
   titles, SSH banners, NetBIOS, mDNS, SSDP, reverse DNS on 10.10.10.1);
4. starts Omini (data in `data-devnet/`, gitignored) and `bootstrap.py`, which creates the admin user and adds
   the integrations: the network scan with Acme's SNMP credentials and one integration per emulated API;
5. forwards http://127.0.0.1:8093 on this machine to Omini inside the namespace.

Credentials are generated test values in `data-devnet/devnet-admin.json` (Omini's admin) and
`data-devnet/devnet-secrets.json` (the emulated devices'), never printed. Delete `data-devnet/` to start over.

| Variable | Default | |
|---|---|---|
| `DEVNET_SPEED` | `60` | simulated seconds per real second (60: an hour a minute, a week in under 3 hours) |
| `DEVNET_START` | where it stopped | first start: Monday 07:00 (people arriving); e.g. `"tue 13:55"` to watch the fiber fail |
| `DEVNET_PORT`, `DEVNET_BIND` | `8093`, `127.0.0.1` | where the UI is served on this machine |
| `DEVNET_POLL` | `15s` | Omini's collection interval |
| `OMINI_DEVNET_PLUGINS` | `..` | folder holding the `omini-plugin-*` repositories |

Move the clock while it runs: `cd testdata/devnet && uv run python -m emulator clock --data ../../data-devnet --at
"thu 14:00"` (or `--speed 1`). `uv run python -m emulator dump --at "tue 10:30" [--node fw-hq-01]` prints what the
network looks like at an instant.

## Layout

- `emulator/model.py` — the data model (nodes, ports, links, VLANs, SSIDs).
- `emulator/acme.py` — Acme's network: every device, port, cable, VLAN and person (generated from a fixed seed),
  and `APIS`, which emulated API serves which device where.
- `emulator/world.py` — what changes with time, as a pure function of the simulated instant: presence, roaming,
  reboots, events, MAC/ARP tables, DHCP leases, LLDP, Wi-Fi associations, health, and traffic. Traffic is routed
  along the real paths (switch ports, LACP members, VLAN interfaces, PPPoE or 5G, tunnels) and every counter is the
  exact integral of the rates since the device's last boot, divided by the clock's speed-up, so the rates Omini
  computes are the designed ones. The week repeats, so tests ask for "Tuesday 14:10".
- `emulator/vendors/` — one emulator per API: OPNsense, Proxmox VE, UniFi Network, MikroTik RouterOS REST, Omada
  Open API, OpenWrt ubus, pfSense REST, the Horaco switch's web pages, the Mercusys Halo local API (with its
  RSA/AES protocol) and SNMP v1/v2c/v3 agents (Cisco, Aruba, Juniper, MikroTik, Synology, QNAP, APC, net-snmp).
- `emulator/scan.py` — what the network scan finds about hosts (ports, titles, banners, NetBIOS, mDNS, SSDP, DNS).
- `emulator/server.py` — the asyncio HTTP(S)/UDP server; `emulator/lab.py` — the namespace; `emulator/control.py`
  — the clock's control API; `emulator/testing.py` — test helpers (serve on random ports, run a plugin like Omini).

## Tests

- `tests/` (`cd testdata/devnet && uv run pytest`): the world (deterministic, consistent counters, the scenario),
  DESIGN.md against `acme.py`, and one file per vendor that serves its API on 127.0.0.1 and runs the **real
  plugin** against it (skipped when the plugin repositories are not found).
- `internal/collector/devnet_test.go`: starts the emulator, runs every integration through Omini's plugin manager
  and SNMP collector at given simulated instants, builds the map like the collector does and checks where every
  device hangs, VLAN/subnet data and the alerts of the scenario. Placements Omini still gets wrong are listed
  there as known issues (logged, not failed).

CI checks the plugin repositories out next to the code (`plugins/`). Neither the tests nor the lab need network
access, beyond installing Python packages once.
