"""Acme's traffic as its HQ firewall exports it: NetFlow v5 to Omini.

Every few seconds the active HQ firewall (the CARP master) sends the
conversations it routes at that instant, from the world's flows: each
device's internet traffic split over the places its kind of device goes
(streaming for TVs, work apps for laptops, the vendor's cloud for cameras),
and the traffic between VLANs. Traffic inside a VLAN never reaches the
firewall and is not exported, as on a real network.

The places are made-up names on documentation addresses (RFC 5737). AdGuard
Home's query log answers the same names with the same addresses (see
``lookups``), so Omini can name where devices go.
"""

from __future__ import annotations

import asyncio
import hashlib
import logging
import os
import socket
import struct
import time
from typing import Any

from .world import Snapshot, World

log = logging.getLogger(__name__)

# Seconds between exports (real time).
EVERY = 10.0
# Where each kind of device goes on the internet: (name, port, share).
PLACES: dict[str, list[tuple[str, int, float]]] = {
    "laptop": [
        ("outlook.office365.com", 443, 0.3),
        ("teams.microsoft.com", 443, 0.25),
        ("github.com", 443, 0.15),
        ("www.google.com", 443, 0.15),
        ("slack.com", 443, 0.15),
    ],
    "desktop": [("outlook.office365.com", 443, 0.5), ("www.google.com", 443, 0.3), ("erp.acme-cloud.com", 443, 0.2)],
    "mobile": [
        ("whatsapp.net", 443, 0.4),
        ("instagram.com", 443, 0.35),
        ("www.youtube.com", 443, 0.25),
    ],
    "guest": [("www.youtube.com", 443, 0.5), ("www.netflix.com", 443, 0.3), ("instagram.com", 443, 0.2)],
    "tv": [("www.netflix.com", 443, 0.7), ("www.youtube.com", 443, 0.3)],
    "camera-cloud": [("cloud.camvendor.com", 443, 1.0)],
    "nvr": [("cloud.camvendor.com", 443, 1.0)],
    "iot": [("a.tuyaus.com", 8883, 0.7), ("time.cloudflare.com", 123, 0.3)],
    "light": [("a.tuyaus.com", 8883, 1.0)],
    "printer": [("printer.hp.com", 443, 1.0)],
    "pos": [("api.stripe.com", 443, 1.0)],
    "dmz-web": [("cdn.cloudflare.com", 443, 1.0)],
    "dmz-mail": [("mx.mailprovider.com", 25, 1.0)],
    "dc": [("time.windows.com", 123, 0.3), ("update.microsoft.com", 443, 0.7)],
    "erp": [("erp.acme-cloud.com", 443, 1.0)],
    "file": [("backup.cloudvault.com", 443, 1.0)],
    "nas": [("backup.cloudvault.com", 443, 1.0)],
    "lab": [("github.com", 443, 0.5), ("registry-1.docker.io", 443, 0.5)],
}
OTHER: list[tuple[str, int, float]] = [("www.google.com", 443, 1.0)]
# The services between VLANs: (destination node → port, protocol).
SERVICES: dict[str, tuple[int, int]] = {
    "file01": (445, 6),
    "erp-app01": (443, 6),
    "pbx01": (5060, 17),
    "print01": (631, 6),
    "homeassistant": (8123, 6),
    "nvr01": (554, 6),
    "unifi01": (8080, 6),
    "bkp-nas01": (445, 6),
}
# One TV asks Google's DNS directly, going around AdGuard Home (the dns_bypass alert).
BYPASS = ("8.8.8.8", 53)
_NETS = ((198, 51, 100), (203, 0, 113), (192, 0, 2))


def address(name: str) -> str:
    """A place's address: stable, on a documentation network."""
    h = hashlib.sha1(name.encode()).digest()
    a, b, c = _NETS[h[0] % len(_NETS)]
    return f"{a}.{b}.{c}.{1 + h[1] % 254}"


def places(world: World, name: str) -> list[tuple[str, int, float]]:
    return PLACES.get(world.nodes[name].traffic or "", OTHER)


def exported(world: World, snap: Snapshot) -> list[tuple[str, str, int, int, int, int, int]]:
    """The conversations the HQ firewall routes now: (src, dst, proto, sport,
    dport, bytes/s src → dst, bytes/s dst → src)."""
    out = []
    for f in getattr(world, "flows", []):
        src = world.nodes[f.src]
        if f.l2 or world.site_gw(src) != "fw" or not src.ip or not snap.up(f.src):
            continue
        up, down = float(f.fwd[snap.b]), float(f.rev[snap.b])
        if up + down < 1:
            continue
        sport = 40000 + int(hashlib.sha1(f"{f.src}{f.dst}".encode()).digest()[0]) * 50
        if f.dst == "internet":
            for i, (place, port, share) in enumerate(places(world, f.src)):
                proto = 17 if port == 123 else 6
                out.append((src.ip, address(place), proto, sport + i, port, int(up * share), int(down * share)))
            continue
        dst = world.nodes.get(f.dst)
        if dst is None or not dst.ip or not snap.up(f.dst):
            continue
        port, proto = SERVICES.get(f.dst, (443, 6))
        out.append((src.ip, dst.ip, proto, sport, port, int(up), int(down)))
    tv = next((n for n in world.nodes.values() if n.traffic == "tv" and n.ip and snap.up(n.name)), None)
    if tv is not None:
        out.append((tv.ip, BYPASS[0], 17, 53000, BYPASS[1], 120, 300))
    return out


def lookups(world: World, snap: Snapshot) -> list[dict[str, Any]]:
    """AdGuard Home's query log: each device's lookups of the places it goes,
    newest first, answered with their addresses."""
    rows = []
    now = time.strftime("%Y-%m-%dT%H:%M:%S.000Z", time.gmtime())
    seen = set()
    for src, dst, _proto, _sport, _dport, _up, _down in exported(world, snap):
        for name in {p for kind in PLACES.values() for p, _, _ in kind} | {p for p, _, _ in OTHER}:
            if address(name) == dst and (src, name) not in seen:
                seen.add((src, name))
                rows.append(
                    {
                        "client": src,
                        "question": {"name": name, "type": "A", "class": "IN"},
                        "answer": [{"type": "A", "value": dst, "ttl": 300}],
                        "status": "NOERROR",
                        "reason": "NotFilteredNotFound",
                        "time": now,
                        "elapsedMs": "12.5",
                        "upstream": "10.10.20.10:53",
                    }
                )
    return rows[:1000]


def v5(exporter_seq: int, records: list[tuple[str, str, int, int, int, int]]) -> bytes:
    """A NetFlow v5 packet: (src, dst, proto, sport, dport, bytes) records."""
    now = time.time()
    head = struct.pack("!HHIIIIBBH", 5, len(records), 0, int(now), 0, exporter_seq, 0, 0, 0)
    body = b""
    for src, dst, proto, sport, dport, octets in records:
        body += struct.pack(
            "!4s4s4sHHIIIIHHBBBBHHBBH",
            socket.inet_aton(src),
            socket.inet_aton(dst),
            b"\0\0\0\0",
            0,
            0,
            max(1, octets // 1200),
            min(octets, 2**32 - 1),
            0,
            0,
            sport,
            dport,
            0,
            0,
            proto,
            0,
            0,
            0,
            0,
            0,
            0,
        )
    return head + body


async def export(ctx: Any, world: World) -> None:
    """Sends the HQ firewall's NetFlow to Omini (DEVNET_NETFLOW, default
    127.0.0.1:2055) until cancelled."""
    host, _, port = os.environ.get("DEVNET_NETFLOW", "127.0.0.1:2055").rpartition(":")
    seq = 0
    while True:
        await asyncio.sleep(EVERY)
        try:
            snap = ctx.snapshot()
            fw = snap.node(snap.master())
            recs = []
            for src, dst, proto, sport, dport, up, down in exported(world, snap):
                recs.append((src, dst, proto, sport, dport, int(up * EVERY)))
                recs.append((dst, src, proto, dport, sport, int(down * EVERY)))
            recs = [r for r in recs if r[5] > 0]
            with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as s:
                s.bind((fw.ip, 0))
                for i in range(0, len(recs), 30):
                    chunk = recs[i : i + 30]
                    s.sendto(v5(seq, chunk), (host or "127.0.0.1", int(port)))
                    seq += len(chunk)
        except OSError as e:
            log.warning("netflow export failed: %s", e)
