"""The static description of a network: nodes, their ports, links, VLANs, SSIDs.

Everything here is plain data. `acme.py` fills it in (the network of DESIGN.md);
`world.py` turns it into what changes with time (presence, traffic, events).
Vendor emulators read both and answer in their own API's shapes.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any


@dataclass
class Vlan:
    id: int
    name: str
    subnet: str | None  # "10.10.20.0/24"; None for L2-only VLANs
    gateway: str | None = None  # the router's address (a CARP VIP at HQ)
    site: str = "hq"
    routed: bool = True  # has an interface on the site's router
    dhcp: tuple[str, str] | None = None  # pool range
    domain: str = "acme.local"
    purpose: str = ""


@dataclass
class Ssid:
    name: str
    vlan: int
    bands: tuple[str, ...]  # "2.4", "5", "6"
    security: str = "wpa2"  # wpa2 | wpa3 | wpa2-enterprise | open
    guest: bool = False
    site: str = "hq"


@dataclass
class Port:
    """A port or interface of a node.

    kind: ethernet, sfp (a cage), lag, vlan (an L3 VLAN interface), bridge,
    wifi (a radio), vap (an SSID on a radio), ppp, wg, ipsec, loopback, tap,
    veth, virtual.
    """

    name: str
    kind: str = "ethernet"
    speed: int | None = 1000  # Mbps when linked; None when unknown/virtual
    mac: str | None = None
    descr: str = ""
    untagged: int | None = None  # access / native VLAN
    tagged: tuple[int, ...] = ()  # trunk VLANs
    lag: str | None = None  # the LAG this port is a member of
    members: tuple[str, ...] = ()  # lag/bridge members
    vlan: int | None = None  # VLAN id of a VLAN interface
    parent: str | None = None  # VLAN interface's parent
    ips: tuple[str, ...] = ()  # "10.10.10.2/24"
    poe: bool = False
    connector: str | None = None  # rj45 | sfp | qsfp
    index: int = 0  # ifIndex / port number
    band: str | None = None  # radios: "2.4" | "5" | "6"
    admin_up: bool = True
    mtu: int = 1500
    role: str = ""  # lan | wan | mgmt | sync | uplink | trunk ...

    def carries(self, vlan: int) -> bool:
        return self.untagged == vlan or vlan in self.tagged


@dataclass
class Node:
    """Anything with a MAC or a port: network devices, servers, VMs, clients."""

    name: str
    kind: str  # router, firewall, switch, ap, server, hypervisor, vm, lxc, nas, ups, phone, camera, nvr,
    # printer, laptop, desktop, mobile, tablet, tv, media, iot, pos, scanner, modem, controller
    site: str = "hq"
    vendor: str = ""
    model: str = ""
    os: str = ""
    version: str = ""  # OS / firmware version
    serial: str = ""
    mac: str = ""  # the node's own (base) MAC
    ip: str | None = None  # main address
    vlan: int | None = None  # the VLAN of `ip`
    ports: dict[str, Port] = field(default_factory=dict)
    hostname: str = ""  # what it sends in DHCP / answers to DNS
    dhcp: bool = False  # gets `ip` from the site's DHCP (reservation if static_lease)
    static_lease: bool = False
    presence: str = "always"  # always | office:<person> | desktop:<person> | guest:<n> | shift
    traffic: str = "idle"  # a traffic profile (world.PROFILES)
    owner: str = ""  # person
    # How the node reaches the network: (switch-like node, its port), or for
    # Wi-Fi: ("wifi", ssid, home AP). Set by links, see Design.attach.
    api: str | None = None  # the emulated API serving this node (see acme.APIS)
    lldp: bool = False  # sends LLDP (switches, APs, phones, servers with lldpd)
    scan: dict[str, Any] = field(default_factory=dict)  # what the network scan finds
    extra: dict[str, Any] = field(default_factory=dict)  # vendor-specific details
    cores: int = 0
    mem_mb: int = 0
    disk_gb: int = 0
    host: str | None = None  # VMs/containers: the hypervisor node
    vmid: int | None = None
    boot: float = 0.0  # last boot before any scheduled reboot (epoch)

    def port(self, name: str) -> Port:
        return self.ports[name]

    def add(self, *ports: Port) -> Node:
        for p in ports:
            if not p.index:
                p.index = len(self.ports) + 1
            self.ports[p.name] = p
        return self


@dataclass
class Link:
    a: str  # node
    pa: str  # port of a
    b: str
    pb: str
    kind: str = "copper"  # copper | fiber | dac | virtual | wifi
    lldp: bool = True  # both ends talk LLDP when both nodes do


@dataclass
class WifiAttach:
    ssid: str
    home: str  # AP the client normally uses


@dataclass
class Design:
    vlans: dict[tuple[str, int], Vlan] = field(default_factory=dict)  # (site, id)
    ssids: dict[str, Ssid] = field(default_factory=dict)
    nodes: dict[str, Node] = field(default_factory=dict)
    links: list[Link] = field(default_factory=list)
    wifi: dict[str, WifiAttach] = field(default_factory=dict)  # client → SSID + home AP
    people: dict[str, dict[str, Any]] = field(default_factory=dict)
    sites: dict[str, dict[str, Any]] = field(default_factory=dict)

    def node(self, name: str) -> Node:
        return self.nodes[name]

    def add(self, node: Node) -> Node:
        if node.name in self.nodes:
            raise ValueError(f"duplicate node {node.name}")
        self.nodes[node.name] = node
        return node

    def link(self, a: str, pa: str, b: str, pb: str, kind: str = "copper") -> None:
        for n, p in ((a, pa), (b, pb)):
            if p not in self.nodes[n].ports:
                raise ValueError(f"{n} has no port {p}")
        self.links.append(Link(a, pa, b, pb, kind))

    def vlan(self, site: str, vid: int) -> Vlan:
        return self.vlans[(site, vid)]
