"""The network scan side of the devnet: DNS, NetBIOS, mDNS, SSDP, web titles and banners of the emulated hosts.

The listeners run on 127.0.0.x addresses (one per design address) and random ports, with a frozen clock.
"""

from __future__ import annotations

import asyncio
import socket
import ssl
import struct
import threading
import time
import urllib.error
import urllib.request
import xml.etree.ElementTree as ET
from collections.abc import Iterator

import pytest

from emulator import scan
from emulator.server import Context, Hub, tls_context
from emulator.world import Clock, World, at_local

TUESDAY = at_local(1, 10, 30)
SUNDAY_NIGHT = at_local(6, 3, 0)
NODES = {
    "nas01",
    "dc01",
    "erp-app01",
    "prn-f2-01",
    "prn-f3-01",
    "ACME-PC-0107",
    "sonos-recepcao",
    "homeassistant",
    "web01",
    "mx01",
    "chromecast-santos",
    "tv-santos",
    "MacBook-Pro-de-Caio",
    "MacBook-Pro-de-Isabela",
    "shelly-luz-f1",
}
# Omini's NBSTAT query (internal/netscan/probes.go).
NBSTAT_QUERY = bytes([0x13, 0x37, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0x20]) + b"CK" + b"A" * 30 + bytes([0, 0, 0x21, 0, 1])
# The service types Omini asks for (internal/netscan/multicast.go).
MDNS_TYPES = [
    "_services._dns-sd._udp",
    "_device-info._tcp",
    "_workstation._tcp",
    "_http._tcp",
    "_ssh._tcp",
    "_smb._tcp",
    "_afpovertcp._tcp",
    "_airplay._tcp",
    "_raop._tcp",
    "_companion-link._tcp",
    "_googlecast._tcp",
    "_spotify-connect._tcp",
    "_sonos._tcp",
    "_hap._tcp",
    "_home-assistant._tcp",
    "_esphomelib._tcp",
    "_ipp._tcp",
    "_printer._tcp",
    "_pdl-datastream._tcp",
]
SEARCH = b'M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: "ssdp:discover"\r\nMX: 2\r\nST: ssdp:all\r\n\r\n'


@pytest.fixture(scope="module")
def world() -> World:
    return World()


class Lab:
    """The scan listeners of NODES on 127.0.0.x."""

    def __init__(self, world: World, at: float) -> None:
        self.ctx = Context(world, Clock(start=at, speed=1.0, frozen=at), {})
        ips = sorted({world.nodes[n].ip for n in NODES})
        self.addr = {ip: f"127.0.0.{k + 2}" for k, ip in enumerate(ips)}
        self.design = {v: k for k, v in self.addr.items()}
        self.loop = asyncio.new_event_loop()
        self.thread = threading.Thread(target=self.loop.run_forever, daemon=True)

    def bind(self, ip: str, port: int) -> tuple[str, int]:
        return self.addr.get(ip, "127.0.0.1"), 0

    def __enter__(self) -> Lab:
        self.thread.start()
        self.hub = Hub(self.ctx, tls_context(["localhost"], ["127.0.0.1"]))
        self.scanner = scan.Scanner(self.ctx, self.hub, scan.services(self.ctx, only=NODES), bind=self.bind)
        self.run(self.scanner.start(interval=0))
        return self

    def __exit__(self, *exc: object) -> None:
        async def shutdown() -> None:
            await asyncio.sleep(0.1)  # let the last connections be accepted and handled
            self.scanner.close()
            self.hub.close()
            await asyncio.sleep(0.1)  # let the transports finish closing

        self.run(shutdown())
        self.loop.call_soon_threadsafe(self.loop.stop)
        self.thread.join(5)

    def run(self, coro):
        return asyncio.run_coroutine_threadsafe(coro, self.loop).result(30)

    def set_time(self, t: float) -> None:
        self.ctx.clock.frozen = self.ctx.clock.start = t
        self.run(self.scanner.sync())

    def at(self, node: str, port: int) -> tuple[str, int]:
        return self.scanner.bound[(self.ctx.world.nodes[node].ip, port)]

    def group(self, port: int) -> tuple[str, int]:
        g = scan.MDNS_GROUP if port == 5353 else scan.SSDP_GROUP
        return self.scanner.bound[g]


@pytest.fixture(scope="module")
def lab(world: World) -> Iterator[Lab]:
    with Lab(world, TUESDAY) as lab:
        yield lab


def udp(addr: tuple[str, int], data: bytes, wait: float = 0.6, many: bool = False) -> list[tuple[bytes, str]]:
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    s.settimeout(wait)
    s.sendto(data, addr)
    out = []
    end = time.monotonic() + wait
    try:
        while time.monotonic() < end:
            b, src = s.recvfrom(9000)
            out.append((b, src[0]))
            if not many:
                break
    except TimeoutError:
        pass
    finally:
        s.close()
    return out


def mdns_query(types: list[str], qid: int = 0) -> bytes:
    m = scan.Message(qid, 0, [(f"{t}.local", scan.TYPE_PTR, scan.CLASS_IN) for t in types])
    return m.encode()


def get(url: str) -> tuple[int, str, dict[str, str]]:
    ctx = ssl.create_default_context()
    ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE

    class NoRedirect(urllib.request.HTTPRedirectHandler):
        def redirect_request(self, *a, **k):
            return None

    opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), NoRedirect)
    try:
        r = opener.open(url, timeout=5)
        return r.status, r.read().decode(), dict(r.headers)
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode(), dict(e.headers)


# ------------------------------------------------------------------ codecs


def test_dns_round_trip() -> None:
    m = scan.Message(
        0x1234,
        0x8400,
        [("_ipp._tcp.local", scan.TYPE_PTR, 1)],
        answers=[scan.RR("_ipp._tcp.local", scan.TYPE_PTR, "HP X._ipp._tcp.local", 4500)],
        additional=[
            scan.RR("HP X._ipp._tcp.local", scan.TYPE_SRV, (0, 0, 631, "prn.local"), 120),
            scan.RR("HP X._ipp._tcp.local", scan.TYPE_TXT, ["ty=HP X", "rp=ipp/print"]),
            scan.RR("prn.local", scan.TYPE_A, "10.10.70.13", 120, 0x8001),
        ],
    )
    d = scan.decode(m.encode())
    assert (d.id, d.flags, d.questions) == (0x1234, 0x8400, [("_ipp._tcp.local", 12, 1)])
    assert d.answers[0].rdata == "HP X._ipp._tcp.local"
    assert d.additional[0].rdata == (0, 0, 631, "prn.local")
    assert d.additional[1].rdata == ["ty=HP X", "rp=ipp/print"]
    assert (d.additional[2].rdata, d.additional[2].cls) == ("10.10.70.13", 0x8001)
    # Compressed names (pointers) decode too.
    buf = (
        struct.pack("!HHHHHH", 0, 0x8400, 1, 1, 0, 0)
        + scan.encode_name("a.local")
        + b"\x00\x0c\x00\x01"
        + b"\xc0\x0c"
        + struct.pack("!HHIH", 12, 1, 10, 4)
        + b"\x01b\xc0\x0e"
    )  # "b" + pointer to "local"
    d = scan.decode(buf)
    assert d.answers[0].name == "a.local" and d.answers[0].rdata == "b.local"


def test_netbios_codec() -> None:
    assert scan.nb_encode("*") == NBSTAT_QUERY[12:46]
    b = scan.nbstat_response(0x1337, [("DC01", 0, False), ("ACME", 0, True)], "aa:bb:cc:dd:ee:ff")
    assert b[:2] == b"\x13\x37" and len(b) == 56 + 1 + 2 * 18 + 46
    assert scan.parse_nbstat_names(b) == [("DC01", 0, 0x0400), ("ACME", 0, 0x8400)]
    assert b[57 + 36 : 57 + 42] == bytes.fromhex("aabbccddeeff")


def test_listeners(world: World) -> None:
    ctx = Context(world, Clock(start=TUESDAY, frozen=TUESDAY), {})
    entries = scan.services(ctx)
    keys = {(k, ip, p) for k, ip, p, _ in entries}
    assert ("https", "10.10.10.30", 5001) in keys and ("http", "10.10.10.30", 5000) in keys  # nas01
    assert ("tcp", "10.10.10.30", 445) in keys and ("udp", "10.10.10.30", 137) in keys
    assert ("tcp", "10.10.20.30", 22) in keys  # erp-app01 SSH
    assert ("http", "10.10.50.123", 1400) in keys  # Sonos: its description lives on 1400
    assert ("http", "10.10.70.13", 49152) in keys  # printers: a description port of their own
    assert ("udp", "10.10.10.1", 53) in keys
    assert ("mcast", "224.0.0.251", 5353) in keys and ("mcast", "239.255.255.250", 1900) in keys
    # Emulated APIs answer their own port: Proxmox 8006 on pve01, OPNsense 443 on the firewalls.
    assert ("https", "10.10.10.11", 8006) not in keys and ("https", "10.10.10.2", 443) not in keys
    assert ("https", "10.10.10.12", 8006) in keys  # pve02 serves its own web UI
    assert len({(ip, p) for k, ip, p, _ in entries if k != "mcast"}) == len([e for e in entries if e[0] != "mcast"])


# ------------------------------------------------------------------ live


def test_dns(lab: Lab) -> None:
    dns = lab.scanner.bound[scan.DNS_GATEWAY]

    def ask(name: str, qtype: int) -> scan.Message:
        q = scan.Message(77, 0x0100, [(name, qtype, 1)], additional=[scan.RR("", scan.TYPE_OPT, b"", 0, 1232)])
        ((b, _),) = udp(dns, q.encode())
        return scan.decode(b)

    r = ask("30.10.10.10.in-addr.arpa", scan.TYPE_PTR)
    assert r.id == 77 and r.flags & 0x000F == 0 and r.flags & 0x8400 == 0x8400
    assert [a.rdata for a in r.answers] == ["nas01.acme.local"]
    assert [a.rdata for a in ask("10.20.10.10.in-addr.arpa", scan.TYPE_PTR).answers] == ["dc01.acme.local"]
    assert [a.rdata for a in ask("20.90.10.10.in-addr.arpa", scan.TYPE_PTR).answers] == ["mx01.acme.com.br"]
    # A branch host, and a firewall's VLAN interface (Unbound registers every interface address).
    branch = next(n for n in lab.ctx.world.nodes.values() if n.site == "branch" and n.ip and not n.dhcp)
    names = [a.rdata for a in ask(scan.reverse_name(branch.ip), scan.TYPE_PTR).answers]
    assert names == [f"{scan.fqdn(lab.ctx.world.d, branch)}"] and names[0].endswith(".cps.acme.local")
    assert [a.rdata for a in ask("2.20.10.10.in-addr.arpa", scan.TYPE_PTR).answers] == ["fw-hq-01.acme.local"]
    # The duplicate address: the printer's static record and the desktop's DHCP registration.
    dup = sorted(a.rdata for a in ask("150.30.10.10.in-addr.arpa", scan.TYPE_PTR).answers)
    assert dup == ["ACME-PC-0107.acme.local", "prn-f3-01.acme.local"]
    assert [a.rdata for a in ask("nas01.acme.local", scan.TYPE_A).answers] == ["10.10.10.30"]
    assert ask("250.250.10.10.in-addr.arpa", scan.TYPE_PTR).flags & 0x000F == 3  # NXDOMAIN


def test_netbios(lab: Lab) -> None:
    ((b, src),) = udp(lab.at("dc01", 137), NBSTAT_QUERY)
    assert src == lab.addr["10.10.20.10"]
    names = scan.parse_nbstat_names(b)
    assert names[0] == ("DC01", 0, 0x0400) and ("ACME", 0x1C, 0x8400) in names
    assert b[-46:-40] == bytes.fromhex(lab.ctx.world.nodes["dc01"].mac.replace(":", ""))
    # The duplicate address answers with the Windows desktop's name.
    ((b, _),) = udp(lab.at("ACME-PC-0107", 137), NBSTAT_QUERY)
    assert scan.parse_nbstat_names(b)[0][0] == "ACME-PC-0107"


def test_mdns_unicast(lab: Lab) -> None:
    ((b, src),) = udp(lab.at("prn-f2-01", 5353), mdns_query(MDNS_TYPES, 0x4242))
    r = scan.decode(b)
    assert src == lab.addr["10.10.70.13"] and r.id == 0x4242 and r.response and len(r.questions) == len(MDNS_TYPES)
    ptrs = {a.name: a.rdata for a in r.answers if a.type == scan.TYPE_PTR and a.name != "_services._dns-sd._udp.local"}
    assert set(ptrs) == {"_ipp._tcp.local", "_pdl-datastream._tcp.local", "_printer._tcp.local"}
    srv = {a.name: a.rdata for a in r.additional if a.type == scan.TYPE_SRV}
    assert srv[ptrs["_ipp._tcp.local"]] == (0, 0, 631, "prn-f2-01.local")
    assert all(a.ttl <= 10 and a.cls == 1 for a in r.answers + r.additional)  # legacy unicast rules
    assert ("prn-f2-01.local", "10.10.70.13") in {(a.name, a.rdata) for a in r.additional if a.type == scan.TYPE_A}
    # A Mac: its model identifier in _device-info.
    ((b, _),) = udp(lab.at("MacBook-Pro-de-Caio", 5353), mdns_query(MDNS_TYPES))
    r = scan.decode(b)
    txt = [a.rdata for a in r.additional if a.type == scan.TYPE_TXT and "_device-info" in a.name]
    assert txt == [["model=Mac16,1", "osxvers=25"]]
    # Nothing asked that it has: no answer at all.
    assert udp(lab.at("prn-f2-01", 5353), mdns_query(["_googlecast._tcp"]), wait=0.3) == []


def test_mdns_group(lab: Lab) -> None:
    got = udp(lab.group(5353), mdns_query(MDNS_TYPES), wait=1.0, many=True)
    by_host: dict[str, set[str]] = {}
    for b, src in got:
        r = scan.decode(b)
        by_host.setdefault(lab.design[src], set()).update(
            a.name.removesuffix(".local") for a in r.answers if a.type == scan.TYPE_PTR
        )
    assert by_host["10.10.50.123"] >= {"_sonos._tcp", "_airplay._tcp", "_spotify-connect._tcp"}
    assert "_home-assistant._tcp" in by_host["10.10.50.10"]
    assert "_googlecast._tcp" in by_host["10.10.50.107"]
    assert "_companion-link._tcp" in by_host["10.10.32.93"]  # MacBook-Pro-de-Caio, in the office
    isabela = lab.ctx.world.nodes["MacBook-Pro-de-Isabela"]
    assert not lab.ctx.snapshot().up(isabela.name) and isabela.ip not in by_host  # not in today


def test_ssdp(lab: Lab) -> None:
    got = udp(lab.group(1900), SEARCH, wait=1.0, many=True)
    seen: dict[str, str] = {}
    for b, src in got:
        head = dict(line.split(": ", 1) for line in b.decode().split("\r\n")[1:] if ": " in line)
        assert head["SERVER"] and head["LOCATION"].startswith(f"http://{src}:")
        seen.setdefault(lab.design[src], head["LOCATION"])
    assert {"10.10.70.13", "10.10.50.123", "10.10.50.107", "10.10.50.131"} <= set(seen)
    ns = "{urn:schemas-upnp-org:device-1-0}"
    want = {
        "10.10.50.123": ("Sonos, Inc.", "Era 100", "urn:schemas-upnp-org:device:ZonePlayer:1"),
        "10.10.50.107": ("Google Inc.", "Eureka Dongle", "urn:dial-multiscreen-org:device:dial:1"),
        "10.10.70.13": ("HP", "Color LaserJet Pro MFP M480f", "urn:schemas-upnp-org:device:Printer:1"),
    }
    for ip, (mfr, model, urn) in want.items():
        status, body, _ = get(seen[ip])
        dev = ET.fromstring(body).find(f"{ns}device")
        assert status == 200 and dev is not None
        assert (dev.findtext(f"{ns}manufacturer"), dev.findtext(f"{ns}modelName"), dev.findtext(f"{ns}deviceType")) == (
            mfr,
            model,
            urn,
        )
    assert seen["10.10.50.123"].endswith("/xml/device_description.xml")
    # A unicast M-SEARCH to one host, for one type.
    st = SEARCH.replace(b"ssdp:all", b"urn:schemas-upnp-org:device:MediaRenderer:1")
    ((b, _),) = udp(lab.at("tv-santos", 1900), st)
    assert b"ST: urn:schemas-upnp-org:device:MediaRenderer:1" in b
    assert udp(lab.at("tv-santos", 1900), SEARCH.replace(b"ssdp:all", b"urn:x:device:Nope:1"), wait=0.3) == []


def test_web_titles(lab: Lab) -> None:
    ip, port = lab.at("nas01", 5001)
    status, body, _ = get(f"https://{ip}:{port}/")
    assert status == 200 and "<title>Synology DiskStation - nas01</title>" in body
    dsm = f"https://{ip}:{port}/"
    for p, scheme in ((80, "http"), (443, "https")):  # DSM's standard ports send you to 5001
        a, b = lab.at("nas01", p)
        status, _, headers = get(f"{scheme}://{a}:{b}/")
        assert (status, headers["Location"]) == (302, dsm)
    ip, port = lab.at("erp-app01", 443)
    assert "<title>Odoo</title>" in get(f"https://{ip}:{port}/")[1]
    # web01 redirects port 80 to HTTPS (the real address of its 443).
    ip, port = lab.at("web01", 80)
    status, _, headers = get(f"http://{ip}:{port}/")
    tip, tport = lab.at("web01", 443)
    assert status == 301 and headers["Location"] == f"https://{tip}:{tport}/"
    assert "Acme | Soluções Industriais" in get(f"https://{tip}:{tport}/")[1]
    # A web port without a title (Chromecast 8008) answers 404; plain HTTP on a TLS port gets nothing.
    ip, port = lab.at("chromecast-santos", 8008)
    assert get(f"http://{ip}:{port}/")[0] == 404
    ip, port = lab.at("nas01", 5001)
    with pytest.raises((urllib.error.URLError, ConnectionError, OSError)):
        get(f"http://{ip}:{port}/")


def banner(addr: tuple[str, int]) -> bytes:
    with socket.create_connection(addr, timeout=3) as s:
        s.settimeout(3)
        try:
            return s.recv(256)
        except TimeoutError:
            return b""


def test_banners(lab: Lab) -> None:
    assert banner(lab.at("erp-app01", 22)) == b"SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13\r\n"
    assert banner(lab.at("mx01", 25)) == b"220 mx01.acme.com.br ESMTP Postfix (Ubuntu)\r\n"
    assert banner(lab.at("dc01", 445)) == b""  # open, says nothing, closes


def test_presence(world: World) -> None:
    """Absent hosts look dead: their ports stop listening and nothing answers for them."""
    with Lab(world, TUESDAY) as lab:
        pc = lab.at("ACME-PC-0107", 135)
        socket.create_connection(pc, timeout=2).close()
        lab.set_time(SUNDAY_NIGHT)
        assert not lab.ctx.snapshot().up("ACME-PC-0107") and lab.ctx.snapshot().up("prn-f3-01")
        with pytest.raises(ConnectionRefusedError):
            socket.create_connection(pc, timeout=2)
        # The duplicate address keeps the printer's ports; NetBIOS goes quiet with the desktop.
        socket.create_connection(lab.at("prn-f3-01", 80), timeout=2).close()
        assert udp(lab.at("ACME-PC-0107", 137), NBSTAT_QUERY, wait=0.3) == []
        macs = {lab.design[src] for _, src in udp(lab.group(5353), mdns_query(MDNS_TYPES), wait=0.8, many=True)}
        assert "10.10.32.93" not in macs and "10.10.70.13" in macs
        lab.set_time(TUESDAY)
        socket.create_connection(pc, timeout=2).close()


def test_nbstat_struct_matches_omini_offsets() -> None:
    """Omini reads the name count at byte 56: header 12 + name 34 + type/class/ttl/rdlength 10."""
    b = scan.nbstat_response(1, [("FILE01", 0, False)], "00:11:22:33:44:55")
    assert b[56] == 1 and struct.unpack("!H", b[54:56])[0] == len(b) - 56
