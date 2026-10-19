# The map

The map is Omini's home screen. It shows every device, how they are linked,
on which ports and at what speed, the traffic each one carries and what needs
attention.

- [Reading the map](#reading-the-map)
- [Layout](#layout)
- [Nodes](#nodes)
- [Links](#links)
- [The device panel](#the-device-panel)
- [Wi-Fi channels](#wi-fi-channels)
- [Folding children into bubbles](#folding-children-into-bubbles)
- [Hiding and deleting devices](#hiding-and-deleting-devices)
- [Areas](#areas)
- [VLAN and subnet filter](#vlan-and-subnet-filter)
- [The device list](#the-device-list)
- [People](#people)
- [Export](#export)

## Reading the map

The bar at the top shows:

- **The menu** (☰): a drawer with four tabs: **Devices** (the
  [device list](#the-device-list), with the switches for offline and hidden
  devices), **People** ([who owns which devices](#people), and pausing a
  person's internet), **VLANs** ([the network's VLANs](#the-networks-vlans) and
  subnets: a click on one is the [network filter](#vlan-and-subnet-filter);
  the [networks' areas](#the-networks-areas) switch) and **DNS & DHCP** (see
  [discovery](discovery.md)). A dot on it means the map is filtered;
  the network being highlighted shows next to it, with an × to clear it.
- **N alerts**: open critical and warning alerts. Click it to open the
  Alerts screen.
- **N problems**: integrations whose last collection failed.
- **Updated ...**: when the map was last built.

The toolbar on the right has the orientation toggle, **New area**,
**Export**, **Reset layout** and **Refresh now** (starts a collection round).

Devices that went offline stay on the map, dimmed, for one hour; after that
they leave the map but stay in the device list. Unchecking **Offline
devices** in the menu's Devices tab removes them at once.

## Layout

Omini lays the map out as a **tidy tree**, rooted at your internet uplinks and
firewall:

- **Left to right** (default): the internet on the left, clients stacked on
  the right. Each level of the network is a column.
- **Top down**: each level is a row. To keep wide networks readable, the leaf
  children of a node (four or more, with nothing below them: clients, apps)
  are packed into a compact grid under it.

The orientation toggle in the toolbar switches between them; the choice is
remembered per browser.

Each subtree gets its own band, a parent is centered on its children, and
siblings stay grouped by the port or Wi-Fi network they hang from, then by
name, so a link's label sits over the devices that use it. Nothing overlaps:
nodes, link labels and areas they do not belong to.

- **Drag** any node to move it. Positions are saved on the server, separately
  for each orientation.
- **Reset layout** forgets the dragged positions and lays the map out again,
  simplified: every node with two or more clients folds them into a bubble
  (what you had unfolded folds again). Areas are kept. Changing the
  threshold in **Settings → Map** goes back to folding above it.
- When you fold or unfold a group, the node you clicked stays still on screen
  and the map makes room around it. The view only refits on first load, on an
  orientation change, on reset and when the sidebar opens or closes.

## Nodes

| Node | What it is |
|---|---|
| Device | A device an integration reads, or a host the network scan found. Its icon is the product logo for homelab software, else the device type with an OS or brand badge |
| WAN | An internet uplink of a router or firewall, drawn as its parent. Several WANs, several parents. Shows the uplink's name, speed (of the physical port, following PPPoE and VLANs down to it), gateway latency and a status dot: green, yellow when a gateway is degraded, red when down. The ISP modem hangs under it |
| Wi-Fi network | A small node between an access point and its clients, one per network and band ("IOT · 2.4 GHz"), its icon colored by band. Click it to fold or unfold its clients. It has no panel |
| App | A self-hosted app found on a device (Jellyfin, Grafana...), one per app, under the machine that runs it, with the container that serves it (see [Apps and containers](#apps-and-containers)) |
| Unmanaged | A device announced by LLDP that no integration reads |
| Unmanaged segment | Several devices behind one switch port with nothing integrated there: probably a small switch, a consumer access point or a hypervisor |
| Bubble | Folded children: "12 clients", or an area's name. Click it to unfold |

Marks on a node:

- A **↓/↑ badge** with its current traffic (received / sent), on top of the
  node. Routers, firewalls and WAN nodes show internet traffic; a device
  reached by a link of its own shows that link's traffic; a Wi-Fi client shows
  what its access point measures. Idle devices show none. Traffic is the
  average of the last collection interval.
- A **red or yellow mark** when the device has an open critical or warning
  alert.
- **Dimmed** when it is offline, or outside the [VLAN filter](#vlan-and-subnet-filter).

## Apps and containers

A self-hosted app is **one node**: the app the network scan found on a port
of its machine, with its icon, its name and a button to its web interface.
When an integration that reads containers (Docker, through the Engine API or
Portainer) knows the container or Swarm service that publishes that port, the
app carries it: its panel has a **Container** section with the container's or
service's name, its image, its state and replicas ("running · 1/1
replicas"), its stack and the ports it publishes. The container is not drawn
again as a node of its own.

| A container... | On the map |
|---|---|
| publishes a port where the scan found an app | That app's node, carrying the container |
| is a Swarm service published through the routing mesh | Every app found on its port on a node of the swarm carries it |
| publishes ports where the scan found no app (it was not scanned yet, or the page has no name Omini knows) | An app node of its own under its host, named and drawn after its image when Omini's app catalog knows it (`jc21/nginx-proxy-manager` is Nginx Proxy Manager), else after its service, with a link to its web page when the scan saw one on that port; offline when no replica runs |
| has an address of its own (macvlan, ipvlan) | A device of its own; its apps come from scanning its address |
| publishes no port | A service in its host's panel |

A container is one app: when several of its ports are found (Nginx Proxy
Manager's 80, 81 and 443), the app the scan named is the node and the others
are listed among its published ports. Matching knows no vendor: any
integration that reports a device with `container` and its published ports,
hung under its host, is shown this way ([writing a plugin](plugins.md)).

## Links

| Line | Meaning |
|---|---|
| Solid | Confirmed: LLDP/CDP/MNDP neighbors, a switch's MAC table, or a link an integration declared (a VM on its host) |
| Dashed | Inferred (ARP, a MAC seen behind an uplink), a site-to-site VPN tunnel between two routers (purple; red when its peer is not connected), or a link spanning tree keeps on standby (one of its ports is blocked) |
| Dotted | Wi-Fi |

A link made of a LAG (bond, aggregate) shows its number of ports and their
speed on its pill: "2 × 10G".

### Where a device stays

A device stays where Omini last saw it **for sure** until it is seen
somewhere else for sure:

- **For sure:** on an access point's list of Wi-Fi clients (it moves when it
  roams to another), alone on a switch port (a cable), or behind a desk phone.
- **Not for sure:** a MAC seen on a port shared with other devices, on a port
  towards other network gear, or only in a router's ARP or NDP (IPv6) table.

So a phone that drops out of its access point's list for a while stays on its
Wi-Fi, instead of being drawn on a cable behind a switch. The place is kept in
the database (it survives restarts) for a year, or until that access point or
switch leaves the map.

Thicker lines are faster links (1G, 2.5G, 10G and up). A link between two
network devices that runs below 1 Gbps is marked as slow.

A **pill** near the end of a link names the port and its maximum speed:
"LAN | 10G". The port name is the one you gave it, else the interface's own
name (`igc1`, `Port 5`); a bridge shows the physical port behind it. The pill
is colored by speed. It appears when the speed is known: switch-port and LLDP
links, and a device port with a single link (a WAN to its modem). A port
shared by several devices (a LAN bridge with a switch behind it) gets one pill
at the port. Wi-Fi links have no pill: the Wi-Fi network node says the band,
and the link rate is in the client's panel. App links have none either.

**Click a link** to see its traffic over time: the history of the upstream
port, else of the device's port, else of the Wi-Fi client. The chart has
ranges of 1 hour, 24 hours, 7 days, 30 days and 1 year, with average and peak,
and gaps where there is no data. Omini keeps one point per minute for 24 hours
and an hourly average and peak for a year. A port with an SFP module keeps its
received power and temperature the same way: the chart shows the weakest
signal and the hottest temperature of the period, and both at the point under
the cursor.

## The device panel

Click a node to open its panel on the side, without leaving the map. Drag its
edge to resize it.

The panel shows little at first: the name, status, type and address, its open
**Alerts**, its **Ports** and its **Connected devices** (click one to open
it). Everything else folds to its title and a short summary (**Details** shows
the version and uptime, **Changes** how many there were); click a title to
open it. The sections you open stay open, for every device (per browser).

**IPv6.** Under the IP address, **IPv6** lists the device's IPv6 addresses
in compact form (`2001:db8::20`), global and ULA ones first; link-local ones
(`fe80::`) are grayed, as they only reach it on its own network. A device
known by IPv6 alone shows its first global address next to its status.

**Note.** At the top of the panel, a free note about the device ("in the
living room, bought 2024"), saved when you leave the field: up to 1,000
characters of plain text. It stays while the device is offline or off the map;
a note mark on the map shows it on hover, the device list's search finds it
and the JSON export carries it. The diagnostics file never includes it.

**Owner.** Below the note, the **Owner** select gives the device to one of
the [people](#people), to **Nobody**, or to a **New person…** named there
(created and picked at once). It is saved as soon as you pick it; a device
has at most one owner.

**Details.** Name (click to rename; the name is kept by Omini, never written
to the device; until then a client is named by a DNS record of its address,
else its DHCP reservation's name, else its DHCP lease's host name, a
router's first, else the names the scan found), type, IP and MAC addresses,
vendor, model, serial, the spanning tree root a switch sees (a link to it
when it is on the map), version, uptime, the integration that reads it,
**Connected to** (its parent and port),
**Runs on** (for a router or firewall running as a VM: the host it runs on,
which is not linked on the map), Wi-Fi network, band, signal and link rate for
Wi-Fi clients, and **Last seen**.

Details also hold the health:

- CPU and memory as bars: green under 60 %, yellow under 85 %, red above.
- Tiles for pending updates (yellow when an update is waiting; "Not checked"
  when the device never checked: Omini never starts a check), the hottest CPU
  temperature (yellow from 70 °C, red from 85 °C) and the load average.
- Bars for swap and for each disk (yellow from 80 %, red from 90 %).

**Ports.** A front view of the device, like a switch's: one jack per physical
port, in order, colored by link speed (10G purple, 5G blue, 2.5G teal, 1G
green, 100M or less amber, empty when down). SFP ports are drawn as cages and
named by generation (SFP, SFP+, SFP28, QSFP+). Hover for details; **click a
port** to open its card:

- interface, status, speed, duplex, media, connector, traffic and errors,
- its VLANs (untagged and tagged), the LAG it belongs to and its spanning tree
  state (in red when blocked: a redundant link on standby),
- the SFP module, when reported: vendor, part, type, temperature, voltage,
  bias, Tx and Rx power (in red when the light is weak: Rx below the module's
  alarm threshold, or below -20 dBm when it has none; or when the module is
  hot: at its own high temperature alarm, or 70 °C without one),
- what is connected to it, and the port's traffic chart,
- the pencil next to its name renames it ("Uplink to the rack"): kept by
  Omini, shown instead of the device's own description; empty restores it,
- **Disable port** / **Enable port**, when the device's integration can change
  it: one short confirmation, then Omini applies the change, checks it took
  effect and saves it on the device; the map shows it at once. A disabled port
  is struck through and shows "Disabled" (a port without a link shows "No
  link"); its client stays there, offline.

- **Change VLAN**, when the device's integration can set its ports' VLANs:
  access (one VLAN) or trunk (tagged VLANs, with a native one or none), from
  the network's VLANs. **See what changes** lists, device by device, what
  Omini will do: create the VLAN where it is missing and tag it on each link
  up to the router, and what it cannot (the router, for now, and devices it
  does not manage) for you to do by hand. Then **Apply**.

Taking a device off the network is an ordinary change. Disabling a port is
**destructive** only when it hurts the network as a whole: when Omini reaches
the device through it, or when it is the way to the router and the internet
(the device's uplink, or the port of the router itself). It is blocked, with
the reason, unless **Settings → Changes → Allow destructive changes** is on,
and then it asks twice. Omini finds where it runs by itself (the device with
one of its machine's MACs or addresses); in a container with a network of its
own it cannot, and then only the way to the internet is protected.

**Buttons for the device** (next to Scan), when its network allows them:

- **Wake** (an offline device with a MAC): a Wake-on-LAN packet, sent by
  Omini on its own networks, or by the router of the device's network
  (OPNsense with os-wol, pfSense, MikroTik, OpenWrt with etherwake). It is
  kept in the device's **Changes**; the device shows online when it answers.
  A device off for more than an hour has left the map: wake it from the
  [device list](#the-device-list).
- **Pause internet** / **Resume internet** (a client on a network whose router
  Omini manages: OPNsense 26.7+, pfSense, MikroTik, OpenWrt): after one short
  confirmation, the router blocks it toward the internet over IPv4 and IPv6 (by its MAC where the router can,
  so its changing IPv6 addresses are covered); your own networks stay
  reachable. If the
  device gets another address, Omini blocks the new one after the next round.
  Omini's own host and network devices are never paused. A device you blocked
  by hand on the router (OPNsense: a simple block rule from its IP, MAC or a
  host alias, to anything) shows as paused too, with the rule's name; resuming
  it turns that rule off, unless the rule blocks other devices as well. A paused device has
  a pause mark on the map and is listed at the top of the device list. To
  pause all of someone's devices at once, see [People](#people).

**Network**, for a device a managed router serves: **Reserve this IP** (a
DHCP reservation on the router, keeping its address; a router that refuses
reservations inside its DHCP range, like pfSense, gets a free address outside
it, proposed in the field) and its **DNS name**: with a fixed address the
device gets `<name>.<domain>` on the DNS server its network uses (AdGuard
Home or Pi-hole when the network's DHCP hands them out, else the router). The
name comes from the device's name ("Living Room TV" → `living-room-tv`) and
follows it when you rename the device; the pencil sets another, **Remove
name** takes it away. The domain is the one the router or DNS server uses,
else `home.arpa`; **Settings → Changes → Domain of local names** sets one.
Other names the DNS servers have for the address, or for one of the
device's IPv6 addresses (AAAA records), are listed below. Omini only sets
IPv4 (A) records, since devices change their IPv6 privacy addresses; AAAA
records made outside Omini are kept, never changed or deleted.

**Configuration**, folded, for a device whose integration keeps backups: the
versions of its configuration, read every day and after each change Omini
makes, kept when they changed (the last 50), each marked **by Omini** or
**outside Omini**. Pick two to see the lines removed and added; **Download**
saves one. Secrets are left out where the device allows it. Omini never
restores a configuration.

**Wi-Fi radios**, folded, for an access point whose integration reports them:
each radio's band, channel, width, **auto** when the channel is automatic,
transmit power and whether it is off; the summary lists the channels
("2.4 GHz 6 · 5 GHz 36"). A mesh unit says so. A conflict between access
points is settled from its alert ([Wi-Fi channels](#wi-fi-channels)).

**Exposed to the internet**, folded, for a router that reports its port forwards:
each external port or range, where it goes (the device's name when Omini knows
the address, else the address, and the port), the protocol, who may use it
(**from anyone**, or an address or prefix), a **UPnP** badge on those a device
opened by itself (listed first) and **off** on disabled ones. The summary counts
the enabled ones and those made by UPnP. It is read-only: Omini never changes
them; each enabled one is an alert (`port_forward`, or `upnp_forward`).

**Extras**, folded, when an integration reports them: services (stopped ones first),
VPN peers (WireGuard, OpenVPN, IPsec: connected, endpoint, last handshake,
traffic), DHCP pool usage and firewall states (yellow from 75 %, red from
90 %), and VLANs.

**Also in the panel:**

- **Open web interface** when Omini finds an admin page on the device (it
  checks ports 443, 80, 8443, 8080, 8006, 5001, 5000, 8123, 8096 and 9443 and
  reads the page title; no credentials are sent; cached for 10 minutes).
- **Scan (nmap)**, when the [nmap integration](discovery.md#scan-one-device)
  is enabled.
- **Identification**: type, OS, brand and product with the evidence for each,
  and the **Device type** and **Icon** you can override.
- With [Traffic flows](discovery.md#traffic-flows) enabled: **Where it goes
  (last 24 h)**, its internet traffic by destination, named by the lookups
  your AdGuard Home or Pi-hole saw; and **On your network (last hour)**, the
  devices it talks to, which **Show on the map** highlights (the rest of the
  map dims). The internet's node shows who is **using the internet now**.
- Its last **Activity** (joined and left the network).
- **Changes**: the changes made from Omini (kept a year), on the device or for
  it (a reservation, pause, DNS name or Wake-on-LAN its router or DNS server
  made, with that node's name), the refused ones with the device's error. A
  refusal repeated as it was (Omini retrying a DNS name or a paused device's
  new address) is kept once; those retries wait longer after each refusal
  (up to 6 hours).
- **Hide** and **Delete**.

## The network's VLANs

The **VLANs** tab of the map's menu (☰) lists the network's VLANs: id, name, color, subnet, and
on which devices and ports each one is. Omini keeps them: the first time it
imports those the devices already have, and from then on you create, rename
and delete them here, and Omini applies it to the devices (a new name to
every device that has the VLAN; a deleted VLAN from every device, which must
not use it on a port any more). A VLAN created on a device by hand is an
alert (`vlan_unknown`). Ports go into VLANs from their card (above).

**Creating a VLAN** asks for its id and name; with a router Omini can change
(OPNsense 26.7 or later), the same form has its gateway (suggested), DHCP
(on, .100 to .250) and its access: **Internet only**, **Whole network** or
**No internet**, plus **Discovery between VLANs**. Omini shows what it will do
on the router and applies it in one go. **Opening a VLAN** shows the front of
each switch: a click on a port puts it in the VLAN, then tags it (trunk),
then takes it out, showing the plan before applying. Each VLAN can have a **Wi-Fi network**: its
name (the VLAN's by default), password and on or off, created on every access
point or controller Omini manages (UniFi, Omada, OpenWrt); the password goes
to them only. From a device's panel (**Network**) you can **fix its address**
(a DHCP reservation on the router; many at once with **Reserve IPs** in the DNS &
DHCP tab) and **move it to another VLAN** (its
switch port changes, with the plan; others on the same port move too). A
Proxmox VM or container moves by its own network device (its tag), with the
switch port of its host tagged on the way; the host's bridge must be VLAN
aware.
The operator's VLAN under the WAN (PPPoE) is not listed. Networks that already
exist on the router (the LAN is VLAN 1) are imported with their gateway,
DHCP and access, and can be changed here too; access set by hand shows as
**Custom** until you pick one of the three.

## Wi-Fi channels

When two access points are on the same or overlapping 2.4 GHz channels, or
one is on a channel other than 1, 6 or 11, the alert `wifi_channel_conflict`
opens (one for the network, shown on each access point's panel too). Its
**Resolve** button, on the Alerts screen and in the device panel's alerts,
shows the plan before anything changes:

- each access point to move, its radio, the channel it leaves and the one it
  goes to, and how many clients reconnect (for a few seconds); **You are on
  it** when the browser you use is one of them;
- the access points left alone and why: an automatic channel (its controller
  would move it back), a mesh unit (a new channel can drop its link; never
  moved), an integration that cannot change the channel, a width other than
  20 MHz.

Omini keeps the access points already on a free channel, moves as few as it
can (the one with fewest clients first) and shares 1, 6 and 11 evenly among
more than three; never a DFS channel nor another width. It moves one access
point at a time: applied, checked that its radio is up on the new channel,
saved, then the next. The first that fails stops it; those already moved stay,
and each step shows **Done**, **Failed** or **Not run**. Every move is in the
access point's **Changes**. When Omini's own machine is on that access point's
Wi-Fi the step is destructive: it needs **Allow destructive changes** in
Settings and a second confirmation. When nothing can be moved (every access
point on automatic channel), the alert has no button.

## Folding children into bubbles

When an access point, a switch port or a segment has more clients than the
threshold (8 by default, **Settings → Map**), they are folded into a bubble
("23 clients", with how many are online). Click the bubble to unfold them.

You can also fold by hand. **Right-click any node** for its menu:

- **Collapse children**: folds everything below it (with what hangs below
  them) into a bubble, even a single child. Works on switches, access points,
  segments and hosts. A hypervisor's VMs fold with their apps.
- **Expand children**: unfolds them.
- **Details**: opens the panel.

A **middle click** on a node also folds or unfolds its children, and a click
on a Wi-Fi network node folds or unfolds its clients. Collapsing an access
point gathers its Wi-Fi networks and their clients in one bubble.

Servers and other infrastructure always stay visible. What is folded is remembered
per browser.

## Hiding and deleting devices

- **Hide** (device panel) takes the device and everything below it off the
  map. It stays in the device list, marked "Hidden". **Hidden devices** in
  the menu's Devices tab brings hidden devices (and hidden areas) back for a while;
  **Show on map** in the panel or the list unhides one.
- **Delete** (click twice to confirm) removes the device and its history from
  Omini. If it is still on the network, it comes back on the next scan: use
  Hide to keep it off the map.

Devices are never deleted automatically: the inventory keeps everything ever
seen, with first and last seen times.

## Areas

Areas are named, colored rectangles that group devices: "Rack", "Living
room", "Lab".

**Drawing.** Click **New area**, then drag a rectangle on the map (Esc
cancels). The devices whose center is inside become its **members**. An area
holds its members and **everything below them** (clients, apps, VMs), and is
always drawn around them, so it follows them when the map is laid out again.

**Rules.**

- A device belongs to **one area**. Putting it in an area takes it out of the
  one it was in.
- An area may sit **wholly inside another** (a rack inside a room), but never
  partly over it.
- The layout treats an area as a box: its members are laid out together, even
  when they come from different branches of the network (their links then
  cross the border), and no other device is ever left inside it.

**Changing an area.**

- Drag a device into an area to add it; drag it out to remove it.
- Drag the area to move it with everything inside.
- Drag its corner to resize it: the members are picked again from what is
  inside, and the area then fits around them.
- Double-click the title to rename it.
- Right-click the title for its menu: **Rename**, **Color** (six presets:
  gray, blue, green, yellow, red, purple, or **Any color** with a picker:
  saturation square, hue bar, hex and R/G/B fields; the area previews the
  color live and **Apply** saves it), **Collapse**, **Hide area** and
  **Delete area**.

**Collapsing** an area folds it into a single bubble with its name; click the
bubble to unfold it. Members folded into a parent's bubble take the area
along: the area wraps that bubble. An area whose members are all off the map
is not drawn.

**Hiding** an area removes its frame; its devices stay. **Show hidden**
brings hidden areas back, dimmed.

Areas are stored on the server and shown in both orientations. **Reset
layout** keeps them.

## VLAN and subnet filter

When the network has more than one VLAN or subnet (VLANs, LAN, WAN...), the
menu's **VLANs** tab lists them (the VLANs, then the other subnets), each
with its color, address range and number of devices. A click on one
**highlights** it, dims everything else and **zooms the map to it** (the menu
closes to show it); a click on it again, or the × next to the menu, clears
it. What is highlighted:

- devices with an interface in it, or an address in a network a router
  reports (its VLAN and gateway),
- links whose port carries it (untagged or tagged),
- clients seen in it in a MAC table,
- Wi-Fi clients of an SSID that puts its clients in it (as their access
  point, or its controller, reports the SSID's VLAN),
- and what hangs below a device that is only in that network.

A router with addresses in several networks lights up alone, without
everything below it. With the [networks' areas](#the-networks-areas) off,
each linked group of highlighted devices gets a **temporary dashed frame**
with the network's name; the frames go away when the filter is cleared.

VLAN 1 is listed only when it has a name. VLANs come from the integrations
(OPNsense, pfSense, MikroTik, UniFi, Omada, OpenWrt, SNMP switches...).

## The networks' areas

Every VLAN and subnet is drawn as an **area** on the map, in its color (a
VLAN's color from the VLANs tab), so the whole network's VLANs and address
ranges are seen at once. They follow the same rules as the areas you draw:
an area holds its devices and what hangs below them, nothing else, and the
layout keeps each network's devices together.

- A device in **several networks** (the router, a trunk switch, an access
  point carrying many VLANs) stays **outside** every network's area.
- A client's own address says which network it is in, whatever the link
  above it carries (a client under a trunk is not in every VLAN of it).
- A device in an area you drew stays in yours.
- The networks' areas are not stored and cannot be moved, resized or renamed:
  they follow their devices. **Network areas** in the menu's VLANs tab
  turns them off.

## The device list

The menu (☰) at the top left opens the inventory in a drawer, in the
**Devices** tab, with the count of devices and clients (`/devices` opens it
too). It lists everything ever seen on the
network:

- name, IP (with its first global IPv6 address below, and how many more:
  hover for all of them, while the device is on the map), MAC, type, where it
  is connected, first and last seen,
- a search box (name, IPv4 or IPv6 address, typed compact or in full, MAC,
  vendor, note) and filters: all, online, offline, only randomized MACs,
- rename, hide or unhide,
- **Wake** (the power button) on an offline device with a MAC, also one
  that left the map (off for more than an hour, a PC off overnight): the same
  Wake-on-LAN as the device's panel, with its last address,
- select several and **Delete selected** (phones with randomized MACs can
  leave many entries behind),
- above it, the devices whose internet is paused (click one to open it).

Click a name to close the drawer and open that device on the map, centered,
its group unfolded.

## People

The menu's **People** tab lists the people of the network: each with a dot
that is green while they are **home** (one of their phones, tablets or
wearables is online) and how many devices they have. Add a person by name,
rename one with the pencil, or delete one after a short confirmation (their
devices stay, with no owner). A device has at most one owner, set with the
**Owner** select in [its panel](#the-device-panel).

Click a person to see their devices; click a device to open it on the map.
**Pause internet** pauses every device of theirs that can be paused, after one
confirmation: each goes through its own [pause](#the-device-panel) on its
router and is kept in its own **Changes**. Omini's own host, routers and
network devices are never paused, and one device the router refuses does not
stop the others; a line per device then says whether it was done, skipped or
failed. **Resume internet** shows while one of their devices is paused and
gives them all their internet back.

The [Internet screen](discovery.md#traffic-flows) sums each person's devices under
**People**. People's names never reach the diagnostics file.

## Export

**Export** in the toolbar opens a dialog with the format, the theme (dark or
light) and the orientation:

| Format | What you get |
|---|---|
| Image (PNG) | The whole map at twice the screen's pixel ratio, scaled down past 8192 px |
| Vector image (SVG) | The whole map as SVG |
| Diagram (draw.io) | An editable [draw.io](https://www.drawio.com) diagram: nodes where they are drawn (under each name its IPv4 address, else its first global IPv6 one), areas as rectangles behind them, links labeled "port \| speed" |
| Data (JSON) | The topology (each node's IPv6 addresses in `ips6`), areas and layout |

Images and diagrams show the **whole map with every group and area
expanded**, laid out in the orientation and theme you picked, whatever the
screen shows. Your screen is put back as it was afterwards, including pan and
zoom.
