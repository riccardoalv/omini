# The map

The map is Omini's home screen. It shows every device, how they are linked,
on which ports and at what speed, the traffic each one carries and what needs
attention.

- [Reading the map](#reading-the-map)
- [Layout](#layout)
- [Nodes](#nodes)
- [Links](#links)
- [The device panel](#the-device-panel)
- [Folding children into bubbles](#folding-children-into-bubbles)
- [Hiding and deleting devices](#hiding-and-deleting-devices)
- [Areas](#areas)
- [VLAN and subnet filter](#vlan-and-subnet-filter)
- [The device list](#the-device-list)
- [Export](#export)

## Reading the map

The bar at the top shows:

- **N devices · N clients**: the network devices (routers, switches, access
  points, servers with an integration, unmanaged gear) and the online clients.
  Click it to open the [device list](#the-device-list).
- **N alerts**: open critical and warning alerts. Click it to open the
  Alerts screen.
- **N problems**: integrations whose last collection failed.
- **Updated ...**: when the map was last built.

The toolbar has **Hide offline**, the [VLAN / subnet filter](#vlan-and-subnet-filter),
**Show hidden (N)**, the orientation toggle, **New area**, **Export**,
**Reset layout** and **Refresh now** (starts a collection round).

Devices that went offline stay on the map, dimmed, for one hour; after that
they leave the map but stay in the device list. **Hide offline** removes them
at once.

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
- **Reset layout** forgets the dragged positions and lays the map out again.
  Areas are kept.
- When you fold or unfold a group, the node you clicked stays still on screen
  and the map makes room around it. The view only refits on first load, on an
  orientation change, on reset and when the sidebar opens or closes.

## Nodes

| Node | What it is |
|---|---|
| Device | A device an integration reads, or a host the network scan found. Its icon is the product logo for homelab software, else the device type with an OS or brand badge |
| WAN | An internet uplink of a router or firewall, drawn as its parent. Several WANs, several parents. Shows the uplink's name, speed (of the physical port, following PPPoE and VLANs down to it), gateway latency and a status dot: green, yellow when a gateway is degraded, red when down. The ISP modem hangs under it |
| Wi-Fi network | A small node between an access point and its clients, one per network and band ("IOT · 2.4 GHz"), its icon colored by band. Click it to fold or unfold its clients. It has no panel |
| App | A self-hosted app found on a device (Jellyfin, Grafana...), one per app, under the machine that runs it |
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

## Links

| Line | Meaning |
|---|---|
| Solid | Confirmed: LLDP/CDP/MNDP neighbors, a switch's MAC table, or a link an integration declared (a VM on its host) |
| Dashed | Inferred (ARP, a MAC seen behind an uplink), or a site-to-site VPN tunnel between two routers |
| Dotted | Wi-Fi |

### Where a device stays

A device stays where Omini last saw it **for sure** until it is seen
somewhere else for sure:

- **For sure:** on an access point's list of Wi-Fi clients (it moves when it
  roams to another), alone on a switch port (a cable), or behind a desk phone.
- **Not for sure:** a MAC seen on a port shared with other devices, on a port
  towards other network gear, or only in a router's ARP table.

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
and an hourly average and peak for a year.

## The device panel

Click a node to open its panel on the side, without leaving the map. Drag its
edge to resize it.

**Overview.** Name (click to rename; the name is kept by Omini, never written
to the device), type, IP and MAC addresses, vendor, model, serial, version,
uptime, the integration that reads it, **Connected to** (its parent and port),
**Runs on** (for a router or firewall running as a VM: the host it runs on,
which is not linked on the map), Wi-Fi network, band, signal and link rate for
Wi-Fi clients, and **Last seen**.

**Health.**

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
- its VLANs (untagged and tagged),
- the SFP module, when reported: vendor, part, type, temperature, voltage,
  bias, Tx and Rx power (Rx in red below the module's alarm threshold, or
  below -20 dBm when it has none),
- **Name this port** ("Uplink to the rack"): kept by Omini, shown instead of
  the device's own description; empty restores it,
- the device connected to it, and the port's traffic chart.

**Extras**, when an integration reports them: services (stopped ones first),
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
- **Connected devices**: its clients.
- **Talks to (last hour)**: its top conversations, with
  [Traffic flows](discovery.md#traffic-flows) enabled.
- Its open **Alerts** and its last **Activity** (joined and left the network).
- **Pin** (a pinned device is never folded into a bubble), **Hide** and
  **Delete**.

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

Pinned devices and servers always stay visible. What is folded is remembered
per browser.

## Hiding and deleting devices

- **Hide** (device panel) takes the device and everything below it off the
  map. It stays in the device list, marked "Hidden". **Show hidden (N)** in
  the toolbar brings hidden devices (and hidden areas) back for a while;
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

When the network has more than one VLAN or subnet, a picker appears in the
toolbar. Pick one to **highlight** it and dim everything else:

- devices with an interface in it,
- links whose port carries it (untagged or tagged),
- clients seen in it in a MAC table,
- and what hangs below a device that is only in that network.

A router with addresses in several networks lights up alone, without
everything below it. Each linked group of highlighted devices gets a
**temporary dashed frame** with the network's name; the frames go away when
you pick **All networks**.

VLAN 1 is listed only when it has a name. VLANs come from the integrations
(OPNsense, MikroTik, UniFi, SNMP switches...).

## The device list

The **N devices · N clients** chip at the top left opens the inventory in a
drawer (`/devices` opens it too). It lists everything ever seen on the
network:

- name, IP, MAC, type, where it is connected, first and last seen,
- a search box and filters: all, online, offline, only randomized MACs,
- rename, pin, hide or unhide,
- select several and **Delete selected** (phones with randomized MACs can
  leave many entries behind).

Click a name to close the drawer and open that device on the map, centered,
its group unfolded.

## Export

**Export** in the toolbar opens a dialog with the format, the theme (dark or
light) and the orientation:

| Format | What you get |
|---|---|
| Image (PNG) | The whole map at twice the screen's pixel ratio, scaled down past 8192 px |
| Vector image (SVG) | The whole map as SVG |
| Diagram (draw.io) | An editable [draw.io](https://www.drawio.com) diagram: nodes where they are drawn, areas as rectangles behind them, links labeled "port \| speed" |
| Data (JSON) | The topology, areas and layout |

Images and diagrams show the **whole map with every group and area
expanded**, laid out in the orientation and theme you picked, whatever the
screen shows. Your screen is put back as it was afterwards, including pan and
zoom.
