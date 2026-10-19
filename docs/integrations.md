# Integrations

An integration is a source of data: the built-in network scan, or a plugin
that reads a firewall, a switch, a Wi-Fi system or a hypervisor through its
own API. Collecting only reads. The integrations that support it can also
**change** their devices from the map; see
[Configuration changes](decisions.md#configuration-changes):

| Integration | Ports on/off | Switch VLANs (ports, create, delete) | VLAN networks (gateway, DHCP, access) and fixed addresses | Wi-Fi per VLAN | Wi-Fi channels | DNS names | Wake-on-LAN | Pause internet | Backups |
|---|---|---|---|---|---|---|---|---|---|
| Network scan (SNMP) | yes | yes (Q-BRIDGE-MIB) | | | | | | | |
| OPNsense | yes (26.7+) | | yes (26.7+) | | | yes | yes (os-wol) | yes (26.7+) | yes |
| pfSense | yes | | yes | | | yes | yes | yes | |
| MikroTik RouterOS | yes | yes | yes | yes (not under CAPsMAN) | yes | yes | yes | yes | yes |
| UniFi Network | yes | yes | yes (its gateway) | yes | yes | yes (Network 8.2+) | | yes (Network 7+) | yes |
| TP-Link Omada | yes | yes | | yes | | | | | yes |
| OpenWrt | yes | yes | yes | yes | yes | yes | yes (etherwake) | yes | yes |
| Proxmox VE | a guest's network device | a guest's network device | | | | | | | yes |
| Horaco switch | yes | ports and create | | | | | | | yes |
| AdGuard Home | | | | | | yes | | | yes |
| Pi-hole | | | | | | yes | | | yes |

Mercusys Halo, Docker, TrueNAS and Synology DSM only read.

Omini also wakes devices on its own networks by itself. Spanning tree and
LAGs are read (never changed) from SNMP (BRIDGE-MIB, IEEE8023-LAG-MIB),
MikroTik, UniFi and Omada (spanning tree and LAGs), OPNsense, pfSense,
OpenWrt and Proxmox (LAGs).

Switch VLAN changes need the device's VLAN filtering (or 802.1Q mode) on:
Omini never turns a global switch mode on by itself, and says where to do it. Their guides
below ask for an account allowed to make those changes; a read-only account
still draws the map, and a change then fails with a message saying which
permission is missing.

- [Adding an integration](#adding-an-integration)
- [Built-in integrations](#built-in-integrations)
- [The plugin store](#the-plugin-store)
- [Trust levels](#trust-levels)
- Plugins: [OPNsense](#opnsense) · [pfSense](#pfsense) · [MikroTik RouterOS](#mikrotik-routeros)
  · [UniFi Network](#unifi-network) · [TP-Link Omada](#tp-link-omada) · [OpenWrt](#openwrt)
  · [Proxmox VE](#proxmox-ve) · [Horaco switch](#horaco-switch) · [Mercusys Halo](#mercusys-halo)
  · [AdGuard Home](#adguard-home) · [Pi-hole](#pi-hole)

## Adding an integration

1. **Integrations → Add integration** opens the store: the built-in
   integrations (marked "Built in"), the plugins you installed and the catalog.
2. Pick one. A plugin that is not installed yet is installed first (it takes
   a minute: Omini builds its Python environment).
3. Fill in its form: usually the device's address and an account for Omini.
4. **Test connection** checks the address and the credentials and says what is
   missing (many plugins list the permissions the account lacks).
5. Save. A collection round starts at once.

Create a **dedicated, least-privilege account** on each device for Omini, as
described below for each plugin. Omini stores its password, key or token
encrypted, never shows it again and never logs it. Plugins receive it only
when they run.

Where an integration's devices overlap with the network scan's (same MAC),
they are merged into one node, keeping the richer data.

## Built-in integrations

| Integration | What it does | Details |
|---|---|---|
| **Network scan** | Finds every device with no setup: ARP, ping, ports, DNS, NetBIOS, mDNS, SSDP, web titles, SSH banners and SNMP v2c/v3. Created automatically | [Discovery](discovery.md#the-network-scan) |
| **nmap** | Operating systems and service versions, with the nmap installed on the server; "Scan (nmap)" in the device panel | [nmap](discovery.md#nmap) |
| **Traffic flows** | Receives NetFlow v5/v9, IPFIX and sFlow v5: who uses the internet and where it goes | [Traffic flows](discovery.md#traffic-flows) |

Each of them can be added only once.

SNMP devices (switches, NAS, Linux servers with Net-SNMP, routers) need no
integration of their own: enable SNMP on the device and give its community or
v3 user to the network scan. See [SNMP](discovery.md#snmp).

## The plugin store

The store opens from **Integrations → Add integration** (`/store` opens it
too). It has a search bar, **All / Installed / Available** tabs, categories
and a trust-level filter. Each card shows the logo, the publisher and trust
badges, the repository, the release that was reviewed and known issues.

| Action | What it does |
|---|---|
| **Install** | Downloads the plugin and builds its environment |
| **Add integration** | Opens the form of an installed plugin |
| **Update** | Installs the latest release again from the same address |
| **Remove** | Deletes the plugin, its environment and its state. Delete the integrations that use it first |
| **+** (Add from a GitHub address) | Installs any public GitHub repository with a `plugin.yaml` |

How installs work:

- The version is resolved to a commit: the latest **release**, else the
  newest commit of the default branch (shown as `main@1a2b3c4`). Installs are
  pinned to that commit until you update.
- Omini downloads that commit's tarball (no `git` needed) into
  `<data dir>/plugins/<id>/src`, and builds a Python environment with
  [uv](https://docs.astral.sh/uv/) in `<data dir>/plugins/<id>/.venv`, with the
  plugin's `requirements.txt` and the SDK of the running Omini.
- Plugins run as separate processes with only the environment they need
  (never Omini's own settings or secret key), one run per collection, with a
  time limit.

The catalog comes from an **index** in Omini's repository, fetched once a day
and cached for offline starts. The store says where its list came from and
offers **Update now**. `OMINI_PLUGIN_INDEX` points it elsewhere, or `off`
keeps the list shipped with Omini.

## Trust levels

Each plugin carries two badges.

**Publisher:** **Official** (maintained by the Omini project) or **Community**
(maintained by its author).

**Trust level**, set by the maintainers for a reviewed release, never by the
author:

| Level | Meaning |
|---|---|
| **Plug and play** | Fully tested on real hardware: works out of the box |
| **Stable** | Tested; known issues are documented |
| **Experimental** | Partially tested: it may not work with every setup |
| **Unverified** | Not reviewed. Always the level of plugins installed from a GitHub address |

A level belongs to a release. When the installed release is newer than the
reviewed one, the store says so. The review process is in
[Plugin review](plugin-review.md).

A plugin runs code on your server and receives the credentials you give it:
install only plugins you trust.

### Current plugins

All official plugins are **Experimental** in this release. Several were built
from the vendors' documentation and recorded answers, not yet on real
hardware; reports from users are welcome.

| Plugin | Repository | Category |
|---|---|---|
| OPNsense | [omini-plugin-opnsense](https://github.com/riccardoalv/omini-plugin-opnsense) | Firewall, router |
| pfSense | [omini-plugin-pfsense](https://github.com/riccardoalv/omini-plugin-pfsense) | Firewall, router |
| MikroTik RouterOS | [omini-plugin-mikrotik](https://github.com/riccardoalv/omini-plugin-mikrotik) | Router, switch, Wi-Fi |
| UniFi Network | [omini-plugin-unifi](https://github.com/riccardoalv/omini-plugin-unifi) | Router, switch, Wi-Fi |
| TP-Link Omada | [omini-plugin-omada](https://github.com/riccardoalv/omini-plugin-omada) | Router, switch, Wi-Fi |
| OpenWrt | [omini-plugin-openwrt](https://github.com/riccardoalv/omini-plugin-openwrt) | Router, Wi-Fi |
| Proxmox VE | [omini-plugin-proxmox](https://github.com/riccardoalv/omini-plugin-proxmox) | Hypervisor |
| Horaco switch | [omini-plugin-horaco](https://github.com/riccardoalv/omini-plugin-horaco) | Switch |
| Mercusys Halo | [omini-plugin-mercusys](https://github.com/riccardoalv/omini-plugin-mercusys) | Wi-Fi |
| AdGuard Home | [omini-plugin-adguard](https://github.com/riccardoalv/omini-plugin-adguard) | DNS server |
| Pi-hole | [omini-plugin-pihole](https://github.com/riccardoalv/omini-plugin-pihole) | DNS server |
| Docker | [omini-plugin-docker](https://github.com/riccardoalv/omini-plugin-docker) | Containers |
| TrueNAS | [omini-plugin-truenas](https://github.com/riccardoalv/omini-plugin-truenas) | NAS |
| Synology DSM | [omini-plugin-synology](https://github.com/riccardoalv/omini-plugin-synology) | NAS |

When the network scan identifies one of these devices and its integration is
not set up, the **Integration available** alert says so.

Each plugin's README has the full list of endpoints it calls and its known
limitations. A summary follows.

## OPNsense

Reads the firewall through OPNsense's official REST API (24.7 or later):
interfaces with status, IPs, link speed and media, traffic and errors; ARP and
NDP (IPv6 neighbors);
DHCP leases (ISC, Kea or dnsmasq); CPU, memory, swap, load, disks,
temperatures, uptime; pending firmware updates (from OPNsense's own last
check); gateways with status, latency and loss; VLANs; SFP modules; services;
VPN peers (WireGuard, OpenVPN, IPsec); DHCP pool usage (Kea, dnsmasq);
firewall states.

On 26.7 and later it also **manages each VLAN's network** (the VLAN on the
port toward your switches, the interface and gateway, Kea or dnsmasq DHCP,
access rules marked "Omini:", the os-mdns-repeater plugin for discovery) and
**fixed addresses**. Rules set by hand show as *Custom* and are left alone.

**Account.**

1. **System → Access → Users → Add**: user `omini`, a random password (it is
   never used). Give it these privileges:

   | Privilege | For |
   |---|---|
   | Lobby: Dashboard | system and health (**required**) |
   | Status: Interfaces | ports, link speed, IPs, counters |
   | Reporting: Traffic | counters of VLANs and other virtual interfaces |
   | Diagnostics: ARP Table, Diagnostics: NDP Table | ARP and IPv6 neighbors (traffic flows over IPv6) |
   | Status: DHCP leases | ISC DHCP leases (26.1+: *Services: ISC DHCPv4: Leases*) |
   | Services: DHCP: Kea(v4) | Kea leases, if you use Kea |
   | Services: Dnsmasq DNS/DHCP: Settings | dnsmasq leases (the default DHCP since 25.7) |
   | System: Gateways | gateway status |
   | System: Firmware | pending updates |
   | Interfaces: VLAN | VLAN names and parent ports |
   | Status: Services | services |
   | Diagnostics: Firewall statistics | state table |
   | VPN: WireGuard: Status, Status: OpenVPN, Status: IPsec | VPN peers, if you use them |
   | **System: Deny config write** | only if Omini should **not** manage networks: it blocks every write |

   Managing networks needs the write privileges the plugin's README lists
   (interfaces, VLANs, Kea or dnsmasq, firewall rules and aliases); so do
   local DNS names (Unbound or dnsmasq), Wake-on-LAN (os-wol), pausing
   devices (aliases, rules, states) and backups (Backup & Restore).

2. Edit the user again and, under **API keys**, click **+**. OPNsense
   downloads a file with the key and the secret.
3. In Omini, enter the firewall's address (`https://192.168.1.1`), the key and
   the secret. **Test connection** lists missing privileges.

*Verify the TLS certificate* is off by default (OPNsense uses a self-signed
certificate). To get names for the devices on your LAN, also enable **Services
→ Unbound DNS → General → Register DHCP leases**.

## pfSense

Reads pfSense CE 2.7.2+ or Plus 24.03+ through the
[REST API package](https://pfrest.org/) v2: interfaces with link speed and
counters, WANs and gateways (status, latency, loss), ARP, DHCP leases (ISC or
Kea) and pool usage, system health, VLANs, services, VPN peers (OpenVPN,
WireGuard, IPsec) and firewall states. It also **manages each VLAN's
network** (VLAN, interface and gateway, DHCP server, access rules marked
"Omini:") and **fixed addresses** (static mappings); discovery between VLANs
(Avahi) has no API and stays in **Services → Avahi**.

**Account.**

1. Install the REST API package from the pfSense shell, with the build for
   your version (see the plugin's README; reinstall it after every pfSense
   upgrade). In **System → REST API → Settings**, enable **API Key**
   authentication.
2. **System → User Manager → Users → Add** user `omini` with a long random
   password, and give it the `REST API - /api/v2/... GET` privileges the plugin
   lists (at least `status/system`). To manage networks, also give it the
   write privileges the plugin's README lists (also for local DNS names,
   Wake-on-LAN and pausing devices); for the map alone, add
   **User - Config: Deny Config Write** instead.
   Temporarily add `REST API - /api/v2/auth/key POST`.
3. Create the key as that user:

   ```sh
   curl -k -u omini:'<password>' -X POST -H 'Content-Type: application/json' \
     -d '{"descr": "Omini"}' https://192.168.1.1/api/v2/auth/key
   ```

   Copy `data.key` from the answer, then remove the *auth/key POST* privilege.
4. In Omini, enter the address and the key.

Do not give the user *WebCfg - All pages*: it allows every change.

## MikroTik RouterOS

Reads RouterOS 7.1+ routers, switches and access points through the REST API:
identity, model, health, temperatures, pending updates (from the device's own
last check), interfaces with link rate and SFP diagnostics, bridges, bonds,
bridge VLANs and host table, ARP and IPv6 neighbors, DHCP leases, MNDP/LLDP/CDP neighbors, Wi-Fi
clients and default-route gateways.

**Account** (in a RouterOS terminal). The `write` policy lets Omini change
the device; leave it out for the map alone:

```routeros
/user group add name=omini policy=read,write,api,rest-api comment="Omini"
/user add name=omini group=omini password="a-long-random-password" address=<omini-ip>/32
# HTTPS for the REST API, if www-ssl has no certificate yet:
/certificate add name=omini-https common-name=router.lan days-valid=3650
/certificate sign omini-https
/ip service set www-ssl certificate=omini-https disabled=no
```

In Omini: address `https://<router>`, user `omini` and its password.
Collecting sends one `POST`: the Ethernet `monitor` command with `once`,
which only reads the link state. Changes (kept across reboots by RouterOS):

- **ports**: the port's `disabled` property;
- **switch VLANs** on a bridge with **VLAN Filtering** on (Bridge → bridge →
  VLAN; Omini never turns it on): a port's PVID, frame types and tagged
  lists, and `/interface/bridge/vlan` entries named by their comment;
- **VLAN networks** on a router: the VLAN interface, gateway address, pool,
  DHCP server and network, firewall rules marked "Omini:" (before the
  default drops) and, on 7.16+, the DNS mDNS repeater;
- **fixed addresses**: static DHCP leases.

A change is read back; if it does not come back as asked, Omini undoes its
writes, newest first.

## UniFi Network

Reads a UniFi OS console or the self-hosted UniFi Network Application (7.x
to 9.x): every adopted gateway, switch and access point with ports, link
speed, LLDP and uplinks; Wi-Fi and wired clients with band, signal and
traffic; VLANs; WAN status; CPU, memory, temperatures and pending firmware.

**Account:** a **local** account (not a UI.com account, no two-factor
authentication) that can manage devices, to change ports, VLANs and Wi-Fi
(**View Only** is enough for the map alone):

- UniFi OS: **Admins & Users → Create New**, **Restrict to local access
  only**, user `omini`, role **Full Management** for the Network application.
- Self-hosted: **Settings → Admins & Users → Add Admin**, a local admin with
  the **Site Admin** role ("Administrator" on older versions).

Changes go to the controller, which provisions the devices:

- **ports**: the port's override (`forward` "disabled");
- **switch VLANs**: the port's override (native network and tagged
  networks; its profile and other settings stay); a VLAN is a site network
  ("VLAN only"), so creating or deleting one applies to every UniFi switch.
  Networks with a gateway are deleted in UniFi itself;
- **Wi-Fi per VLAN**: the site's WLANs.

In Omini: the console (`https://192.168.1.1`) or application
(`https://controller:8443`) address, the account, and the **site** (the part
after `/manage/` in the Network app's address; `default` for the first site).
One integration reads one site.

## TP-Link Omada

Reads an Omada Software Controller or OC200/OC300, version 5.9 or later,
through the official Open API: gateways, switches and access points with
ports, link speed, SFP diagnostics, LLDP, uplinks, WANs with latency and loss,
LAN networks, radios, clients, and pending firmware. The cloud-based
controller is not supported.

**Application.**

1. As an administrator, in the **Global** view: **Settings → Platform
   Integration → Open API → Add New App**.
2. App name `Omini`, mode **Client**, role **Administrator** (or a custom
   role with **Modify** permission on devices and networks) to change ports,
   VLANs and Wi-Fi (**Viewer** is enough for the map alone), and only the
   sites Omini should map.
3. Copy the **Client ID**, the **Client Secret** and the **Interface Access
   Address**.
4. In Omini: the controller address (e.g. `https://192.168.1.10:8043`), the
   client ID and secret, and the site name (empty: the default site).

Changes: **ports** on and off; **switch VLANs** as site networks ("VLAN"
purpose) and port profiles: a port gets an existing profile with exactly the
asked VLANs and the same other settings, or a new one named "Omini …"
(profiles are never edited); **Wi-Fi per VLAN** (SSIDs). Every Omada port has
a native VLAN, LAG ports and ports with Profile Override are not changed.

## OpenWrt

Reads OpenWrt 21.02+ routers and access points through ubus over HTTP (the
API LuCI uses): board and system info, ports with link speed and counters,
bridge VLANs, WANs and gateways, host hints, DHCP leases, Wi-Fi clients with
signal and rates, ARP and temperatures.

**Account:** an rpcd login with an ACL that allows only the methods and files
Omini reads, plus changes to `network`, `wireless`, `dhcp` and `firewall`
in `/etc/config`. Over SSH on the router, create `/usr/share/rpcd/acl.d/omini.json`:

```json
{
  "omini": {
    "description": "Omini: reads for the network map, changes ports, VLANs, networks and Wi-Fi",
    "read": {
      "ubus": {
        "system": ["board", "info"],
        "network.device": ["status"],
        "network.interface": ["dump"],
        "luci-rpc": ["getHostHints", "getDHCPLeases"],
        "iwinfo": ["devices", "info", "assoclist"],
        "file": ["read", "list"],
        "uci": ["get"]
      },
      "uci": ["network", "wireless", "dhcp", "firewall", "system"],
      "file": {
        "/proc/net/arp": ["read"],
        "/sys/class/thermal": ["list"],
        "/sys/class/thermal/thermal_zone*/type": ["read"],
        "/sys/class/thermal/thermal_zone*/temp": ["read"]
      }
    },
    "write": {
      "ubus": {
        "uci": ["set", "add", "delete", "revert", "apply", "confirm", "rollback"],
        "file": ["stat", "exec"]
      },
      "uci": ["network", "wireless", "dhcp", "firewall"],
      "file": {
        "/usr/bin/etherwake": ["list", "exec"]
      }
    }
  }
}
```

Then add the login:

```sh
uci add rpcd login
uci set rpcd.@login[-1].username='omini'
uci set rpcd.@login[-1].password="$(uhttpd -m 'a-long-random-password')"
uci add_list rpcd.@login[-1].read='omini'
uci add_list rpcd.@login[-1].write='omini'
uci commit rpcd
/etc/init.d/rpcd restart
```

In Omini: the LuCI address (`https://192.168.1.1`), user `omini` and its
password. For the map alone, leave out the `"write"` block and the
`write` line. Changes: **ports** (the `enabled` option); **switch VLANs**
(`bridge-vlan` sections of a bridge with **Bridge VLAN filtering** on, in
LuCI's Network → Interfaces → Devices; Omini never turns it on); **VLAN
networks** (interface, gateway, dnsmasq range and a firewall zone with rules
marked "Omini:") and **fixed addresses** (`config host`); **Wi-Fi per
VLAN**; **local DNS names** (dnsmasq `config domain`); **pausing a device's
internet** (an fw4 ipset `omini_paused` and a rule "Omini pause" per LAN zone
towards the WAN zone). Each is applied with rollback: unless Omini sees the
port change and confirms it within 30 seconds, the router restores the old
configuration by itself. Routers with an old switch chip (swconfig) are not
supported for changes. **Wake-on-LAN** runs `etherwake` (install it on the
router) through rpcd's `file exec`, which the ACL allows for that one binary
only. **Backups** are the `network`, `wireless`, `dhcp`, `firewall` and
`system` configs as `uci export` prints them, without Wi-Fi keys or other
secrets (`system` is read only). Bonding interfaces are read as LAGs.

## Proxmox VE

Reads a Proxmox VE host or a whole cluster (7 and later) with a read-only API
token: node health, network interfaces, storage, and every running VM and
container as its own device, **placed under the node that runs it** on the
bridge it uses. With the plugin, guests are placed for real, even with several
Proxmox hosts on the network.

**Read-only token** (from a node's shell):

```bash
pveum user add omini@pve --comment "Omini (read-only)"
pveum acl modify / --users omini@pve --roles PVEAuditor
pveum user token add omini@pve omini --privsep 1
pveum acl modify / --tokens 'omini@pve!omini' --roles PVEAuditor
```

The third command prints the token secret once. With privilege separation on,
the token needs its own permission (the last command): a token without it
sees nothing. **PVEAuditor** on `/` gives `Sys.Audit`, `VM.Audit` and
`Datastore.Audit`, which is all the plugin needs. Guest IPs of VMs come from
the QEMU guest agent and need `VM.GuestAgent.Audit` on Proxmox VE 9.

In Omini: the address (`https://192.168.1.10:8006`), the token ID
(`omini@pve!omini`) and the secret.

## Horaco switch

Reads the Horaco HC-SWTGW218AS and switches with the same web interface
(no SNMP on their stock firmware): model, firmware, MAC, uptime, port status
and speed, RJ45 or SFP per port, port counters and the MAC table.

These switches have a single admin account: use it (`admin` by default). The
plugin signs in with the same form as the login page and reads pages; it
enables and disables ports through the **Port Setting** form, keeping each
port's other settings, then **Save Configuration**.
The switch keeps one session, so signing in from Omini may sign you out of
its web interface in your browser. Its web server is slow: the plugin reads
one page at a time, and a collection takes a few seconds.

## Mercusys Halo

Reads Mercusys Halo mesh systems (tested with the H60X) through the units'
local web interface: every unit as an access point, Wi-Fi and wired clients
with names, band and current traffic, the Wi-Fi network names (the passwords
in the same answer are never kept), and CPU and memory of the main unit.
TP-Link Deco units use the same interface and may work.

Use the main unit's address and the web interface password. The plugin keeps
its session between collections and signs in again only when asked; signing
in may sign you out of the web interface in your browser. The local API has
no per-client signal or link rate.

## AdGuard Home

Reads AdGuard Home (the standalone server or the Home Assistant add-on): its
version, the local names it answers (DNS rewrites to an IPv4 address) and,
when its DHCP server is on, the leases, fixed addresses and pool usage.
Omini writes the [local DNS names](map.md#the-device-panel) of devices with a
fixed address there when your network's DHCP hands AdGuard Home out as DNS
(`/control/rewrite/add|update|delete`), and keeps its settings as
configuration backups (no passwords). With [Traffic flows](discovery.md#traffic-flows),
its query log (`/control/querylog`) names where devices go: which client
looked up which name, and the addresses it got. Omini keeps them in memory for
a day.

**Account:** the AdGuard Home user (basic auth); it has full access, so use a
long password. In Omini: the address (`http://192.168.1.53:3000`), the user
and the password.

## Pi-hole

Reads Pi-hole v6: its version, local DNS records (`dns.hosts`) and domain
and, when its DHCP server is on, the leases, fixed addresses and pool usage.
Omini writes the local DNS names of devices with a fixed address there when
your network's DHCP hands Pi-hole out as DNS (`/api/config/dns/hosts`), and
keeps its configuration as backups (no passwords or keys).
With [Traffic flows](discovery.md#traffic-flows), its query log (`/api/queries`)
names where devices go: Pi-hole logs which client looked up which name but not
the addresses, so Omini asks the Pi-hole for them. Blocked lookups are left
out. Omini keeps them in memory for a day.

**Account:** an **application password** (Settings → Web interface / API →
Configure app password). To let it change local names, turn on
`webserver.api.app_sudo` (Settings → All settings → Webserver and API).
In Omini: the address (`http://pi.hole` or `http://192.168.1.53`) and the
application password.

## Docker

Reads a Docker host through the Docker Engine API, or every Docker environment
of a Portainer: the host (version, CPU count, memory), its containers and, on
a Swarm manager, its services. A container or Swarm service that publishes
ports is the app the network scan finds on them (the app's panel shows its
container: image, stack, state and replicas), or an app of its own when the
scan found none ([apps and containers](map.md#apps-and-containers)). A
container with an address of its own (macvlan, ipvlan) is a device under its
host; the others are the host's services. Read-only.

**Account:** never expose the Docker socket itself. Put a read-only socket
proxy in front of it (for example `tecnativa/docker-socket-proxy` with
`CONTAINERS=1`, `NETWORKS=1`, `INFO=1` and `POST=0`, plus `SERVICES=1`,
`TASKS=1` and `NODES=1` on a Swarm manager) and give Omini its address, or use
a Portainer access token. The plugin's README has the details.

## TrueNAS

Reads TrueNAS SCALE and Community Edition through the REST API (HTTPS only):
system, version and uptime, network interfaces (IPv4 and IPv6), pools as
storage (so `disk_full` works), disk temperatures and apps. Read-only.

**Account:** a user with the **Read-Only Administrator** role (24.10 or
later) and an API key of that user.

## Synology DSM

Reads a Synology NAS through the DSM Web API: model, serial, version and
uptime, CPU and memory, network interfaces, volumes as storage, disk
temperatures and Container Manager containers. Read-only.

**Account:** try a regular user with the DSM application privilege first;
some data needs the administrators group (the connection test lists what is
missing). Two-step verification must be off for this account.

## Writing your own

A plugin is a small Python program in its own Git repository. See
[Writing a plugin](plugins.md).
