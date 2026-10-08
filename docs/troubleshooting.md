# Troubleshooting

Start with two places:

- **Integrations**: each integration shows its last collection, its error and
  what it found. **Network scan → On this server** says what discovery can do
  where Omini runs.
- **The log**: `docker logs omini` (or the service's journal). Set
  `OMINI_LOG_LEVEL=debug` for details, including what plugins print.

## Questions

- [No devices are found, or only a few](#no-devices-are-found-or-only-a-few)
- [Devices have no names or models](#devices-have-no-names-or-models)
- [Devices are in the wrong place on the map](#devices-are-in-the-wrong-place-on-the-map)
- [The same device appears twice](#the-same-device-appears-twice)
- [There is no traffic on the map](#there-is-no-traffic-on-the-map)
- [A plugin fails](#a-plugin-fails)
- [Proxmox guests are missing or misplaced](#proxmox-guests-are-missing-or-misplaced)
- [nmap does not detect operating systems](#nmap-does-not-detect-operating-systems)
- [The Flows screen stays empty](#the-flows-screen-stays-empty)
- [Notifications do not arrive](#notifications-do-not-arrive)
- [I forgot the admin password](#i-forgot-the-admin-password)
- [Credentials stopped working after moving Omini](#credentials-stopped-working-after-moving-omini)

## No devices are found, or only a few

**Omini is not on your LAN.** In Docker's default bridge network, Omini sees
only Docker's own network: no MAC addresses, no multicast. Run it with
`network_mode: host` (Compose) or `--network host` (`docker run`). The
**Network discovery is limited** alert and **On this server** both say when
this is the problem.

On Docker Desktop (macOS, Windows) the container runs inside a VM, and host
networking may not reach your LAN; run Omini on a Linux machine, VM or
Raspberry Pi on the network you want to map.

**Ping is not allowed.** Without root or unprivileged ping sockets, the scan
falls back to TCP and misses devices with no open port. Allow ping sockets:

```bash
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

**The wrong networks are scanned.** With **Subnets** set to `auto`, Omini
scans the networks the server is connected to. VLANs reached through a router
are scanned only with **Also scan the routers' networks** and an integration
that reports them (the firewall's plugin, an SNMP router). You can also list
networks by hand: `192.168.1.0/24, 10.0.20.0/24`. Networks larger than a /22
are reduced to the /22 around the server.

**Give it a few rounds.** The first round finds what answers quickly; ports,
names and identification fill in over the next minutes.

## Devices have no names or models

Names and models come from mDNS, SSDP, NetBIOS, reverse DNS and the router.

- **Multicast.** mDNS and SSDP need host networking, and the host's firewall
  must let in **UDP 5353** and **UDP 1900**. With `ufw`:

  ```bash
  sudo ufw allow 5353/udp
  sudo ufw allow 1900/udp
  ```

  **On this server → Multicast** shows whether it works. The integration's
  **Found by** list shows how many devices each method found: zero for mDNS
  usually means multicast is blocked.
- **The router's DNS.** Let the router register DHCP leases in its DNS
  (OPNsense: **Services → Unbound DNS → General → Register DHCP leases**), or
  add the router's integration: its DHCP leases carry the names.
- **Rename it yourself.** Click the name in the device panel, or use the device
  list. Names you give are kept by Omini and win over discovered ones.

## Devices are in the wrong place on the map

Omini places a device from what the switches, access points and routers say
about it. Things that move devices around:

- **Switches forget idle MACs** after about five minutes (a phone asleep).
  Omini remembers where each MAC was last learned for six hours and keeps the
  device there while it is still present by other means (ARP, DHCP, Wi-Fi).
  A device seen only by ARP, never by a switch, hangs under the router.
- **Data of different ages.** Omini collects in rounds, from the access points
  to the router, and builds the map once per round, so a round is one
  consistent picture. A device that roams between access points moves with
  the next round. A shorter round interval makes the map follow faster.
- **No LLDP.** Without LLDP between your switches, their links are inferred
  from MAC tables (dashed lines). Enable LLDP on managed switches.
- **An unmanaged switch or access point** behind a port shows up as an
  "Unmanaged segment", with the devices behind it under it. That is expected:
  nothing tells Omini what that box is. Add its integration if it has one.
- **Proxmox guests** are drawn under their host only with the Proxmox plugin,
  or when the network has exactly one Proxmox host.

You can always drag nodes, put them in [areas](map.md#areas), and **Reset
layout** to start over.

## The same device appears twice

- **Randomized MACs.** Phones and laptops use a private MAC per network, and
  some change it over time: each new MAC is a new entry. The device list can
  filter **Only randomized MACs** and delete them in bulk. Old entries never
  come back unless the MAC is seen again.
- **No common MAC.** Omini merges what two integrations report about one
  machine by a shared MAC (or by address when one side has no MACs). If a
  device reports a different MAC than the one the network sees (a bridge MAC,
  a VM without guest agent), the two may not meet. Check the device's own
  integration for the right MAC; for a Proxmox node, see
  [Proxmox](#proxmox-guests-are-missing-or-misplaced).

## There is no traffic on the map

Traffic is computed from interface counters, between two collections:

- The first round has no rates; wait for the second.
- The network scan alone has no counters. Traffic comes from SNMP, from a
  plugin (firewall, switch, controller), or for Wi-Fi clients from an access
  point that reports it.
- A device that rebooted or reset its counters skips one round.
- Some plugins report no byte counters (Horaco switches count packets only;
  OpenWrt has no per-client rates).

## A plugin fails

1. Open the integration and press **Test connection**: plugins say what is
   wrong (address, credentials, missing permissions).
2. Check the error under the integration's name. "did not answer within ..."
   means it ran past its time limit (the device is slow or unreachable).
3. Turn on `OMINI_LOG_LEVEL=debug` and look at the log: the plugin's own
   output (stderr) and tracebacks are there.

Common causes:

| Message | What to do |
|---|---|
| plugins need uv to create their Python environment | Install [uv](https://docs.astral.sh/uv/) or set `OMINI_UV`. The Docker image has it |
| uses protocol N, this Omini supports [1] | Update Omini or the plugin |
| unexpected error: ... | A bug in the plugin: report it in its repository with the log |
| the plugin returned invalid data | The plugin sent something the data model refuses: report it |
| TLS / certificate errors | Most devices use self-signed certificates: leave **Verify the TLS certificate** off, or install a trusted certificate |
| Login refused, then nothing for a while | Some plugins pause after a refused login (10 to 15 minutes) so the device does not lock the account. Fix the password; saving retries |

A plugin's Python environment is rebuilt when its requirements or the SDK
change. If it is broken, **Update** it in the store, or remove and install it
again.

## Proxmox guests are missing or misplaced

- **The token sees nothing.** With privilege separation on, an API token has
  only the permissions given **to the token itself**, not its user's. Give the
  token the **PVEAuditor** role on `/`:

  ```bash
  pveum acl modify / --tokens 'omini@pve!omini' --roles PVEAuditor
  ```

- **No guests.** Listing VMs and containers and reading their configuration
  needs **VM.Audit** (part of PVEAuditor). Node status and network need
  **Sys.Audit**, storage **Datastore.Audit**. **Test connection** lists any
  missing privilege.
- **Only running guests** are reported; stopped guests and templates are not.
- **VMs without IPs.** VM addresses come from the QEMU guest agent: install
  and enable it in the VM, and on Proxmox VE 9 give the token
  **VM.GuestAgent.Audit**. The network scan usually finds them by MAC anyway.
- **The node and its scanned twin.** The Proxmox API exposes no MAC for the
  node's own ports. Omini adopts the MAC its address has in ARP or scans; you
  can also add `hwaddress <MAC>` to `vmbr0` in `/etc/network/interfaces`.

Without the plugin, guests are placed under the host only when the network has
exactly one Proxmox host.

## nmap does not detect operating systems

OS detection needs raw sockets. The Docker image runs as root and has them.
Running the binary as a normal user, give nmap the capabilities and set
`OMINI_NMAP_PRIVILEGED=true`; see [nmap without root](discovery.md#nmap-without-root).
**On this server → Raw sockets** shows whether they are available.

If nmap is not installed, the integration says how to install it; in Docker,
set `OMINI_NMAP=install`.

## The Flows screen stays empty

- The **Traffic flows** integration must be added and enabled. "Flow
  collection is off" means it is not.
- The exporter must send to the Omini server's address on **UDP 2055**
  (NetFlow/IPFIX) or **UDP 6343** (sFlow), and the host's firewall must let it
  in. **Receiving from** lists the exporters Omini hears.
- NetFlow v9 and IPFIX need the exporter's templates first; most exporters
  resend them every few minutes.
- Omini stops listening when the integration has not run for 10 minutes. Keep
  the round interval at 10 minutes or less while you use flows.

## Notifications do not arrive

1. Press **Send a test** on the channel. The answer, or the channel's last
   error, says what failed.
2. Check **Send alerts from**: with "Critical only", warnings are not sent.
   **Info** alerts (new devices, unmanaged switches) need "Everything".
3. An alert that opens again within 30 minutes of its last notification is
   not sent again, and the resolution of a dismissed alert is not sent.
4. Messages are sent at the end of a collection round, when an alert opens or
   resolves, not for alerts that were already open when the channel was
   added.

By channel:

- **Telegram**: the chat ID must be the one in `getUpdates` after you sent the
  bot a message (groups start with `-`). The bot must be in the group.
- **E-mail**: Gmail and Outlook need an app password. Port 587 is STARTTLS,
  465 is TLS. Look in the spam folder.
- **Slack, Discord, ntfy**: paste the webhook or topic URL as is; the format is
  set from the address.
- **Webhook**: the endpoint must answer 2xx within 15 seconds. With a signing
  secret, check `X-Omini-Signature` against the raw body.
- The server running Omini must be able to reach the service (outbound HTTPS,
  or SMTP).

## I forgot the admin password

The UI has no password change or reset yet. To create the admin account
again, remove the user from the database; everything else (integrations,
inventory, history) is kept:

```bash
docker stop omini
cp /path/to/data/omini.db /path/to/data/omini.db.bak     # just in case
sqlite3 /path/to/data/omini.db "DELETE FROM sessions; DELETE FROM users;"
docker start omini
```

The next visit shows the setup screen: create the admin again. Use the
`sqlite3` tool on the host, on the data directory or volume (the image does
not include it).

## Credentials stopped working after moving Omini

Device passwords, keys and tokens are encrypted with the secret key in
`<data dir>/secret.key` (or `OMINI_SECRET_KEY`). If you moved the database
without the key, or the key changed, Omini cannot read them: integrations
fail until you enter their secrets again. Always back up and move
`omini.db` and `secret.key` together.
