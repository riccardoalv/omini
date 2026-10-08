# Integrations

An integration is a source of data: the built-in network scan, or a plugin
that reads a firewall, a switch, a Wi-Fi system or a hypervisor through its
own API. Every integration is **read-only**: none sends a request that
changes a device's configuration.

- [Adding an integration](#adding-an-integration)
- [Built-in integrations](#built-in-integrations)
- [The plugin store](#the-plugin-store)
- [Trust levels](#trust-levels)
- Plugins: [OPNsense](#opnsense) · [pfSense](#pfsense) · [MikroTik RouterOS](#mikrotik-routeros)
  · [UniFi Network](#unifi-network) · [TP-Link Omada](#tp-link-omada) · [OpenWrt](#openwrt)
  · [Proxmox VE](#proxmox-ve) · [Horaco switch](#horaco-switch) · [Mercusys Halo](#mercusys-halo)

## Adding an integration

1. **Integrations → Add integration** opens the store: the built-in
   integrations (marked "Built in"), the plugins you installed and the catalog.
2. Pick one. A plugin that is not installed yet is installed first (it takes
   a minute: Omini builds its Python environment).
3. Fill in its form: usually the device's address and a read-only account.
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
| **Traffic flows** | Receives NetFlow v5/v9, IPFIX and sFlow v5: who talks to whom | [Traffic flows](discovery.md#traffic-flows) |

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

When the network scan identifies one of these devices and its integration is
not set up, the **Integration available** alert says so.

Each plugin's README has the full list of endpoints it calls and its known
limitations. A summary follows.

## OPNsense

Reads the firewall through OPNsense's official REST API (24.7 or later):
interfaces with status, IPs, link speed and media, traffic and errors; ARP;
DHCP leases (ISC, Kea or dnsmasq); CPU, memory, swap, load, disks,
temperatures, uptime; pending firmware updates (from OPNsense's own last
check); gateways with status, latency and loss; VLANs; SFP modules; services;
VPN peers (WireGuard, OpenVPN, IPsec); DHCP pool usage (Kea, dnsmasq);
firewall states.

**Read-only account.**

1. **System → Access → Users → Add**: user `omini`, a random password (it is
   never used). Give it these privileges:

   | Privilege | For |
   |---|---|
   | Lobby: Dashboard | system and health (**required**) |
   | Status: Interfaces | ports, link speed, IPs, counters |
   | Reporting: Traffic | counters of VLANs and other virtual interfaces |
   | Diagnostics: ARP Table | ARP |
   | Status: DHCP leases | ISC DHCP leases (26.1+: *Services: ISC DHCPv4: Leases*) |
   | Services: DHCP: Kea(v4) | Kea leases, if you use Kea |
   | Services: Dnsmasq DNS/DHCP: Settings | dnsmasq leases (the default DHCP since 25.7) |
   | System: Gateways | gateway status |
   | System: Firmware | pending updates |
   | Interfaces: VLAN | VLAN names and parent ports |
   | Status: Services | services |
   | Diagnostics: Firewall statistics | state table |
   | VPN: WireGuard: Status, Status: OpenVPN, Status: IPsec | VPN peers, if you use them |
   | **System: Deny config write** | **recommended**: blocks configuration writes |

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
WireGuard, IPsec) and firewall states.

**Read-only account.**

1. Install the REST API package from the pfSense shell, with the build for
   your version (see the plugin's README; reinstall it after every pfSense
   upgrade). In **System → REST API → Settings**, enable **API Key**
   authentication.
2. **System → User Manager → Users → Add** user `omini` with a long random
   password, and give it the `REST API - /api/v2/... GET` privileges the plugin
   lists (at least `status/system`), plus **User - Config: Deny Config Write**.
   Temporarily add `REST API - /api/v2/auth/key POST`.
3. Create the key as that user:

   ```sh
   curl -k -u omini:'<password>' -X POST -H 'Content-Type: application/json' \
     -d '{"descr": "Omini"}' https://192.168.1.1/api/v2/auth/key
   ```

   Copy `data.key` from the answer, then remove the *auth/key POST* privilege.
4. In Omini, enter the address and the key.

Do not give the user *WebCfg - All pages*: it allows changes.

## MikroTik RouterOS

Reads RouterOS 7.1+ routers, switches and access points through the REST API:
identity, model, health, temperatures, pending updates (from the device's own
last check), interfaces with link rate and SFP diagnostics, bridges, bonds,
bridge VLANs and host table, ARP, DHCP leases, MNDP/LLDP/CDP neighbors, Wi-Fi
clients and default-route gateways.

**Read-only account** (in a RouterOS terminal):

```routeros
/user group add name=omini-read policy=read,api,rest-api comment="Omini (read-only)"
/user add name=omini group=omini-read password="a-long-random-password" address=<omini-ip>/32
# HTTPS for the REST API, if www-ssl has no certificate yet:
/certificate add name=omini-https common-name=router.lan days-valid=3650
/certificate sign omini-https
/ip service set www-ssl certificate=omini-https disabled=no
```

In Omini: address `https://<router>`, user `omini` and its password.
The one `POST` the plugin sends runs the Ethernet `monitor` command with
`once`, which only reads the link state.

## UniFi Network

Reads a UniFi OS console or the self-hosted UniFi Network Application (7.x
to 9.x): every adopted gateway, switch and access point with ports, link
speed, LLDP and uplinks; Wi-Fi and wired clients with band, signal and
traffic; VLANs; WAN status; CPU, memory, temperatures and pending firmware.

**Read-only account:** a **local** account (not a UI.com account, no
two-factor authentication) with the **View Only** role.

- UniFi OS: **Admins & Users → Create New**, **Restrict to local access
  only**, user `omini`, role **View Only** for the Network application.
- Self-hosted: **Settings → Admins & Users → Add Admin**, a local admin with
  the **View Only** role.

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

**Read-only application.**

1. As an administrator, in the **Global** view: **Settings → Platform
   Integration → Open API → Add New App**.
2. App name `Omini`, mode **Client**, role **Viewer**, and only the sites
   Omini should map.
3. Copy the **Client ID**, the **Client Secret** and the **Interface Access
   Address**.
4. In Omini: the controller address (e.g. `https://192.168.1.10:8043`), the
   client ID and secret, and the site name (empty: the default site).

## OpenWrt

Reads OpenWrt 21.02+ routers and access points through ubus over HTTP (the
API LuCI uses): board and system info, ports with link speed and counters,
bridge VLANs, WANs and gateways, host hints, DHCP leases, Wi-Fi clients with
signal and rates, ARP and temperatures.

**Read-only account:** an rpcd login with an ACL that allows only the methods
and files Omini reads. Over SSH on the router, create
`/usr/share/rpcd/acl.d/omini.json`:

```json
{
  "omini": {
    "description": "Omini: read-only access for the network map",
    "read": {
      "ubus": {
        "system": ["board", "info"],
        "network.device": ["status"],
        "network.interface": ["dump"],
        "luci-rpc": ["getHostHints", "getDHCPLeases"],
        "iwinfo": ["devices", "info", "assoclist"],
        "file": ["read", "list"]
      },
      "file": {
        "/proc/net/arp": ["read"],
        "/sys/class/thermal": ["list"],
        "/sys/class/thermal/thermal_zone*/type": ["read"],
        "/sys/class/thermal/thermal_zone*/temp": ["read"]
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
uci commit rpcd
/etc/init.d/rpcd restart
```

In Omini: the LuCI address (`https://192.168.1.1`), user `omini` and its
password.

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
plugin signs in with the same form as the login page and only reads pages.
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

## Writing your own

A plugin is a small Python program in its own Git repository. See
[Writing a plugin](plugins.md).
