"""What changes with time in Acme's network: presence, roaming, traffic, events.

The world repeats every simulated week (Monday 00:00 local time, UTC-3): the
same instant of the week always gives the same answer, so tests can ask for
"Tuesday 14:10" and get the fiber outage. Byte counters are exact integrals
of the traffic rates (weekly periodic, 5-minute steps), counted from each
device's last boot and divided by the clock's speed-up, so the rates Omini
computes from two readings are the designed ones whatever the speed.

`World(design).at(t)` gives a `Snapshot` at simulated time `t` (epoch
seconds); vendor emulators only read snapshots.
"""

from __future__ import annotations

import hashlib
import ipaddress
from collections import defaultdict, deque
from dataclasses import dataclass, field
from functools import cached_property
from typing import Any

import numpy as np

from . import acme
from .model import Design, Node, Port

WEEK = 7 * 86400
STEP = 300  # seconds per traffic step
NB = WEEK // STEP  # steps per week
# A Monday 00:00 in São Paulo (2026-10-05T03:00:00Z): week numbers count from here.
WEEK0 = 1791169200
MBPS = 125_000.0  # bytes/s in one Mbit/s

BRIDGING = {"switch", "ap", "hypervisor", "phone"}
ROUTERS = {"firewall", "router"}


def h01(*parts: Any) -> float:
    """A stable pseudo-random number in [0, 1) from any values."""
    d = hashlib.blake2b("/".join(map(str, parts)).encode(), digest_size=8).digest()
    return int.from_bytes(d, "big") / 2**64


def wsec(t: float) -> float:
    """Seconds since Monday 00:00 local time."""
    return (t - WEEK0) % WEEK


def step_of(t: float) -> int:
    return int(wsec(t) // STEP) % NB


def week_index(t: float) -> int:
    return int((t - WEEK0) // WEEK)


def at_local(day: int, hh: int, mm: int = 0, week: int = 0) -> int:
    """Epoch of a local weekday (0 = Monday) and time, in week `week` from WEEK0."""
    return WEEK0 + week * WEEK + day * 86400 + hh * 3600 + mm * 60


def hours_array(fn) -> np.ndarray:
    """An array over the week's steps from fn(day, hour_float)."""
    out = np.zeros(NB)
    for b in range(NB):
        s = b * STEP + STEP / 2
        out[b] = fn(int(s // 86400), (s % 86400) / 3600)
    return out


# Activity of an office day by hour (0..1).
OFFICE = [0.02] * 7 + [0.25, 0.65, 1.0, 1.0, 0.95, 0.55, 0.8, 1.0, 1.0, 0.95, 0.7, 0.3, 0.12, 0.08, 0.05, 0.03, 0.02]


def business(day: int, hour: float) -> float:
    if day >= 5:
        return 0.03
    return OFFICE[int(hour)]


@dataclass(frozen=True)
class Event:
    name: str
    days: tuple[int, ...]  # weekdays it happens on
    start: float  # hours (local)
    end: float
    params: tuple = ()

    def active(self, s: float) -> bool:
        day, hour = int(s // 86400), (s % 86400) / 3600
        return day in self.days and self.start <= hour < self.end


ALL = (0, 1, 2, 3, 4, 5, 6)
WEEKDAYS = (0, 1, 2, 3, 4)
# The scenario calendar (DESIGN.md, "Scenario"). Times are local (UTC-3).
EVENTS = [
    Event("backup_vzdump", ALL, 1.0, 3.0),
    Event("backup_hyper", ALL, 2.0, 4.0),
    Event("backup_offsite", ALL, 4.5, 6.5),
    Event("fiber_down", (1, 3), 14 + 5 / 60, 14 + 20 / 60),
    Event("lte_degraded", ALL, 11.0, 11.5),
    Event("wg_branch_down", ALL, 16.0, 16.2),
    Event("ipsec_store_down", ALL, 3.5, 3.5 + 10 / 60),
    Event("erp_batch", (4,), 17.0, 19.0),
    Event("nas_disk_hot", (2, 3, 4), 10.0, 18.0),
    Event("carp_failover", (5,), 22 + 28 / 60, 22 + 36 / 60),  # fw-hq-01 upgrading: fw-hq-02 is master
    Event("ceph_recovery", (6,), 2.5, 3.5),
]
# Scheduled reboots: node → [(weekday, hour, minutes down)].
REBOOTS: dict[str, list[tuple[int, float, float]]] = {
    "fw-hq-02": [(5, 22.0, 6)],
    "fw-hq-01": [(5, 22.5, 6)],
    "pve01": [(6, 2.0, 8)],
    "pve02": [(6, 2 + 20 / 60, 8)],
    "pve03": [(6, 2 + 40 / 60, 8)],
    "sw-f2-01": [(5, 23.0, 4)],
    "halo-wh-02": [(d, 4.0, 2) for d in ALL],
    "store-rt01": [(0, 5.0, 2)],
}
for _k, _ap in enumerate(
    ("ap-f1-01", "ap-f1-02", "ap-cafe-01", "ap-f2-01", "ap-f2-02", "ap-f3-01", "ap-f3-02", "ap-mr-01", "ap-patio-01")
):
    REBOOTS[_ap] = [(2, 3.0 + _k * 2 / 60, 3)]
# While a Proxmox node reboots its guests run on the next one (live migration).
MIGRATE_TO = {"pve01": "pve02", "pve02": "pve03", "pve03": "pve01"}
MIGRATION_LEAD = 5 * 60  # guests leave 5 minutes before the reboot, come back after it

# Rooms: name → (AP, TV, Chromecast)
ROOMS = {
    "santos": "ap-f2-01",
    "campinas": "ap-f2-02",
    "paulista": "ap-mr-01",
    "ibirapuera": "ap-mr-01",
}


@dataclass
class Leg:
    """A path inside one VLAN: hops of (node, in_port, out_port)."""

    hops: list[tuple[str, str | None, str | None]]
    vlan: int


@dataclass
class PortState:
    up: bool
    speed: int | None
    rx_bytes: int
    tx_bytes: int
    rx_packets: int
    tx_packets: int
    rx_errors: int
    tx_errors: int
    rx_rate: float  # bytes/s now
    tx_rate: float
    duplex: str = "full"


@dataclass
class Assoc:
    client: Node
    ap: str
    radio: str  # the AP's radio port
    band: str
    ssid: str
    signal: int  # dBm
    noise: int
    tx_rate: int  # Mbps (AP → client)
    rx_rate: int
    since: float  # association time (epoch)
    channel: int
    rx_bytes: int  # from the client (uplink)
    tx_bytes: int  # to the client
    rx_rate_now: float  # bytes/s
    tx_rate_now: float


@dataclass
class Lease:
    node: Node
    ip: str
    mac: str
    hostname: str
    start: float
    end: float
    vlan: int
    static: bool


@dataclass
class Neighbor:
    local_port: str
    node: Node
    port: Port
    mgmt_ip: str | None


@dataclass
class Flow:
    src: str
    dst: str  # node name or "internet"
    fwd: np.ndarray  # bytes/s src → dst
    rev: np.ndarray  # bytes/s dst → src
    l2: bool = False  # same VLAN, no router
    vlan: int | None = None  # the VLAN of an L2 flow when not the source's own


class World:
    def __init__(self, design: Design | None = None) -> None:
        self.d = design or acme.build()
        self.nodes = self.d.nodes
        self._assign_ips()
        self._index()
        self._schedules()
        self._traffic()

    # ------------------------------------------------------------------ setup

    def _assign_ips(self) -> None:
        used: set[str] = {n.ip for n in self.nodes.values() if n.ip}
        for n in self.nodes.values():
            for p in n.ports.values():
                for a in p.ips:
                    used.add(a.split("/")[0])
        pools: dict[tuple[str, int], Any] = {}
        for n in sorted(self.nodes.values(), key=lambda x: x.name):
            if n.ip or not n.dhcp or n.vlan is None:
                continue
            v = self.d.vlans.get((n.site, n.vlan)) or self.d.vlans.get(("hq", n.vlan))
            if not v or not v.dhcp:
                raise ValueError(f"{n.name}: no DHCP pool in VLAN {n.vlan}")
            key = (v.site, v.id)
            if key not in pools:
                lo, hi = (int(ipaddress.IPv4Address(x)) for x in v.dhcp)
                pools[key] = iter(range(lo, hi + 1))
            for a in pools[key]:
                ip = str(ipaddress.IPv4Address(a))
                if ip not in used:
                    n.ip = ip
                    used.add(ip)
                    break
            else:
                raise ValueError(f"pool of VLAN {v.name} ({v.site}) is full")
        # prn-f3-01 has a static address inside the USERS pool: a desktop's lease collides with it.
        printer = next(n for n in self.nodes.values() if n.extra.get("static_in_pool"))
        victim = sorted(
            (n for n in self.nodes.values() if n.kind == "desktop" and n.vlan == 30 and n.site == "hq"),
            key=lambda x: x.name,
        )[0]
        victim.ip = printer.ip
        victim.extra["duplicate_of"] = printer.name
        self.duplicate_ip = (printer.name, victim.name, printer.ip)

    def _index(self) -> None:
        d = self.d
        self.adj: dict[str, list[tuple[str, str, str]]] = defaultdict(list)  # node → (port, peer, peer port)
        self.peer: dict[tuple[str, str], tuple[str, str]] = {}  # (node, port) → (peer, peer port), incl. members
        self.member_links: list[tuple[str, str, str, str]] = []
        for lk in d.links:
            pa, pb = d.nodes[lk.a].ports[lk.pa], d.nodes[lk.b].ports[lk.pb]
            self.peer[(lk.a, lk.pa)] = (lk.b, lk.pb)
            self.peer[(lk.b, lk.pb)] = (lk.a, lk.pa)
            if pa.lag and pb.lag:
                self.member_links.append((lk.a, lk.pa, lk.b, lk.pb))
                continue
            self.adj[lk.a].append((lk.pa, lk.b, lk.pb))
            self.adj[lk.b].append((lk.pb, lk.a, lk.pa))
        self.domain: dict[tuple[str, str], str] = {}
        for n in d.nodes.values():
            bridges = {p.name: p for p in n.ports.values() if p.kind == "bridge"}
            for p in n.ports.values():
                dom = "default"
                if p.parent and p.parent in bridges:
                    dom = p.parent
                else:
                    for b in bridges.values():
                        if p.name in b.members:
                            dom = b.name
                if n.kind == "switch":
                    # Out-of-band management ports are not bridged with the switch's other ports.
                    dom = "mgmt" if p.descr in ("mgmt", "OOB mgmt") else "default"
                self.domain[(n.name, p.name)] = dom
        # Wi-Fi mesh: the satellite depends on the unit it meshes through.
        self.mesh_parent = {lk.b: lk.a for lk in d.links if lk.kind == "wifi"}
        self.by_mac = {n.mac: n for n in d.nodes.values() if n.mac}
        self.by_ip = {n.ip: n for n in d.nodes.values() if n.ip}
        self.wifi_clients = list(d.wifi)

    def own_port(self, n: Node) -> str:
        """The port a node's own traffic leaves from."""
        for p in n.ports.values():
            if n.ip and any(a.split("/")[0] == n.ip for a in p.ips):
                return p.name
        if len(n.ports) == 1 or n.kind in ("phone",):
            return next(iter(n.ports))
        if n.extra.get("mesh_uplink"):  # a mesh AP: its traffic goes over the mesh link, its jack is unused
            return next(p.name for p in n.ports.values() if p.descr == "mesh uplink")
        for name in ("eth0", "net0", "wlan0", "LAN", "eno1"):
            if name in n.ports:
                return name
        return next(iter(n.ports))

    # ------------------------------------------------------------- schedules

    def _schedules(self) -> None:
        d = self.d
        steps = np.arange(NB) * STEP + STEP / 2
        self.day = (steps // 86400).astype(int)
        self.hour = (steps % 86400) / 3600
        self.business = np.array([business(int(dd), hh) for dd, hh in zip(self.day, self.hour, strict=True)])
        self.events = {e.name: np.array([e.active(s) for s in steps]) for e in EVENTS}
        self.fiber_up = ~self.events["fiber_down"]
        # Reboots: per node, a list of (week second of the reboot, seconds down).
        self.reboots: dict[str, list[tuple[float, float]]] = {
            n: sorted((dd * 86400 + hh * 3600, mins * 60) for dd, hh, mins in r) for n, r in REBOOTS.items()
        }
        self.down = {
            n: np.array([any(s0 <= s < s0 + dur for s0, dur in r) for s in steps]) for n, r in self.reboots.items()
        }
        # CARP master: fw-hq-01 unless it is rebooting or the failover window is on.
        fw2 = self.events["carp_failover"] | self.down["fw-hq-01"]
        self.master = np.where(fw2, 1, 0)  # index into ("fw-hq-01", "fw-hq-02")
        self.tunnel_up = {"wg-branch": ~self.events["wg_branch_down"], "ipsec-store": ~self.events["ipsec_store_down"]}

        # Presence.
        self.presence: dict[str, np.ndarray] = {}
        for n in d.nodes.values():
            self.presence[n.name] = self._presence(n)
        # Meetings: room → list of (day, start hour, end hour, participants)
        self.meetings: list[tuple[str, int, float, float, list[str]]] = []
        hq_people = sorted(k for k, p in d.people.items() if p["site"] == "hq" and p["floor"] > 0)
        for room in ROOMS:
            for day in WEEKDAYS:
                hour = 9
                while hour < 18:
                    if hour != 12 and h01("meeting", room, day, hour) < 0.45:
                        length = 2 if h01("long", room, day, hour) < 0.2 and hour < 16 else 1
                        count = 3 + int(h01("count", room, day, hour) * 6)
                        who = sorted(hq_people, key=lambda k: h01("who", room, day, hour, k))[:count]
                        self.meetings.append((room, day, hour, hour + length, who))
                        hour += length
                    else:
                        hour += 1
        self.room_busy = {r: np.zeros(NB, dtype=bool) for r in ROOMS}
        for room, day, start, end, _who in self.meetings:
            self.room_busy[room] |= (self.day == day) & (self.hour >= start) & (self.hour < end)
        # Wi-Fi attachment: per client, a list of APs and an index per step (-1 = absent).
        self.attach_aps: dict[str, list[str]] = {}
        self.attach_idx: dict[str, np.ndarray] = {}
        for cname, wa in d.wifi.items():
            n = d.nodes[cname]
            aps = [wa.home]
            idx = np.zeros(NB, dtype=int)
            owner = n.owner
            if owner and n.site == "hq" and n.kind in ("laptop", "mobile"):
                for room, day, start, end, who in self.meetings:
                    if owner in who:
                        ap = ROOMS[room]
                        if ap not in aps:
                            aps.append(ap)
                        mask = (self.day == day) & (self.hour >= start) & (self.hour < end)
                        idx[mask] = aps.index(ap)
                if n.kind == "mobile" and d.people.get(owner, {}).get("lunch_cafe"):
                    if "ap-cafe-01" not in aps:
                        aps.append("ap-cafe-01")
                    mask = (self.day < 5) & (self.hour >= 12) & (self.hour < 13)
                    idx[mask] = aps.index("ap-cafe-01")
            idx[~self.presence[cname]] = -1
            self.attach_aps[cname] = aps
            self.attach_idx[cname] = idx
        # Guests' devices: home host per step (VMs move while their node reboots).
        self.vm_host: dict[str, np.ndarray] = {}
        self.vm_hosts: dict[str, list[str]] = {}
        for n in d.nodes.values():
            if not n.host:
                continue
            hosts = [n.host]
            idx = np.zeros(NB, dtype=int)
            if n.vlan != acme.LAB_LAN and n.name not in ("lab-fw01", "homeassistant"):
                for s0, dur in self.reboots.get(n.host, []):
                    mask = (steps >= s0 - MIGRATION_LEAD) & (steps < s0 + dur + MIGRATION_LEAD)
                    other = MIGRATE_TO[n.host]
                    if other not in hosts:
                        hosts.append(other)
                    idx[mask] = hosts.index(other)
            self.vm_hosts[n.name] = hosts
            self.vm_host[n.name] = idx
        self.migrants: dict[str, list[str]] = defaultdict(list)  # host → guests that may move in
        self.guests_home: dict[str, list[str]] = defaultdict(list)
        for g, hosts in self.vm_hosts.items():
            gn = d.nodes[g]
            self.guests_home[hosts[0]].append(g)
            for h in hosts[1:]:
                self.migrants[h].append(g)
                hp = d.nodes[h]
                tap = self.tap_name(gn, h)
                if tap not in hp.ports:
                    bridge = "vmbr0" if gn.vlan == 10 else "vmbr1"
                    hp.add(
                        Port(
                            tap,
                            "veth" if gn.kind == "lxc" else "tap",
                            10000,
                            untagged=gn.vlan,
                            parent=bridge,
                            descr=gn.name,
                        )
                    )
                    self.domain[(h, tap)] = bridge

    def _presence(self, n: Node) -> np.ndarray:
        d = self.d
        day, hour = None, None
        steps = np.arange(NB) * STEP + STEP / 2
        day = (steps // 86400).astype(int)
        minute = (steps % 86400) / 60
        kind, _, arg = n.presence.partition(":")
        if kind == "always":
            out = np.ones(NB, dtype=bool)
        elif kind in ("office", "desktop") and arg in d.people:
            p = d.people[arg]
            arrive, stay = p["arrive"], p["stay"]
            out = np.zeros(NB, dtype=bool)
            for dd in (0, 1, 2, 3, 4, 5) if n.site == "store" else WEEKDAYS:  # the store opens on Saturdays
                if dd in p["off_days"]:
                    continue
                a = arrive + (h01("late", arg, dd) - 0.5) * 30
                out |= (day == dd) & (minute >= a) & (minute < a + stay)
            if kind == "desktop" and h01("always-on", n.name) < 0.4:
                out = np.ones(NB, dtype=bool)  # never shut down
        elif kind == "desktop":  # a shared PC
            out = (day < 6) & (minute >= 7 * 60) & (minute < 17 * 60)
        elif kind == "guest":
            out = np.zeros(NB, dtype=bool)
            k = int(arg)
            for dd in WEEKDAYS if n.site != "store" else (0, 1, 2, 3, 4, 5):
                if h01("visit", k, dd) < 0.55:
                    start = 9 * 60 + h01("visit-start", k, dd) * 7 * 60
                    length = 45 + h01("visit-len", k, dd) * 150
                    out |= (day == dd) & (minute >= start) & (minute < start + length)
        elif kind == "shift":
            out = (day >= 5) | (minute < 7 * 60) | (minute >= 19 * 60)
        elif kind == "store":
            out = (day < 6) & (minute >= 8 * 60 + 30) & (minute < 20 * 60 + 30)
        else:
            raise ValueError(f"{n.name}: unknown presence {n.presence}")
        _ = hour
        return out

    # --------------------------------------------------------------- traffic

    def _profile(self, n: Node) -> list[tuple[str, np.ndarray, np.ndarray, bool]]:
        """A node's flows: (destination, down bytes/s, up bytes/s, l2)."""
        name = n.name
        rng = np.random.default_rng(int(h01("noise", name) * 2**32))
        noise = rng.uniform(0.55, 1.45, NB)
        pres = self.presence[name].astype(float)
        biz = self.business
        prof = n.traffic
        out: list[tuple[str, np.ndarray, np.ndarray, bool]] = []
        mb = MBPS

        def flow(dst: str, down: np.ndarray, up: np.ndarray, l2: bool = False) -> None:
            out.append((dst, down, up, l2))

        site_servers = n.site in ("hq", "branch", "store")
        if prof == "laptop":
            act = pres * np.maximum(biz, 0.15) * noise
            flow("internet", 1.6 * mb * act, 0.35 * mb * act)
            if site_servers:
                srv = "file01" if h01("srv", name) < 0.6 else "erp-app01"
                flow(srv, 0.7 * mb * act, 0.15 * mb * act)
        elif prof == "mobile":
            act = pres * np.maximum(biz, 0.3) * noise
            flow("internet", 0.35 * mb * act, 0.07 * mb * act)
        elif prof == "guest":
            act = pres * noise
            flow("internet", 2.2 * mb * act, 0.4 * mb * act)
        elif prof == "desktop":
            act = pres * np.maximum(biz, 0.05) * noise
            flow("internet", 0.8 * mb * act, 0.2 * mb * act)
            if site_servers:
                flow("file01" if h01("srv", name) < 0.5 else "erp-app01", 1.0 * mb * act, 0.25 * mb * act)
        elif prof == "voip":
            calls = np.array([h01("call", name, b) < 0.35 * biz[b] for b in range(NB)], dtype=float)
            rate = 0.1 * mb * calls + 0.002 * mb
            if n.site == "hq":
                flow("pbx01", rate, rate, l2=True)
            else:
                flow("pbx01", rate, rate)
        elif prof == "camera":
            flow("nvr01", 0.02 * mb * np.ones(NB), 4.0 * mb * noise * 0.5 + 2.0 * mb, l2=True)
        elif prof == "camera-cloud":
            flow("internet", 0.02 * mb * np.ones(NB), 1.2 * mb * np.ones(NB))
        elif prof == "nvr":
            flow("internet", 0.05 * mb * biz, 0.6 * mb * biz * noise)
        elif prof == "printer":
            jobs = np.array([h01("job", name, b) < 0.25 * biz[b] for b in range(NB)], dtype=float)
            if n.site == "hq" and n.vlan == 70:
                flow("print01", 2.5 * mb * jobs + 0.001 * mb, 0.05 * mb * jobs + 0.001 * mb, l2=True)
            else:
                flow("internet", 0.001 * mb * np.ones(NB), 0.001 * mb * np.ones(NB))
        elif prof == "tv":
            room = name.split("-", 1)[1] if "-" in name else ""
            busy = self.room_busy.get(room, np.zeros(NB, dtype=bool)).astype(float)
            flow("internet", (6.0 * busy + 0.01) * mb * noise, 0.15 * mb * busy + 0.002 * mb)
        elif prof == "iot":
            flow("internet", 0.004 * mb * noise, 0.003 * mb * noise)
            if n.vlan == 50 and n.site == "hq":
                flow("homeassistant", 0.001 * mb * np.ones(NB), 0.002 * mb * noise, l2=True)
        elif prof == "pos":
            act = pres * noise
            flow("erp-app01", 0.05 * mb * act, 0.04 * mb * act)
            flow("internet", 0.02 * mb * act, 0.02 * mb * act)
        elif prof == "lab":
            flow("internet", 0.5 * mb * noise * (0.3 + biz), 0.1 * mb * noise * (0.3 + biz))
        elif prof == "dmz-web":
            flow("internet", 2.0 * mb * (0.4 + biz) * noise, 18.0 * mb * (0.4 + biz) * noise)
            flow("erp-app01", 0.5 * mb * biz, 0.5 * mb * biz)
        elif prof == "dmz-mail":
            flow("internet", 0.8 * mb * (0.3 + biz) * noise, 0.8 * mb * (0.3 + biz) * noise)
        elif prof == "dc":
            other = "dc02" if name == "dc01" else None
            if other:
                flow(other, 0.4 * mb * noise, 0.4 * mb * noise, l2=True)
            flow("internet", 0.2 * mb * noise, 0.1 * mb * noise)
        elif prof == "erp":
            if name == "erp-app01":
                batch = self.events["erp_batch"].astype(float)
                flow(
                    "erp-db01",
                    (25.0 * biz + 180.0 * batch) * mb * noise,
                    (8.0 * biz + 40.0 * batch) * mb * noise,
                    l2=True,
                )
            flow("internet", 0.3 * mb * noise, 0.2 * mb * noise)
        elif prof == "file":
            flow("internet", 0.5 * mb * noise, 0.2 * mb * noise)
        elif prof == "pbs":
            vz = self.events["backup_vzdump"].astype(float)
            flow("bkp-nas01", 0.01 * mb * np.ones(NB), 6800.0 * mb * vz, l2=True)
        elif prof == "nas":
            hb = self.events["backup_hyper"].astype(float)
            flow("bkp-nas01", 0.01 * mb * np.ones(NB), 2900.0 * mb * hb, l2=True)
            flow("internet", 0.1 * mb * noise, 0.1 * mb * noise)
        elif prof == "light":
            flow("internet", 0.05 * mb * noise, 0.04 * mb * noise)
        return out

    def _flows(self) -> list[Flow]:
        flows: list[Flow] = []
        for n in self.nodes.values():
            for dst, down, up, l2 in self._profile(n):
                flows.append(Flow(n.name, dst, up, down, l2))
        # Hypervisors: vzdump to pbs01 and Ceph replication between the nodes.
        vz = self.events["backup_vzdump"].astype(float)
        rec = self.events["ceph_recovery"].astype(float)
        nodes = ("pve01", "pve02", "pve03")
        for i, a in enumerate(nodes):
            b = nodes[(i + 1) % 3]
            rate = (150.0 + 250.0 * self.business + 1200.0 * vz + 2500.0 * rec) * MBPS
            flows.append(Flow(a, b, rate, rate * 0.9, l2=True, vlan=102))
            flows.append(Flow(a, b, 0.05 * MBPS * np.ones(NB), 0.05 * MBPS * np.ones(NB), l2=True, vlan=103))
            if a != "pve03":
                flows.append(Flow(a, "pbs01", 2300.0 * MBPS * vz, 0.02 * MBPS * vz, l2=True, vlan=110))
        flows.append(
            Flow(
                "bkp-nas01",
                "internet",
                300.0 * MBPS * self.events["backup_offsite"],
                0.0 * vz + 2.0 * MBPS * self.events["backup_offsite"],
            )
        )
        # UniFi devices report to the controller; switches and APs are polled by mon01.
        for n in self.nodes.values():
            if n.api == "unifi" and n.kind in ("ap", "switch"):
                flows.append(Flow(n.name, "unifi01", np.full(NB, 0.02 * MBPS), np.full(NB, 0.01 * MBPS)))
        return flows

    def _traffic(self) -> None:
        self.rx: dict[tuple[str, str], np.ndarray] = defaultdict(lambda: np.zeros(NB))
        self.tx: dict[tuple[str, str], np.ndarray] = defaultdict(lambda: np.zeros(NB))
        self.err: dict[tuple[str, str], np.ndarray] = {}
        self._legs: dict[tuple, Leg | None] = {}
        flows = self._flows()
        # WAN capacity: scale internet traffic down when the active uplink is full.
        cap_down = np.where(self.fiber_up, 940.0, 280.0) * MBPS
        cap_up = np.where(self.fiber_up, 470.0, 55.0) * MBPS
        hq_down = np.zeros(NB)
        hq_up = np.zeros(NB)
        for f in flows:
            if f.dst == "internet" and self.site_gw(self.nodes[f.src]) in ("fw", "lab-fw01"):
                hq_down += f.rev
                hq_up += f.fwd
        scale_down = np.minimum(1.0, cap_down / np.maximum(hq_down, 1.0))
        scale_up = np.minimum(1.0, cap_up / np.maximum(hq_up, 1.0))
        self.wan_demand = (hq_down, hq_up)
        for f in flows:
            if f.dst == "internet" and self.site_gw(self.nodes[f.src]) in ("fw", "lab-fw01"):
                f.fwd = f.fwd * scale_up
                f.rev = f.rev * scale_down
            self._route_flow(f)
        # Errors: a bad patch cable on sw-f1-01 port 12, and the failing fiber to the storage switch.
        self.err[("sw-f1-01", "12")] = (
            0.4 * self.business * (self.presence[self._on_port("sw-f1-01", "12")].astype(float))
        )
        rx_dbm = self.fiber_rx_dbm_array()
        crc = np.clip(-(rx_dbm + 11.0), 0, None) * 25.0
        self.err[("sw-stor-01", "sfp28-16")] = crc
        self.err[("core-sw01", "Te2/1/6")] = crc * 0.05
        # Prefix sums for counters.
        self.cum: dict[tuple[str, str, str], np.ndarray] = {}
        self.runs: dict[tuple[int, int], tuple[int, int]] = {}
        self.assoc_masks: dict[tuple[str, str], np.ndarray] = {}

    def _on_port(self, node: str, port: str) -> str:
        peer = self.peer.get((node, port))
        return peer[0] if peer else node

    def site_gw(self, n: Node) -> str:
        if n.name in ("lab-fw01",):
            return "fw"
        return acme.GATEWAYS.get(n.site, "fw")

    def _route_flow(self, f: Flow) -> None:
        src = self.nodes[f.src]
        dst = self.nodes.get(f.dst)
        # Variant per step: (attachment of src, host of src, attachment of dst, host of dst, master, fiber, tunnel)
        codes = np.zeros(NB, dtype=np.int64)
        parts: list[np.ndarray] = []
        for n in (src, dst):
            if n is None:
                parts += [np.zeros(NB, dtype=int), np.zeros(NB, dtype=int)]
                continue
            parts.append(self.attach_idx.get(n.name, np.zeros(NB, dtype=int)) + 1)
            parts.append(self.vm_host.get(n.name, np.zeros(NB, dtype=int)))
        parts.append(self.master)
        parts.append(self.fiber_up.astype(int))
        tun = np.ones(NB, dtype=int)
        if src.site in ("branch", "store") or (dst is not None and dst.site in ("branch", "store")):
            site = src.site if src.site in ("branch", "store") else dst.site
            if f.dst != "internet" and (dst is None or dst.site != src.site):
                tun = self.tunnel_up["wg-branch" if site == "branch" else "ipsec-store"].astype(int)
        parts.append(tun)
        for p in parts:
            codes = codes * 16 + p
        for code in np.unique(codes):
            mask = codes == code
            if not (f.fwd[mask].any() or f.rev[mask].any()):
                continue
            b = int(np.flatnonzero(mask)[0])
            hops = self.route(f.src, f.dst, b, l2=f.l2, vlan=f.vlan)
            if hops is None:
                continue
            fwd = np.where(mask, f.fwd, 0.0)
            rev = np.where(mask, f.rev, 0.0)
            salt = h01("lag", f.src, f.dst)
            for node, port, direction in hops:
                if direction == "out":  # fwd leaves through this port
                    self.tx[(node, port)] += fwd
                    self.rx[(node, port)] += rev
                else:
                    self.rx[(node, port)] += fwd
                    self.tx[(node, port)] += rev
                p = self.nodes[node].ports[port]
                if p.kind == "lag" and p.members:
                    m = p.members[int(salt * len(p.members)) % len(p.members)]
                    if direction == "out":
                        self.tx[(node, m)] += fwd
                        self.rx[(node, m)] += rev
                    else:
                        self.rx[(node, m)] += fwd
                        self.tx[(node, m)] += rev

    # ---------------------------------------------------------------- routing

    def attach_at(self, name: str, b: int) -> str | None:
        """The AP a Wi-Fi client is on at step b (None when away)."""
        if name not in self.attach_idx:
            return None
        i = int(self.attach_idx[name][b])
        return None if i < 0 else self.attach_aps[name][i]

    def host_at(self, name: str, b: int) -> str | None:
        if name not in self.vm_host:
            return None
        return self.vm_hosts[name][int(self.vm_host[name][b])]

    def radio_for(self, client: Node, ap: Node) -> str:
        """The AP radio (or SSID interface) a client uses: its SSID's VLAN, its best band."""
        wa = self.d.wifi.get(client.name)
        vlan = self.d.ssids[wa.ssid].vlan if wa else client.vlan
        ssid_bands = self.d.ssids[wa.ssid].bands if wa else ("2.4", "5", "6")
        radios = [p for p in ap.ports.values() if p.kind == "wifi" and p.band in ssid_bands and p.carries(vlan or -1)]
        if not radios:
            radios = [p for p in ap.ports.values() if p.kind == "wifi" and p.band]
        bands = [p.band for p in radios]
        want = "5"
        if client.kind in ("iot", "scanner") or client.traffic == "iot":
            want = "2.4"
        elif "6" in bands and client.model in ("iPhone17,1", "SM-S931B", "MacBook Pro (14-inch, M4, 2024)"):
            want = "6"
        for p in radios:
            if p.band == want:
                return p.name
        return radios[0].name

    def links_at(self, name: str, b: int) -> list[tuple[str, str, str]]:
        """A node's links at step b: cables, plus Wi-Fi associations and guests' taps."""
        out = []
        n = self.nodes[name]
        for port, peer, pport in self.adj.get(name, []):
            pn = self.nodes[peer]
            if n.kind == "hypervisor" and pn.host and self.host_at(peer, b) != name:
                continue  # the guest has moved away
            if n.host and pn.kind == "hypervisor" and self.host_at(name, b) != peer:
                continue
            out.append((port, peer, pport))
        if n.host:
            h = self.host_at(name, b)
            if h and h != n.host:
                out.append((self.own_port(n), h, self.tap_name(n, h)))
        if n.kind == "hypervisor":
            for g in self.migrants.get(name, ()):
                if self.host_at(g, b) == name:
                    gn = self.nodes[g]
                    out.append((self.tap_name(gn, name), g, self.own_port(gn)))
        if name in self.d.wifi:
            ap = self.attach_at(name, b)
            if ap:
                out.append((self.own_port(n), ap, self.radio_for(n, self.nodes[ap])))
        if n.kind in ("ap", "router"):
            for c in self.wifi_by_home.get(name, ()):
                if self.attach_at(c, b) == name:
                    cn = self.nodes[c]
                    out.append((self.radio_for(cn, n), c, self.own_port(cn)))
        return out

    @cached_property
    def wifi_by_home(self) -> dict[str, list[str]]:
        out: dict[str, list[str]] = defaultdict(list)
        for c, aps in self.attach_aps.items():
            for ap in aps:
                out[ap].append(c)
        return out

    def tap_name(self, guest: Node, host: str) -> str:
        """The tap (VM) or veth (container) of a guest on a host."""
        return f"{'veth' if guest.kind == 'lxc' else 'tap'}{guest.vmid}i0"

    def carries(self, node: str, port: str, vlan: int) -> bool:
        p = self.nodes[node].ports[port]
        if p.lag and p.kind != "lag":
            return False  # a LAG member: the LAG carries the traffic
        return p.carries(vlan)

    def leg(self, a: str, z: str, vlan: int, b: int, a_port: str | None = None, a_dom: str | None = None) -> Leg | None:
        """The path from node a to node z inside one VLAN at step b (BFS over bridging nodes)."""
        key = (a, z, vlan, a_port, a_dom, self._leg_ctx(a, b), self._leg_ctx(z, b))
        if key in self._legs:
            return self._legs[key]
        an = self.nodes[a]
        prev: dict[str, tuple[str, str, str]] = {}  # node → (previous node, its exit port, entry port)
        q = deque([(a, None)])
        seen = {a}
        found = a == z
        while q and not found:
            cur, in_port = q.popleft()
            dom = self.domain.get((cur, in_port)) if in_port else a_dom
            for port, peer, pport in self.links_at(cur, b):
                if peer in seen:
                    continue
                if cur == a:
                    if a_port is not None and port != a_port:
                        continue
                    if not self.carries(cur, port, vlan):
                        continue
                    if an.kind in BRIDGING and dom is not None and self.domain.get((cur, port)) != dom:
                        continue
                else:
                    if not self.carries(cur, port, vlan) or self.domain.get((cur, port)) != dom:
                        continue
                pn = self.nodes[peer]
                if not self.carries(peer, pport, vlan):
                    continue
                if peer != z and pn.kind not in BRIDGING:
                    continue
                seen.add(peer)
                prev[peer] = (cur, port, pport)
                if peer == z:
                    found = True
                    break
                q.append((peer, pport))
        if not found:
            self._legs[key] = None
            return None
        chain = [z]
        while chain[-1] != a:
            chain.append(prev[chain[-1]][0])
        chain.reverse()
        hops: list[tuple[str, str | None, str | None]] = []
        for i, node in enumerate(chain):
            inp = prev[node][2] if i > 0 else None
            outp = prev[chain[i + 1]][1] if i + 1 < len(chain) else None
            hops.append((node, inp, outp))
        leg = Leg(hops, vlan)
        self._legs[key] = leg
        return leg

    def _leg_ctx(self, name: str, b: int) -> Any:
        if name in self.attach_idx:
            return self.attach_at(name, b)
        if name in self.vm_host:
            return self.host_at(name, b)
        if self.nodes[name].kind == "hypervisor":
            return tuple(g for g in self.migrants.get(name, ()) if self.host_at(g, b) == name) + tuple(
                g for g in self.guests_home.get(name, ()) if self.host_at(g, b) != name
            )
        return None

    def vlan_port(self, n: Node, vlan: int) -> str:
        """The interface a node's own traffic in a VLAN uses (vmbr1.110 on a Proxmox node, else its own port)."""
        for p in n.ports.values():
            if p.kind == "vlan" and p.vlan == vlan:
                return p.name
        for p in n.ports.values():
            if p.kind == "bridge" and p.carries(vlan) and p.ips:
                return p.name
        return self.own_port(n)

    def count(self, leg: Leg, src_own: str | None, dst_own: str | None) -> list[tuple[str, str, str]]:
        """The ports a leg crosses, with the direction the forward traffic takes through each."""
        out: list[tuple[str, str, str]] = []
        last = len(leg.hops) - 1
        for i, (node, inp, outp) in enumerate(leg.hops):
            if i == 0 and src_own and src_own != outp:
                out.append((node, src_own, "out"))
            if inp is not None:
                out.append((node, inp, "in"))
            if outp is not None:
                out.append((node, outp, "out"))
            if i == last and dst_own and dst_own != inp:
                out.append((node, dst_own, "in"))
        return out

    def _own(self, node: str, port: str | None, vlan: int) -> str | None:
        n = self.nodes[node]
        if n.kind in ROUTERS:
            return self.l3_iface(node, port, vlan)
        if n.kind == "hypervisor":
            return self.vlan_port(n, vlan)
        return self.own_port(n)

    def gw_node(self, site_gw: str, b: int) -> str:
        if site_gw == "fw":
            return ("fw-hq-01", "fw-hq-02")[int(self.master[b])]
        return site_gw

    def _between(
        self, a: str, z: str, vlan: int, b: int, a_port: str | None = None
    ) -> list[tuple[str, str, str]] | None:
        an = self.nodes[a]
        dom = None
        if an.kind == "hypervisor":
            dom = self.domain.get((a, self.vlan_port(an, vlan)))
            if dom == "default":
                dom = None
            vp = an.ports[self.vlan_port(an, vlan)]
            if vp.kind == "vlan" and vp.parent:
                dom = vp.parent
        leg = self.leg(a, z, vlan, b, a_port=a_port, a_dom=dom)
        if leg is None:
            return None
        first_out = leg.hops[0][2]
        last_in = leg.hops[-1][1]
        src_own = None if a_port else self._own(a, first_out, vlan)
        dst_own = self._own(z, last_in, vlan)
        return self.count(leg, src_own, dst_own)

    def route(
        self, src: str, dst: str, b: int, l2: bool = False, vlan: int | None = None
    ) -> list[tuple[str, str, str]] | None:
        """Every port a flow crosses at step b, with the direction of its forward traffic."""
        s = self.nodes[src]
        out: list[tuple[str, str, str]] = []
        if dst != "internet":
            d = self.nodes[dst]
            if l2 or vlan is not None:
                return self._between(src, dst, vlan if vlan is not None else s.vlan, b)
            if s.vlan == d.vlan and s.site == d.site:
                return self._between(src, dst, s.vlan, b)
            r1 = self.gw_node("lab-fw01" if s.site == "lab" else self.site_gw(s), b)
            r2 = self.gw_node("lab-fw01" if d.site == "lab" else self.site_gw(d), b)
            first = self._between(src, r1, s.vlan, b)
            if first is None:
                return None
            out += first
            if r1 != r2 and not self.tunnel(r1, r2, b, out):
                return None
            last = self._between(r2, dst, d.vlan, b)
            if last is None:
                return None
            return out + last
        r = self.gw_node("lab-fw01" if s.site == "lab" else self.site_gw(s), b)
        if src != r:
            first = self._between(src, r, s.vlan, b)
            if first is None:
                return None
            out += first
        if r == "lab-fw01":
            fw = self.gw_node("fw", b)
            mid = self._between("lab-fw01", fw, 120, b)
            if mid is None:
                return None
            out += mid
            r = fw
        self.wan(r, b, out)
        return out

    def wan(self, r: str, b: int, out: list[tuple[str, str, str]]) -> None:
        if r.startswith("fw-hq"):
            if self.fiber_up[b]:
                out.append((r, "pppoe0", "out"))
                path = self._between(r, "ont-fiber", 900, b, a_port="igc0")
            else:
                path = self._between(r, "lte-gw01", 901, b, a_port="igc1")
            out += path or []
        elif r == "br-gw01":
            out += [("br-gw01", "ether1", "out"), ("br-ont", "eth0", "in")]
        elif r == "store-rt01":
            out += [("store-rt01", "eth1", "out"), ("store-modem", "eth0", "in")]

    def tunnel(self, r1: str, r2: str, b: int, out: list[tuple[str, str, str]]) -> bool:
        """From router r1 to router r2: the lab router is behind HQ; the branch and the store use tunnels."""
        if "lab-fw01" in (r1, r2):
            other = r2 if r1 == "lab-fw01" else r1
            if not other.startswith("fw-hq"):
                return False
            path = self._between(r1, r2, 120, b)
            if path is None:
                return False
            out += path
            return True
        remote = r1 if not r1.startswith("fw-hq") else r2
        hq = r2 if remote == r1 else r1
        if not hq.startswith("fw-hq") or remote not in ("br-gw01", "store-rt01"):
            return False
        t = "wg-branch" if remote == "br-gw01" else "ipsec-store"
        if not self.tunnel_up[t][b]:
            return False
        seq: list[tuple[str, str, str]] = []
        if remote == "br-gw01":
            seq += [("br-gw01", "wg-hq", "out"), ("br-gw01", "ether1", "out"), ("br-ont", "eth0", "in")]
        else:
            seq += [("store-rt01", "eth1", "out"), ("store-modem", "eth0", "in")]
        if self.fiber_up[b]:
            seq += [
                ("ont-fiber", "eth0", "out"),
                ("core-sw01", "Tw1/0/2", "in"),
                ("core-sw01", "Tw1/0/1" if hq == "fw-hq-01" else "Tw2/0/1", "out"),
                (hq, "igc0", "in"),
                (hq, "pppoe0", "in"),
            ]
        else:
            seq += [
                ("lte-gw01", "eth0", "out"),
                ("core-sw01", "Tw2/0/2", "in"),
                ("core-sw01", "Tw1/0/3" if hq == "fw-hq-01" else "Tw2/0/3", "out"),
                (hq, "igc1", "in"),
            ]
        seq.append((hq, "wg0" if remote == "br-gw01" else "enc0", "in"))
        if remote != r1:  # HQ → remote: the same ports, the other way
            seq = [(n, p, "in" if d == "out" else "out") for n, p, d in reversed(seq)]
        out += seq
        return True

    def l3_iface(self, router: str, port: str | None, vlan: int) -> str | None:
        """The router's L3 interface for traffic entering on a port in a VLAN."""
        n = self.nodes[router]
        if port is None:
            return None
        p = n.ports[port]
        dom = self.domain.get((router, port))
        for q in n.ports.values():
            if q.kind == "vlan" and q.vlan == vlan and (q.parent == port or q.parent == dom):
                return q.name
        if p.ips and p.carries(vlan):
            return port
        for q in n.ports.values():
            if q.kind == "bridge" and port in q.members and q.ips:
                return q.name
        return port

    # ---------------------------------------------------------------- queries

    def fiber_rx_dbm_array(self) -> np.ndarray:
        """Rx power of the failing fiber (sw-stor-01 sfp28-16): -4 dBm on Monday, -14.5 by Sunday night."""
        frac = (np.arange(NB) * STEP) / WEEK
        return -4.0 - 10.5 * frac

    def at(self, t: float, speed: float = 1.0) -> Snapshot:
        return Snapshot(self, t, speed)

    def integral(self, arr: np.ndarray, key: tuple, t: float) -> float:
        """∫ arr from WEEK0 to t (bytes), arr being a weekly step function."""
        cum = self.cum.get(key)
        if cum is None:
            cum = np.concatenate(([0.0], np.cumsum(arr) * STEP))
            self.cum[key] = cum
        w = (t - WEEK0) // WEEK
        s = wsec(t)
        b = min(int(s // STEP), NB - 1)
        return w * cum[-1] + cum[b] + arr[b] * (s - b * STEP)

    def boot_time(self, name: str, t: float) -> float:
        """The last boot of a node at or before t."""
        n = self.nodes[name]
        if n.host:
            if len(self.vm_hosts.get(name, ())) > 1:
                return self._first_boot(n)  # live-migrated while its node rebooted: still running
            return max(self.boot_time(n.host, t), self._first_boot(n))  # stopped with its node
        best = self._first_boot(n)
        w0 = t - wsec(t)
        for s0, dur in self.reboots.get(name, []):
            for k in (0, -1):
                tb = w0 + k * WEEK + s0 + dur
                if tb <= t and tb > best:
                    best = tb
        return best

    def _first_boot(self, n: Node) -> float:
        if n.boot:
            return n.boot
        return 1788000000 + h01("boot", n.name) * 2_000_000  # some time in Aug/Sep 2026

    def is_down(self, name: str, t: float) -> bool:
        parent = self.mesh_parent.get(name)
        if parent and self.is_down(parent, t):
            return True  # meshed over Wi-Fi through a unit that is rebooting
        arr = self.down.get(name)
        if arr is None:
            n = self.nodes[name]
            if n.host:
                h = self.host_at(name, step_of(t))
                return bool(self.down.get(h, np.zeros(NB, dtype=bool))[step_of(t)])
            return False
        s = wsec(t)
        return any(s0 <= s < s0 + dur for s0, dur in self.reboots[name])


class Snapshot:
    """The network at one instant. Everything vendor emulators need."""

    def __init__(self, world: World, t: float, speed: float = 1.0) -> None:
        self.w = world
        self.t = float(t)
        self.speed = max(float(speed), 1e-9)
        self.b = step_of(t)
        self.s = wsec(t)
        self.design = world.d
        self.nodes = world.nodes

    # -- time and events --
    def active(self, event: str) -> bool:
        return bool(self.w.events[event][self.b])

    @property
    def events(self) -> set[str]:
        return {k for k, v in self.w.events.items() if v[self.b]}

    def local(self) -> tuple[int, int, int]:
        """(weekday, hour, minute) local."""
        return int(self.s // 86400), int(self.s % 86400 // 3600), int(self.s % 3600 // 60)

    # -- nodes --
    def node(self, name: str) -> Node:
        return self.nodes[name]

    def up(self, name: str) -> bool:
        return not self.w.is_down(name, self.t) and self.present(name)

    def present(self, name: str) -> bool:
        return bool(self.w.presence[name][self.b])

    def boot(self, name: str) -> float:
        return self.w.boot_time(name, self.t)

    def uptime(self, name: str) -> int:
        return int(self.t - self.boot(name))

    def _run(self, arr: np.ndarray) -> tuple[float, float] | None:
        """(start, end) of the current run of True steps, or of the last one; None if never."""
        key = (id(arr), self.b)
        hit = self.w.runs.get(key)
        if hit is not None:
            start_off, end_off = hit
        else:
            if not arr.any():
                return None
            if arr.all():
                return (float("-inf"), float("inf"))
            k = 0
            while not arr[(self.b - k) % NB]:
                k += 1
            j = k
            while arr[(self.b - j) % NB]:
                j += 1
            start_off, end_off = j - 1, k - 1  # in steps before the current step's start
            self.w.runs[key] = (start_off, end_off)
        t0 = self.t - (self.s % STEP)
        end = self.t if end_off < 0 else t0 - end_off * STEP
        return t0 - start_off * STEP, end

    def last_seen(self, name: str) -> float | None:
        """When the node was last on the network (now when present), within a week."""
        r = self._run(self.w.presence[name])
        if r is None:
            return None
        return min(r[1], self.t)

    def arrived(self, name: str) -> float:
        """When the node's current (or last) presence started (its boot when always there)."""
        r = self._run(self.w.presence[name])
        if r is None or r[0] == float("-inf"):
            return self.boot(name)
        return max(r[0], self.boot(name))

    def master(self) -> str:
        return ("fw-hq-01", "fw-hq-02")[int(self.w.master[self.b])]

    def carp_state(self, fw: str) -> str:
        if self.w.is_down(fw, self.t):
            return "INIT"
        return "MASTER" if self.master() == fw else "BACKUP"

    def host_of(self, guest: str) -> str | None:
        return self.w.host_at(guest, self.b)

    def guests(self, host: str) -> list[Node]:
        return [
            self.nodes[g]
            for g in self.w.vm_host
            if self.w.host_at(g, self.b) == host and not self.w.is_down(host, self.t)
        ]

    # -- ports --
    def port(self, node: str, port: str) -> PortState:
        w = self.w
        n = self.nodes[node]
        p = n.ports[port]
        key = (node, port)
        boot = self.boot(node)
        rx_arr, tx_arr = w.rx.get(key), w.tx.get(key)
        rx = tx = 0.0
        if rx_arr is not None:
            rx = (
                w.integral(rx_arr, ("rx", node, port), self.t) - w.integral(rx_arr, ("rx", node, port), boot)
            ) / self.speed
        if tx_arr is not None:
            tx = (
                w.integral(tx_arr, ("tx", node, port), self.t) - w.integral(tx_arr, ("tx", node, port), boot)
            ) / self.speed
        errs = 0.0
        if key in w.err:
            errs = (
                w.integral(w.err[key], ("err", node, port), self.t) - w.integral(w.err[key], ("err", node, port), boot)
            ) / self.speed
        base = 900.0 if p.kind not in ("sfp", "lag") else 1200.0
        rx_rate = float(rx_arr[self.b]) if rx_arr is not None else 0.0
        tx_rate = float(tx_arr[self.b]) if tx_arr is not None else 0.0
        up = self.port_up(node, port)
        bc = 64 * 1500  # a little broadcast keeps idle ports' counters moving
        live = max(self.t - boot, 0) / self.speed
        if up and p.kind in ("ethernet", "sfp", "lag"):
            rx += live * 0.02 * bc / 1500
            tx += live * 0.05 * bc / 1500
        return PortState(
            up=up,
            speed=self.port_speed(node, port) if up else None,
            rx_bytes=int(rx),
            tx_bytes=int(tx),
            rx_packets=int(rx / base),
            tx_packets=int(tx / base),
            rx_errors=int(errs),
            tx_errors=0,
            rx_rate=rx_rate if up else 0.0,
            tx_rate=tx_rate if up else 0.0,
        )

    def peer(self, node: str, port: str) -> tuple[Node, Port] | None:
        """The node and port at the other end of a cable (or Wi-Fi mesh link)."""
        pp = self.w.peer.get((node, port))
        if pp:
            n = self.nodes[pp[0]]
            if n.host and self.host_of(n.name) != node and self.nodes[node].kind == "hypervisor":
                return None
            return n, n.ports[pp[1]]
        n = self.nodes[node]
        if n.kind == "hypervisor":
            for g in self.guests(node):
                if g.host != node and self.w.tap_name(g, node) == port:
                    return g, g.ports[self.w.own_port(g)]
        return None

    def port_up(self, node: str, port: str) -> bool:
        n = self.nodes[node]
        p = n.ports[port]
        if not p.admin_up or not self.up(node):
            return False
        if p.kind in ("vlan", "bridge", "loopback", "wg", "ipsec"):
            if p.kind == "wg":
                return bool(self.w.tunnel_up["wg-branch"][self.b])
            return True
        if p.kind == "ppp":
            return node == self.master() and not self.active("fiber_down")
        if p.kind == "wifi":
            if p.descr.startswith("mesh"):
                pr = self.peer(node, port)
                return bool(pr and self.up(pr[0].name))
            return True
        if p.kind == "lag":
            return any(self.port_up(node, m) for m in p.members)
        pr = self.peer(node, port)
        if pr is None:
            return False
        other = pr[0]
        if other.kind not in BRIDGING and other.kind not in ROUTERS and not self.present(other.name):
            return False
        return not self.w.is_down(other.name, self.t)

    def port_speed(self, node: str, port: str) -> int | None:
        p = self.nodes[node].ports[port]
        if p.kind == "lag":
            return sum(self.port_speed(node, m) or 0 for m in p.members if self.port_up(node, m)) or None
        pr = self.peer(node, port)
        if pr is None or p.speed is None:
            return p.speed
        if pr[1].speed is None:
            return p.speed
        return min(p.speed, pr[1].speed)

    # -- Wi-Fi --
    def ap_of(self, client: str) -> str | None:
        if not self.present(client):
            return None
        ap = self.w.attach_at(client, self.b)
        if ap and not self.up(ap):
            return None
        return ap

    def assoc(self, client: str) -> Assoc | None:
        ap = self.ap_of(client)
        if ap is None:
            return None
        w = self.w
        c = self.nodes[client]
        apn = self.nodes[ap]
        radio = w.radio_for(c, apn)
        band = apn.ports[radio].band or "5"
        ssid = self.design.wifi[client].ssid
        base = -47 - int(h01("sig", client, ap) * 22)
        if ap == "ap-patio-01":
            base = -76 - int(h01("sig", client, ap) * 6)
        if ap == "halo-wh-03":
            base -= 6
        signal = base + int((h01("sig-step", client, self.b) - 0.5) * 6)
        quality = max(0.05, min(1.0, (signal + 90) / 45))
        top = {"2.4": 287, "5": 1201, "6": 2402}.get(band, 866)
        if c.kind in ("iot", "scanner") or c.traffic == "iot":
            top = 72 if band == "2.4" else 433
        tx_rate = max(6, int(top * quality))
        rx_rate = max(6, int(tx_rate * (0.7 + 0.3 * h01("rx", client, self.b))))
        # Association started when the client last moved to this AP (or the AP rebooted).
        key = ("assoc", client)
        same = w.assoc_masks.get((client, ap))
        if same is None:
            same = w.attach_idx[client] == w.attach_aps[client].index(ap)
            w.assoc_masks[(client, ap)] = same
        r = self._run(same)
        since = r[0] if r and r[0] != float("-inf") else self.boot(ap)
        since = max(since, self.boot(ap))
        _ = key
        own = w.own_port(c)
        st = self.port(client, own)
        assoc_rx = st.tx_bytes - self._port_at(client, own, since).tx_bytes
        assoc_tx = st.rx_bytes - self._port_at(client, own, since).rx_bytes
        chan = {"2.4": [1, 6, 11], "5": [36, 52, 100, 116, 132, 149], "6": [37, 69, 101]}.get(band, [36])
        channel = chan[int(h01("chan", ap, band) * len(chan))]
        return Assoc(
            c,
            ap,
            radio,
            band,
            ssid,
            signal,
            -95 if band != "2.4" else -92,
            tx_rate,
            rx_rate,
            since,
            channel,
            max(assoc_rx, 0),
            max(assoc_tx, 0),
            st.tx_rate,
            st.rx_rate,
        )

    def _port_at(self, node: str, port: str, t: float) -> PortState:
        return Snapshot(self.w, t, self.speed).port(node, port)

    def wifi_clients(self, ap: str) -> list[Assoc]:
        out = []
        for c in self.w.wifi_by_home.get(ap, ()):
            if self.ap_of(c) == ap:
                a = self.assoc(c)
                if a:
                    out.append(a)
        return out

    # -- L2 / L3 tables --
    def endpoints(self) -> list[Node]:
        """Every node with an address that is on the network now (or recently)."""
        return [n for n in self.nodes.values() if n.mac]

    def location(self, name: str) -> tuple[str, str] | None:
        """(node, port) where a node plugs in now: its switch port, AP radio or hypervisor tap."""
        n = self.nodes[name]
        if name in self.design.wifi:
            ap = self.ap_of(name)
            if not ap:
                return None
            return ap, self.w.radio_for(n, self.nodes[ap])
        if n.host:
            h = self.host_of(name)
            if h is None:
                return None
            return h, self.w.tap_name(n, h) if h != n.host else next(
                pp for pp, peer, _ in self.w.adj[h] if peer == name
            )
        own = self.w.own_port(n)
        pr = self.w.peer.get((name, own))
        return pr

    def macs_on_network(self, max_age: float) -> list[tuple[Node, str, int, str]]:
        """(node, mac, vlan, ip) seen within max_age seconds."""
        out = []
        for n in self.nodes.values():
            if not n.mac or (n.kind in ("switch",) and n.extra.get("unmanaged")):
                continue
            if self.w.is_down(n.name, self.t):
                continue
            ls = self.last_seen(n.name)
            if ls is None or self.t - ls > max_age:
                continue
            out.append((n, n.mac, n.vlan or 0, n.ip or ""))
        return out

    def fdb(self, switch: str, max_age: float = 300) -> list[tuple[str, str, int]]:
        """The switch's MAC table now: (mac, port, vlan)."""
        out = []
        sw = self.nodes[switch]
        if not self.up(switch):
            return out
        w = self.w
        hq_firewalls = ("fw-hq-01", "fw-hq-02")
        for n, m, vlan, _ip in self.macs_on_network(max_age):
            # The firewalls speak from their interfaces' MACs (below), not the chassis'.
            if n.name == switch or n.name in hq_firewalls:
                continue
            port = self.port_toward(switch, n.name, vlan)
            if port:
                out.append((m, port, vlan))
        # Routers' and CARP's MACs per VLAN.
        for fw in hq_firewalls:
            if sw.site != "hq" or not self.up(fw):
                continue
            # The WAN ports, in their VLANs through the core: PPPoE only on the
            # CARP master (the backup keeps its dial-up disconnected), the 5G
            # link on both (each has its own address there).
            wans = [("igc1", 901)] + ([("igc0", 900)] if fw == self.master() else [])
            for pname, v in wans:
                wp = self.nodes[fw].ports[pname]
                port = self.port_toward(switch, fw, v) if self.port_up(fw, pname) else None
                if port and wp.mac:
                    out.append((wp.mac, port, v))
            lag_mac = self.nodes[fw].ports["lagg0"].mac
            for v in self.nodes[fw].ports["lagg0"].tagged:
                port = self.port_toward(switch, fw, v)
                if port:
                    out.append((lag_mac, port, v))
                    if fw == self.master():
                        out.append((acme.carp_mac(v), port, v))
            dmz = self.nodes[fw].ports["igc2"]  # the DMZ is a physical port of each firewall
            port = self.port_toward(switch, fw, 90) if self.port_up(fw, "igc2") else None
            if port:
                out.append((dmz.mac, port, 90))
                if fw == self.master():
                    out.append((acme.carp_mac(90), port, 90))
        _ = w
        return sorted(set(out))

    def port_toward(self, switch: str, target: str, vlan: int) -> str | None:
        """The port of `switch` that leads to `target` in a VLAN (None when not reachable)."""
        leg = self.w.leg(switch, target, vlan, self.b, a_port=None)
        if leg is None or len(leg.hops) < 2:
            return None
        return leg.hops[0][2]

    def arp(self, router: str, max_age: float = 1200) -> list[tuple[str, str, str]]:
        """A router's ARP table: (ip, mac, L3 interface)."""
        r = self.nodes[router]
        if not self.up(router):
            return []
        out = []
        ifaces = {}
        for p in r.ports.values():
            for a in p.ips:
                try:
                    net = ipaddress.ip_interface(a).network
                except ValueError:
                    continue
                if net.prefixlen < 32:
                    ifaces[p.name] = net
        backup = r.kind == "firewall" and r.name.startswith("fw-hq") and self.master() != router
        for n, m, _vlan, ip in self.macs_on_network(max_age):
            if not ip or n.name == router:
                continue
            addr = ipaddress.ip_address(ip)
            for pname, net in ifaces.items():
                if addr in net:
                    if backup and not (n.kind in ("switch", "firewall", "hypervisor") or n.name in ("lte-gw01",)):
                        break
                    out.append((ip, m, pname))
                    break
        # Each other node's own interface addresses (switch SVIs, NAS extra IPs, the peer firewall).
        for n in self.nodes.values():
            if n.name == router or not self.up(n.name) or not self.present(n.name):
                continue
            for p in n.ports.values():
                for a in p.ips:
                    ip = a.split("/")[0]
                    if ip == n.ip or ip.startswith("127.") or not p.mac:
                        continue
                    addr = ipaddress.ip_address(ip)
                    for pname, net in ifaces.items():
                        if (addr in net and net.prefixlen < 31) or (addr in net and pname == "igc3"):
                            out.append((ip, p.mac, pname))
                            break
        # HQ's gateways are CARP addresses: other routers in those VLANs (the lab router) learn their virtual MACs.
        if not router.startswith("fw-hq") and self.up(self.master()):
            for (site, vid), v in self.design.vlans.items():
                if site != "hq" or not v.gateway:
                    continue
                for pname, net in ifaces.items():
                    if ipaddress.ip_address(v.gateway) in net:
                        out.append((v.gateway, acme.carp_mac(vid), pname))
        return sorted(set(out), key=lambda x: ipaddress.ip_address(x[0]))

    def scope_of(self, n: Node) -> tuple[str, int] | None:
        v = self.design.vlans.get((n.site, n.vlan or -1)) or self.design.vlans.get(("hq", n.vlan or -1))
        return (v.site, v.id) if v else None

    def leases(self, site: str) -> list[Lease]:
        """Active DHCP leases of a site (HQ: Kea on the firewalls)."""
        out = []
        for n in self.nodes.values():
            if not n.dhcp or not n.ip:
                continue
            sc = self.scope_of(n)
            if not sc or sc[0] != site:
                continue
            v = self.design.vlans[sc]
            lease_time = {80: 3600, 32: 28800, 34: 28800}.get(v.id, 86400)
            if site == "store":
                lease_time = 43200
            if site == "lab":
                lease_time = 7200
            pres = self.w.presence[n.name]
            if pres.all():
                start = self.t - (self.t + h01("lease", n.name) * lease_time) % (lease_time / 2)
            else:
                ls = self.last_seen(n.name)
                if ls is None:
                    continue
                start = self.arrived(n.name)
                start = max(start, ls - lease_time / 2)
                if not pres[self.b] and self.t > start + lease_time:
                    continue
            out.append(Lease(n, n.ip, n.mac, n.hostname, start, start + lease_time, v.id, n.static_lease))
        return sorted(out, key=lambda x: ipaddress.ip_address(x.ip))

    def lldp(self, name: str) -> list[Neighbor]:
        """LLDP (and LLDP-MED) neighbors a node hears now, per local port."""
        n = self.nodes[name]
        if not n.lldp or not self.up(name):
            return []
        out = []
        for (a, pa), (b, pb) in self.w.peer.items():
            if a != name:
                continue
            other = self.nodes[b]
            p = n.ports[pa]
            if p.kind == "lag" or p.kind in ("tap", "veth", "virtual") or other.kind in ("vm", "lxc"):
                continue
            if not other.lldp or not self.up(b) or not self.port_up(name, pa):
                continue
            out.append(Neighbor(pa, other, other.ports[pb], other.ip))
        return sorted(out, key=lambda x: (n.ports[x.local_port].index, x.local_port))

    # -- health --
    def cpu(self, name: str) -> float:
        n = self.nodes[name]
        biz = float(self.w.business[self.b])
        base = {"firewall": 6, "router": 4, "switch": 7, "ap": 4, "hypervisor": 18, "vm": 8, "lxc": 2, "nas": 9}.get(
            n.kind, 5
        )
        load = base + 22 * biz * (0.6 + 0.8 * h01("cpu", name))
        if name in ("erp-app01", "pve01") and self.active("erp_batch"):
            load = 96 if name == "erp-app01" else 81
        if n.kind == "hypervisor" and self.active("backup_vzdump"):
            load += 25
        if name.startswith("fw-hq") and self.master() != name:
            load = 3
        load += (h01("cpu-step", name, self.b) - 0.5) * 6
        return round(max(1.0, min(99.0, load)), 1)

    def mem(self, name: str) -> float:
        n = self.nodes[name]
        base = {"firewall": 34, "router": 28, "switch": 41, "ap": 52, "hypervisor": 62, "nas": 47}.get(n.kind, 40)
        if name == "docker01":
            base = 93
        if name == "erp-db01":
            base = 88
        return round(base + 8 * h01("mem", name) + (h01("mem-step", name, self.b) - 0.5) * 2, 1)

    def temperature(self, name: str, sensor: str = "cpu") -> float:
        n = self.nodes[name]
        base = {"firewall": 48, "router": 52, "switch": 44, "ap": 58, "hypervisor": 46, "nas": 38}.get(n.kind, 45)
        t = base + 10 * self.cpu(name) / 100 + h01("temp", name, sensor) * 4
        if name == "nas01" and sensor == "disk3" and self.active("nas_disk_hot"):
            t = 61 + h01("hot", self.b) * 3
        return round(t, 1)

    def nas_used_pct(self) -> float:
        """nas01's volume fills through the week (a cleanup job runs on Monday 00:00)."""
        return round(82.0 + 9.5 * self.s / WEEK, 2)

    def fiber_rx_dbm(self) -> float:
        return round(float(self.w.fiber_rx_dbm_array()[self.b]), 2)

    def gateway(self, which: str) -> dict[str, Any]:
        """Gateway monitoring at HQ: which = "fiber" | "lte"."""
        if which == "fiber":
            if self.active("fiber_down"):
                return {"up": False, "latency_ms": None, "loss_pct": 100.0, "status": "down"}
            return {
                "up": True,
                "latency_ms": round(3.1 + h01("lat", self.b) * 1.6, 2),
                "loss_pct": 0.0,
                "status": "online",
            }
        if self.active("lte_degraded"):
            return {
                "up": True,
                "latency_ms": round(180 + h01("lat-lte", self.b) * 90, 1),
                "loss_pct": round(8 + h01("loss", self.b) * 7, 1),
                "status": "degraded",
            }
        return {
            "up": True,
            "latency_ms": round(34 + h01("lat-lte", self.b) * 12, 1),
            "loss_pct": 0.0,
            "status": "online",
        }

    def tunnel(self, name: str) -> dict[str, Any]:
        """wg-branch (WireGuard HQ ↔ Campinas) or ipsec-store (IPsec HQ ↔ Santos)."""
        up = bool(self.w.tunnel_up[name][self.b])
        hq = self.master()
        if name == "wg-branch":
            st = self.port(hq, "wg0")
            last = self.t - (self.s % 90) if up else self.t - (self.s % 86400 - 16 * 3600)
        else:
            st = self.port(hq, "enc0")
            last = self.t - (self.s % 3600) if up else None
        return {"up": up, "rx_bytes": st.rx_bytes, "tx_bytes": st.tx_bytes, "last_handshake": last}

    def reachable(self, api: str) -> bool:
        """Whether Omini (in HQ's MGMT VLAN) can reach an emulated API now."""
        spec = acme.APIS[api]
        node = spec["node"]
        tun = acme.BEHIND.get(api)
        if tun and not self.w.tunnel_up[tun][self.b]:
            return False
        if self.w.is_down(node, self.t):
            return False
        n = self.nodes[node]
        return not (n.host and self.w.is_down(self.host_of(node) or n.host, self.t))

    def firmware(self, name: str) -> dict[str, Any]:
        """Installed and pending firmware (the OPNsense pair updates on Saturday nights)."""
        n = self.nodes[name]
        week = week_index(self.t)
        if name.startswith("fw-hq"):
            done = (5, 22.0 if name == "fw-hq-02" else 22.5)
            day, hour = self.s // 86400, (self.s % 86400) / 3600
            updated = (day, hour) >= (done[0], done[1] + 0.1)
            patch = 2 + week + (1 if updated else 0)
            cur = f"26.7.{patch}"
            pending = (day, hour) >= (0, 9.0) and not updated
            return {
                "current": cur,
                "latest": f"26.7.{patch + 1}" if pending else cur,
                "pending": pending,
                "needs_reboot": pending,
            }
        return {"current": n.version, "latest": n.version, "pending": False, "needs_reboot": False}


@dataclass
class Clock:
    """Simulated time: sim = start + (real - real_start) × speed, or frozen at a fixed instant."""

    start: float
    speed: float = 60.0
    real_start: float = 0.0
    frozen: float | None = None
    extra: dict[str, Any] = field(default_factory=dict)

    def now(self, real: float) -> float:
        if self.frozen is not None:
            return self.frozen
        return self.start + (real - self.real_start) * self.speed
