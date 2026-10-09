# Discovery

Omini finds devices in three ways, from no setup at all to full detail:

1. The built-in **network scan** finds every device on your networks and
   identifies it, with nothing installed on the devices. It also reads SNMP
   from devices that have it enabled.
2. The optional built-in **nmap** and **Traffic flows** integrations add
   service versions, operating systems and who talks to whom.
3. **Plugins** read a firewall, switch, access point or hypervisor through its
   own API: ports, link speeds, clients, health. See [Integrations](integrations.md).

Everything here is read-only: Omini never changes a device's configuration.

- [The network scan](#the-network-scan)
- [What each method needs](#what-each-method-needs)
- [Subnets and the routers' networks](#subnets-and-the-routers-networks)
- [SNMP](#snmp)
- [nmap](#nmap)
- [Device identification](#device-identification)
- [Traffic flows](#traffic-flows)

## The network scan

The **Network scan** integration is created on first start and runs in every
[collection round](configuration.md#collection-rounds). There can be only one;
its settings are in **Integrations → Network scan**.

| Method | Setting | What it finds |
|---|---|---|
| ARP | ARP | Every device on the local network, even ones that answer nothing else: a UDP probe fills the system's ARP cache, which Omini then reads. MAC addresses give the vendor (IEEE registry, embedded) |
| Ping | Ping (ICMP) | Devices that answer ping; the reply's TTL hints the operating system |
| TCP liveness | (always) | Devices on routed subnets, where there are no MAC addresses: a connection to a few common ports, accepted or refused, proves the host is up |
| Open ports | Open ports | Services on about 100 common homelab ports (web, SSH, SMB, printers, cameras, media servers, *arr apps, Home Assistant, Proxmox...). Set your own list in **Advanced → Ports to check** (up to 1024) |
| Reverse DNS | Reverse DNS | Names from this server's DNS, then from the gateway's DNS |
| NetBIOS | NetBIOS | Names of Windows and Samba machines |
| mDNS / Bonjour | mDNS / Bonjour | What devices announce: names, models, services (AirPlay, Chromecast, printers, HomeKit...). Omini both asks and listens on UDP 5353 |
| SSDP / UPnP | SSDP / UPnP | Manufacturer and model of TVs, routers and media players, from their UPnP descriptions |
| Web page titles | Web page titles | The title of each web interface, to recognize apps (Proxmox, TrueNAS, Home Assistant...) |
| SSH banners | SSH banners | The SSH version line, which often names the operating system |
| DHCP fingerprints | DHCP fingerprints | What each device asks for when it requests an address (see [DHCP fingerprints](#dhcp-fingerprints)) |
| SNMP | SNMP | Ports, traffic, neighbors, MAC tables and ARP from managed devices. See [SNMP](#snmp) |

To keep the network quiet, ports, names, banners and web titles are checked
once for each new device and again every 6 hours (**Advanced → Re-check ports
and names every**). ARP, ping and SNMP reads run every round.

Hosts found by the scan hang under the gateway on the map until a better
source places them: a switch's MAC table, an access point's client list, LLDP.

The details of the integration show **Found by**: which methods found how many
devices. That helps you see whether, for example, mDNS works at all.

## What each method needs

Each method needs something different from the system. The official Docker
image (root, `network_mode: host`) has all of it. Omini checks what works where
it runs, when it starts, and shows it in **Integrations → Network scan → On
this server**, with the fix for anything missing. While something is missing,
the **Network discovery is limited** alert stays open.

| Method | Needs | Without it |
|---|---|---|
| ARP (MACs, vendors) | Being on the LAN: `network_mode: host` in Docker | In a Docker bridge network Omini only sees Docker's own network |
| Ping (ICMP) | Unprivileged ping sockets (`sysctl net.ipv4.ping_group_range="0 2147483647"`) or root | Falls back to TCP: devices with no open port are missed |
| mDNS, SSDP (names, models) | Multicast: `network_mode: host`, and UDP 5353 and 1900 allowed in by the host's firewall | Names and models announced by devices are missed |
| NetBIOS, reverse DNS, ports, web titles, SSH banners | Nothing special | — |
| nmap OS detection and traceroute | Root, or `CAP_NET_RAW` on the nmap binary | nmap still finds ports and versions |
| DHCP fingerprints | Raw sockets: root (the Docker image) or `CAP_NET_RAW`; Linux; on the same network as the devices (`network_mode: host`) | Operating systems are guessed from other clues only |
| Traffic flows (NetFlow, IPFIX, sFlow) | UDP 2055 and 6343 reachable from the exporter | The router cannot send them |

"On this server" checks four things: **Host network**, **Multicast (mDNS,
SSDP)**, **Ping (ICMP)** and **Raw sockets**.

To make ping work for a non-root user permanently:

```bash
echo 'net.ipv4.ping_group_range = 0 2147483647' | sudo tee /etc/sysctl.d/90-ping.conf
sudo sysctl --system
```

## Subnets and the routers' networks

**Subnets** is `auto` by default: Omini scans the IPv4 networks this server is
connected to. You can list networks instead, separated by commas:
`192.168.1.0/24, 10.0.20.0/24`. Networks larger than a /22 (1024 addresses)
are reduced to the /22 around the server's own address.

**Also scan the routers' networks** (on by default, only with `auto`): other
integrations tell Omini about more networks, for example the VLANs and LANs of
your firewall. Omini also scans those private IPv4 networks (from /22 to /30,
up to 16 of them, WAN interfaces excluded). They are reached through the
router, so the scan uses ping and open ports and sees no MAC addresses there.

To get names for devices on the router's networks, let the router register
its DHCP leases in its DNS (OPNsense: **Services → Unbound DNS → General →
Register DHCP leases**), or add the router's own integration, which reports
its leases.

## SNMP

SNMP is a method of the network scan, on by default. Every host found is
probed once per deep scan with your SNMP credentials; the hosts that answer
are read in full on every round:

| Data | From |
|---|---|
| Name, description, uptime, vendor | `SNMPv2-MIB` |
| Ports: name, status, speed, MAC, byte and error counters | `IF-MIB` |
| Neighbors | `LLDP-MIB` |
| MAC table (which device is on which port, VLAN) | `BRIDGE-MIB`, `Q-BRIDGE-MIB` |
| ARP | `IP-MIB` |
| CPU, memory, temperatures, firmware, serial, model | [SNMP profiles](snmp-profiles.md) |

To use it:

1. Enable SNMP on the device, read-only. Enable **LLDP** too if it has it:
   links between switches are then confirmed instead of inferred.
2. In **Integrations → Network scan**, set **SNMP communities**: the
   read-only communities to try, separated by commas (`public, homelab`).
   `public` is the default.
3. For **SNMP v3**, fill in the **SNMP v3** group: user, authentication
   (none, MD5, SHA-1, SHA-224, SHA-256, SHA-384 or SHA-512) with its password,
   and privacy (none, DES, AES-128, AES-192 or AES-256) with its password. v3
   is tried before the communities. Omini remembers which credential each host
   answered to.

Communities and passwords are stored encrypted and never shown again.

### SNMP profiles

Standard MIBs do not say a device's CPU, memory or temperatures. **Profiles**
add them: YAML files matched by `sysObjectID`, `sysDescr` or an OID that
exists. Omini ships profiles for Net-SNMP (Linux, with lm-sensors), MikroTik,
Cisco IOS, Juniper, FortiGate, Synology, QNAP and HPE Aruba. You can add your
own or replace a shipped one in `<data dir>/profiles/`. The format is in
[SNMP profiles](snmp-profiles.md).

## nmap

The **nmap** integration runs the [nmap](https://nmap.org) installed on the
server. Omini does not ship nmap; install it with your package manager, or set
`OMINI_NMAP=install` in Docker. Without nmap the integration says how to
install it.

| Setting | Default | What it does |
|---|---|---|
| Subnets | `auto` | The networks to scan, like the network scan's |
| Detect operating systems | on | `-O`, only with raw-socket permission (see below) |
| Detect service versions | on | `-sV --version-light` |
| Scan again every (hours) | 24 | How often the whole network is scanned |

The full scan runs **in the background**: a /24 takes about a minute, and
until it finishes each round reports the last results. It uses
`--top-ports 100`. Hosts merge with the network scan's by MAC (or by address
when nmap sees no MACs). Service versions become banners, and OS guesses of 90 %
accuracy or more become the device's operating system.

### Scan one device

With nmap added and enabled, the device panel has **Scan (nmap)**. It scans
that device now and updates the map when it ends. How deep is set in the
integration's **Scan one device** group:

| Setting | Default | |
|---|---|---|
| Ports | 100 | How many of the most common ports (`--top-ports`); 65535 is every port |
| Service versions | Quick | Off; Quick (`--version-intensity 0`, about 15 s); Light (about 30 s); Full (all probes, can take minutes) |
| Default scripts | off | `-sC` |
| Operating system and route | on | `-O --traceroute`, with raw-socket permission |

Only devices on the map can be scanned. The result is kept until the next full
scan.

### nmap without root

nmap detects operating systems only with raw sockets. The Docker image runs as
root, so it does. When you run the binary as a normal user, give nmap the
permission and tell Omini:

```bash
sudo setcap cap_net_raw,cap_net_admin,cap_net_bind_service+eip "$(command -v nmap)"
OMINI_NMAP_PRIVILEGED=true ./omini
```

On NixOS the store is read-only; use a wrapper instead, which puts nmap in
`/run/wrappers/bin`:

```nix
security.wrappers.nmap = {
  source = "${pkgs.nmap}/bin/nmap";
  capabilities = "cap_net_raw,cap_net_admin,cap_net_bind_service+eip";
  owner = "root";
  group = "root";
};
```

## DHCP fingerprints

When a device joins the network (or renews its address) it broadcasts a DHCP
request. What it asks for, in its own order (option 55), and what it says it
is (the vendor class, option 60: `android-dhcp-14`, `MSFT 5.0`, `dhcpcd-…
:Linux`), tell its operating system, even with a private MAC address. Omini
**only watches** those requests: it never answers, never opens port 67, and
never gets in the way of a DHCP server running on the same machine (it reads
them with a packet socket filtered in the kernel to UDP port 67).

- It sees the requests that reach this server: devices on its own network.
  Devices on other VLANs reach the DHCP server through a relay, unseen.
- A device is fingerprinted the next time it asks for an address (when it
  joins, or halfway through its lease); fingerprints are kept across restarts.
- The name a device gives in its request (option 12) is used too.
- Recognized: Android (any version), Windows 7 to 11, Apple (iOS, iPadOS,
  macOS; told apart by other clues), Linux (dhcpcd, dhclient, BusyBox udhcp),
  and desk phones that name themselves. No third-party database is used.

Turn it off in **Integrations → Network scan → Methods → DHCP fingerprints**.

## Device identification

Every device gets a **type**, and when Omini can tell, an **operating
system**, a **brand** and a **product**. The device panel's **Identification**
section shows each conclusion with the evidence behind it: the web page title,
an open port, mDNS, the MAC vendor, the name, the SSH banner, the TTL, the
DHCP fingerprint, the model, UPnP, an integration, or "this server" (the
machine Omini runs on).

| | Examples |
|---|---|
| Types | Firewall, router, switch, access point, server, NAS, hypervisor, virtual machine, computer, phone, tablet, TV, media player, speaker, printer, camera, IP phone, UPS, solar inverter, smart home device, air conditioner, appliance, game console, wearable |
| Operating systems | Windows, macOS, iOS, Android, Linux distributions, FreeBSD-based firewalls... |
| Brands | From the MAC vendor, mDNS, UPnP or the device's own pages |
| Products | Homelab software and appliances: Proxmox, TrueNAS, OPNsense, Home Assistant... |

Some details:

- **Model names.** Apple identifiers and Android model codes become names:
  `iPhone14,2` is an iPhone 13 Pro, `SM-S911B` a Samsung Galaxy S23. This also
  applies to DHCP names that are model codes.
- **Apps.** Self-hosted apps found on a device's web ports (Jellyfin,
  qBittorrent, Grafana... about 2,600 are known) appear as their own small
  nodes under the machine that runs them, one per app.
- **Proxmox guests.** A device with a Proxmox MAC (a VM or container) is drawn
  under the Proxmox host when the network has exactly one. With several hosts,
  add the [Proxmox plugin](integrations.md#proxmox-ve), which places each
  guest under the node that really runs it.
- **Randomized MACs.** Phones that use a private MAC address are marked so,
  and the device list can filter them.
- **Your corrections win.** In the device panel you can change the device
  type and its icon; "Automatic" goes back to Omini's choice.

Icons: homelab software shows its own logo; other devices show their type's
icon with a badge for the OS or brand.

## Traffic flows

The map shows how much traffic each link carries. **Traffic flows** shows
**who talks to whom**: conversations between two addresses, with protocol,
service and bytes each way.

1. Add **Traffic flows** in **Integrations → Add integration**. While it is
   enabled, Omini listens on **UDP 2055** for NetFlow v5, NetFlow v9 and IPFIX,
   and on **UDP 6343** for sFlow v5. Both ports can be changed (0 turns one
   off).
2. Point your router's, firewall's or switch's flow export at the Omini
   server's address and that port. Allow the port in the host's firewall.
3. Open **Flows** in the sidebar (it appears while the integration is
   enabled).

Omini only listens; it never configures an exporter. Sampling rates are
applied, so sampled sFlow and NetFlow give estimated totals. Flows are summed
per minute; the 300 busiest conversations of each minute are kept for 24
hours. Listeners close when the integration is disabled or deleted (after
10 minutes, or three rounds when rounds are further apart).

The **Flows** screen has ranges from 15 minutes to 24 hours, a search box,
services named by port, and the exporters Omini is receiving from. Each
device's panel shows **Talks to (last hour)**: its top five conversations.

### OPNsense example

1. In OPNsense, open **Reporting → NetFlow**.
2. **Listening interfaces**: your LAN interfaces (and VLANs) whose traffic you
   want to see. **WAN interfaces**: your WAN, if you also want internet
   traffic.
3. **Version**: v9.
4. **Destinations**: `<omini address>:2055`.
5. Apply. Conversations appear on the Flows screen within a minute or two.

Other exporters work the same way: a MikroTik's **IP → Traffic Flow** with a
target at port 2055, or a switch's sFlow agent pointed at port 6343.
