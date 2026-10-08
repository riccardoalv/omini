"""What Omini's built-in network scan finds on every emulated host.

`services(ctx)` lists the listeners that make the design's hosts look real to
`internal/netscan` and `internal/webui`, from each node's `scan` info
(acme.py): open TCP ports (web pages with their titles, SSH banners, bare
ports that only accept), NetBIOS node status on UDP 137, mDNS (UDP 5353),
SSDP (UDP 1900) with a UPnP description, and the HQ gateway's DNS (PTR and A
records of every host). `Scanner` starts them on a `Hub` and keeps them in step
with the world: a host that is absent or down answers nothing, and its TCP
ports stop listening (a listening socket would accept the handshake).

Kinds in the tuples: "http" / "https" (`ScanHttp`), "tcp" (`RawTcp`: banner or
accept-and-close), "udp" (a `UdpService` bound to that address) and "mcast"
(a responder joined to a multicast group, answering from each host's address).
"""

from __future__ import annotations

import asyncio
import contextlib
import ipaddress
import logging
import re
import socket
import struct
import time
import uuid
from collections.abc import Callable, Iterable
from dataclasses import dataclass, field
from typing import Any

from . import acme
from .model import Node
from .server import Context, HttpService, Request, Response, Service, Snapshot, UdpService, _http_handler

log = logging.getLogger("devnet.scan")

# --------------------------------------------------------------------- policy

# Ports that speak TLS (unless the node's `scan["plain"]` lists them).
TLS_PORTS = {443, 1443, 5001, 6443, 8002, 8006, 8007, 8443, 8531, 9443}
# Ports that are not web servers: they accept the connection (and may send a banner).
RAW_PORTS = {
    21,
    22,
    23,
    25,
    53,
    88,
    110,
    111,
    135,
    139,
    143,
    389,
    445,
    515,
    548,
    554,
    587,
    636,
    1883,
    3128,
    3268,
    3306,
    3389,
    5060,
    5432,
    6053,
    8000,
    8009,
    9100,
    10051,
    29810,
    29811,
    62078,
}
BANNERS = {
    21: "220 ({vendor}) FTP server ready",
    25: "220 {fqdn} ESMTP Postfix (Ubuntu)",
    587: "220 {fqdn} ESMTP Postfix (Ubuntu)",
}
DNS_GATEWAY = ("10.10.10.1", 53)  # HQ's CARP VIP on MGMT: Unbound on the OPNsense pair
NETBIOS_DOMAIN = {"hq": "ACME", "branch": "ACME", "store": "LOJA", "lab": "WORKGROUP"}
DESCRIPTION_PORT = 49152
MDNS_GROUP = ("224.0.0.251", 5353)
SSDP_GROUP = ("239.255.255.250", 1900)
# Sites whose hosts Omini hears over multicast (HQ: the firewalls repeat mDNS and SSDP between VLANs).
MCAST_SITES = {"hq"}

# mDNS: the port each service type advertises.
MDNS_PORTS = {
    "_smb._tcp": 445,
    "_afpovertcp._tcp": 548,
    "_ssh._tcp": 22,
    "_companion-link._tcp": 49153,
    "_airplay._tcp": 7000,
    "_raop._tcp": 7000,
    "_googlecast._tcp": 8009,
    "_sonos._tcp": 1443,
    "_spotify-connect._tcp": 1400,
    "_hap._tcp": 8080,
    "_home-assistant._tcp": 8123,
    "_esphomelib._tcp": 6053,
    "_shelly._tcp": 80,
    "_http._tcp": 80,
    "_ipp._tcp": 631,
    "_ipps._tcp": 443,
    "_pdl-datastream._tcp": 9100,
    "_printer._tcp": 515,
    "_device-info._tcp": 0,
}
# Apple's model identifiers (what `_device-info._tcp` says), by the design's model names.
APPLE_IDS = {"MacBook Pro (14-inch, M4, 2024)": "Mac16,1", "MacBook Air (13-inch, M3, 2024)": "Mac15,12"}
# Where a vendor serves its UPnP description: (port, path).
DESCRIPTIONS = {
    "Sonos, Inc.": (1400, "/xml/device_description.xml"),
    "Google Inc.": (8008, "/ssdp/device-desc.xml"),
    "Samsung Electronics": (9197, "/dmr"),
}
DEVICE_URNS = {"dial": "urn:dial-multiscreen-org:device:dial:1"}

# ------------------------------------------------------------- host helpers


def alive(snap: Snapshot, n: Node) -> bool:
    """Whether Omini (HQ MGMT) gets an answer from this host now."""
    if not snap.up(n.name):
        return False
    w = snap.w
    if n.site == "branch" and not w.tunnel_up["wg-branch"][snap.b]:
        return False
    if n.site == "store" and not w.tunnel_up["ipsec-store"][snap.b]:
        return False
    return not (n.site == "lab" and "lab-fw01" in snap.nodes and not snap.up("lab-fw01"))


def _label(s: str) -> str:
    return re.sub(r"-+", "-", re.sub(r"[^A-Za-z0-9-]", "-", s)).strip("-")


def fqdn(d: Any, n: Node) -> str | None:
    """The host's name in the site's DNS ("pve01.acme.local", "ACME-PC-0123.acme.local")."""
    host = n.hostname or ("" if n.dhcp else n.name)
    if not host:
        return None
    if "." in host:
        return host
    label = _label(host)
    if not label:
        return None
    return f"{label}.{d.sites.get(n.site, {}).get('domain', 'acme.local')}"


def local_name(n: Node) -> str:
    """The host's mDNS name, without ".local"."""
    return _label((n.hostname or n.name).split(".")[0]) or _label(n.name)


def instance_name(n: Node, stype: str) -> str:
    """A service instance name like the device would pick (no dots)."""
    if n.kind == "printer":
        return f"{n.vendor} {n.model} ({n.name})".replace(".", "")
    if stype == "_companion-link._tcp" or n.vendor == "Apple":
        return (n.hostname or n.name).replace("-", " ").replace(".", "")
    if stype in ("_googlecast._tcp", "_airplay._tcp", "_hap._tcp") and n.kind in ("tv", "media"):
        return f"{n.vendor} {n.model}".replace(".", "")
    return local_name(n)


def _uuid(n: Node) -> str:
    return str(uuid.uuid5(uuid.NAMESPACE_DNS, f"{n.name}.devnet.acme"))


def _ssh_banner(n: Node) -> str:
    if n.scan.get("ssh"):
        return n.scan["ssh"]
    o = f"{n.os} {n.vendor}".lower()
    if "ubuntu" in o:
        return "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13"
    if "windows" in o:
        return "SSH-2.0-OpenSSH_for_Windows_9.5"
    if "macos" in o or "apple" in o:
        return "SSH-2.0-OpenSSH_9.9"
    if any(x in o for x in ("debian", "proxmox", "raspberry")) or n.kind in ("hypervisor", "lxc", "vm"):
        return "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u7"
    return "SSH-2.0-OpenSSH_8.4"


# -------------------------------------------------------------- DNS codec

TYPE_A, TYPE_PTR, TYPE_TXT, TYPE_AAAA, TYPE_SRV, TYPE_OPT, TYPE_ANY = 1, 12, 16, 28, 33, 41, 255
CLASS_IN, CACHE_FLUSH = 1, 0x8000


def encode_name(name: str) -> bytes:
    out = b""
    for label in name.rstrip(".").split("."):
        if label:
            b = label.encode()
            out += bytes([len(b)]) + b
    return out + b"\x00"


def decode_name(buf: bytes, off: int) -> tuple[str, int]:
    labels: list[str] = []
    end = None
    for _ in range(128):
        n = buf[off]
        if n & 0xC0 == 0xC0:
            if end is None:
                end = off + 2
            off = ((n & 0x3F) << 8) | buf[off + 1]
            continue
        off += 1
        if n == 0:
            break
        labels.append(buf[off : off + n].decode(errors="replace"))
        off += n
    return ".".join(labels), end if end is not None else off


@dataclass
class RR:
    name: str
    type: int
    rdata: Any  # A: "1.2.3.4"; PTR: name; TXT: [str]; SRV: (prio, weight, port, target); else bytes
    ttl: int = 120
    cls: int = CLASS_IN

    def encode(self) -> bytes:
        if self.type == TYPE_A:
            rd = socket.inet_aton(self.rdata)
        elif self.type == TYPE_PTR:
            rd = encode_name(self.rdata)
        elif self.type == TYPE_TXT:
            rd = b"".join(bytes([len(s.encode())]) + s.encode() for s in (self.rdata or [""]))
        elif self.type == TYPE_SRV:
            prio, weight, port, target = self.rdata
            rd = struct.pack("!HHH", prio, weight, port) + encode_name(target)
        else:
            rd = bytes(self.rdata)
        return encode_name(self.name) + struct.pack("!HHIH", self.type, self.cls, self.ttl, len(rd)) + rd


@dataclass
class Message:
    id: int = 0
    flags: int = 0
    questions: list[tuple[str, int, int]] = field(default_factory=list)  # (name, type, class)
    answers: list[RR] = field(default_factory=list)
    authority: list[RR] = field(default_factory=list)
    additional: list[RR] = field(default_factory=list)
    raw_questions: bytes = b""  # as received (echoed in answers, so names keep their case)

    @property
    def response(self) -> bool:
        return bool(self.flags & 0x8000)

    def encode(self) -> bytes:
        qd = self.raw_questions or b"".join(encode_name(n) + struct.pack("!HH", t, c) for n, t, c in self.questions)
        head = struct.pack(
            "!HHHHHH",
            self.id,
            self.flags,
            len(self.questions),
            len(self.answers),
            len(self.authority),
            len(self.additional),
        )
        return head + qd + b"".join(r.encode() for r in (*self.answers, *self.authority, *self.additional))


def decode(buf: bytes) -> Message:
    """Parses a DNS/mDNS message (raises on malformed input)."""
    mid, flags, qd, an, ns, ar = struct.unpack("!HHHHHH", buf[:12])
    off = 12
    m = Message(mid, flags)
    for _ in range(qd):
        name, off = decode_name(buf, off)
        t, c = struct.unpack("!HH", buf[off : off + 4])
        off += 4
        m.questions.append((name, t, c))
    m.raw_questions = buf[12:off]
    for section, count in ((m.answers, an), (m.authority, ns), (m.additional, ar)):
        for _ in range(count):
            name, off = decode_name(buf, off)
            t, c, ttl, rdlen = struct.unpack("!HHIH", buf[off : off + 10])
            off += 10
            rd = buf[off : off + rdlen]
            if len(rd) != rdlen:
                raise ValueError("truncated record")
            data: Any = rd
            if t == TYPE_A and rdlen == 4:
                data = socket.inet_ntoa(rd)
            elif t == TYPE_PTR:
                data = decode_name(buf, off)[0]
            elif t == TYPE_SRV:
                data = (*struct.unpack("!HHH", rd[:6]), decode_name(buf, off + 6)[0])
            elif t == TYPE_TXT:
                data, i = [], 0
                while i < rdlen:
                    data.append(rd[i + 1 : i + 1 + rd[i]].decode(errors="replace"))
                    i += 1 + rd[i]
            section.append(RR(name, t, data, ttl, c))
            off += rdlen
    return m


def reverse_name(ip: str) -> str:
    return ipaddress.ip_address(ip).reverse_pointer


# --------------------------------------------------------------- NetBIOS


def nb_encode(name: str) -> bytes:
    """First-level encoding of a 16-byte NetBIOS name ("*" for NBSTAT)."""
    raw = name.encode().ljust(16, b"\x00" if name == "*" else b" ")[:16]
    return bytes([32]) + bytes(c for b in raw for c in (0x41 + (b >> 4), 0x41 + (b & 0x0F))) + b"\x00"


def nbstat_response(tid: int, names: list[tuple[str, int, bool]], mac: str) -> bytes:
    """A node status answer: (name, suffix, group) entries plus the unit id (MAC)."""
    body = bytes([len(names)])
    for name, suffix, group in names:
        flags = 0x0400 | (0x8000 if group else 0)  # active, B-node
        body += name.upper().encode()[:15].ljust(15, b" ") + bytes([suffix]) + struct.pack("!H", flags)
    body += bytes.fromhex(mac.replace(":", "")) + bytes(40)
    head = struct.pack("!HHHHHH", tid, 0x8400, 0, 1, 0, 0)
    return head + nb_encode("*") + struct.pack("!HHIH", 0x21, 1, 0, len(body)) + body


def parse_nbstat_names(b: bytes) -> list[tuple[str, int, int]]:
    off = 56
    out = []
    for i in range(b[off]):
        p = off + 1 + i * 18
        out.append((b[p : p + 15].decode().rstrip(), b[p + 15], struct.unpack("!H", b[p + 16 : p + 18])[0]))
    return out


# ------------------------------------------------------------- services


class _ScanService(Service):
    """A listener of one (ip, port), shared by every node with that address (a duplicate IP)."""

    def __init__(self, ctx: Context, nodes: list[Node], ip: str, port: int, kind: str) -> None:
        super().__init__(
            ctx, f"scan:{kind}:{ip}:{port}", {"kind": "scan", "node": nodes[0].name, "ip": ip, "port": port}
        )
        self.nodes = nodes
        self.ip = ip
        self.port = port

    def alive_node(self, snap: Snapshot) -> Node | None:
        return next((n for n in self.nodes if alive(snap, n)), None)

    def reachable(self, snap: Snapshot) -> bool:
        return self.alive_node(snap) is not None


class ScanHttp(_ScanService, HttpService):
    """A web interface: `GET /` answers the page title, a redirect, or a page without a title."""

    def __init__(self, ctx: Context, nodes: list[Node], ip: str, port: int, tls: bool) -> None:
        super().__init__(ctx, nodes, ip, port, "https" if tls else "http")
        self.tls = tls
        self.locate: Callable[[str, int], tuple[str, int]] = lambda ip, port: (ip, port)

    def handle(self, req: Request, snap: Snapshot) -> Response:
        n = self.alive_node(snap)
        if n is None:
            return Response.text("", 503)
        desc = ssdp_location(n)
        if desc and desc[0] == self.port and req.path == desc[1]:
            return Response(200, upnp_description(n), [("Content-Type", 'text/xml; charset="utf-8"')])
        if req.path not in ("/", "/index.html"):
            return _not_found()
        titles = {int(k): v for k, v in n.scan.get("titles", {}).items()}
        title = titles.get(self.port)
        to = _redirect_target(n, self.port, titles)
        if title and re.match(r"^30[1278]\b", title):
            return Response.text(
                f"<html><head><title>{title}</title></head><body><center><h1>{title}</h1></center>"
                "<hr><center>nginx</center></body></html>",
                int(title[:3]),
                headers=[("Location", self.url(to or 443)), ("Server", "nginx")],
            )
        if title:
            return Response.html_title(title, f'<div id="app" data-host="{n.name}"></div>')
        if to:
            return Response.text("", 302, headers=[("Location", self.url(to))])
        return _not_found()

    def url(self, port: int) -> str:
        """https://<this host>[:port]/, where that port is really served."""
        ip, real = self.locate(self.ip, port)
        return f"https://{ip}/" if real == 443 else f"https://{ip}:{real}/"


def _not_found() -> Response:
    return Response.text("<html><head><title>404 Not Found</title></head><body><h1>Not Found</h1></body></html>", 404)


def _redirect_target(n: Node, port: int, titles: dict[int, str]) -> int | None:
    """The standard web ports of a device whose interface lives on another HTTPS port redirect there (Synology:
    80, 443 and 5000 → 5001): that port."""
    if port not in (80, 443, 5000):
        return None
    for p in (443, 5001, 8443, 9443, 8006):
        if p != port and p in titles and _is_tls(n, p) and not re.match(r"^30[1278]\b", titles[p]):
            return p
    return None


def _is_tls(n: Node, port: int) -> bool:
    return port in TLS_PORTS and port not in n.scan.get("plain", ())


class RawTcp(_ScanService):
    """A port that is open but not a web server: an optional banner line, then the connection closes."""

    def __init__(self, ctx: Context, nodes: list[Node], ip: str, port: int) -> None:
        super().__init__(ctx, nodes, ip, port, "tcp")

    def banner(self, snap: Snapshot) -> bytes:
        n = self.alive_node(snap)
        if n is None:
            return b""
        if self.port == 22:
            return (_ssh_banner(n) + "\r\n").encode()
        tpl = BANNERS.get(self.port)
        if tpl:
            return (tpl.format(fqdn=fqdn(self.ctx.world.d, n) or n.name, vendor=n.vendor) + "\r\n").encode()
        return b""

    async def connected(self, reader: asyncio.StreamReader, writer: asyncio.StreamWriter) -> None:
        try:
            snap = self.ctx.snapshot()
            if self.reachable(snap):
                data = self.banner(snap)
                if data:
                    writer.write(data)
                    await writer.drain()
                    with contextlib.suppress(asyncio.TimeoutError):  # like sshd: wait for the client's line
                        await asyncio.wait_for(reader.read(256), 2)
        except (ConnectionError, OSError):
            pass
        finally:
            with contextlib.suppress(Exception):
                writer.close()


class NetbiosService(_ScanService, UdpService):
    """NetBIOS name service (UDP 137): answers node status (NBSTAT) queries."""

    def __init__(self, ctx: Context, nodes: list[Node], ip: str, port: int = 137) -> None:
        super().__init__(ctx, nodes, ip, port, "udp")

    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        n = next((x for x in self.nodes if x.scan.get("netbios") and alive(snap, x)), None)
        if n is None or len(data) < 50 or data[2] & 0x80:
            return None
        qtype = struct.unpack("!H", data[46:48])[0]
        if qtype != 0x21:
            return None
        return nbstat_response(struct.unpack("!H", data[:2])[0], netbios_names(n), n.mac)


def netbios_names(n: Node) -> list[tuple[str, int, bool]]:
    name = n.scan["netbios"]
    dom = NETBIOS_DOMAIN.get(n.site, "WORKGROUP")
    names = [(name, 0x00, False), (dom, 0x00, True), (name, 0x20, False)]
    if n.name.startswith("dc") and "Server" in n.os:  # domain controllers
        names.append((dom, 0x1C, True))
        if n.name == "dc01":  # the PDC emulator: the domain master browser
            names.append((dom, 0x1B, False))
    else:
        names.append((dom, 0x1E, True))  # browser elections
    return names


class DnsService(_ScanService, UdpService):
    """The HQ gateway's DNS: PTR and A records of every host of every site, NXDOMAIN otherwise."""

    def __init__(self, ctx: Context, ip: str = DNS_GATEWAY[0], port: int = DNS_GATEWAY[1]) -> None:
        gw = [n for n in ctx.world.nodes.values() if n.name in ("fw-hq-01", "fw-hq-02")]
        super().__init__(ctx, gw or list(ctx.world.nodes.values())[:1], ip, port, "udp")
        self._cache: tuple[int, dict[str, list[str]], dict[str, list[str]]] | None = None

    def reachable(self, snap: Snapshot) -> bool:
        return any(snap.up(n.name) for n in self.nodes)  # the CARP VIP moves to the backup

    def records(self, snap: Snapshot) -> tuple[dict[str, list[str]], dict[str, list[str]]]:
        """(PTR name → host names, host name → addresses), registered statically or by DHCP leases."""
        if self._cache and self._cache[0] == snap.b:
            return self._cache[1], self._cache[2]
        d = self.ctx.world.d
        leased = {lease.node.name for site in d.sites for lease in snap.leases(site)}
        ptr: dict[str, list[str]] = {}
        fwd: dict[str, list[str]] = {}
        for n in sorted(self.ctx.world.nodes.values(), key=lambda x: x.name):
            if not n.ip or (n.dhcp and not n.static_lease and n.name not in leased):
                continue
            name = fqdn(d, n)
            if not name:
                continue
            addrs = [n.ip]
            for p in n.ports.values():
                for a in p.ips:
                    ip = a.split("/")[0]
                    if ip not in addrs and ipaddress.ip_address(ip).is_private and not ip.startswith("127."):
                        addrs.append(ip)
            for ip in addrs:
                ptr.setdefault(reverse_name(ip), []).append(name)
            fwd.setdefault(name.lower(), []).append(n.ip)  # the A record: the main address only
        self._cache = (snap.b, ptr, fwd)
        return ptr, fwd

    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        try:
            q = decode(data)
        except Exception:
            return None
        if q.response or len(q.questions) != 1:
            return None
        name, qtype, _ = q.questions[0]
        ptr, fwd = self.records(snap)
        flags = 0x8000 | 0x0400 | (q.flags & 0x0100) | 0x0080  # response, authoritative, RD echoed, RA
        r = Message(q.id, flags, [q.questions[0]], raw_questions=q.raw_questions)
        key = name.lower().rstrip(".")
        if key in ptr:
            if qtype in (TYPE_PTR, TYPE_ANY):
                r.answers = [RR(name, TYPE_PTR, target + ".", 3600) for target in ptr[key]]
        elif key in fwd:
            if qtype in (TYPE_A, TYPE_ANY):
                r.answers = [RR(name, TYPE_A, ip, 3600) for ip in fwd[key]]
        else:
            r.flags |= 3  # NXDOMAIN
        if any(a.type == TYPE_OPT for a in q.additional):  # EDNS(0), like Unbound
            r.additional = [RR("", TYPE_OPT, b"", 0, 1232)]
        return r.encode()


# ------------------------------------------------------------------ mDNS


def mdns_records(n: Node, ip: str, questions: list[tuple[str, int, int]], legacy: bool) -> Message | None:
    """The answer of host `n` to an mDNS query (None when it has nothing to say)."""
    types = list(n.scan.get("mdns", []))
    if not types:
        return None
    host = local_name(n) + ".local"
    ttl_short, ttl_long = (10, 10) if legacy else (120, 4500)
    flush = 0 if legacy else CACHE_FLUSH
    answers: list[RR] = []
    extra: list[RR] = []
    seen: set[str] = set()

    def service(t: str) -> None:
        inst = f"{instance_name(n, t)}.{t}.local"
        answers.append(RR(f"{t}.local", TYPE_PTR, inst, ttl_long))
        if inst in seen:
            return
        seen.add(inst)
        extra.append(RR(inst, TYPE_SRV, (0, 0, MDNS_PORTS.get(t, 80), host), ttl_short, CLASS_IN | flush))
        extra.append(RR(inst, TYPE_TXT, txt_of(n, t), ttl_long, CLASS_IN | flush))

    for qname, qtype, _ in questions:
        q = qname.lower().rstrip(".")
        if qtype in (TYPE_PTR, TYPE_ANY) and q == "_services._dns-sd._udp.local":
            answers += [RR("_services._dns-sd._udp.local", TYPE_PTR, f"{t}.local", ttl_long) for t in types]
        elif qtype in (TYPE_PTR, TYPE_ANY) and q.removesuffix(".local") in types:
            service(q.removesuffix(".local"))
        elif qtype in (TYPE_A, TYPE_ANY) and q == host.lower():
            answers.append(RR(host, TYPE_A, ip, ttl_short, CLASS_IN | flush))
    if not answers:
        return None
    if extra or not any(a.type == TYPE_A for a in answers):
        extra.append(RR(host, TYPE_A, ip, ttl_short, CLASS_IN | flush))
    model = APPLE_IDS.get(n.model) if n.vendor == "Apple" else None
    if model and extra:
        extra.append(
            RR(f"{instance_name(n, '')}._device-info._tcp.local", TYPE_TXT, [f"model={model}", "osxvers=25"], ttl_long)
        )
    return Message(0, 0x8400, answers=answers, additional=extra)


def txt_of(n: Node, stype: str) -> list[str]:
    if stype in ("_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp"):
        return [
            "txtvers=1",
            f"ty={n.vendor} {n.model}",
            f"product=({n.model})",
            f"usb_MFG={n.vendor}",
            f"usb_MDL={n.model}",
            "rp=ipp/print",
        ]
    if stype == "_googlecast._tcp":
        return [f"id={_uuid(n).replace('-', '')}", f"md={n.model}", f"fn={n.name}", "ve=05", "ca=465413"]
    if stype == "_esphomelib._tcp":
        return ["version=2025.9.1", f"mac={n.mac.replace(':', '')}", "platform=ESP32", "board=esp32-c3-devkitm-1"]
    if stype == "_shelly._tcp":
        return ["gen=2", f"app={n.model.replace('Shelly ', '').replace(' ', '')}", "ver=1.4.4"]
    if stype in ("_airplay._tcp", "_raop._tcp"):
        return [f"model={n.model}", f"manufacturer={n.vendor}", f"deviceid={n.mac.upper()}"]
    if stype == "_home-assistant._tcp":
        return ["location_name=Acme", "version=2025.10.1", "internal_url=http://homeassistant.local:8123"]
    return []


class MdnsService(_ScanService, UdpService):
    """A host's mDNS responder on its own address (UDP 5353): legacy unicast queries, and the sender of its
    answers to multicast queries (`MulticastGroup`) and of its announcements."""

    def __init__(self, ctx: Context, nodes: list[Node], ip: str, port: int = 5353) -> None:
        super().__init__(ctx, nodes, ip, port, "udp")
        self.transport: asyncio.DatagramTransport | None = None

    def answer(self, data: bytes, snap: Snapshot, legacy: bool) -> bytes | None:
        n = next((x for x in self.nodes if x.scan.get("mdns") and alive(snap, x)), None)
        if n is None:
            return None
        try:
            q = decode(data)
        except Exception:
            return None
        if q.response or not q.questions:
            return None
        r = mdns_records(n, self.ip, q.questions, legacy)
        if r is None:
            return None
        if legacy:  # RFC 6762 6.7: the ID and the questions are echoed
            r.id, r.questions, r.raw_questions = q.id, q.questions, q.raw_questions
        return r.encode()

    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        return self.answer(data, snap, legacy=addr[1] != 5353)

    def announcement(self, snap: Snapshot) -> bytes | None:
        n = next((x for x in self.nodes if x.scan.get("mdns") and alive(snap, x)), None)
        if n is None:
            return None
        r = mdns_records(n, self.ip, [(f"{t}.local", TYPE_PTR, CLASS_IN) for t in n.scan["mdns"]], legacy=False)
        return r.encode() if r else None


# ------------------------------------------------------------------ SSDP


def ssdp_location(n: Node) -> tuple[int, str] | None:
    """(port, path) of the node's UPnP description."""
    info = n.scan.get("ssdp")
    if not info:
        return None
    port, path = DESCRIPTIONS.get(info.get("manufacturer", ""), (DESCRIPTION_PORT, "/description.xml"))
    return port, path


def device_urn(n: Node) -> str:
    t = n.scan["ssdp"].get("type", "Basic")
    return DEVICE_URNS.get(t, f"urn:schemas-upnp-org:device:{t}:1")


def friendly_name(n: Node) -> str:
    info = n.scan["ssdp"]
    if n.kind == "tv" and "Samsung" in info.get("manufacturer", ""):
        return f"[TV] Samsung {info['model']}"
    if n.kind == "tv":
        return f"[LG] webOS TV {info['model']}"
    if n.kind == "printer":
        return f"{info['manufacturer']} {info['model']} ({n.name})"
    return n.name.replace("-", " ").title() if n.kind == "media" else n.name


def upnp_description(n: Node) -> bytes:
    info = n.scan["ssdp"]
    serial = n.serial or n.mac.replace(":", "").upper()
    return (
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<root xmlns="urn:schemas-upnp-org:device-1-0">\n'
        "  <specVersion><major>1</major><minor>0</minor></specVersion>\n"
        "  <device>\n"
        f"    <deviceType>{device_urn(n)}</deviceType>\n"
        f"    <friendlyName>{_xml(friendly_name(n))}</friendlyName>\n"
        f"    <manufacturer>{_xml(info.get('manufacturer', n.vendor))}</manufacturer>\n"
        f"    <modelName>{_xml(info.get('model', n.model))}</modelName>\n"
        f"    <serialNumber>{_xml(serial)}</serialNumber>\n"
        f"    <UDN>uuid:{_uuid(n)}</UDN>\n"
        "  </device>\n"
        "</root>\n"
    ).encode()


def _xml(s: str) -> str:
    return s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")


def _server_header(n: Node) -> str:
    info = n.scan["ssdp"]
    vendor = re.sub(r"[^A-Za-z0-9]", "", info.get("manufacturer", n.vendor).split(",")[0].split(" ")[0]) or "UPnP"
    return f"Linux/5.4 UPnP/1.0 {vendor}/1.0"


class SsdpService(_ScanService, UdpService):
    """A host's SSDP responder (UDP 1900): answers M-SEARCH with where its description is."""

    def __init__(
        self,
        ctx: Context,
        nodes: list[Node],
        ip: str,
        port: int = 1900,
        locate: Callable[[str, int], tuple[str, int]] | None = None,
    ) -> None:
        super().__init__(ctx, nodes, ip, port, "udp")
        self.transport: asyncio.DatagramTransport | None = None
        self.locate = locate or (lambda ip, port: (ip, port))  # design address → where it really listens

    def replies(self, data: bytes, snap: Snapshot) -> list[bytes]:
        n = next((x for x in self.nodes if x.scan.get("ssdp") and alive(snap, x)), None)
        if n is None:
            return []
        text = data.decode("latin-1", "replace")
        if not text.startswith("M-SEARCH"):
            return []
        headers = {}
        for line in text.split("\r\n")[1:]:
            k, sep, v = line.partition(":")
            if sep:
                headers[k.strip().upper()] = v.strip()
        if headers.get("MAN", "").strip('"') != "ssdp:discover":
            return []
        st = headers.get("ST", "")
        u = f"uuid:{_uuid(n)}"
        targets = ["upnp:rootdevice", u, device_urn(n)]
        if st == "ssdp:all":
            sts = targets
        elif st in targets:
            sts = [st]
        else:
            return []
        port, path = ssdp_location(n) or (DESCRIPTION_PORT, "/description.xml")
        hip, hport = self.locate(self.ip, port)
        date = time.strftime("%a, %d %b %Y %H:%M:%S GMT", time.gmtime())
        out = []
        for s in sts:
            usn = u if s == u else f"{u}::{s}"
            out.append(
                (
                    "HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\n"
                    f"DATE: {date}\r\nEXT:\r\nLOCATION: http://{hip}:{hport}{path}\r\n"
                    f"SERVER: {_server_header(n)}\r\nST: {s}\r\nUSN: {usn}\r\n"
                    "BOOTID.UPNP.ORG: 1\r\nCONFIGID.UPNP.ORG: 1\r\n\r\n"
                ).encode()
            )
        return out

    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        out = self.replies(data, snap)
        if self.transport and len(out) > 1:
            for extra in out[1:]:
                self.transport.sendto(extra, addr)
        return out[0] if out else None


class MulticastGroup(Service):
    """Joined to a multicast group (mDNS or SSDP): each query is answered by every host that has something to
    say, from that host's own socket, so the answer comes from the host's address."""

    def __init__(self, ctx: Context, group: tuple[str, int], members: list[MdnsService] | list[SsdpService]) -> None:
        super().__init__(
            ctx, f"scan:mcast:{group[0]}:{group[1]}", {"kind": "scan", "node": "", "ip": group[0], "port": group[1]}
        )
        self.group = group
        self.members = members

    def reachable(self, snap: Snapshot) -> bool:
        return True

    def received(self, data: bytes, addr: tuple[str, int]) -> None:
        if not data:
            return
        snap = self.ctx.snapshot()
        for m in self.members:
            if m.transport is None or not any(n.site in MCAST_SITES for n in m.nodes):
                continue
            try:
                if isinstance(m, MdnsService):
                    legacy = addr[1] != 5353
                    out = m.answer(data, snap, legacy)
                    if out:
                        m.transport.sendto(out, addr if legacy else self.group)
                else:
                    for out in m.replies(data, snap):
                        m.transport.sendto(out, addr)  # SSDP answers are always unicast
            except OSError as e:
                log.debug("%s: answer to %s failed: %s", m.name, addr, e)

    def announce(self) -> int:
        """mDNS: every live host announces its services on the group (what a listener hears unasked)."""
        snap = self.ctx.snapshot()
        sent = 0
        for m in self.members:
            if isinstance(m, MdnsService) and m.transport and any(n.site in MCAST_SITES for n in m.nodes):
                out = m.announcement(snap)
                if out:
                    with contextlib.suppress(OSError):
                        m.transport.sendto(out, self.group)
                        sent += 1
        return sent


# ------------------------------------------------------------------ build


Entry = tuple[str, str, int, Service]


def services(
    ctx: Context,
    dns: tuple[str, int] | None = DNS_GATEWAY,
    only: set[str] | None = None,
    taken: set[tuple[str, int, str]] | None = None,
) -> list[Entry]:
    """Every listener of the scanned hosts: (kind, ip, port, service).

    `only` limits them to some nodes; `taken` holds the (ip, port, "tcp" | "udp") the emulated APIs already
    serve (default: every acme.APIS address), which answer `GET /` themselves.
    """
    world = ctx.world
    by_ip: dict[str, list[Node]] = {}
    for n in sorted(world.nodes.values(), key=lambda x: x.name):
        if n.ip and n.scan and (only is None or n.name in only):
            by_ip.setdefault(n.ip, []).append(n)
    if taken is None:
        taken = {(s["ip"], s["port"], "udp" if s["kind"] == "snmp" else "tcp") for s in acme.APIS.values()}
    out: list[Entry] = []
    for ip, nodes in sorted(by_ip.items(), key=lambda kv: ipaddress.ip_address(kv[0])):
        ports: dict[int, list[Node]] = {}
        for n in nodes:
            for p in n.scan.get("ports", []):
                ports.setdefault(int(p), []).append(n)
            loc = ssdp_location(n)
            if loc:
                ports.setdefault(loc[0], []).append(n)
        for port, owners in sorted(ports.items()):
            if (ip, port, "tcp") in taken:
                continue
            owners = list({o.name: o for o in owners}.values())
            titled = any(port in {int(k) for k in o.scan.get("titles", {})} for o in owners)
            described = any((ssdp_location(o) or (0,))[0] == port for o in owners)
            if port in RAW_PORTS and not titled and not described:
                out.append(("tcp", ip, port, RawTcp(ctx, owners, ip, port)))
            else:
                tls = _is_tls(owners[0], port)
                out.append(("https" if tls else "http", ip, port, ScanHttp(ctx, owners, ip, port, tls)))
        if any(n.scan.get("netbios") for n in nodes) and (ip, 137, "udp") not in taken:
            out.append(("udp", ip, 137, NetbiosService(ctx, nodes, ip)))
        if any(n.scan.get("mdns") for n in nodes) and (ip, 5353, "udp") not in taken:
            out.append(("udp", ip, 5353, MdnsService(ctx, nodes, ip)))
        if any(n.scan.get("ssdp") for n in nodes) and (ip, 1900, "udp") not in taken:
            out.append(("udp", ip, 1900, SsdpService(ctx, nodes, ip)))
    mdns = [s for k, _, p, s in out if k == "udp" and p == 5353]
    ssdp = [s for k, _, p, s in out if k == "udp" and p == 1900]
    out.append(("mcast", *MDNS_GROUP, MulticastGroup(ctx, MDNS_GROUP, mdns)))  # type: ignore[arg-type]
    out.append(("mcast", *SSDP_GROUP, MulticastGroup(ctx, SSDP_GROUP, ssdp)))  # type: ignore[arg-type]
    if dns:
        out.append(("udp", dns[0], dns[1], DnsService(ctx, *dns)))
    return out


# ------------------------------------------------------------------ run


IP_FREEBIND = 15  # Linux: bind an address that is not (yet) on an interface


def _socket(kind: int, ip: str, port: int) -> socket.socket:
    s = socket.socket(socket.AF_INET, kind)
    s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    if hasattr(socket, "SO_REUSEPORT") and kind == socket.SOCK_DGRAM:
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEPORT, 1)  # 5353/1900 are shared with Omini and others
    with contextlib.suppress(OSError):
        s.setsockopt(socket.IPPROTO_IP, IP_FREEBIND, 1)
    s.bind((ip, port))
    s.setblocking(False)
    return s


class _Proto(asyncio.DatagramProtocol):
    def __init__(self, svc: Service) -> None:
        self.svc = svc

    def connection_made(self, transport) -> None:  # type: ignore[override]
        self.transport = transport
        if isinstance(self.svc, (MdnsService, SsdpService)):
            self.svc.transport = transport

    def datagram_received(self, data: bytes, addr) -> None:  # type: ignore[override]
        svc = self.svc
        if isinstance(svc, MulticastGroup):
            svc.received(data, addr[:2])
            return
        assert isinstance(svc, UdpService)
        snap = svc.ctx.snapshot()
        if not svc.reachable(snap):
            return
        try:
            out = svc.datagram(data, addr[:2], snap)
        except Exception:
            log.exception("%s: datagram from %s failed", svc.name, addr)
            return
        if out:
            self.transport.sendto(out, addr)


class Scanner:
    """Runs the scan listeners on a hub's event loop and keeps the TCP ones in step with presence.

    `bind(ip, port)` maps a design address to where it is served (tests: 127.0.0.x and port 0);
    `group_iface` is the address of the interface multicast groups are joined on (default: the route's).
    """

    def __init__(
        self,
        ctx: Context,
        hub: Any,
        entries: Iterable[Entry] | None = None,
        bind: Callable[[str, int], tuple[str, int]] | None = None,
        group_iface: str = "0.0.0.0",
    ) -> None:
        self.ctx = ctx
        self.hub = hub
        self.entries = list(entries if entries is not None else services(ctx))
        self.bind = bind or (lambda ip, port: (ip, port))
        self.group_iface = group_iface
        self.bound: dict[tuple[str, int], tuple[str, int]] = {}  # design (ip, port) → served at
        self.tcp: dict[tuple[str, int], asyncio.AbstractServer] = {}
        self._task: asyncio.Task | None = None
        for _, _, _, svc in self.entries:
            if isinstance(svc, (SsdpService, ScanHttp)):
                svc.locate = self.locate

    def locate(self, ip: str, port: int) -> tuple[str, int]:
        return self.bound.get((ip, port)) or self.bind(ip, port)

    async def start(self, interval: float = 5.0, announce_every: float = 120.0) -> None:
        loop = asyncio.get_running_loop()
        for kind, ip, port, svc in self.entries:
            if kind not in ("udp", "mcast"):
                continue
            try:
                if kind == "mcast":
                    sock = _socket(socket.SOCK_DGRAM, *self.bind(ip, port))
                    with contextlib.suppress(OSError):
                        mreq = socket.inet_aton(ip) + socket.inet_aton(self.group_iface)
                        sock.setsockopt(socket.IPPROTO_IP, socket.IP_ADD_MEMBERSHIP, mreq)
                else:
                    sock = _socket(socket.SOCK_DGRAM, *self.bind(ip, port))
                    if isinstance(svc, MdnsService):
                        sock.setsockopt(socket.IPPROTO_IP, socket.IP_MULTICAST_TTL, 255)
                transport, _ = await loop.create_datagram_endpoint(lambda s=svc: _Proto(s), sock=sock)
            except OSError as e:
                log.warning("scan: cannot listen on udp %s:%s: %s", ip, port, e)
                continue
            self.hub.servers.append(transport)
            self.bound[(ip, port)] = transport.get_extra_info("sockname")[:2]
        await self.sync()
        if interval > 0:
            self._task = loop.create_task(self._run(interval, announce_every))

    async def _run(self, interval: float, announce_every: float) -> None:
        last = 0.0
        while True:
            await asyncio.sleep(interval)
            try:
                await self.sync()
                if announce_every and time.monotonic() - last >= announce_every:
                    last = time.monotonic()
                    for _, _, _, svc in self.entries:
                        if isinstance(svc, MulticastGroup) and svc.group == MDNS_GROUP:
                            svc.announce()
            except Exception:
                log.exception("scan: sync failed")

    async def sync(self) -> None:
        """Opens the TCP ports of hosts that are up and closes those of hosts that are not."""
        snap = self.ctx.snapshot()
        for kind, ip, port, svc in self.entries:
            if kind not in ("http", "https", "tcp"):
                continue
            key = (ip, port)
            want = svc.reachable(snap)
            server = self.tcp.get(key)
            if want and server is None:
                try:
                    sock = _socket(
                        socket.SOCK_STREAM, *self.bind(ip, port) if key not in self.bound else self.bound[key]
                    )
                    sock.listen(128)
                    if kind == "tcp":
                        assert isinstance(svc, RawTcp)
                        server = await asyncio.start_server(svc.connected, sock=sock)
                    else:
                        assert isinstance(svc, ScanHttp)
                        server = await asyncio.start_server(
                            _http_handler(svc, svc.tls), sock=sock, ssl=self.hub.tls if svc.tls else None
                        )
                except OSError as e:
                    log.warning("scan: cannot listen on tcp %s:%s: %s", ip, port, e)
                    continue
                self.tcp[key] = server
                self.bound[key] = server.sockets[0].getsockname()[:2]
            elif not want and server is not None:
                _stop(server)
                del self.tcp[key]

    def close(self) -> None:
        if self._task:
            self._task.cancel()
        for server in self.tcp.values():
            _stop(server)
        self.tcp.clear()


def _stop(server: asyncio.AbstractServer) -> None:
    """Stops listening (connections in progress finish on their own)."""
    server.close()


async def start(hub: Any, ctx: Context, taken: set[tuple[str, int, str]] | None = None, **kw: Any) -> Scanner:
    """Starts every scan listener on its real address (the lab), next to the emulated APIs in `taken`."""
    scanner = Scanner(ctx, hub, services(ctx, taken=taken))
    await scanner.start(**kw)
    log.info("scan: %d listeners", len(scanner.bound))
    return scanner
