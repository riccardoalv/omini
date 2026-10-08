"""Devnet: a large, deliberately messy simulated network for developing Omini.

Every devnet plugin imports this module and returns the part of the network it
"owns" (``devices(plugin_id, now)``). The whole network is defined here, in one
place, as a pure function of the time: the same instant always gives the same
answer, counters grow with time (so live traffic, history and the traffic
badges work) and a few things change on a schedule (a phone roaming between two
access points, a laptop leaving, a gateway degraded, a disk filling, the branch
tunnel dropping...). Nothing here ever touches a real device.

Run it directly to print what every plugin would return::

    uv run --project sdk/python python testdata/devnet/_shared/devnet.py dump [--at EPOCH]
"""

from __future__ import annotations

import argparse
import ipaddress
import json
import math
import sys
import time
import zlib
from collections import defaultdict
from collections.abc import Callable
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Any

BASE = 1_767_225_600  # 2026-01-01T00:00:00Z: counters and boots are counted from here

PLUGINS = (
    "devnet-wifi",
    "devnet-access",
    "devnet-core",
    "devnet-fw",
    "devnet-pve",
    "devnet-lab",
    "devnet-branch",
    "devnet-dnat",
    "devnet-storage",
    "devnet-scan",
)


class Unreachable(Exception):
    """The plugin's devices cannot be reached right now (the plugin reports an error)."""


# --- deterministic helpers -------------------------------------------------------------


def seed(*parts: object) -> int:
    """A stable hash (Python's hash() is randomized per process)."""
    return zlib.crc32("|".join(str(p) for p in parts).encode())


def mac(prefix: str, *parts: object) -> str:
    """A MAC with a real vendor prefix (OUI), the rest derived from parts."""
    h = seed(prefix, *parts)
    return f"{prefix}:{(h >> 16) & 255:02x}:{(h >> 8) & 255:02x}:{h & 255:02x}"


def rmac(*parts: object) -> str:
    """A randomized (locally administered) MAC, like phones use per network."""
    h = seed("random", *parts)
    first = ((h >> 24) & 0xFC) | 0x02  # locally administered, unicast
    rest = [(seed("r2", *parts) >> s) & 255 for s in (0, 8, 16, 24)]
    return ":".join(
        f"{b:02x}" for b in [first, (h >> 16) & 255, (h >> 8) & 255, h & 255, *rest[:2]]
    )


def window(t: float, period: float, start: float, length: float) -> bool:
    """True during [start, start+length) of every period."""
    return 0 <= (t % period) - start < length


def iso(t: float) -> str:
    return datetime.fromtimestamp(t, tz=timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")


def boot(key: str, t: float, period: float = 40 * 86400) -> float:
    """When the device last booted: it reboots every period (offset per device)."""
    return t - ((t - BASE + seed("boot", key) % period) % period)


@dataclass(frozen=True)
class Burst:
    """Extra traffic during a window of every period (e.g. a nightly backup)."""

    period: float
    start: float
    length: float
    bps: float

    def on(self, t: float) -> bool:
        return window(t, self.period, self.start, self.length)

    def seconds(self, t: float) -> float:
        """Seconds the burst was on between BASE and t."""
        x = t - BASE
        return (x // self.period) * self.length + min(
            max(x % self.period - self.start, 0), self.length
        )


def _wave(key: str) -> tuple[float, float, float, float]:
    s = seed("wave", key)
    p1 = 1200 + s % 2400  # 20 to 60 min
    p2 = 90 + (s >> 11) % 270  # 1.5 to 6 min
    f1 = ((s >> 3) % 1000) / 1000 * 2 * math.pi
    f2 = ((s >> 17) % 1000) / 1000 * 2 * math.pi
    return p1, p2, f1, f2


A1, A2 = 0.55, 0.3  # amplitudes of the two sines: the rate never drops below 15 % of the average


def rate(key: str, avg: float, t: float, burst: Burst | None = None) -> int:
    """Bits per second at t: the average with two slow sines on top."""
    p1, p2, f1, f2 = _wave(key)
    r = avg * (
        1 + A1 * math.sin(2 * math.pi * t / p1 + f1) + A2 * math.sin(2 * math.pi * t / p2 + f2)
    )
    if burst and burst.on(t):
        r += burst.bps
    return max(int(r), 0)


def octets(key: str, avg: float, t: float, since: float, burst: Burst | None = None) -> int:
    """Bytes counted between since and t: the integral of rate(), so it always grows."""
    p1, p2, f1, f2 = _wave(key)

    def integral(x: float) -> float:
        return (
            x
            - A1 * p1 / (2 * math.pi) * math.cos(2 * math.pi * x / p1 + f1)
            - A2 * p2 / (2 * math.pi) * math.cos(2 * math.pi * x / p2 + f2)
        )

    total = avg * (integral(t) - integral(since))
    if burst:
        total += burst.bps * (burst.seconds(t) - burst.seconds(since))
    return max(int(total / 8), 0)


def clean(v: Any) -> Any:
    """Drops None values (the models' defaults), recursively."""
    if isinstance(v, dict):
        return {k: clean(x) for k, x in v.items() if x is not None}
    if isinstance(v, list):
        return [clean(x) for x in v]
    return v


# --- addressing --------------------------------------------------------------------------

# VLAN id → (name, subnet, gateway on the firewall)
VLANS: dict[int, tuple[str, str]] = {
    10: ("MGMT", "10.10.10.0/24"),
    20: ("USERS", "10.10.20.0/23"),
    30: ("VOICE", "10.10.30.0/24"),
    40: ("IOT", "10.10.40.0/24"),
    50: ("CAMERAS", "10.10.50.0/26"),
    66: ("GUEST", "172.16.66.0/24"),
    99: ("LAB", "10.99.0.0/24"),
    100: ("SERVERS", "10.100.0.0/24"),
}
LAB_INNER_VLAN, LAB_INNER = 199, "192.168.199.0/24"  # behind the lab router (double NAT)
NAT_LAN = "192.168.0.0/24"  # behind the marketing router (double NAT)
BRANCH_LAN = "10.20.0.0/24"  # the branch office, behind an IPsec tunnel


def ip_at(cidr: str, n: int) -> str:
    return str(ipaddress.ip_network(cidr).network_address + n)


def vip(vlan: int, n: int) -> str:
    return ip_at(VLANS[vlan][1], n)


def gw_cidr(vlan: int) -> str:
    net = ipaddress.ip_network(VLANS[vlan][1])
    return f"{net.network_address + 1}/{net.prefixlen}"


def in_cidr(ip: str, cidr: str) -> bool:
    return ipaddress.ip_address(ip) in ipaddress.ip_network(cidr)


# --- stations: everything with a MAC on the headquarters' wired/Wi-Fi fabric ----------

Loc = tuple  # ("sw", switch, port) | ("core", {core: port}) | ("ap", ap name)


@dataclass
class Station:
    name: str  # DHCP hostname ("" when the device sends none)
    mac: str
    ip: str | None
    vlan: int | None
    loc: Loc
    avg: float = 300_000  # average download, bits/s (upload is a fifth)
    dhcp: bool = True
    managed: bool = False  # reported by a devnet plugin (not a client)
    wifi: dict[str, Any] | None = None  # ssid, band, signal, rate
    scan: dict[str, Any] | None = None  # what the network scanner learns (Host fields)
    present: Callable[[float], bool] | None = None
    burst: Burst | None = None

    def up(self, t: float) -> bool:
        return self.present is None or self.present(t)


CORES = ("core-sw1", "core-sw2")


def both(port1: str, port2: str | None = None) -> Loc:
    """Attached to both core switches (a LAG split across them) or via the ISL."""
    return ("core", {"core-sw1": port1, "core-sw2": port2 or port1.replace("1/", "2/", 1)})


# Access switches: where each hangs (its parent) and its own uplink port.
ACC_PARENT: dict[str, Loc] = {
    "acc-floor1": ("core", {"core-sw1": "Po11", "core-sw2": "Po11"}),
    # Two uplinks, one per core; spanning tree blocks the one to core-sw2,
    # which then learns floor 2 through the inter-switch link.
    "acc-floor2": ("core", {"core-sw1": "Te1/1/4", "core-sw2": "Te2/1/2"}),
    "acc-lab": ("sw", "acc-floor2", "Port 24"),
}
ACC_UPLINK = {"acc-floor1": "Trk1", "acc-floor2": "SFP+ 1", "acc-lab": "Gi1/0/24"}

# Where each access point is plugged (the mesh satellite reaches ap-office2 over the air).
AP_LOC: dict[str, Loc] = {
    "ap-lobby": ("sw", "acc-floor1", "1"),
    "ap-office1": ("sw", "acc-floor1", "2"),
    "ap-office2": ("sw", "acc-floor2", "Port 1"),
    "ap-meeting": ("sw", "acc-floor2", "Port 2"),  # behind a desk switch, with two wired clients
    "ap-warehouse": ("ap", "ap-office2"),
}


def path(loc: Loc) -> list[tuple[str, str]]:
    """(switch, port) pairs a station's frames cross, from its edge port up to the cores."""
    while loc[0] == "ap":
        loc = AP_LOC[loc[1]]
    out: list[tuple[str, str]] = []
    if loc[0] == "sw":
        sw, port = loc[1], loc[2]
        out.append((sw, port))
        parent = ACC_PARENT[sw]
        while parent[0] == "sw":
            out.append((parent[1], parent[2]))
            parent = ACC_PARENT[parent[1]]
        loc = parent
    out += [(c, loc[1][c]) for c in CORES]
    return out


def port_towards(switch: str, loc: Loc) -> str:
    """The port of a switch where a station's MAC is learned."""
    for sw, port in path(loc):
        if sw == switch:
            return port
    return ACC_UPLINK[switch]


# --- the headquarters' clients ------------------------------------------------------------

ROAM_PERIOD = 240  # the roaming phone changes access point every 4 minutes


def away(period: float, start: float, length: float) -> Callable[[float], bool]:
    return lambda t: not window(t, period, start, length)


def roaming_ap(t: float) -> str:
    return "ap-office1" if int(t // ROAM_PERIOD) % 2 == 0 else "ap-office2"


class Alloc:
    """Hands out addresses per VLAN in order, so the network reads naturally."""

    def __init__(self) -> None:
        self.next: dict[int, int] = defaultdict(lambda: 100)

    def __call__(self, vlan: int) -> str:
        n = self.next[vlan]
        self.next[vlan] += 1
        return vip(vlan, n)


SSIDS = {"corp": ("Acme-Corp", 20), "iot": ("Acme-IoT", 40), "guest": ("Acme-Guest", 66)}
BAND_IF = {"2.4ghz": "wifi0", "5ghz": "wifi1", "6ghz": "wifi2"}
RATES = {"2.4ghz": 144.4, "5ghz": 866.7, "6ghz": 1201.0}


def wifi_station(
    alloc: Alloc,
    name: str,
    mac_: str,
    ap: str | Callable[[float], str],
    net: str,
    band: str,
    signal: int,
    avg: float = 2_000_000,
    **kw: Any,
) -> Station:
    ssid, vlan = SSIDS[net]
    st = Station(
        name, mac_, alloc(vlan), vlan, ("ap", ap if isinstance(ap, str) else "?"), avg, **kw
    )
    st.wifi = {"ssid": ssid, "band": band, "signal": signal, "ap": ap}
    return st


def wired(alloc: Alloc, name: str, mac_: str, vlan: int, sw: str, port: str, **kw: Any) -> Station:
    ip = kw.pop("ip", None) or alloc(vlan)
    return Station(name, mac_, ip, vlan, ("sw", sw, port), **kw)


def hq_stations() -> list[Station]:
    """Every station of the headquarters, managed devices included (not filtered by time)."""
    a = Alloc()
    s: list[Station] = []
    f1, f2, lab = "acc-floor1", "acc-floor2", "acc-lab"

    # Floor 1 desks: PCs, three of them behind a desk phone (two MACs on one port).
    pcs = [
        ("DESKTOP-4KQ7T2B", "18:66:da"),
        ("DESKTOP-9QW2LMX", "18:66:da"),
        ("DESKTOP-H7D2PZ3", "f8:bc:12"),
        ("nuc-reception", "a0:36:9f"),
        ("DESKTOP-ERR7C4B", "18:66:da"),  # its cable is bad: errors grow on its port
        ("DESKTOP-8HF2KQ1", "d0:50:99"),
        ("fedora-ws", "e0:d5:5e"),
        ("ubuntu-dev", "2c:fd:a1"),
    ]
    for i, (name, oui) in enumerate(pcs):
        port = str(3 + i)
        s.append(wired(a, name, mac(oui, "pc1", i), 20, f1, port, avg=4_000_000))
        if i < 3:
            s.append(
                wired(a, f"SIP-T54W-{i + 1}", mac("80:5e:c0", "ph1", i), 30, f1, port, avg=90_000)
            )
    # Meeting room A: a desk switch nobody manages, five devices behind it.
    for name, oui, vlan in [
        ("mtg-a-pc", "a0:36:9f", 20),
        ("LG-webOS-TV", "a8:23:fe", 40),
        ("SIP-CP965", "00:15:65", 30),
        ("signage-a", "dc:a6:32", 20),
        ("", "00:0e:c6", 20),  # a USB-C dock: no name
    ]:
        s.append(wired(a, name, mac(oui, "mtgA", name), vlan, f1, "12", avg=1_500_000))
    # The printer has a static address inside the DHCP range: a laptop got it too.
    s.append(
        wired(
            a,
            "",
            mac("a0:d3:c1", "print1"),
            20,
            f1,
            "15",
            ip=vip(20, 90),
            dhcp=False,
            avg=60_000,
            scan={
                "open_ports": [80, 443, 631, 9100],
                "titles": ["HP LaserJet Pro M404dn"],
                "model": "HP LaserJet Pro M404dn",
                "hostnames": ["NPI8C2F1A"],
            },
        )
    )
    s.append(wired(a, "BRW3C2AF4C1D2E0", mac("3c:2a:f4", "print2"), 20, f1, "16", avg=40_000))
    s.append(
        wired(
            a,
            "Philips-hue",
            mac("ec:b5:fa", "hue"),
            40,
            f1,
            "20",
            avg=30_000,
            scan={"titles": ["hue personal wireless lighting"], "services": ["_hue._tcp"]},
        )
    )
    s.append(
        wired(
            a,
            "Sonos-Lobby",
            mac("5c:aa:fd", "sonos1"),
            40,
            f1,
            "21",
            avg=800_000,
            scan={"services": ["_sonos._tcp"], "open_ports": [1400]},
        )
    )

    # Floor 2 desks.
    for i, (name, oui) in enumerate(
        [
            ("DESKTOP-Q1W2E3R", "34:64:a9"),
            ("surface-dock-ana", "c0:33:5e"),
            ("DESKTOP-ZX81SPC", "34:64:a9"),
            ("archlinux", "e0:d5:5e"),
            ("DESKTOP-M0N1T0R", "f8:bc:12"),
        ]
    ):
        s.append(wired(a, name, mac(oui, "pc2", i), 20, f2, f"Port {5 + i}", avg=3_500_000))
    # The boardroom: an access point and two wired devices share a desk switch on Port 2.
    s.append(
        wired(a, "Samsung-QN85B", mac("8c:79:f5", "tv-board"), 40, f2, "Port 2", avg=6_000_000)
    )
    s.append(
        wired(a, "LT-BOARDROOM", mac("00:1a:a0", "dock-board"), 20, f2, "Port 2", avg=2_000_000)
    )
    # Cameras (static addresses, PoE).
    for i in range(6):
        s.append(
            wired(
                a,
                "",
                mac("44:19:b6" if i % 2 else "c0:56:e3", "cam", i),
                50,
                f2,
                f"Port {10 + i}",
                ip=vip(50, 11 + i),
                dhcp=False,
                avg=4_000_000,
                scan={
                    "open_ports": [80, 443, 554, 8000],
                    "titles": ["Hikvision DS-2CD2387G2-LU"],
                    "model": "DS-2CD2387G2-LU",
                },
            )
        )
    s.append(
        wired(
            a,
            "amcrest-dock",
            mac("9c:8e:cd", "cam7"),
            50,
            f2,
            "Port 16",
            ip=vip(50, 17),
            dhcp=False,
            avg=2_500_000,
        )
    )
    # An IP phone that speaks LLDP: nobody manages it (unknown neighbor).
    s.append(
        wired(
            a,
            "SEPF87B20A1B2C3",
            "f8:7b:20:a1:b2:c3",
            30,
            f2,
            "Port 20",
            ip=vip(30, 150),
            avg=90_000,
        )
    )
    s.append(
        wired(
            a,
            "SUN2000-10KTL-M1",
            mac("48:46:fb", "inverter"),
            40,
            f2,
            "Port 31",
            avg=20_000,
            scan={"open_ports": [502, 6607], "model": "SUN2000-10KTL-M1"},
        )
    )
    s.append(wired(a, "EPSON-WF-C5790", mac("38:1a:52", "print3"), 20, f2, "Port 32", avg=40_000))
    s.append(
        wired(
            a,
            "slzb-06",
            mac("24:0a:c4", "zigbee"),
            40,
            f2,
            "Port 41",
            avg=10_000,
            scan={"titles": ["SLZB-06 · ESPHome"], "services": ["_esphomelib._tcp"]},
        )
    )
    s.append(wired(a, "kiosk", mac("e4:5f:01", "kiosk"), 20, f2, "Port 42", avg=500_000))

    # The lab switch: an old NAS in the LAB VLAN, a desk switch that speaks LLDP
    # (nobody manages it) with four devices behind it.
    s.append(wired(a, "old-nas", mac("00:25:90", "oldnas"), 99, lab, "Gi1/0/20", avg=1_000_000))

    # Wi-Fi clients.
    w = lambda *args, **kw: s.append(wifi_station(a, *args, **kw))  # noqa: E731
    for i in range(8):  # visitors in the lobby: randomized MACs
        w(
            f"Galaxy-A5{i}" if i % 3 else "",
            rmac("guest-lobby", i),
            "ap-lobby",
            "guest",
            "5ghz" if i % 2 else "2.4ghz",
            -52 - 3 * i,
            avg=1_200_000,
        )
    w("MacBook-Pro-de-Bruno", mac("f0:18:98", "mbp", 1), "ap-lobby", "corp", "5ghz", -58)
    w("LT-SALES-01", mac("3c:fd:fe", "lt", 1), "ap-lobby", "corp", "5ghz", -63)
    for i in range(2):
        w(
            f"tuya-plug-lobby-{i}",
            mac("d8:1f:12", "plug-lobby", i),
            "ap-lobby",
            "iot",
            "2.4ghz",
            -60 - 4 * i,
            avg=8_000,
        )

    for i in range(3):
        w(
            f"MacBook-Air-{i + 1}",
            mac("a4:83:e7", "mba", i),
            "ap-office1",
            "corp",
            "6ghz",
            -50 - 3 * i,
            avg=5_000_000,
        )
    for i in range(4):
        name = f"LT-SALES-{i + 2:02d}"
        kw: dict[str, Any] = {}
        if i == 1:  # leaves for a quarter of an hour every hour
            kw["present"] = away(3600, 600, 900)
        w(
            name,
            mac("3c:fd:fe", "lt", i + 2),
            "ap-office1",
            "corp",
            "5ghz",
            -55 - 2 * i,
            avg=3_000_000,
            **kw,
        )
    for i in range(2):
        w(
            "iPhone",
            rmac("iphone-office1", i),
            "ap-office1",
            "corp",
            "5ghz",
            -61 - 5 * i,
            avg=1_000_000,
        )
    for i in range(3):
        w(
            f"tuya-plug-{i + 1:02d}",
            mac("1c:90:ff" if i % 2 else "10:5a:17", "plug", i),
            "ap-office1",
            "iot",
            "2.4ghz",
            -66 - 2 * i,
            avg=6_000,
        )
    for i in range(2):
        w(
            f"esp-sensor-office-{i + 1}",
            mac("a4:cf:12", "esp-office", i),
            "ap-office1",
            "iot",
            "2.4ghz",
            -70 - 3 * i,
            avg=4_000,
            scan={"services": ["_esphomelib._tcp"]},
        )
    # This phone roams between ap-office1 and ap-office2 every few minutes.
    w(
        "iPhone-de-Ricardo",
        mac("88:66:5a", "roamer"),
        roaming_ap,
        "corp",
        "5ghz",
        -57,
        avg=1_500_000,
    )

    for i in range(2):
        w(
            f"MacBook-Pro-{i + 1}",
            mac("3c:22:fb", "mbp2", i),
            "ap-office2",
            "corp",
            "6ghz",
            -48 - 4 * i,
            avg=6_000_000,
        )
    for i in range(4):
        w(
            f"LT-ENG-{i + 1:02d}",
            mac("3c:fd:fe", "lteng", i),
            "ap-office2",
            "corp",
            "5ghz",
            -54 - 2 * i,
            avg=4_000_000,
        )
    w("SM-S911B", rmac("galaxy-s23"), "ap-office2", "corp", "5ghz", -59)
    w("Pixel-8", rmac("pixel8"), "ap-office2", "corp", "5ghz", -62)
    w("Echo-Dot", mac("fc:65:de", "echo"), "ap-office2", "iot", "2.4ghz", -64, avg=60_000)
    w(
        "Google-Nest-Hub",
        mac("f4:f5:d8", "nest"),
        "ap-office2",
        "iot",
        "2.4ghz",
        -58,
        avg=300_000,
        scan={"services": ["_googlecast._tcp"]},
    )
    for i in range(2):
        w(
            f"esp-air-quality-{i + 1}",
            mac("84:f3:eb", "esp-aq", i),
            "ap-office2",
            "iot",
            "2.4ghz",
            -67 - 2 * i,
            avg=4_000,
            scan={"services": ["_esphomelib._tcp"]},
        )

    for i in range(3):
        w(
            f"LT-MEET-{i + 1:02d}",
            mac("3c:fd:fe", "ltmeet", i),
            "ap-meeting",
            "corp",
            "5ghz",
            -52 - 3 * i,
            avg=3_000_000,
        )
    for i in range(3):
        w("", rmac("guest-meeting", i), "ap-meeting", "guest", "5ghz", -60 - 4 * i, avg=900_000)

    # The warehouse is far from everything: weak signals (alerts).
    for i in range(4):
        w(
            f"scanner-wh-{i + 1:02d}",
            mac("ec:fa:bc", "scanner", i),
            "ap-warehouse",
            "corp",
            "2.4ghz",
            -77 - 2 * i,
            avg=50_000,
        )
    for i in range(2):
        w(
            f"tuya-plug-wh-{i + 1}",
            mac("50:8a:06", "plug-wh", i),
            "ap-warehouse",
            "iot",
            "2.4ghz",
            -80 - 2 * i,
            avg=5_000,
        )
    w("", rmac("guest-wh"), "ap-warehouse", "guest", "2.4ghz", -83, avg=200_000)
    return s


# --- managed devices of the headquarters (their MACs, addresses and where they hang) ------

FW_MAC = mac("f4:90:ea", "fw", "lagg0")
FW_IX0, FW_IX1 = FW_MAC, mac("f4:90:ea", "fw", "ix1")
FW_IGC = [mac("f4:90:ea", "fw", f"igc{i}") for i in range(4)]

CORE_MAC = {"core-sw1": mac("70:69:5a", "core1"), "core-sw2": mac("70:69:5a", "core2")}
ACC_MAC = {
    "acc-floor1": mac("94:b4:0f", "floor1"),
    "acc-floor2": mac("fc:ec:da", "floor2"),
    "acc-lab": mac("50:c7:bf", "lab"),
}
AP_MAC = {
    "ap-lobby": mac("74:83:c2", "ap-lobby"),
    "ap-office1": mac("e0:63:da", "ap-office1"),
    "ap-office2": mac("78:8a:20", "ap-office2"),
    "ap-meeting": mac("24:5a:4c", "ap-meeting"),
    "ap-warehouse": mac("68:d7:9a", "ap-warehouse"),
}
PVE_MAC = {
    h: (mac("3c:fd:fe", h, "eno1"), mac("3c:fd:fe", h, "eno2")) for h in ("pve1", "pve2", "pve3")
}
NAS_MAC = (mac("00:11:32", "nas", 0), mac("00:11:32", "nas", 1))
BKP_MAC = mac("f8:bc:12", "bkp")
UPS_MAC = mac("00:c0:b7", "ups")
MON_MAC = mac("d8:3a:dd", "mon")
DNAT_WAN_MAC = mac("e8:48:b8", "mkt", "wan")
NETGEAR_MAC = mac("a0:40:a0", "gs108e")
ONT_MAC = mac("28:6f:b9", "ont")
LTE_MAC = mac("48:46:fb", "lte")

PVE_PORT = {"pve1": "Po21", "pve2": "Po22", "pve3": "Po23"}
PVE_IP = {"pve1": vip(10, 31), "pve2": vip(10, 32), "pve3": vip(10, 33)}


@dataclass
class Guest:
    vmid: int
    name: str
    kind: str  # qemu | lxc
    host: str
    nics: list[tuple[str, int, str]]  # (MAC, VLAN, IP)
    cpus: int
    mem_gb: int
    os: str
    avg: float = 2_000_000
    role: str = "server"
    mem_pct: float = 55
    scan: dict[str, Any] | None = None
    present: Callable[[float], bool] | None = None


def gmac(vmid: int, nic: int = 0) -> str:
    return mac("bc:24:11", "vm", vmid, nic)


def guests() -> list[Guest]:
    sv = lambda n: vip(100, n)  # noqa: E731
    docker_web = [
        (3000, "Grafana"),
        (3001, "Uptime Kuma"),
        (8081, "Vaultwarden Web"),
        (8443, "Nextcloud"),
        (8000, "Paperless-ngx"),
        (9443, "Portainer"),
    ]
    return [
        Guest(
            101,
            "dc-01",
            "qemu",
            "pve1",
            [(gmac(101), 100, sv(10))],
            4,
            8,
            "Windows Server 2025",
            scan={"open_ports": [53, 88, 135, 389, 445, 636, 3389], "ttl": 128},
        ),
        Guest(102, "k8s-cp-1", "qemu", "pve1", [(gmac(102), 100, sv(21))], 4, 8, "Talos 1.11"),
        Guest(
            103,
            "pihole",
            "lxc",
            "pve1",
            [(gmac(103), 100, sv(53))],
            1,
            1,
            "Debian 12",
            scan={
                "open_ports": [53, 80],
                "titles": ["Pi-hole"],
                "web": [{"port": 80, "url": f"http://{sv(53)}/admin/", "title": "Pi-hole"}],
            },
        ),
        Guest(
            104,
            "unifi-ctrl",
            "lxc",
            "pve1",
            [(gmac(104), 100, sv(30))],
            2,
            4,
            "Debian 12",
            scan={
                "open_ports": [8080, 8443],
                "titles": ["UniFi Network"],
                "web": [{"port": 8443, "url": f"https://{sv(30)}:8443/", "title": "UniFi Network"}],
            },
        ),
        Guest(
            105,
            "docker-01",
            "qemu",
            "pve1",
            [(gmac(105), 100, sv(40))],
            8,
            32,
            "Ubuntu 24.04",
            avg=20_000_000,
            mem_pct=78,
            scan={
                "open_ports": sorted([22] + [p for p, _ in docker_web]),
                "titles": [title for _, title in docker_web],
                "banners": ["SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.5"],
                "web": [
                    {"port": p, "url": f"http://{sv(40)}:{p}/", "title": title}
                    for p, title in docker_web
                ],
            },
        ),
        Guest(
            106,
            "postgres-01",
            "lxc",
            "pve1",
            [(gmac(106), 100, sv(45))],
            2,
            4,
            "Debian 12",
            scan={"open_ports": [22, 5432]},
        ),
        Guest(
            201,
            "k8s-w-1",
            "qemu",
            "pve2",
            [(gmac(201), 100, sv(22))],
            8,
            16,
            "Talos 1.11",
            avg=8_000_000,
        ),
        Guest(
            202,
            "k8s-w-2",
            "qemu",
            "pve2",
            [(gmac(202), 100, sv(23))],
            8,
            16,
            "Talos 1.11",
            avg=8_000_000,
        ),
        Guest(
            203,
            "frigate",
            "qemu",
            "pve2",
            [(gmac(203), 100, sv(50)), (gmac(203, 1), 50, vip(50, 40))],
            4,
            8,
            "Debian 12",
            avg=30_000_000,
            scan={
                "open_ports": [5000, 8554],
                "titles": ["Frigate"],
                "web": [{"port": 5000, "url": f"http://{sv(50)}:5000/", "title": "Frigate"}],
            },
        ),
        Guest(
            204,
            "homeassistant",
            "qemu",
            "pve2",
            [(gmac(204), 100, sv(60)), (gmac(204, 1), 40, vip(40, 10))],
            2,
            4,
            "Home Assistant OS 16.2",
            scan={
                "open_ports": [8123],
                "titles": ["Home Assistant"],
                "services": ["_home-assistant._tcp"],
            },
        ),
        # Stopped for ten minutes every two hours (it leaves and comes back).
        Guest(
            205,
            "win11-vdi",
            "qemu",
            "pve2",
            [(gmac(205), 20, vip(20, 60))],
            4,
            16,
            "Windows 11",
            mem_pct=94,
            present=away(7200, 2700, 600),
            scan={"open_ports": [3389], "ttl": 128},
        ),
        Guest(
            206,
            "minio",
            "lxc",
            "pve2",
            [(gmac(206), 100, sv(80))],
            2,
            4,
            "Debian 12",
            scan={"open_ports": [9000, 9001], "titles": ["MinIO Console"]},
        ),
        Guest(
            301,
            "lab-router",
            "qemu",
            "pve3",
            [(gmac(301), 99, vip(99, 2)), (gmac(301, 1), LAB_INNER_VLAN, ip_at(LAB_INNER, 1))],
            2,
            4,
            "OPNsense 26.1",
            role="router",
        ),
        Guest(
            302,
            "k8s-w-3",
            "qemu",
            "pve3",
            [(gmac(302), 100, sv(24))],
            8,
            16,
            "Talos 1.11",
            avg=8_000_000,
        ),
        Guest(
            303,
            "truenas-scale",
            "qemu",
            "pve3",
            [(gmac(303), 100, sv(90))],
            4,
            32,
            "TrueNAS SCALE 25.04",
            avg=15_000_000,
            scan={"open_ports": [80, 443, 445, 2049], "titles": ["TrueNAS"]},
        ),
        Guest(304, "gitlab-runner", "lxc", "pve3", [(gmac(304), 100, sv(95))], 4, 8, "Debian 12"),
        Guest(
            305,
            "lab-web-01",
            "lxc",
            "pve3",
            [(gmac(305), LAB_INNER_VLAN, ip_at(LAB_INNER, 20))],
            1,
            1,
            "Alpine 3.22",
        ),
        Guest(
            306,
            "lab-db-01",
            "lxc",
            "pve3",
            [(gmac(306), LAB_INNER_VLAN, ip_at(LAB_INNER, 21))],
            1,
            2,
            "Debian 12",
        ),
    ]


def lab_stations() -> list[Station]:
    """Behind the lab router (VLAN 199, 192.168.199.0/24, its own DHCP): on the lab switch."""
    s: list[Station] = []
    for i in range(4):
        s.append(
            Station(
                f"lab-pi-{i + 1:02d}",
                mac("2c:cf:67" if i % 2 else "d8:3a:dd", "labpi", i),
                ip_at(LAB_INNER, 100 + i),
                LAB_INNER_VLAN,
                ("sw", "acc-lab", f"Gi1/0/{i + 1}"),
                avg=400_000,
            )
        )
    for i in range(2):
        s.append(
            Station(
                f"esp-dev-{i + 1:02d}",
                mac("24:0a:c4", "espdev", i),
                ip_at(LAB_INNER, 110 + i),
                LAB_INNER_VLAN,
                ("sw", "acc-lab", f"Gi1/0/{5 + i}"),
                avg=5_000,
            )
        )
    # Behind the desk switch on Gi1/0/8 (it speaks LLDP; nobody manages it).
    for i, (name, oui) in enumerate(
        [
            ("lab-nuc-01", "a0:36:9f"),
            ("jetson-orin", "48:b0:2d"),
            ("rigol-ds1104", "00:19:af"),
            ("", "08:00:27"),
        ]
    ):
        s.append(
            Station(
                name,
                mac(oui, "behind-gs108e", i),
                ip_at(LAB_INNER, 120 + i),
                LAB_INNER_VLAN,
                ("sw", "acc-lab", "Gi1/0/8"),
                avg=700_000,
            )
        )
    return s


def infra_stations() -> list[Station]:
    """Managed devices and LLDP-only neighbors, as stations (their MACs are in every table)."""
    m = lambda *a, **kw: Station(*a, managed=True, dhcp=False, **kw)  # noqa: E731
    s = [
        m(
            "fw-edge",
            FW_MAC,
            vip(10, 1),
            10,
            ("core", {"core-sw1": "Po1", "core-sw2": "Po1"}),
            avg=0,
        ),
        m(
            "core-sw1",
            CORE_MAC["core-sw1"],
            vip(10, 2),
            10,
            ("core", {"core-sw1": "CPU", "core-sw2": "Te2/1/2"}),
        ),
        m(
            "core-sw2",
            CORE_MAC["core-sw2"],
            vip(10, 3),
            10,
            ("core", {"core-sw1": "Te1/1/2", "core-sw2": "CPU"}),
        ),
        m("acc-floor1", ACC_MAC["acc-floor1"], vip(10, 11), 10, ("sw", "acc-floor1", "CPU")),
        m("acc-floor2", ACC_MAC["acc-floor2"], vip(10, 12), 10, ("sw", "acc-floor2", "CPU")),
        m("acc-lab", ACC_MAC["acc-lab"], vip(10, 13), 10, ("sw", "acc-lab", "CPU")),
        m("nas-01", NAS_MAC[0], vip(100, 20), 100, both("Po30", "Po30"), avg=40_000_000),
        m(
            "bkp-01",
            BKP_MAC,
            vip(100, 26),
            100,
            both("Te1/0/7", "Te2/1/2"),
            avg=5_000_000,
            burst=Burst(3600, 0, 900, 880_000_000),
        ),
        m("ups-01", UPS_MAC, vip(10, 40), 10, ("sw", "acc-floor2", "Port 30"), avg=10_000),
        m("mon-01", MON_MAC, vip(10, 50), 10, both("Te1/0/8", "Te2/1/2"), avg=600_000),
        m(
            "mkt-router",
            DNAT_WAN_MAC,
            vip(20, 250),
            20,
            ("sw", "acc-floor2", "Port 40"),
            avg=12_000_000,
        ),
        m("lab-gs108e", NETGEAR_MAC, None, LAB_INNER_VLAN, ("sw", "acc-lab", "Gi1/0/8"), avg=0),
    ]
    for i, ap in enumerate(AP_MAC):
        s.append(m(ap, AP_MAC[ap], vip(10, 21 + i), 10, AP_LOC[ap], avg=0))
    for h, (m1, _) in PVE_MAC.items():
        s.append(m(h, m1, PVE_IP[h], 10, both(PVE_PORT[h], PVE_PORT[h]), avg=1_000_000))
    for g in guests():
        for n, (gm, vlan, ip) in enumerate(g.nics):
            st = m(
                g.name,
                gm,
                ip,
                vlan,
                both(PVE_PORT[g.host], PVE_PORT[g.host]),
                avg=g.avg if n == 0 else 0,
                present=g.present,
            )
            s.append(st)
    return s


def present_stations(t: float) -> list[Station]:
    return [st for st in hq_stations() + lab_stations() + infra_stations() if st.up(t)]


def station_loc(st: Station, t: float) -> Loc:
    if st.wifi and callable(st.wifi["ap"]):
        return ("ap", st.wifi["ap"](t))
    return st.loc


# --- traffic over the fabric -------------------------------------------------------------


def fabric_load(t: float) -> dict[tuple[str, str], float]:
    """Average download (bits/s) through each (switch, port), from the stations behind it."""
    load: dict[tuple[str, str], float] = defaultdict(float)
    for st in present_stations(t):
        avg = st.avg + (st.burst.bps if st.burst and st.burst.on(t) else 0)
        for sw, port in path(station_loc(st, t)):
            share = avg / 2 if sw in CORES else avg  # each core carries about half
            load[(sw, port)] += share
            if sw in ACC_UPLINK:
                load[(sw, ACC_UPLINK[sw])] += share
    return load


def iface(
    name: str,
    key: str,
    t: float,
    since: float,
    down: float = 0,
    upload: float | None = None,
    burst: Burst | None = None,
    **kw: Any,
) -> dict[str, Any]:
    """An interface with counters: down is what flows out of it to the far side (tx)."""
    d: dict[str, Any] = {"name": name, **kw}
    if d.get("up", True) and (down or upload):
        upv = down / 5 if upload is None else upload
        d["tx_bytes"] = octets(key + "/tx", down, t, since, burst)
        d["rx_bytes"] = octets(key + "/rx", upv, t, since)
    return d


def sfp(
    vendor: str, part: str, typ: str, key: str, t: float, rx: float | None = None, nm: float = 850
) -> dict:
    s = seed("sfp", key)
    return {
        "vendor": vendor,
        "part": part,
        "serial": f"F{s % 10**8:08d}",
        "type": typ,
        "wavelength_nm": nm if typ != "DAC" else None,
        "temperature_c": round(38 + (s % 90) / 10 + 2 * math.sin(t / 900), 1),
        "voltage_v": 3.29,
        "bias_ma": None if typ == "DAC" else round(6 + (s % 30) / 10, 2),
        "tx_power_dbm": None if typ == "DAC" else round(-2.1 - (s % 20) / 10, 2),
        "rx_power_dbm": None
        if typ == "DAC"
        else (rx if rx is not None else round(-3.5 - (s % 40) / 10, 2)),
        "rx_power_low_dbm": None if typ == "DAC" else -20.0,
    }


# --- devnet-fw: the edge firewall (OPNsense-like) -----------------------------------------

FIBER_DOWN = (14400, 6000, 180)  # down 3 minutes every 4 hours
FIBER_DEGRADED = (3600, 2400, 300)  # packet loss 5 minutes every hour
LTE_DEGRADED = (3600, 1200, 600)  # degraded 10 minutes every hour
IPSEC_DOWN = (1800, 1500, 180)  # the branch tunnel drops 3 minutes every 30 minutes


def fw_device(t: float) -> dict[str, Any]:
    since = boot("fw-edge", t)
    stations = present_stations(t)
    fiber_down = window(t, *FIBER_DOWN)
    wan_avg = 0.0 if fiber_down else 180_000_000
    lte_avg = 40_000_000 if fiber_down else 30_000
    lan_load = sum(st.avg for st in stations if not st.managed) + 60_000_000

    vlan_ifaces = []
    for v, (name, _cidr) in VLANS.items():
        ips = [gw_cidr(v)]
        if v in (20, 100, 10):
            ips.append(f"2001:db8:{v:x}::1/64")
        vlan_ifaces.append(
            iface(
                f"vlan0.{v}",
                f"fw/vlan{v}",
                t,
                since,
                down=sum(st.avg for st in stations if st.vlan == v) or 50_000,
                type="vlan",
                description=name,
                parent="lagg0",
                vlan=v,
                mac=FW_MAC,
                ips=ips,
                up=True,
                speed_mbps=20000,
            )
        )
    interfaces = [
        iface(
            "igc0",
            "fw/igc0",
            t,
            since,
            down=wan_avg / 5,
            upload=wan_avg,
            description="Fiber ONT",
            type="ethernet",
            connector="rj45",
            mac=FW_IGC[0],
            up=True,
            speed_mbps=1000,
            media="1000baseT <full-duplex>",
            duplex="full",
        ),
        iface(
            "pppoe0",
            "fw/pppoe0",
            t,
            since,
            down=wan_avg / 5,
            upload=wan_avg,
            type="other",
            description="Fiber (PPPoE)",
            parent="igc0",
            wan=True,
            up=not fiber_down,
            ips=["203.0.113.45/32", "2001:db8:ffff:45::1/64"],
            speed_mbps=1000,
        ),
        iface(
            "igc1",
            "fw/igc1",
            t,
            since,
            down=lte_avg / 4,
            upload=lte_avg,
            description="LTE backup",
            type="ethernet",
            connector="rj45",
            mac=FW_IGC[1],
            wan=True,
            up=True,
            ips=["192.168.8.100/24"],
            speed_mbps=1000,
            media="1000baseT <full-duplex>",
        ),
        {"name": "igc2", "type": "ethernet", "connector": "rj45", "mac": FW_IGC[2], "up": False},
        {"name": "igc3", "type": "ethernet", "connector": "rj45", "mac": FW_IGC[3], "up": False},
        iface(
            "ix0",
            "fw/ix0",
            t,
            since,
            down=lan_load / 2,
            type="ethernet",
            connector="sfp",
            mac=FW_IX0,
            up=True,
            speed_mbps=10000,
            media="10Gbase-SR <full-duplex>",
            transceiver=sfp("FS", "SFP-10GSR-85", "10GBASE-SR", "fw/ix0", t),
        ),
        iface(
            "ix1",
            "fw/ix1",
            t,
            since,
            down=lan_load / 2,
            type="ethernet",
            connector="sfp",
            mac=FW_IX1,
            up=True,
            speed_mbps=10000,
            media="10Gbase-SR <full-duplex>",
            transceiver=sfp("FS", "SFP-10GSR-85", "10GBASE-SR", "fw/ix1", t),
        ),
        iface(
            "lagg0",
            "fw/lagg0",
            t,
            since,
            down=lan_load,
            type="lag",
            members=["ix0", "ix1"],
            mac=FW_MAC,
            up=True,
            speed_mbps=20000,
            description="Core (LACP)",
        ),
        *vlan_ifaces,
        iface(
            "wg0",
            "fw/wg0",
            t,
            since,
            down=2_000_000,
            type="tunnel",
            description="Road warriors",
            ips=["10.200.0.1/24"],
            up=True,
        ),
        iface(
            "ipsec1",
            "fw/ipsec1",
            t,
            since,
            down=0 if window(t, *IPSEC_DOWN) else 6_000_000,
            type="tunnel",
            description="Branch (IPsec VTI)",
            ips=["10.255.0.1/30"],
            up=not window(t, *IPSEC_DOWN),
        ),
    ]

    arp = [
        {"ip": "192.168.100.1", "mac": ONT_MAC, "interface": "igc0"},
        {"ip": "192.168.8.1", "mac": LTE_MAC, "interface": "igc1"},
    ]
    leases = []
    seen_ips: set[str] = set()
    for st in stations:
        if not st.ip or st.vlan not in VLANS or st.ip in seen_ips:
            continue
        seen_ips.add(st.ip)
        arp.append({"ip": st.ip, "mac": st.mac, "interface": f"vlan0.{st.vlan}"})
        if st.dhcp and not st.managed:
            leases.append(
                {
                    "ip": st.ip,
                    "mac": st.mac,
                    "hostname": st.name or None,
                    "expires_at": iso(t + 3600 + seed("lease", st.mac) % 3600),
                }
            )
    # The laptop that took the printer's address (a duplicate IP).
    dup = mac("3c:fd:fe", "lt-dup")
    arp.append({"ip": vip(20, 90), "mac": dup, "interface": "vlan0.20"})
    leases.append(
        {"ip": vip(20, 90), "mac": dup, "hostname": "LT-TEMP-07", "expires_at": iso(t + 1800)}
    )
    # Leases outlive devices: the laptop that left still has one (it does not make it present).
    for st in hq_stations():
        if st.ip and st.dhcp and not st.up(t):
            leases.append(
                {
                    "ip": st.ip,
                    "mac": st.mac,
                    "hostname": st.name or None,
                    "expires_at": iso(t + 600),
                }
            )

    gateways = []
    if fiber_down:
        gateways.append(
            {
                "name": "FIBER_PPPOE",
                "interface": "pppoe0",
                "address": "203.0.113.1",
                "status": "down",
                "loss_pct": 100.0,
            }
        )
    elif window(t, *FIBER_DEGRADED):
        gateways.append(
            {
                "name": "FIBER_PPPOE",
                "interface": "pppoe0",
                "address": "203.0.113.1",
                "status": "degraded",
                "rtt_ms": 38.5,
                "loss_pct": 7.0,
            }
        )
    else:
        gateways.append(
            {
                "name": "FIBER_PPPOE",
                "interface": "pppoe0",
                "address": "203.0.113.1",
                "status": "up",
                "rtt_ms": round(4.2 + math.sin(t / 300), 1),
                "loss_pct": 0.0,
            }
        )
    lte_bad = window(t, *LTE_DEGRADED)
    gateways.append(
        {
            "name": "LTE_DHCP",
            "interface": "igc1",
            "address": "192.168.8.1",
            "status": "degraded" if lte_bad else "up",
            "rtt_ms": 185.0 if lte_bad else 42.0,
            "loss_pct": 12.0 if lte_bad else 0.0,
        }
    )
    gateways.append({"name": "WG_ROADWARRIOR", "interface": "wg0", "status": "unknown"})

    ipsec_up = not window(t, *IPSEC_DOWN)
    peers = []
    for i, who in enumerate(
        ["ana-laptop", "bruno-phone", "carla-macbook", "diego-ipad", "erik-laptop"]
    ):
        connected = (int(t // 900) + i) % 3 != 0
        peers.append(
            {
                "name": who,
                "protocol": "wireguard",
                "endpoint": f"198.51.100.{20 + i}:{51820 + i}",
                "address": f"10.200.0.{10 + i}/32",
                "connected": connected,
                "last_handshake": iso(t - (40 + 13 * i if connected else 3600 + 600 * i)),
                "rx_bytes": octets(f"wg/{who}/rx", 300_000, t, since),
                "tx_bytes": octets(f"wg/{who}/tx", 900_000, t, since),
            }
        )
    peers.append(
        {
            "name": "branch-office",
            "protocol": "ipsec",
            "endpoint": "203.0.113.200",
            "address": BRANCH_LAN,
            "connected": ipsec_up,
            "rx_bytes": octets("ipsec/rx", 3_000_000, t, since),
            "tx_bytes": octets("ipsec/tx", 6_000_000, t, since),
        }
    )
    peers.append(
        {
            "name": "legacy-vendor",
            "protocol": "openvpn",
            "endpoint": "192.0.2.77:1194",
            "connected": False,
        }
    )

    used: dict[int, int] = defaultdict(int)
    for lease in leases:
        for v, (_n, cidr) in VLANS.items():
            if in_cidr(lease["ip"], cidr):
                used[v] += 1
    pool_size = {10: 50, 20: 300, 30: 100, 40: 100, 50: 20, 66: used[66] + 1, 99: 100, 100: 50}
    pools = [
        {"network": VLANS[v][0], "total": pool_size[v], "used": min(used[v], pool_size[v])}
        for v in VLANS
    ]

    temp = 63 + 9 * math.sin(2 * math.pi * t / 2700)
    return {
        "key": FW_MAC,
        "name": "fw-edge",
        "host": vip(10, 1),
        "role": "firewall",
        "vendor": "Deciso",
        "model": "DEC2752",
        "os_version": "OPNsense 26.1.4",
        "serial": "DEC2752-0042",
        "uptime_s": int(t - since),
        "cpu_pct": round(18 + 12 * math.sin(t / 400) ** 2, 1),
        "mem_pct": 41.5,
        "cpu_count": 8,
        "mem_total_bytes": 32 * 2**30,
        "mem_used_bytes": int(13.3 * 2**30),
        "swap_pct": 0.0,
        "load_avg": [0.62, 0.71, 0.66],
        "temperatures": [
            {"sensor": f"CPU {i}", "kind": "cpu", "celsius": round(temp + i * 0.7, 1)}
            for i in range(4)
        ]
        + [
            {"sensor": "nda0", "kind": "disk", "celsius": 41.0},
            {"sensor": "board", "kind": "board", "celsius": 38.0},
        ],
        "storage": [
            {
                "mount": "/",
                "device": "zroot/ROOT/default",
                "fs_type": "zfs",
                "total_bytes": 220 * 10**9,
                "used_bytes": 18 * 10**9,
            }
        ],
        "firmware": {
            "current": "26.1.4",
            "latest": "26.1.6",
            "update_available": True,
            "updates": 12,
            "needs_reboot": True,
            "checked_at": iso(t - 3 * 3600),
        },
        "macs": [FW_MAC, FW_IX1, *FW_IGC],
        "ips": [vip(10, 1), "203.0.113.45", "2001:db8:a::1"],
        "interfaces": interfaces,
        "arp": arp,
        "dhcp_leases": leases,
        "gateways": gateways,
        "vlans": [
            {"id": v, "name": n, "interface": f"vlan0.{v}", "subnet": c}
            for v, (n, c) in VLANS.items()
        ],
        "services": [
            {
                "name": "configd",
                "description": "System Configuration Daemon",
                "running": True,
                "enabled": True,
            },
            {"name": "unbound", "description": "Unbound DNS", "running": True, "enabled": True},
            {
                "name": "kea-dhcp4",
                "description": "Kea DHCPv4 server",
                "running": True,
                "enabled": True,
            },
            {"name": "wireguard", "description": "WireGuard", "running": True, "enabled": True},
            {
                "name": "strongswan",
                "description": "IPsec VPN",
                "running": ipsec_up,
                "enabled": True,
            },
            {
                "name": "ntpd",
                "description": "Network Time Daemon",
                "running": True,
                "enabled": True,
            },
            {
                "name": "ddclient",
                "description": "Dynamic DNS client",
                "running": False,
                "enabled": True,
            },
            {
                "name": "suricata",
                "description": "Intrusion Detection",
                "running": False,
                "enabled": False,
            },
        ],
        "vpn_peers": peers,
        "dhcp_pools": pools,
        "firewall_states": {
            "current": 38_000 + int(30_000 * (1 + math.sin(t / 700))),
            "limit": 100_000,
        },
    }


# --- devnet-core: the core switch pair (MLAG) ---------------------------------------------

# port → (LAG it belongs to, neighbor name, neighbor MAC, neighbor port, speed, kind)
CORE_LINKS = {
    "core-sw1": {
        "Te1/1/1": ("Po1", "fw-edge", FW_MAC, "ix0", 10000, "sfp"),
        "Te1/1/2": (None, "core-sw2", CORE_MAC["core-sw2"], "Te2/1/2", 10000, "dac"),
        "Te1/1/3": ("Po11", "acc-floor1", ACC_MAC["acc-floor1"], "49", 10000, "lr"),
        "Te1/1/4": (None, "acc-floor2", ACC_MAC["acc-floor2"], "SFP+ 1", 10000, "sfp"),
        "Te1/0/1": ("Po21", "pve1", PVE_MAC["pve1"][0], "eno1", 10000, None),
        "Te1/0/2": ("Po22", "pve2", PVE_MAC["pve2"][0], "eno1", 10000, None),
        "Te1/0/3": ("Po23", "pve3", PVE_MAC["pve3"][0], "eno1", 10000, None),
        "Te1/0/4": ("Po30", "nas-01", NAS_MAC[0], "eth4", 10000, None),
        "Te1/0/7": (None, "bkp-01", BKP_MAC, "eno1", 1000, None),
        "Te1/0/8": (None, "mon-01", MON_MAC, "eth0", 1000, None),
    },
    "core-sw2": {
        "Te2/1/1": ("Po1", "fw-edge", FW_MAC, "ix1", 10000, "sfp"),
        "Te2/1/2": (None, "core-sw1", CORE_MAC["core-sw1"], "Te1/1/2", 10000, "dac"),
        "Te2/1/3": ("Po11", "acc-floor1", ACC_MAC["acc-floor1"], "50", 10000, "lr-dirty"),
        "Te2/1/4": (None, "acc-floor2", ACC_MAC["acc-floor2"], "SFP+ 2", 10000, "sfp"),
        "Te2/0/1": ("Po21", "pve1", PVE_MAC["pve1"][0], "eno2", 10000, None),
        "Te2/0/2": ("Po22", "pve2", PVE_MAC["pve2"][0], "eno2", 10000, None),
        "Te2/0/3": ("Po23", "pve3", PVE_MAC["pve3"][0], "eno2", 10000, None),
        "Te2/0/4": ("Po30", "nas-01", NAS_MAC[0], "eth5", 10000, None),
    },
}
CORE_LLDP_SPEAKERS = {
    "fw-edge",
    "core-sw1",
    "core-sw2",
    "acc-floor1",
    "acc-floor2",
    "pve1",
    "pve2",
    "pve3",
    "nas-01",
    "bkp-01",
    "mon-01",
}
TRUNK_ALL = sorted(set(VLANS) | {LAB_INNER_VLAN})


def switch_fdb(switch: str, t: float, own: set[str]) -> list[dict[str, Any]]:
    out = []
    for st in present_stations(t):
        if st.mac in own:
            continue
        port = port_towards(switch, station_loc(st, t))
        if port == "CPU":
            continue
        out.append({"mac": st.mac, "port": port, "vlan": st.vlan or 1})
    return out


def core_device(name: str, t: float) -> dict[str, Any]:
    since = boot(name, t, 120 * 86400)
    load = fabric_load(t)
    unit = "1" if name == "core-sw1" else "2"
    links = CORE_LINKS[name]
    lags: dict[str, list[str]] = defaultdict(list)
    interfaces = []
    neighbors = []
    for port, link in links.items():
        if link[0]:
            lags[link[0]].append(port)
    for slot, count, connector in (("0", 24, "rj45"), ("1", 8, "sfp")):
        for n in range(1, count + 1):
            port = f"Te{unit}/{slot}/{n}"
            link = links.get(port)
            if not link:
                interfaces.append(
                    {
                        "name": port,
                        "type": "ethernet",
                        "connector": connector,
                        "up": False,
                        "vlans": {"untagged": 1},
                    }
                )
                continue
            lag, peer, pmac, pport, speed, kind = link
            down = load.get((name, lag or port), 0) or 200_000
            if port.endswith("1/2"):  # the ISL carries what the other core cannot reach directly
                down = load.get((name, port), 0) + 500_000_000
            d = iface(
                port,
                f"{name}/{port}",
                t,
                since,
                down=down,
                type="ethernet",
                connector="sfp" if kind else connector,
                up=True,
                speed_mbps=speed,
                duplex="full",
                description=f"{peer} {pport}",
                vlans={"untagged": 1, "tagged": TRUNK_ALL},
            )
            if kind:
                typ = {
                    "sfp": ("10GBASE-SR", "SFP-10GSR-85", 850.0),
                    "dac": ("DAC", "SFP-H10GB-CU1M", 0.0),
                    "lr": ("10GBASE-LR", "SFP-10GLR-31", 1310.0),
                    "lr-dirty": ("10GBASE-LR", "SFP-10GLR-31", 1310.0),
                }
                tt, part, nm = typ[kind]
                rx = (
                    -21.4 + 0.3 * math.sin(t / 600) if kind == "lr-dirty" else None
                )  # a dirty fiber
                d["transceiver"] = clean(
                    sfp("Cisco" if kind == "dac" else "FS", part, tt, f"{name}/{port}", t, rx, nm)
                )
            if port in ("Te1/0/7",):
                d["vlans"] = {"untagged": 100}
            if port in ("Te1/0/8",):
                d["vlans"] = {"untagged": 10}
            interfaces.append(d)
            if peer in CORE_LLDP_SPEAKERS:
                neighbors.append(
                    {
                        "local_port": port,
                        "protocol": "lldp",
                        "remote_name": peer,
                        "remote_mac": pmac,
                        "remote_port": pport,
                    }
                )
    for lag, members in lags.items():
        interfaces.append(
            iface(
                lag,
                f"{name}/{lag}",
                t,
                since,
                down=load.get((name, lag), 0) or 100_000,
                type="lag",
                members=members,
                up=True,
                speed_mbps=10000 * len(members),
                vlans={"untagged": 1, "tagged": TRUNK_ALL},
            )
        )
    interfaces.append(
        {
            "name": "Vlan10",
            "type": "vlan",
            "vlan": 10,
            "mac": CORE_MAC[name],
            "up": True,
            "ips": [f"{vip(10, 2 if unit == '1' else 3)}/24"],
            "description": "MGMT",
        }
    )
    own = {CORE_MAC[name]}
    return {
        "key": CORE_MAC[name],
        "name": name,
        "host": vip(10, 2 if unit == "1" else 3),
        "role": "switch",
        "vendor": "Cisco",
        "model": "C9300-24UX",
        "os_version": "IOS-XE 17.12.4",
        "serial": f"FOC27{unit}1X0AB",
        "uptime_s": int(t - since),
        "cpu_pct": round(9 + 4 * math.sin(t / 333 + int(unit)), 1),
        "mem_pct": 47.0,
        "temperatures": [
            {"sensor": "Inlet", "kind": "board", "celsius": 27.0},
            {"sensor": "Hotspot", "kind": "board", "celsius": 49.0},
        ],
        "firmware": {
            "current": "17.12.4",
            "latest": "17.12.4",
            "update_available": False,
            "checked_at": iso(t - 86400),
        },
        "macs": [CORE_MAC[name]],
        "ips": [vip(10, 2 if unit == "1" else 3)],
        "interfaces": interfaces,
        "neighbors": neighbors,
        "fdb": switch_fdb(name, t, own),
        "vlans": [{"id": v, "name": n} for v, (n, _c) in VLANS.items()]
        + [{"id": LAB_INNER_VLAN, "name": "LAB-INNER"}],
    }


# --- devnet-access: three access switches --------------------------------------------------

ACC_INFO = {
    "acc-floor1": (
        "Aruba",
        "2930F-48G-PoE+-4SFP+",
        "WC.16.11.0021",
        [str(n) for n in range(1, 53)],
        49,
    ),
    "acc-floor2": (
        "Ubiquiti",
        "USW-Pro-48-PoE",
        "7.1.26",
        [f"Port {n}" for n in range(1, 49)] + [f"SFP+ {n}" for n in range(1, 5)],
        49,
    ),
    "acc-lab": (
        "TP-Link",
        "TL-SG3428",
        "1.2.4 Build 20240821",
        [f"Gi1/0/{n}" for n in range(1, 29)],
        25,
    ),
}
# (switch, port) → (neighbor name, MAC, its port, link speed, protocol, platform)
ACC_LINKS: dict[tuple[str, str], tuple[str, str, str, int, str, str | None]] = {
    ("acc-floor1", "49"): (
        "core-sw1",
        CORE_MAC["core-sw1"],
        "Te1/1/3",
        10000,
        "lldp",
        "Cisco IOS-XE",
    ),
    ("acc-floor1", "50"): (
        "core-sw2",
        CORE_MAC["core-sw2"],
        "Te2/1/3",
        10000,
        "lldp",
        "Cisco IOS-XE",
    ),
    ("acc-floor1", "1"): ("ap-lobby", AP_MAC["ap-lobby"], "eth0", 2500, "lldp", "U6-Pro"),
    ("acc-floor1", "2"): (
        "ap-office1",
        AP_MAC["ap-office1"],
        "eth0",
        2500,
        "lldp",
        "U6-Enterprise",
    ),
    ("acc-floor2", "SFP+ 1"): (
        "core-sw1",
        CORE_MAC["core-sw1"],
        "Te1/1/4",
        10000,
        "lldp",
        "Cisco IOS-XE",
    ),
    ("acc-floor2", "SFP+ 2"): (
        "core-sw2",
        CORE_MAC["core-sw2"],
        "Te2/1/4",
        10000,
        "lldp",
        "Cisco IOS-XE",
    ),
    ("acc-floor2", "Port 1"): ("ap-office2", AP_MAC["ap-office2"], "eth0", 2500, "lldp", "U7-Pro"),
    ("acc-floor2", "Port 2"): ("ap-meeting", AP_MAC["ap-meeting"], "eth0", 1000, "lldp", "U6-Lite"),
    ("acc-floor2", "Port 20"): (
        "SEPF87B20A1B2C3",
        "f8:7b:20:a1:b2:c3",
        "Port 1",
        100,
        "lldp",
        "Cisco IP Phone 8845",
    ),
    ("acc-floor2", "Port 24"): (
        "acc-lab",
        ACC_MAC["acc-lab"],
        "Gi1/0/24",
        100,
        "lldp",
        "TL-SG3428",
    ),
    ("acc-lab", "Gi1/0/24"): (
        "acc-floor2",
        ACC_MAC["acc-floor2"],
        "Port 24",
        100,
        "lldp",
        "USW-Pro-48-PoE",
    ),
    ("acc-lab", "Gi1/0/8"): ("lab-gs108e", NETGEAR_MAC, "g1", 1000, "lldp", "Netgear GS108Ev3"),
}
ERROR_PORT = ("acc-floor1", "7")  # a bad cable: errors grow on every poll


def access_device(name: str, t: float) -> dict[str, Any]:
    reboot = 6 * 3600 if name == "acc-lab" else 60 * 86400  # the lab switch reboots every 6 hours
    since = boot(name, t, reboot)
    vendor, model, version, ports, sfp_from = ACC_INFO[name]
    load = fabric_load(t)
    stations = present_stations(t)
    port_vlans: dict[str, set[int]] = defaultdict(set)
    for st in stations:
        loc = station_loc(st, t)
        if loc[0] == "sw" and loc[1] == name and st.vlan:
            port_vlans[loc[2]].add(st.vlan)
        for sw, port in path(loc):
            if sw == name and st.vlan:
                port_vlans[port].add(st.vlan)
    interfaces, neighbors = [], []
    for i, port in enumerate(ports, start=1):
        link = ACC_LINKS.get((name, port))
        used = port in port_vlans or link is not None
        connector = "sfp" if i >= sfp_from else "rj45"
        if not used:
            interfaces.append(
                {"name": port, "type": "ethernet", "connector": connector, "up": False}
            )
            continue
        speed = link[3] if link else (1000 if connector == "rj45" else 10000)
        if name == "acc-floor2" and port == "Port 2":
            speed = 1000
        if link and link[0].startswith("ap-"):
            vl = {"untagged": 10, "tagged": [20, 40, 66]}
        elif link and link[0] in ("core-sw1", "core-sw2", "acc-floor2", "acc-lab"):
            vl = {"untagged": 1, "tagged": TRUNK_ALL}
        else:
            vs = sorted(port_vlans.get(port, {1}))
            vl = {"untagged": vs[0]} if len(vs) == 1 else {"untagged": vs[-1], "tagged": vs[:-1]}
        down = load.get((name, port), 0)
        if (link and link[0] == "acc-floor2") or (name, port) == ("acc-floor2", "Port 24"):
            down = min(down, 60_000_000)  # a 100 Mbps link cannot carry more
        if name == "acc-floor2" and port == "SFP+ 2":
            down = 0  # blocked by spanning tree
        d = iface(
            port,
            f"{name}/{port}",
            t,
            since,
            down=down or 30_000,
            type="ethernet",
            connector=connector,
            up=True,
            speed_mbps=speed,
            duplex="full",
            vlans=vl,
        )
        if link:
            d["description"] = f"{link[0]} {link[2]}"
        if (name, port) == ERROR_PORT:
            d["rx_errors"] = int((t - since) / 6)
            d["tx_errors"] = int((t - since) / 90)
        if connector == "sfp":
            if name == "acc-floor1":
                rx = -20.9 if port == "50" else None
                d["transceiver"] = clean(
                    sfp("FS", "SFP-10GLR-31", "10GBASE-LR", f"{name}/{port}", t, rx, 1310.0)
                )
            else:
                d["transceiver"] = clean(
                    sfp("Ubiquiti", "UACC-OM-MM-10G-D", "10GBASE-SR", f"{name}/{port}", t)
                )
        interfaces.append(d)
        if link:
            nb = {
                "local_port": port,
                "protocol": link[4],
                "remote_name": link[0],
                "remote_mac": link[1],
                "remote_port": link[2],
                "remote_platform": link[5],
            }
            if link[0] == "SEPF87B20A1B2C3":
                nb["remote_ip"] = "10.10.30.150"
            neighbors.append(nb)
    if name == "acc-floor1":
        interfaces.append(
            iface(
                "Trk1",
                f"{name}/Trk1",
                t,
                since,
                down=load.get((name, "Trk1"), 0),
                type="lag",
                members=["49", "50"],
                up=True,
                speed_mbps=20000,
                vlans={"untagged": 1, "tagged": TRUNK_ALL},
                description="Uplink to core",
            )
        )
    own = {ACC_MAC[name]}
    fdb = switch_fdb(name, t, own)
    if name == "acc-floor1":
        # A stale entry on the access point's port: only this table knows the MAC.
        fdb.append({"mac": mac("a4:83:e7", "stale"), "port": "1", "vlan": 20})
    sw_ip = {"acc-floor1": vip(10, 11), "acc-floor2": vip(10, 12), "acc-lab": vip(10, 13)}[name]
    firmware = {"current": version, "checked_at": iso(t - 7200)}
    if name == "acc-floor2":
        firmware.update(latest="7.2.123", update_available=True, updates=1)
    else:
        firmware.update(latest=version, update_available=False)
    return {
        "key": ACC_MAC[name],
        "name": name,
        "host": sw_ip,
        "role": "switch",
        "vendor": vendor,
        "model": model,
        "os_version": version,
        "uptime_s": int(t - since),
        "cpu_pct": round(12 + 6 * math.sin(t / 250), 1),
        "mem_pct": 38.0,
        "macs": [ACC_MAC[name]],
        "ips": [sw_ip],
        "temperatures": [
            {"sensor": "System", "kind": "board", "celsius": 44.0 if name != "acc-lab" else 58.0}
        ],
        "firmware": firmware,
        "interfaces": [
            *interfaces,
            {
                "name": "vlan10",
                "type": "vlan",
                "vlan": 10,
                "mac": ACC_MAC[name],
                "ips": [f"{sw_ip}/24"],
                "up": True,
            },
        ],
        "neighbors": neighbors,
        "fdb": fdb,
    }


# --- devnet-wifi: the access points, as a controller reports them -------------------------

AP_INFO = {
    "ap-lobby": ("U6-Pro", ("2.4ghz", "5ghz"), 2500),
    "ap-office1": ("U6-Enterprise", ("2.4ghz", "5ghz", "6ghz"), 2500),
    "ap-office2": ("U7-Pro", ("2.4ghz", "5ghz", "6ghz"), 2500),
    "ap-meeting": ("U6-Lite", ("2.4ghz", "5ghz"), 1000),
    "ap-warehouse": ("U6-Mesh", ("2.4ghz", "5ghz"), 1000),
}


def wifi_clients(t: float) -> dict[str, list[tuple[Station, str]]]:
    out: dict[str, list[tuple[Station, str]]] = defaultdict(list)
    for st in hq_stations():
        if st.wifi and st.up(t):
            ap = st.wifi["ap"](t) if callable(st.wifi["ap"]) else st.wifi["ap"]
            out[ap].append((st, ap))
    return out


def ap_device(name: str, t: float, clients: list[tuple[Station, str]]) -> dict[str, Any]:
    model, bands, eth_speed = AP_INFO[name]
    since = boot(name, t, 30 * 86400)
    total = sum(st.avg for st, _ in clients)
    wl = []
    for st, _ in clients:
        band = st.wifi["band"]
        signal = st.wifi["signal"] + round(2 * math.sin(t / 120 + seed(st.mac) % 7))
        link = RATES[band] * (1 if signal > -65 else 0.5 if signal > -75 else 0.2)
        wl.append(
            {
                "mac": st.mac,
                "interface": BAND_IF[band],
                "ssid": st.wifi["ssid"],
                "band": band,
                "signal_dbm": min(signal, -30),
                "tx_rate_mbps": round(link, 1),
                "rx_rate_mbps": round(link * 0.8, 1),
                "rx_bps": rate(st.mac + "/dl", st.avg, t),
                "tx_bps": rate(st.mac + "/ul", st.avg / 5, t),
            }
        )
    interfaces = [
        iface(
            "eth0",
            f"{name}/eth0",
            t,
            since,
            down=total / 5,
            upload=total,
            type="ethernet",
            connector="rj45",
            mac=AP_MAC[name],
            up=name != "ap-warehouse",
            speed_mbps=eth_speed if name != "ap-warehouse" else None,
        ),
    ]
    for band in bands:
        btotal = sum(st.avg for st, _ in clients if st.wifi["band"] == band)
        interfaces.append(
            iface(
                BAND_IF[band],
                f"{name}/{band}",
                t,
                since,
                down=btotal or 10_000,
                type="wireless",
                up=True,
                description=band.replace("ghz", " GHz"),
            )
        )
    neighbors = []
    loc = AP_LOC[name]
    if loc[0] == "sw":
        sw, port = loc[1], loc[2]
        neighbors.append(
            {
                "local_port": "eth0",
                "protocol": "lldp",
                "remote_name": sw,
                "remote_mac": ACC_MAC[sw],
                "remote_port": port,
            }
        )
    else:  # a mesh satellite: it declares its uplink unit (one side only)
        neighbors.append(
            {
                "local_port": "wifi1",
                "protocol": "other",
                "remote_name": loc[1],
                "remote_mac": AP_MAC[loc[1]],
                "remote_port": "wifi1",
            }
        )
    ip = vip(10, 21 + list(AP_MAC).index(name))
    return {
        "key": AP_MAC[name],
        "name": name,
        "host": ip,
        "role": "ap",
        "vendor": "Ubiquiti",
        "model": model,
        "os_version": "6.7.31",
        "uptime_s": int(t - since),
        "cpu_pct": round(8 + len(clients) * 1.5, 1),
        "mem_pct": 52.0,
        "macs": [AP_MAC[name]],
        "ips": [ip],
        "interfaces": interfaces,
        "neighbors": neighbors,
        "wireless_clients": wl,
        "firmware": {
            "current": "6.7.31",
            "latest": "6.7.31",
            "update_available": False,
            "checked_at": iso(t - 3600),
        },
    }


def wifi_devices(t: float) -> list[dict[str, Any]]:
    per_ap = wifi_clients(t)
    devices = [ap_device(ap, t, per_ap.get(ap, [])) for ap in AP_MAC]
    # The controller also knows each client's name and address.
    hosts = [
        {
            "ip": st.ip,
            "mac": st.mac,
            "hostnames": [st.name] if st.name else None,
            "sources": ["controller"],
        }
        for ap in per_ap
        for st, _ in per_ap[ap]
    ]
    devices[0]["hosts"] = hosts
    return devices


# --- devnet-pve: a three-node Proxmox cluster -----------------------------------------------


def pve_devices(t: float) -> list[dict[str, Any]]:
    out = []
    load = fabric_load(t)
    for n, (h, (m1, m2)) in enumerate(PVE_MAC.items()):
        since = boot(h, t, 50 * 86400)
        hot = h == "pve2" and window(t, 3600, 1800, 600)  # busy ten minutes every hour
        down = load.get(("core-sw1", PVE_PORT[h]), 0) * 2
        out.append(
            {
                "key": m1,
                "name": h,
                "host": PVE_IP[h],
                "role": "server",
                "vendor": "Proxmox",
                "model": "Proxmox VE",
                "os_version": "pve-manager/9.0.10",
                "serial": f"S{seed(h) % 10**9:09d}",
                "uptime_s": int(t - since),
                "cpu_pct": 96.5 if hot else round(25 + 10 * math.sin(t / 500 + n), 1),
                "mem_pct": 71.0 + n * 4,
                "cpu_count": 32,
                "mem_total_bytes": 256 * 2**30,
                "mem_used_bytes": int((0.71 + n * 0.04) * 256 * 2**30),
                "swap_pct": 2.0 + n,
                "load_avg": [14.2, 12.0, 11.1] if hot else [2.1 + n, 2.4, 2.2],
                "temperatures": [
                    {"sensor": "Package id 0", "kind": "cpu", "celsius": 88.0 if hot else 55.0 + n},
                    {"sensor": "nvme0", "kind": "disk", "celsius": 44.0},
                ],
                "storage": [
                    {
                        "mount": "/",
                        "device": "rpool/ROOT/pve-1",
                        "fs_type": "zfs",
                        "total_bytes": 900 * 10**9,
                        "used_bytes": (120 + 60 * n) * 10**9,
                    }
                ],
                "firmware": {
                    "current": "9.0.10",
                    "latest": "9.0.11",
                    "update_available": True,
                    "updates": 34,
                    "needs_reboot": n == 0,
                    "checked_at": iso(t - 5 * 3600),
                },
                "macs": [m1, m2],
                "ips": [PVE_IP[h], f"fd00:10::{31 + n:x}"],
                "interfaces": [
                    iface(
                        "eno1",
                        f"{h}/eno1",
                        t,
                        since,
                        down=down / 2,
                        type="ethernet",
                        connector="sfp",
                        mac=m1,
                        up=True,
                        speed_mbps=10000,
                    ),
                    iface(
                        "eno2",
                        f"{h}/eno2",
                        t,
                        since,
                        down=down / 2,
                        type="ethernet",
                        connector="sfp",
                        mac=m2,
                        up=True,
                        speed_mbps=10000,
                    ),
                    iface(
                        "bond0",
                        f"{h}/bond0",
                        t,
                        since,
                        down=down,
                        type="lag",
                        members=["eno1", "eno2"],
                        mac=m1,
                        up=True,
                        speed_mbps=20000,
                    ),
                    iface(
                        "vmbr0",
                        f"{h}/vmbr0",
                        t,
                        since,
                        down=down,
                        type="bridge",
                        members=["bond0"],
                        mac=m1,
                        up=True,
                        ips=[f"{PVE_IP[h]}/24"],
                    ),
                ],
            }
        )
    for g in guests():
        if g.present and not g.present(t):
            continue
        since = boot(f"vm{g.vmid}", t, 20 * 86400)
        host_mac = PVE_MAC[g.host][0]
        ifaces = []
        for n, (gm, vlan, ip) in enumerate(g.nics):
            prefix = (
                24 if vlan == LAB_INNER_VLAN else ipaddress.ip_network(VLANS[vlan][1]).prefixlen
            )
            d = iface(
                f"net{n}",
                f"vm{g.vmid}/net{n}",
                t,
                since,
                down=g.avg if n == 0 else 200_000,
                type="ethernet",
                mac=gm,
                up=True,
                ips=[f"{ip}/{prefix}"],
                description=f"vmbr0 tag {vlan}",
            )
            ifaces.append(d)
        out.append(
            {
                "key": g.nics[0][0],
                "name": g.name,
                "host": g.nics[0][2],
                "role": g.role,
                "model": "QEMU virtual machine" if g.kind == "qemu" else "LXC container",
                "os_version": g.os,
                "uptime_s": int(t - since),
                "cpu_pct": round(5 + seed(g.name) % 40 + 5 * math.sin(t / 300), 1),
                "mem_pct": g.mem_pct,
                "cpu_count": g.cpus,
                "mem_total_bytes": g.mem_gb * 2**30,
                "mem_used_bytes": int(g.mem_gb * 2**30 * g.mem_pct / 100),
                "macs": [gm for gm, _, _ in g.nics],
                "ips": [ip for _, _, ip in g.nics],
                "interfaces": ifaces,
                "neighbors": [
                    {
                        "local_port": "net0",
                        "protocol": "other",
                        "remote_name": g.host,
                        "remote_mac": host_mac,
                        "remote_port": "vmbr0",
                    }
                ],
            }
        )
    return out


# --- devnet-lab: the lab router (an OPNsense VM on pve3, double NAT) -------------------------


def lab_device(t: float) -> dict[str, Any]:
    since = boot("lab-router", t, 7 * 86400)
    wan, lan = gmac(301), gmac(301, 1)
    stations = [st for st in lab_stations() if st.up(t)]
    vms = [g for g in guests() if g.nics[0][1] == LAB_INNER_VLAN]
    arp = [{"ip": vip(99, 1), "mac": FW_MAC, "interface": "vtnet0"}]
    arp += [{"ip": st.ip, "mac": st.mac, "interface": "vtnet1"} for st in stations if st.ip]
    arp += [{"ip": g.nics[0][2], "mac": g.nics[0][0], "interface": "vtnet1"} for g in vms]
    leases = [
        {"ip": st.ip, "mac": st.mac, "hostname": st.name or None, "expires_at": iso(t + 7200)}
        for st in stations
        if st.ip
    ]
    total = sum(st.avg for st in stations)
    return {
        "key": wan,
        "name": "lab-router",
        "host": vip(99, 2),
        "role": "router",
        "vendor": "OPNsense",
        "model": "Virtual machine",
        "os_version": "OPNsense 26.1.4",
        "uptime_s": int(t - since),
        "cpu_pct": 6.0,
        "mem_pct": 33.0,
        "cpu_count": 2,
        "mem_total_bytes": 4 * 2**30,
        "macs": [wan, lan],
        "ips": [vip(99, 2), ip_at(LAB_INNER, 1), "fd00:199::1"],
        "interfaces": [
            iface(
                "vtnet0",
                "lab/vtnet0",
                t,
                since,
                down=total / 5,
                upload=total,
                type="ethernet",
                mac=wan,
                up=True,
                wan=True,
                description="WAN (LAB VLAN)",
                ips=[f"{vip(99, 2)}/24"],
                speed_mbps=10000,
            ),
            iface(
                "vtnet1",
                "lab/vtnet1",
                t,
                since,
                down=total,
                type="ethernet",
                mac=lan,
                up=True,
                description="LAB-INNER",
                ips=[f"{ip_at(LAB_INNER, 1)}/24", "fd00:199::1/64"],
                speed_mbps=10000,
            ),
        ],
        "gateways": [
            {
                "name": "WAN_DHCP",
                "interface": "vtnet0",
                "address": vip(99, 1),
                "status": "up",
                "rtt_ms": 0.4,
                "loss_pct": 0.0,
            }
        ],
        "arp": arp,
        "dhcp_leases": leases,
        "dhcp_pools": [{"network": "LAB-INNER", "total": 50, "used": len(leases)}],
        "vlans": [
            {"id": LAB_INNER_VLAN, "name": "LAB-INNER", "interface": "vtnet1", "subnet": LAB_INNER}
        ],
        "firmware": {
            "current": "26.1.4",
            "latest": "26.1.6",
            "update_available": True,
            "updates": 12,
            "checked_at": iso(t - 86400),
        },
    }


# --- devnet-dnat: a consumer router plugged into the office network (double NAT) -------------

DNAT_LAN_MAC = mac("e8:48:b8", "mkt", "lan")


def dnat_device(t: float) -> dict[str, Any]:
    since = boot("mkt-router", t, 4 * 3600)  # it reboots every 4 hours: counters reset
    clients = [
        ("iPhone", rmac("mkt", 1), "5ghz", -51, 2_000_000),
        ("MacBook-Air-Marketing", mac("f0:18:98", "mkt-mba"), "5ghz", -47, 6_000_000),
        ("Chromecast", mac("54:60:09", "mkt-cc"), "5ghz", -60, 8_000_000),
        ("Pixel-7a", rmac("mkt", 2), "2.4ghz", -69, 600_000),
        ("iPad", mac("a4:83:e7", "mkt-ipad"), "5ghz", -56, 1_500_000),
        ("", rmac("mkt", 3), "2.4ghz", -73, 200_000),
    ]
    wl, arp, leases = [], [], []
    for i, (name, m, band, sig, avg) in enumerate(clients):
        ip = ip_at(NAT_LAN, 100 + i)
        wl.append(
            {
                "mac": m,
                "interface": "wlan1" if band == "5ghz" else "wlan0",
                "ssid": "Marketing-5G" if band == "5ghz" else "Marketing",
                "band": band,
                "signal_dbm": sig,
                "rx_bps": rate(m, avg, t),
                "tx_bps": rate(m + "/ul", avg / 5, t),
            }
        )
        arp.append({"ip": ip, "mac": m, "interface": "br-lan"})
        leases.append({"ip": ip, "mac": m, "hostname": name or None, "expires_at": iso(t + 7200)})
    printer = mac("00:26:ab", "mkt-printer")
    arp.append({"ip": ip_at(NAT_LAN, 50), "mac": printer, "interface": "br-lan"})
    leases.append({"ip": ip_at(NAT_LAN, 50), "mac": printer, "hostname": "EPSON5F1A2B"})
    arp.append({"ip": vip(20, 1), "mac": FW_MAC, "interface": "WAN"})
    total = sum(c[4] for c in clients)
    return {
        "key": DNAT_WAN_MAC,
        "name": "mkt-router",
        "host": vip(20, 250),
        "role": "router",
        "vendor": "TP-Link",
        "model": "Archer AX55",
        "os_version": "1.1.4 Build 20240411",
        "uptime_s": int(t - since),
        "cpu_pct": 14.0,
        "mem_pct": 61.0,
        "macs": [DNAT_WAN_MAC, DNAT_LAN_MAC],
        "ips": [vip(20, 250), "192.168.0.1"],
        "interfaces": [
            iface(
                "WAN",
                "mkt/wan",
                t,
                since,
                down=total / 5,
                upload=total,
                type="ethernet",
                connector="rj45",
                mac=DNAT_WAN_MAC,
                up=True,
                wan=True,
                speed_mbps=1000,
                ips=[f"{vip(20, 250)}/23"],
            ),
            iface(
                "LAN1",
                "mkt/lan1",
                t,
                since,
                down=100_000,
                type="ethernet",
                connector="rj45",
                mac=DNAT_LAN_MAC,
                up=True,
                speed_mbps=1000,
            ),
            {"name": "LAN2", "type": "ethernet", "connector": "rj45", "up": False},
            iface(
                "br-lan",
                "mkt/br-lan",
                t,
                since,
                down=total,
                type="bridge",
                members=["LAN1", "wlan0", "wlan1"],
                mac=DNAT_LAN_MAC,
                up=True,
                ips=["192.168.0.1/24"],
            ),
            iface(
                "wlan0",
                "mkt/wlan0",
                t,
                since,
                down=total / 4,
                type="wireless",
                up=True,
                description="2.4 GHz",
            ),
            iface(
                "wlan1",
                "mkt/wlan1",
                t,
                since,
                down=total * 3 / 4,
                type="wireless",
                up=True,
                description="5 GHz",
            ),
        ],
        "gateways": [
            {
                "name": "WAN",
                "interface": "WAN",
                "address": vip(20, 1),
                "status": "up",
                "rtt_ms": 0.8,
            }
        ],
        "wireless_clients": wl,
        "arp": arp,
        "dhcp_leases": leases,
        "fdb": [{"mac": printer, "port": "LAN1"}],
    }


# --- devnet-branch: a branch office behind a site-to-site IPsec tunnel (MikroTik) -----------

BR_GW, BR_SW, BR_CAP = mac("48:8f:5a", "br-gw"), mac("dc:2c:6e", "br-sw"), mac("c4:ad:34", "br-cap")


def branch_devices(t: float) -> list[dict[str, Any]]:
    if window(t, *IPSEC_DOWN):
        raise Unreachable(
            "br-gw (10.20.0.1) did not answer: the IPsec tunnel to the branch is down"
        )
    wired_clients = [
        (f"br-pc-{i + 1:02d}", mac("34:64:a9" if i % 2 else "18:66:da", "brpc", i), f"ether{i + 1}")
        for i in range(6)
    ]
    wired_clients += [
        ("BRWB0C2D1E0F1A2", mac("00:1b:a9", "brprint"), "ether10"),
        ("", mac("bc:ad:28", "brnvr"), "ether11"),
        ("SIP-T46U-BR1", mac("00:15:65", "brph", 1), "ether12"),
        ("SIP-T46U-BR2", mac("00:15:65", "brph", 2), "ether13"),
    ]
    wifi = [("iPhone", rmac("br", i), "5ghz" if i % 2 else "2.4ghz", -55 - 3 * i) for i in range(5)]
    wifi += [
        ("MacBook-Pro-Branch", mac("3c:22:fb", "brmbp"), "5ghz", -50),
        ("tuya-plug-br", mac("7c:f6:66", "brplug"), "2.4ghz", -64),
    ]
    arp, leases, fdb = [], [], []
    for i, (name, m, port) in enumerate(wired_clients):
        ip = ip_at(BRANCH_LAN, 100 + i)
        arp.append({"ip": ip, "mac": m, "interface": "bridge-lan"})
        leases.append({"ip": ip, "mac": m, "hostname": name or None, "expires_at": iso(t + 86400)})
        fdb.append({"mac": m, "port": port})
    wl = []
    for i, (name, m, band, sig) in enumerate(wifi):
        ip = ip_at(BRANCH_LAN, 150 + i)
        arp.append({"ip": ip, "mac": m, "interface": "bridge-lan"})
        leases.append({"ip": ip, "mac": m, "hostname": name or None, "expires_at": iso(t + 86400)})
        fdb.append({"mac": m, "port": "ether24"})
        wl.append(
            {
                "mac": m,
                "interface": "wifi2" if band == "5ghz" else "wifi1",
                "ssid": "Acme-Branch",
                "band": band,
                "signal_dbm": sig,
                "tx_rate_mbps": 433.3 if band == "5ghz" else 72.2,
                "rx_bps": rate(m, 1_000_000, t),
                "tx_bps": rate(m + "/ul", 200_000, t),
            }
        )
    for m in (BR_CAP,):
        fdb.append({"mac": m, "port": "ether24"})
    since = boot("br-gw", t, 90 * 86400)
    gw = {
        "key": BR_GW,
        "name": "br-gw",
        "host": ip_at(BRANCH_LAN, 1),
        "role": "router",
        "vendor": "MikroTik",
        "model": "RB5009UG+S+",
        "os_version": "RouterOS 7.19.4",
        "serial": "HFD08ABCDEF",
        "uptime_s": int(t - since),
        "cpu_pct": 7.0,
        "mem_pct": 22.0,
        "cpu_count": 4,
        "macs": [BR_GW],
        "ips": [ip_at(BRANCH_LAN, 1), "198.51.100.200", "10.255.0.2"],
        "temperatures": [{"sensor": "cpu", "kind": "cpu", "celsius": 51.0}],
        "interfaces": [
            iface(
                "ether1",
                "br/ether1",
                t,
                since,
                down=1_000_000,
                upload=8_000_000,
                type="ethernet",
                connector="rj45",
                mac=BR_GW,
                up=True,
                wan=True,
                speed_mbps=1000,
                ips=["198.51.100.200/24"],
                description="ISP",
            ),
            iface(
                "sfp-sfpplus1",
                "br/sfp1",
                t,
                since,
                down=8_000_000,
                type="ethernet",
                connector="sfp",
                up=True,
                speed_mbps=10000,
                description="br-sw",
            ),
            iface(
                "bridge-lan",
                "br/bridge",
                t,
                since,
                down=8_000_000,
                type="bridge",
                members=["sfp-sfpplus1"],
                up=True,
                ips=[f"{ip_at(BRANCH_LAN, 1)}/24"],
            ),
        ]
        + [
            {"name": f"ether{n}", "type": "ethernet", "connector": "rj45", "up": False}
            for n in range(2, 9)
        ],
        "neighbors": [
            {
                "local_port": "sfp-sfpplus1",
                "protocol": "mndp",
                "remote_name": "br-sw",
                "remote_mac": BR_SW,
                "remote_port": "sfp-sfpplus1",
                "remote_platform": "MikroTik",
            }
        ],
        "gateways": [
            {
                "name": "ISP",
                "interface": "ether1",
                "address": "198.51.100.1",
                "status": "up",
                "rtt_ms": 9.0,
            }
        ],
        "arp": arp,
        "dhcp_leases": leases,
        "dhcp_pools": [{"network": "bridge-lan", "total": 100, "used": len(leases)}],
        "firmware": {
            "current": "7.19.4",
            "latest": "7.20.1",
            "update_available": True,
            "updates": 1,
            "checked_at": iso(t - 12 * 3600),
        },
    }
    sw_since = boot("br-sw", t)
    sw_ports = []
    used = {p for _, _, p in wired_clients} | {"ether24"}
    for n in range(1, 25):
        p = f"ether{n}"
        if p in used:
            sw_ports.append(
                iface(
                    p,
                    f"br-sw/{p}",
                    t,
                    sw_since,
                    down=900_000,
                    type="ethernet",
                    connector="rj45",
                    up=True,
                    speed_mbps=1000,
                )
            )
        else:
            sw_ports.append({"name": p, "type": "ethernet", "connector": "rj45", "up": False})
    sw_ports.append(
        iface(
            "sfp-sfpplus1",
            "br-sw/sfp1",
            t,
            sw_since,
            down=1_500_000,
            upload=8_000_000,
            type="ethernet",
            connector="sfp",
            up=True,
            speed_mbps=10000,
        )
    )
    sw = {
        "key": BR_SW,
        "name": "br-sw",
        "host": ip_at(BRANCH_LAN, 2),
        "role": "switch",
        "vendor": "MikroTik",
        "model": "CRS326-24G-2S+",
        "os_version": "RouterOS 7.19.4",
        "uptime_s": int(t - sw_since),
        "macs": [BR_SW],
        "ips": [ip_at(BRANCH_LAN, 2)],
        "interfaces": sw_ports,
        "neighbors": [
            {
                "local_port": "sfp-sfpplus1",
                "protocol": "mndp",
                "remote_name": "br-gw",
                "remote_mac": BR_GW,
                "remote_port": "sfp-sfpplus1",
            },
            {
                "local_port": "ether24",
                "protocol": "mndp",
                "remote_name": "br-cap",
                "remote_mac": BR_CAP,
                "remote_port": "ether1",
            },
        ],
        "fdb": [*fdb, {"mac": BR_GW, "port": "sfp-sfpplus1"}],
    }
    cap_since = boot("br-cap", t)
    cap = {
        "key": BR_CAP,
        "name": "br-cap",
        "host": ip_at(BRANCH_LAN, 3),
        "role": "ap",
        "vendor": "MikroTik",
        "model": "cAP ax",
        "os_version": "RouterOS 7.19.4",
        "uptime_s": int(t - cap_since),
        "macs": [BR_CAP],
        "ips": [ip_at(BRANCH_LAN, 3)],
        "interfaces": [
            iface(
                "ether1",
                "br-cap/ether1",
                t,
                cap_since,
                down=1_000_000,
                type="ethernet",
                connector="rj45",
                mac=BR_CAP,
                up=True,
                speed_mbps=1000,
            ),
            iface("wifi1", "br-cap/wifi1", t, cap_since, down=1_000_000, type="wireless", up=True),
            iface("wifi2", "br-cap/wifi2", t, cap_since, down=4_000_000, type="wireless", up=True),
        ],
        "neighbors": [
            {
                "local_port": "ether1",
                "protocol": "mndp",
                "remote_name": "br-sw",
                "remote_mac": BR_SW,
                "remote_port": "ether24",
            }
        ],
        "wireless_clients": wl,
    }
    return [gw, sw, cap]


# --- devnet-storage: NAS, backup server and UPS ------------------------------------------------


def storage_devices(t: float) -> list[dict[str, Any]]:
    load = fabric_load(t)
    since = boot("nas-01", t, 100 * 86400)
    fill = 0.86 + 0.11 * ((t - BASE) % 21600) / 21600  # fills up over 6 hours, then is cleaned up
    vol1 = 48 * 10**12
    nas_down = load.get(("core-sw1", "Po30"), 0) * 2
    nas = {
        "key": NAS_MAC[0],
        "name": "nas-01",
        "host": vip(100, 20),
        "role": "server",
        "vendor": "Synology",
        "model": "RS2423RP+",
        "os_version": "DSM 7.2.2-72806 Update 4",
        "serial": "2390RQR012345",
        "uptime_s": int(t - since),
        "cpu_pct": round(22 + 15 * math.sin(t / 800) ** 2, 1),
        "mem_pct": 64.0,
        "cpu_count": 8,
        "mem_total_bytes": 32 * 2**30,
        "mem_used_bytes": int(20.5 * 2**30),
        "temperatures": [{"sensor": "System", "kind": "board", "celsius": 41.0}]
        + [
            {"sensor": f"Drive {i + 1}", "kind": "disk", "celsius": 34.0 + i % 4} for i in range(12)
        ],
        "storage": [
            {
                "mount": "/volume1",
                "device": "md2 (SHR-2, 12 drives)",
                "fs_type": "btrfs",
                "total_bytes": vol1,
                "used_bytes": int(vol1 * fill),
            },
            {
                "mount": "/volume2",
                "device": "md3 (RAID 1, SSD)",
                "fs_type": "btrfs",
                "total_bytes": 4 * 10**12,
                "used_bytes": int(2.4 * 10**12),
            },
        ],
        "firmware": {
            "current": "7.2.2-72806 Update 4",
            "latest": "7.2.2-72806 Update 4",
            "update_available": False,
            "checked_at": iso(t - 6 * 3600),
        },
        "macs": list(NAS_MAC),
        "ips": [vip(100, 20), "fd00:100::20"],
        "interfaces": [
            iface(
                "eth4",
                "nas/eth4",
                t,
                since,
                down=nas_down / 2,
                type="ethernet",
                connector="sfp",
                mac=NAS_MAC[0],
                up=True,
                speed_mbps=10000,
                transceiver=clean(sfp("Synology", "E10G21-F2 DAC", "DAC", "nas/eth4", t)),
            ),
            iface(
                "eth5",
                "nas/eth5",
                t,
                since,
                down=nas_down / 2,
                type="ethernet",
                connector="sfp",
                mac=NAS_MAC[1],
                up=True,
                speed_mbps=10000,
            ),
            iface(
                "bond0",
                "nas/bond0",
                t,
                since,
                down=nas_down,
                type="lag",
                members=["eth4", "eth5"],
                mac=NAS_MAC[0],
                up=True,
                speed_mbps=20000,
                ips=[f"{vip(100, 20)}/24"],
            ),
            *[
                {"name": f"eth{i}", "type": "ethernet", "connector": "rj45", "up": False}
                for i in range(4)
            ],
        ],
        "services": [
            {"name": "smbd", "description": "SMB file service", "running": True, "enabled": True},
            {"name": "nfsd", "description": "NFS", "running": True, "enabled": True},
            {
                "name": "hyper-backup",
                "description": "Hyper Backup",
                "running": True,
                "enabled": True,
            },
            {
                "name": "synology-drive",
                "description": "Synology Drive Server",
                "running": False,
                "enabled": True,
            },
        ],
    }
    bk_since = boot("bkp-01", t, 30 * 86400)
    backup = Burst(3600, 0, 900, 880_000_000)  # the first quarter of every hour: the link saturates
    bkp = {
        "key": BKP_MAC,
        "name": "bkp-01",
        "host": vip(100, 26),
        "role": "server",
        "vendor": "Dell",
        "model": "PowerEdge R740xd",
        "os_version": "Ubuntu 24.04.3 LTS",
        "serial": "7XK2Q93",
        "uptime_s": int(t - bk_since),
        "cpu_pct": 35.0 if backup.on(t) else 4.0,
        "mem_pct": 28.0,
        "cpu_count": 24,
        "mem_total_bytes": 128 * 2**30,
        "load_avg": [3.1, 2.2, 1.4],
        "storage": [
            {
                "mount": "/",
                "device": "/dev/sda2",
                "fs_type": "ext4",
                "total_bytes": 480 * 10**9,
                "used_bytes": 61 * 10**9,
            },
            {
                "mount": "/backup",
                "device": "backup/data",
                "fs_type": "zfs",
                "total_bytes": 120 * 10**12,
                "used_bytes": 84 * 10**12,
            },
        ],
        "temperatures": [
            {"sensor": "CPU1", "kind": "cpu", "celsius": 61.0 if backup.on(t) else 44.0}
        ],
        "macs": [BKP_MAC],
        "ips": [vip(100, 26)],
        "interfaces": [
            iface(
                "eno1",
                "bkp/eno1",
                t,
                bk_since,
                down=5_000_000,
                upload=5_000_000,
                type="ethernet",
                connector="rj45",
                mac=BKP_MAC,
                up=True,
                speed_mbps=1000,
                ips=[f"{vip(100, 26)}/24"],
            )
        ],
        "firmware": {
            "current": "Ubuntu 24.04.3",
            "update_available": True,
            "updates": 7,
            "checked_at": iso(t - 7200),
        },
    }
    # Its backups flow into the server: count them on the receive side.
    bkp["interfaces"][0]["rx_bytes"] = octets("bkp/eno1/rx", 5_000_000, t, bk_since, backup)
    ups_since = boot("ups-01", t, 365 * 86400)
    ups = {
        "key": UPS_MAC,
        "name": "ups-01",
        "host": vip(10, 40),
        "role": "unknown",
        "vendor": "APC",
        "model": "Smart-UPS SRT 3000",
        "os_version": "NMC AOS 2.5.0.8",
        "serial": "AS2208123456",
        "uptime_s": int(t - ups_since),
        "macs": [UPS_MAC],
        "ips": [vip(10, 40)],
        "temperatures": [{"sensor": "Battery", "kind": "other", "celsius": 31.0}],
        "interfaces": [
            iface(
                "eth0",
                "ups/eth0",
                t,
                ups_since,
                down=10_000,
                type="ethernet",
                connector="rj45",
                mac=UPS_MAC,
                up=True,
                speed_mbps=100,
            )
        ],
        "firmware": {
            "current": "2.5.0.8",
            "latest": "2.6.1.3",
            "update_available": True,
            "updates": 1,
            "checked_at": iso(t - 30 * 86400),
        },
    }
    return [nas, bkp, ups]


# --- devnet-scan: a monitoring box that scans the network --------------------------------------


def scan_devices(t: float) -> list[dict[str, Any]]:
    since = boot("mon-01", t)
    hosts = []
    for st in present_stations(t):
        if not st.ip or st.vlan not in VLANS:
            continue
        h: dict[str, Any] = {"ip": st.ip, "sources": ["icmp", "tcp"]}
        if st.vlan == 10:  # its own network: it sees MACs (ARP)
            h["mac"] = st.mac
            h["sources"] = ["arp", "icmp"]
        if st.name and not st.managed:
            h["hostnames"] = [f"{st.name.lower()}.acme.lan"]
        if st.scan:
            h.update(st.scan)
        hosts.append(h)
    for g in guests():
        if g.scan and (not g.present or g.present(t)):
            h = {
                "ip": g.nics[0][2],
                "sources": ["icmp", "tcp"],
                "hostnames": [f"{g.name}.acme.lan"],
                **g.scan,
            }
            hosts = [x for x in hosts if x["ip"] != h["ip"]] + [h]
    hosts += [
        {
            "ip": PVE_IP[p],
            "mac": PVE_MAC[p][0],
            "sources": ["arp", "tcp"],
            "open_ports": [22, 8006],
            "titles": [f"{p} - Proxmox Virtual Environment"],
            "web": [
                {
                    "port": 8006,
                    "url": f"https://{PVE_IP[p]}:8006/",
                    "title": f"{p} - Proxmox Virtual Environment",
                }
            ],
            "banners": ["SSH-2.0-OpenSSH_10.0p2 Debian-7"],
        }
        for p in PVE_IP
    ]
    hosts += [
        {
            "ip": vip(10, 40),
            "mac": UPS_MAC,
            "sources": ["arp", "tcp"],
            "open_ports": [80, 443],
            "titles": ["APC | Network Management Card"],
        },
        {
            "ip": vip(100, 20),
            "sources": ["tcp"],
            "open_ports": [80, 443, 445, 5000, 5001],
            "titles": ["Synology DiskStation - nas-01"],
        },
        # Only the scan sees these (another subnet, no MAC).
        {
            "ip": vip(100, 200),
            "sources": ["icmp", "tcp"],
            "hostnames": ["old-esxi.acme.lan"],
            "open_ports": [22, 443, 902],
            "titles": ["VMware ESXi"],
        },
        {
            "ip": vip(10, 1),
            "mac": FW_MAC,
            "sources": ["arp", "tcp"],
            "open_ports": [22, 53, 443],
            "titles": ["fw-edge.acme.lan | OPNsense"],
        },
    ]
    hosts = sorted(
        {h["ip"]: h for h in hosts}.values(), key=lambda h: ipaddress.ip_address(h["ip"])
    )
    return [
        {
            "key": MON_MAC,
            "name": "mon-01",
            "host": vip(10, 50),
            "role": "server",
            "vendor": "Raspberry Pi",
            "model": "Raspberry Pi 5 Model B Rev 1.0",
            "os_version": "Debian 13 (trixie)",
            "uptime_s": int(t - since),
            "cpu_pct": 12.0,
            "mem_pct": 45.0,
            "cpu_count": 4,
            "mem_total_bytes": 8 * 2**30,
            "temperatures": [{"sensor": "cpu_thermal", "kind": "cpu", "celsius": 52.0}],
            "macs": [MON_MAC],
            "ips": [vip(10, 50)],
            "interfaces": [
                iface(
                    "eth0",
                    "mon/eth0",
                    t,
                    since,
                    down=600_000,
                    type="ethernet",
                    connector="rj45",
                    mac=MON_MAC,
                    up=True,
                    speed_mbps=1000,
                    ips=[f"{vip(10, 50)}/24"],
                )
            ],
            "hosts": hosts,
        }
    ]


# --- entry points -------------------------------------------------------------------------------


def devices(plugin_id: str, now: float | None = None) -> list[dict[str, Any]]:
    """The devices a devnet plugin reports at a time (default: now). Raises Unreachable."""
    t = time.time() if now is None else float(now)
    builders: dict[str, Callable[[float], list[dict[str, Any]]]] = {
        "devnet-fw": lambda t: [fw_device(t)],
        "devnet-core": lambda t: [core_device(c, t) for c in CORES],
        "devnet-access": lambda t: [access_device(s, t) for s in ACC_INFO],
        "devnet-wifi": wifi_devices,
        "devnet-pve": pve_devices,
        "devnet-lab": lambda t: [lab_device(t)],
        "devnet-branch": branch_devices,
        "devnet-dnat": lambda t: [dnat_device(t)],
        "devnet-storage": storage_devices,
        "devnet-scan": scan_devices,
    }
    if plugin_id not in builders:
        raise KeyError(f"unknown devnet plugin {plugin_id!r}")
    return clean(builders[plugin_id](t))


def network(now: float) -> dict[str, list[dict[str, Any]] | str]:
    """Every plugin's devices at a time; a plugin that fails gives its error message."""
    out: dict[str, list[dict[str, Any]] | str] = {}
    for p in PLUGINS:
        try:
            out[p] = devices(p, now)
        except Unreachable as e:
            out[p] = str(e)
    return out


def main(argv: list[str] | None = None) -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    sub = ap.add_subparsers(dest="cmd", required=True)
    dump = sub.add_parser("dump", help="print every plugin's devices as JSON")
    dump.add_argument("--at", type=float, default=None, help="Unix time (default: now)")
    args = ap.parse_args(argv)
    if args.cmd == "dump":
        json.dump(
            network(time.time() if args.at is None else args.at), sys.stdout, separators=(",", ":")
        )
        sys.stdout.write("\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
