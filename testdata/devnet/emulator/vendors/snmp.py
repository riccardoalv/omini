"""SNMP agents (v1, v2c and v3/USM) of Acme's managed devices, answering from the world.

One `SnmpAgent` per `acme.APIS` entry of kind "snmp". Each agent dresses the
world snapshot the way its vendor's real agent does: sysDescr / sysObjectID
strings, interface naming and ifIndex numbering, LLDP port id subtypes, how
the bridge port table maps to interfaces, and the vendor MIBs Omini's SNMP
profiles read (CPU, memory, temperatures, firmware, serials).

Vendor quirks kept on purpose (they are what real devices do):

- Cisco IOS-XE answers SNMPv3 only here; BRIDGE-MIB is per VLAN, through the
  v3 context "vlan-<id>" (the default context is VLAN 1), and there is no
  Q-BRIDGE FDB. Port-channels and SVIs are ifType propVirtual (53).
  lldpRemLocalPortNum is an internal number, not the ifIndex.
- ArubaOS-Switch names ports "1".."52"; trunks are ifType 161 at ifIndex 289+.
- Junos bridges logical units: dot1dBasePortIfIndex points to "ge-0/0/10.0",
  the Q-BRIDGE FDB id is an internal VLAN index, LLDP port ids are the
  locally assigned ifIndex, and the OOB port me0 is never in the bridge.
- RouterOS has no LLDP-MIB (its neighbors are in MIKROTIK-MIB) and reports
  health (temperatures) in mtxrGaugeTable since v7.
- Synology DSM and QNAP QTS are Net-SNMP agents (sysObjectID 8072.3.2.10)
  with their vendor MIBs on top; the APC NMC has no ifXTable.

Implemented here without libraries: BER, the v1/v2c message, the v3 message
with USM (engine ID discovery, time window, HMAC-MD5/SHA-1/SHA-2 auth, DES /
AES-128/192/256 privacy with gosnmp's key localization and extensions).
"""

from __future__ import annotations

import bisect
import hashlib
import hmac
import ipaddress
import itertools
import os
import re
import struct
from collections.abc import Callable
from dataclasses import dataclass, field
from typing import Any

from cryptography.hazmat.decrepit.ciphers.algorithms import TripleDES
from cryptography.hazmat.decrepit.ciphers.modes import CFB
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

from .. import acme, configs
from ..model import Node, Port
from ..server import UdpService
from ..world import Snapshot, h01

# ---------------------------------------------------------------------------
# BER

INTEGER, OCTETS, NULL, OBJID, SEQUENCE = 0x02, 0x04, 0x05, 0x06, 0x30
IPADDR, COUNTER32, GAUGE32, TICKS, OPAQUE, COUNTER64 = 0x40, 0x41, 0x42, 0x43, 0x44, 0x46
NO_SUCH_OBJECT, NO_SUCH_INSTANCE, END_OF_MIB = 0x80, 0x81, 0x82
GET, GETNEXT, RESPONSE, SET, TRAP1, GETBULK, INFORM, TRAP2, REPORT = range(0xA0, 0xA9)
NUMERIC = (INTEGER, COUNTER32, GAUGE32, TICKS, COUNTER64)

Oid = tuple[int, ...]
Value = tuple[int, Any]  # (BER tag, Python value)


def OI(s: str) -> Oid:
    return tuple(int(x) for x in s.strip(".").split(".") if x)


def enc_len(n: int) -> bytes:
    if n < 0x80:
        return bytes([n])
    b = n.to_bytes((n.bit_length() + 7) // 8, "big")
    return bytes([0x80 | len(b)]) + b


def tlv(tag: int, content: bytes) -> bytes:
    return bytes([tag]) + enc_len(len(content)) + content


def int_bytes(v: int) -> bytes:
    """Minimal two's complement (an unsigned value gets a leading 0 when its top bit is set)."""
    n = (v + (v < 0)).bit_length() // 8 + 1
    return v.to_bytes(n, "big", signed=True)


def enc_int(v: int, tag: int = INTEGER) -> bytes:
    return tlv(tag, int_bytes(v))


def enc_oid(oid: Oid) -> bytes:
    if len(oid) < 2:
        oid = (*oid, 0, 0)[:2]
    out = bytearray()
    for arc in (oid[0] * 40 + oid[1], *oid[2:]):
        chunk = [arc & 0x7F]
        arc >>= 7
        while arc:
            chunk.append(0x80 | (arc & 0x7F))
            arc >>= 7
        out += bytes(reversed(chunk))
    return bytes(out)


def enc_value(v: Value) -> bytes:
    tag, x = v
    if tag in NUMERIC:
        return enc_int(int(x), tag)
    if tag == OBJID:
        return tlv(OBJID, enc_oid(x))
    if tag in (NULL, NO_SUCH_OBJECT, NO_SUCH_INSTANCE, END_OF_MIB):
        return tlv(tag, b"")
    return tlv(tag, bytes(x))


def enc_varbind(oid: Oid, v: Value) -> bytes:
    return tlv(SEQUENCE, tlv(OBJID, enc_oid(oid)) + enc_value(v))


def enc_pdu(tag: int, reqid: int, a: int, b: int, varbinds: list[bytes]) -> bytes:
    return tlv(tag, enc_int(reqid) + enc_int(a) + enc_int(b) + tlv(SEQUENCE, b"".join(varbinds)))


def dec_tlv(buf: bytes, pos: int) -> tuple[int, int, int]:
    """(tag, content start, content end) of the TLV at pos (absolute offsets)."""
    tag = buf[pos]
    first = buf[pos + 1]
    pos += 2
    if first < 0x80:
        length = first
    else:
        n = first & 0x7F
        if n == 0 or n > 4:
            raise ValueError("bad BER length")
        length = int.from_bytes(buf[pos : pos + n], "big")
        pos += n
    if pos + length > len(buf):
        raise ValueError("truncated BER")
    return tag, pos, pos + length


def children(buf: bytes, start: int, end: int) -> list[tuple[int, int, int]]:
    out = []
    pos = start
    while pos < end:
        t = dec_tlv(buf, pos)
        out.append(t)
        pos = t[2]
    return out


def dec_int(b: bytes) -> int:
    return int.from_bytes(b, "big", signed=True) if b else 0


def dec_oid(b: bytes) -> Oid:
    arcs = []
    arc = 0
    for x in b:
        arc = (arc << 7) | (x & 0x7F)
        if not x & 0x80:
            arcs.append(arc)
            arc = 0
    if not arcs:
        return ()
    first = min(arcs[0] // 40, 2)
    return (first, arcs[0] - 40 * first, *arcs[1:])


def dec_value(tag: int, b: bytes) -> Value:
    if tag == INTEGER:
        return tag, dec_int(b)
    if tag in (COUNTER32, GAUGE32, TICKS, COUNTER64):
        return tag, int.from_bytes(b, "big")
    if tag == OBJID:
        return tag, dec_oid(b)
    if tag in (NULL, NO_SUCH_OBJECT, NO_SUCH_INSTANCE, END_OF_MIB):
        return tag, None
    return tag, bytes(b)


@dataclass
class Pdu:
    tag: int
    reqid: int
    a: int  # error-status / non-repeaters
    b: int  # error-index / max-repetitions
    varbinds: list[tuple[Oid, Value]]


def dec_pdu(buf: bytes, t: tuple[int, int, int]) -> Pdu:
    tag, s, e = t
    parts = children(buf, s, e)
    vbs = []
    for _, vs, ve in children(buf, parts[3][1], parts[3][2]):
        (_, os_, oe), (vt, vs2, ve2) = children(buf, vs, ve)[:2]
        vbs.append((dec_oid(buf[os_:oe]), dec_value(vt, buf[vs2:ve2])))
    return Pdu(
        tag,
        dec_int(buf[parts[0][1] : parts[0][2]]),
        dec_int(buf[parts[1][1] : parts[1][2]]),
        dec_int(buf[parts[2][1] : parts[2][2]]),
        vbs,
    )


# value constructors
def Int(v: int) -> Value:
    return INTEGER, int(v)


def Str(s: str | bytes) -> Value:
    return OCTETS, s.encode() if isinstance(s, str) else bytes(s)


def ObjId(s: str | Oid) -> Value:
    return OBJID, OI(s) if isinstance(s, str) else s


def Ip(s: str) -> Value:
    return IPADDR, ipaddress.IPv4Address(s).packed


def C32(v: float) -> Value:
    return COUNTER32, int(v) % 2**32


def G32(v: float) -> Value:
    return GAUGE32, max(0, min(int(v), 2**32 - 1))


def Ticks(v: float) -> Value:
    return TICKS, int(v) % 2**32


def C64(v: float) -> Value:
    return COUNTER64, int(v) % 2**64


def Mac(m: str | None) -> Value:
    return Str(bytes.fromhex(m.replace(":", "")) if m else b"")


def Bits(*bits: int) -> Value:
    b = bytearray(1)
    for n in bits:
        while len(b) <= n // 8:
            b.append(0)
        b[n // 8] |= 0x80 >> (n % 8)
    return Str(bytes(b))


def mac_oid(m: str) -> Oid:
    return tuple(int(x, 16) for x in m.split(":"))


def ip_oid(ip: str) -> Oid:
    return tuple(int(x) for x in ip.split("."))


# ---------------------------------------------------------------------------
# The MIB tree: disjoint subtrees, each built on first use


class Mib:
    def __init__(self, sections: list[tuple[Oid, Callable[[], dict[Oid, Value]]]]) -> None:
        self.sections = sorted(sections, key=lambda s: s[0])
        self._built: dict[int, tuple[list[Oid], dict[Oid, Value]]] = {}

    def _section(self, i: int) -> tuple[list[Oid], dict[Oid, Value]]:
        hit = self._built.get(i)
        if hit is None:
            d = self.sections[i][1]()
            hit = (sorted(d), d)
            self._built[i] = hit
        return hit

    def get(self, oid: Oid) -> Value:
        for i, (prefix, _) in enumerate(self.sections):
            if oid[: len(prefix)] == prefix:
                keys, d = self._section(i)
                if oid in d:
                    return d[oid]
                parent = oid[:-1]
                j = bisect.bisect_left(keys, parent)
                if j < len(keys) and keys[j][: len(parent)] == parent:
                    return NO_SUCH_INSTANCE, None
                return NO_SUCH_OBJECT, None
        return NO_SUCH_OBJECT, None

    def next(self, oid: Oid) -> tuple[Oid, Value] | None:
        for i, (prefix, _) in enumerate(self.sections):
            if oid[: len(prefix)] == prefix or oid < prefix:
                keys, d = self._section(i)
                j = bisect.bisect_right(keys, oid)
                if j < len(keys):
                    return keys[j], d[keys[j]]
        return None


def put_table(d: dict[Oid, Value], entry: Oid, rows: list[tuple[Oid, dict[int, Value]]]) -> None:
    for idx, cols in rows:
        for col, v in cols.items():
            if v is not None:
                d[(*entry, col, *idx)] = v


# ---------------------------------------------------------------------------
# Interfaces


@dataclass
class IfRow:
    index: int
    name: str  # ifName
    descr: str  # ifDescr
    alias: str = ""
    type: int = 6
    mtu: int = 1500
    speed: int = 0  # Mbps
    mac: str | None = None
    admin: bool = True
    oper: int = 2  # 1 up, 2 down, 7 lowerLayerDown
    rx: int = 0
    tx: int = 0
    rxp: int = 0
    txp: int = 0
    rxerr: int = 0
    txerr: int = 0
    last_change: int = 0  # ticks
    port: str | None = None  # the design port it shows
    ips: tuple[str, ...] = ()  # "a.b.c.d/nn"


def port_row(snap: Snapshot, node: Node, pname: str, index: int, **kw: Any) -> IfRow:
    p = node.ports[pname]
    st = snap.port(node.name, pname)
    boot = snap.boot(node.name)
    up_ticks = int(max(snap.t - boot, 0) * 100)
    change = min(6000, up_ticks)
    peer = snap.peer(node.name, pname)
    if peer is not None and p.kind not in ("lag", "vlan", "bridge"):
        other = peer[0].name
        when = snap.arrived(other) if st.up else (snap.last_seen(other) or boot)
        change = int(min(max(when - boot, 60), snap.t - boot) * 100)
    row = IfRow(
        index=index,
        name=pname,
        descr=pname,
        alias=p.descr,
        mtu=p.mtu,
        speed=(st.speed if st.up and st.speed else p.speed) or 0,
        mac=p.mac,
        admin=p.admin_up,
        oper=1 if st.up else 2,
        rx=st.rx_bytes,
        tx=st.tx_bytes,
        rxp=st.rx_packets,
        txp=st.tx_packets,
        rxerr=st.rx_errors,
        txerr=st.tx_errors,
        last_change=max(change, 0),
        port=pname,
        ips=p.ips,
    )
    for k, v in kw.items():
        setattr(row, k, v)
    return row


def soft_row(snap: Snapshot, node: Node, index: int, name: str, rate: float = 600.0, **kw: Any) -> IfRow:
    """An interface the world does not model (an SVI, a loopback, a spare NIC): steady small counters."""
    up = max(snap.t - snap.boot(node.name), 0)
    k = 0.6 + 0.8 * h01("soft", node.name, name)
    rx, tx = int(up * rate * k), int(up * rate * k * 0.7)
    row = IfRow(
        index=index,
        name=name,
        descr=name,
        rx=rx,
        tx=tx,
        rxp=rx // 180,
        txp=tx // 160,
        oper=1,
        last_change=min(6000, int(up * 100)),
    )
    for key, v in kw.items():
        setattr(row, key, v)
    if row.oper != 1:
        row.rx = row.tx = row.rxp = row.txp = 0
    return row


def if_sections(
    rows: list[IfRow],
    xtable: bool,
    stack: list[tuple[int, int]] | None = None,
) -> list[tuple[Oid, Callable[[], dict[Oid, Value]]]]:
    def iftable() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {OI("1.3.6.1.2.1.2.1.0"): Int(len(rows))}
        out = []
        for r in rows:
            mc = r.rxp // 90
            out.append(
                (
                    (r.index,),
                    {
                        1: Int(r.index),
                        2: Str(r.descr),
                        3: Int(r.type),
                        4: Int(r.mtu),
                        5: G32(min(r.speed * 1_000_000, 2**32 - 1)),
                        6: Mac(r.mac),
                        7: Int(1 if r.admin else 2),
                        8: Int(r.oper if r.admin else 2),
                        9: Ticks(r.last_change),
                        10: C32(r.rx),
                        11: C32(r.rxp - mc),
                        12: C32(mc),
                        13: C32(0),
                        14: C32(r.rxerr),
                        15: C32(0),
                        16: C32(r.tx),
                        17: C32(r.txp - r.txp // 70),
                        18: C32(r.txp // 70),
                        19: C32(0),
                        20: C32(r.txerr),
                        21: G32(0),
                        22: ObjId("0.0"),
                    },
                )
            )
        put_table(d, OI("1.3.6.1.2.1.2.2.1"), out)
        return d

    def ifx() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {}
        out = []
        for r in rows:
            imc, ibc, omc, obc = r.rxp // 120, r.rxp // 400, r.txp // 100, r.txp // 300
            out.append(
                (
                    (r.index,),
                    {
                        1: Str(r.name),
                        2: C32(imc),
                        3: C32(ibc),
                        4: C32(omc),
                        5: C32(obc),
                        6: C64(r.rx),
                        7: C64(r.rxp - imc - ibc),
                        8: C64(imc),
                        9: C64(ibc),
                        10: C64(r.tx),
                        11: C64(r.txp - omc - obc),
                        12: C64(omc),
                        13: C64(obc),
                        14: Int(1 if r.type in (6, 161) else 2),
                        15: G32(r.speed),
                        16: Int(2),
                        17: Int(1 if r.type == 6 else 2),
                        18: Str(r.alias),
                        19: Ticks(0),
                    },
                )
            )
        put_table(d, OI("1.3.6.1.2.1.31.1.1.1"), out)
        for hi, lo in stack or []:
            d[(*OI("1.3.6.1.2.1.31.1.2.1.3"), hi, lo)] = Int(1)
        d[OI("1.3.6.1.2.1.31.1.5.0")] = Ticks(0)
        return d

    out: list[tuple[Oid, Callable[[], dict[Oid, Value]]]] = [(OI("1.3.6.1.2.1.2"), iftable)]
    if xtable:
        out.append((OI("1.3.6.1.2.1.31"), ifx))
    return out


# ---------------------------------------------------------------------------
# What a node says about itself in LLDP (the remote side of a neighbor)


@dataclass
class Advert:
    chassis_subtype: int
    chassis: bytes
    port_subtype: int
    port_id: bytes
    port_desc: str
    sys_name: str
    sys_desc: str
    caps: tuple[int, ...]  # LLDP capability bits: 2 bridge, 3 AP, 4 router, 5 telephone, 7 station


CISCO_LONG = {
    "Tw": "TwoGigabitEthernet",
    "Te": "TenGigabitEthernet",
    "Gi": "GigabitEthernet",
    "Po": "Port-channel",
    "Vl": "Vlan",
    "Fo": "FortyGigabitEthernet",
    "Hu": "HundredGigE",
}


def cisco_long(name: str) -> str:
    m = re.match(r"([A-Za-z]+)(.*)", name)
    if not m:
        return name
    return CISCO_LONG.get(m.group(1), m.group(1)) + m.group(2)


def junos_index(pname: str) -> int:
    if pname == "me0":
        return 33
    m = re.match(r"(ge|xe)-0/(\d)/(\d+)$", pname)
    if not m:
        return 0
    k = int(m.group(3))
    return 513 + 2 * k if m.group(1) == "ge" else 561 + 2 * k


def mac_bytes(m: str | None) -> bytes:
    return bytes.fromhex(m.replace(":", "")) if m else b""


def sys_descr(node: Node) -> str:
    """sysDescr of the devices Acme has, as their agents print it."""
    v = node.vendor
    if v == "Cisco":
        return (
            "Cisco IOS Software [Dublin], Catalyst L3 Switch Software (CAT9K_IOSXE), Version 17.12.4, "
            "RELEASE SOFTWARE (fc3)\r\nTechnical Support: http://www.cisco.com/techsupport\r\n"
            "Copyright (c) 1986-2024 by Cisco Systems, Inc.\r\nCompiled Wed 18-Sep-24 10:32 by mcpre"
        )
    if v == "Aruba":
        return (
            f"Aruba JL256A 2930F-48G-PoE+-4SFP+ Switch, revision {node.version}, ROM WC.16.01.0010 "
            "(/ws/swbuildm/rel_hartford_qaoff/code/build/anm(swbuildm_rel_hartford_qaoff_rel_hartford))"
        )
    if v == "Juniper":
        return (
            f"Juniper Networks, Inc. ex2300-24t Ethernet Switch, kernel JUNOS {node.version}.9, "
            "Build date: 2024-12-05 03:22:25 UTC Copyright (c) 1996-2024 Juniper Networks, Inc."
        )
    if v == "MikroTik":
        return f"RouterOS {node.model}"
    if v == "Synology":
        return "Linux nas01 4.4.302+ #72806 SMP Thu Sep 5 13:44:21 CST 2024 x86_64"
    if v == "QNAP":
        return f"Linux {node.hostname} 5.10.60-qnap #1 SMP Thu Jul 24 03:30:40 CST 2025 x86_64"
    if v == "APC":
        return (
            f"APC Web/SNMP Management Card (MB:v4.5.1 PF:v3.2.1.0 PN:apc_hw21_aos_3.2.1.0.bin AF1:v3.2.1.0 "
            f"AN1:apc_hw21_su_3.2.1.0.bin MN:{node.extra.get('nmc', 'AP9641')} HR:05 SN: ZA2316012345 "
            "MD:04/18/2023) (Embedded PowerNet SNMP Agent SW v2.2 compatible)"
        )
    if node.name == "mon01":
        return "Linux mon01 6.8.0-85-generic #85-Ubuntu SMP PREEMPT_DYNAMIC Thu Sep 18 15:26:59 UTC 2025 x86_64"
    return f"{node.os} {node.version}".strip()


def lldp_sys_desc(node: Node) -> str:
    """The System Description TLV a node sends (lldpd and the like when not one of our agents)."""
    if node.vendor in ("Cisco", "Aruba", "Juniper"):
        return sys_descr(node)
    if node.vendor == "MikroTik":
        return f"MikroTik RouterOS {node.version} (stable) {node.model}"
    if node.vendor == "Ubiquiti":
        return f"{node.model}, {node.version}"
    if node.kind == "phone":
        return f"{node.model} {node.version}"
    if node.os == "OPNsense":
        return f"OPNsense {node.version} FreeBSD 14.3-RELEASE-p4 amd64"
    if node.os == "Proxmox VE":
        kernel = node.extra.get("kernel", "6.14.11-2-pve")
        return f"Debian GNU/Linux 13 (trixie) Linux {kernel} #1 SMP PREEMPT_DYNAMIC PMX {kernel} x86_64"
    if node.os.startswith("Ubuntu"):
        return f"{node.os} Linux 6.8.0-85-generic #85-Ubuntu SMP PREEMPT_DYNAMIC x86_64"
    return f"{node.os} {node.version}".strip()


def advert(node: Node, port: Port, mgmt_ip: str | None = None) -> Advert:
    v = node.vendor
    name = node.hostname or node.name
    chassis = (4, mac_bytes(node.mac))
    if v == "Cisco":
        return Advert(
            *chassis,
            5,
            port.name.encode(),
            cisco_long(port.name),
            f"{node.name}.acme.local",
            lldp_sys_desc(node),
            (2, 4),
        )
    if v == "Aruba":
        return Advert(*chassis, 7, port.name.encode(), port.name, name, lldp_sys_desc(node), (2,))
    if v == "Juniper":
        return Advert(
            *chassis,
            7,
            str(junos_index(port.name)).encode(),
            port.descr or port.name,
            name,
            lldp_sys_desc(node),
            (2, 4),
        )
    if v == "MikroTik":
        return Advert(*chassis, 5, port.name.encode(), port.name, name, lldp_sys_desc(node), (2, 4))
    if v == "Ubiquiti":
        sub = 7 if node.kind == "switch" else 5
        caps = (2,) if node.kind == "switch" else (2, 3)
        return Advert(*chassis, sub, port.name.encode(), port.name, name, lldp_sys_desc(node), caps)
    if node.kind == "phone":
        ip = ipaddress.IPv4Address(mgmt_ip or node.ip or "0.0.0.0").packed
        return Advert(
            5, b"\x01" + ip, 3, mac_bytes(port.mac or node.mac), "WAN PORT", node.name, lldp_sys_desc(node), (2, 5)
        )
    caps = {"firewall": (4,), "router": (4,), "hypervisor": (2, 4), "ap": (2, 3)}.get(node.kind, (7,))
    return Advert(*chassis, 3, mac_bytes(port.mac or node.mac), port.name, name, lldp_sys_desc(node), caps)


def lldp_section(
    snap: Snapshot,
    node: Node,
    local: list[tuple[int, str]],
    rows: dict[str, IfRow],
) -> Callable[[], dict[Oid, Value]]:
    """LLDP-MIB: local system and ports (local = [(lldp port number, port name)]), remote table, mgmt addresses."""

    def build() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {}
        base = OI("1.0.8802.1.1.2.1")
        d[(*base, 1, 1, 0)] = Int(30)  # lldpMessageTxInterval
        d[(*base, 1, 2, 0)] = Int(4)
        loc = (*base, 3)
        any_port = next(iter(node.ports.values()))
        me = advert(node, any_port, node.ip)
        d[(*loc, 1, 0)] = Int(me.chassis_subtype)
        d[(*loc, 2, 0)] = Str(me.chassis)
        d[(*loc, 3, 0)] = Str(me.sys_name)
        d[(*loc, 4, 0)] = Str(me.sys_desc)
        d[(*loc, 5, 0)] = Bits(*me.caps)
        d[(*loc, 6, 0)] = Bits(*me.caps)
        nums = {}
        for num, pname in local:
            a = advert(node, node.ports[pname], node.ip)
            nums[pname] = num
            put_table(d, (*loc, 7, 1), [((num,), {2: Int(a.port_subtype), 3: Str(a.port_id), 4: Str(a.port_desc)})])
        mgmt_if = next((r.index for r in rows.values() if any(a.split("/")[0] == node.ip for a in r.ips)), 0)
        if node.ip:
            put_table(
                d, (*loc, 8, 1), [((1, 4, *ip_oid(node.ip)), {3: Int(5), 4: Int(2), 5: Int(mgmt_if), 6: ObjId("0.0")})]
            )
        rem = (*base, 4, 1, 1)
        man = (*base, 4, 2, 1)
        per_port: dict[str, int] = {}
        for nb in snap.lldp(node.name):
            num = nums.get(nb.local_port)
            if num is None:
                continue
            per_port[nb.local_port] = per_port.get(nb.local_port, 0) + 1
            ri = per_port[nb.local_port]
            a = advert(nb.node, nb.port, nb.mgmt_ip)
            idx = (0, num, ri)
            put_table(
                d,
                rem,
                [
                    (
                        idx,
                        {
                            4: Int(a.chassis_subtype),
                            5: Str(a.chassis),
                            6: Int(a.port_subtype),
                            7: Str(a.port_id),
                            8: Str(a.port_desc),
                            9: Str(a.sys_name),
                            10: Str(a.sys_desc),
                            11: Bits(*a.caps),
                            12: Bits(*a.caps),
                        },
                    )
                ],
            )
            if nb.mgmt_ip:
                put_table(d, man, [((*idx, 1, 4, *ip_oid(nb.mgmt_ip)), {3: Int(2), 4: Int(0), 5: ObjId("0.0")})])
        return d

    return build


# ---------------------------------------------------------------------------
# Shared sections


def system_section(
    snap: Snapshot,
    node: Node,
    descr: str,
    objid: str,
    name: str,
    services: int,
) -> Callable[[], dict[Oid, Value]]:
    def build() -> dict[Oid, Value]:
        b = OI("1.3.6.1.2.1.1")
        return {
            (*b, 1, 0): Str(descr),
            (*b, 2, 0): ObjId(objid),
            (*b, 3, 0): Ticks(snap.uptime(node.name) * 100),
            (*b, 4, 0): Str("TI Acme <ti@acme.com.br>"),
            (*b, 5, 0): Str(name),
            (*b, 6, 0): Str("Acme HQ - Sala de servidores"),
            (*b, 7, 0): Int(services),
            (*b, 8, 0): Ticks(0),
        }

    return build


def ip_section(
    rows: list[IfRow],
    forwarding: bool,
    arp: list[tuple[int, str, str, int]],
    ttl: int = 64,
) -> Callable[[], dict[Oid, Value]]:
    """IP-MIB: ipForwarding, ipAddrTable, ipNetToMediaTable (arp = [(ifIndex, ip, mac, type)])."""

    def build() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {OI("1.3.6.1.2.1.4.1.0"): Int(1 if forwarding else 2), OI("1.3.6.1.2.1.4.2.0"): Int(ttl)}
        addrs = []
        for r in rows:
            for a in r.ips:
                itf = ipaddress.ip_interface(a)
                addrs.append(
                    (
                        ip_oid(str(itf.ip)),
                        {1: Ip(str(itf.ip)), 2: Int(r.index), 3: Ip(str(itf.netmask)), 4: Int(1), 5: Int(65535)},
                    )
                )
        put_table(d, OI("1.3.6.1.2.1.4.20.1"), addrs)
        put_table(
            d,
            OI("1.3.6.1.2.1.4.22.1"),
            [((ifi, *ip_oid(ip)), {1: Int(ifi), 2: Mac(m), 3: Ip(ip), 4: Int(t)}) for ifi, ip, m, t in arp],
        )
        return d

    return build


def engine_section(agent: SnmpAgent, snap: Snapshot) -> Callable[[], dict[Oid, Value]]:
    def build() -> dict[Oid, Value]:
        boots, etime = agent.engine_clock(snap)
        b = OI("1.3.6.1.6.3.10.2.1")
        return {
            (*b, 1, 0): Str(agent.engine_id),
            (*b, 2, 0): Int(boots),
            (*b, 3, 0): Int(etime),
            (*b, 4, 0): Int(agent.dev.max_size),
        }

    return build


def bridge_section(
    base_mac: str | None,
    ports: list[tuple[int, int]],
    fdb: list[tuple[str, int, int]],
    qbridge: bool,
    vlan_names: dict[int, str] | None = None,
) -> Callable[[], dict[Oid, Value]]:
    """BRIDGE-MIB base ports (ports = [(bridge port, ifIndex)]) and the FDB [(mac, bridge port, fdb id)]."""

    def build() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {
            OI("1.3.6.1.2.1.17.1.1.0"): Mac(base_mac),
            OI("1.3.6.1.2.1.17.1.2.0"): Int(len(ports)),
            OI("1.3.6.1.2.1.17.1.3.0"): Int(2),
        }
        put_table(
            d,
            OI("1.3.6.1.2.1.17.1.4.1"),
            [((bp,), {1: Int(bp), 2: Int(ifi), 3: ObjId("0.0"), 4: C32(0), 5: C32(0)}) for bp, ifi in ports],
        )
        if qbridge:
            put_table(
                d, OI("1.3.6.1.2.1.17.7.1.2.2.1"), [((fid, *mac_oid(m)), {2: Int(bp), 3: Int(3)}) for m, bp, fid in fdb]
            )
            for vid, name in (vlan_names or {}).items():
                d[(*OI("1.3.6.1.2.1.17.7.1.4.3.1.1"), vid)] = Str(name)
                d[(*OI("1.3.6.1.2.1.17.7.1.4.3.1.5"), vid)] = Int(1)
        else:
            put_table(
                d, OI("1.3.6.1.2.1.17.4.3.1"), [(mac_oid(m), {1: Mac(m), 2: Int(bp), 3: Int(3)}) for m, bp, _ in fdb]
            )
        return d

    return build


def entity_section(rows: list[tuple[int, dict[int, Value]]]) -> Callable[[], dict[Oid, Value]]:
    def build() -> dict[Oid, Value]:
        d: dict[Oid, Value] = {}
        put_table(d, OI("1.3.6.1.2.1.47.1.1.1.1"), [((i,), cols) for i, cols in rows])
        return d

    return build


def gateway_arp(snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
    """A server's ARP cache: the default gateway (the HQ firewalls' CARP address) of each routed subnet."""
    out = []
    for r in rows:
        for a in r.ips:
            net = ipaddress.ip_interface(a).network
            for (site, vid), v in snap.design.vlans.items():
                if site == "hq" and v.subnet and v.gateway and v.routed and ipaddress.ip_network(v.subnet) == net:
                    out.append((r.index, v.gateway, acme.carp_mac(vid), 3))
    return out


# ---------------------------------------------------------------------------
# Net-SNMP pieces (UCD-SNMP-MIB, HOST-RESOURCES-MIB), shared by Linux, DSM and QTS


@dataclass
class LinuxHealth:
    cores: int
    mem_kb: int
    swap_kb: int
    disks: list[tuple[str, int, float]]  # (mount, size kB, used %)
    sensors: list[tuple[str, float]] = field(default_factory=list)  # lm-sensors temperatures (°C)


def netsnmp_sections(snap: Snapshot, node: Node, h: LinuxHealth) -> list[tuple[Oid, Callable[[], dict[Oid, Value]]]]:
    cpu = snap.cpu(node.name)
    mem = snap.mem(node.name)
    loads = [
        cpu / 100 * h.cores * f * (0.9 + 0.2 * h01("la", node.name, k, snap.b)) for k, f in enumerate((1.15, 1.0, 0.9))
    ]
    swap_used = h.swap_kb * (0.01 + 0.04 * h01("swap", node.name))
    # mem% = 1 - (avail + buffers + cached) / total, the way Omini's net-snmp profile computes it
    unused = h.mem_kb * (1 - mem / 100)
    buffers, cached = unused * 0.08, unused * 0.55
    avail = unused - buffers - cached
    used_hr = h.mem_kb - avail  # hrStorage "Physical memory" counts buffers and cache as used

    def ucd() -> dict[Oid, Value]:
        b = OI("1.3.6.1.4.1.2021")
        d: dict[Oid, Value] = {
            (*b, 4, 1, 0): Int(0),
            (*b, 4, 2, 0): Str("swap"),
            (*b, 4, 3, 0): Int(h.swap_kb),
            (*b, 4, 4, 0): Int(h.swap_kb - swap_used),
            (*b, 4, 5, 0): Int(h.mem_kb),
            (*b, 4, 6, 0): Int(avail),
            (*b, 4, 11, 0): Int(avail + h.swap_kb - swap_used),
            (*b, 4, 12, 0): Int(16000),
            (*b, 4, 13, 0): Int(h.mem_kb // 40),
            (*b, 4, 14, 0): Int(buffers),
            (*b, 4, 15, 0): Int(cached),
            (*b, 4, 100, 0): Int(0),
            (*b, 11, 1, 0): Int(1),
            (*b, 11, 2, 0): Str("systemStats"),
            (*b, 11, 9, 0): Int(round(cpu * 0.7)),
            (*b, 11, 10, 0): Int(round(cpu * 0.3)),
            (*b, 11, 11, 0): Int(max(0, round(100 - cpu))),
        }
        up = snap.uptime(node.name)
        idle = up * 100 * h.cores * (1 - cpu / 100)
        for col, part in ((50, 0.62), (51, 0.01), (52, 0.3), (53, 1.0), (54, 0.04)):  # ssCpuRaw* (ticks)
            d[(*b, 11, col, 0)] = C32(idle if col == 53 else up * h.cores * cpu * part)
        for k, (name, v) in enumerate(zip(("Load-1", "Load-5", "Load-15"), loads, strict=True), start=1):
            put_table(
                d,
                (*b, 10, 1),
                [
                    (
                        (k,),
                        {
                            1: Int(k),
                            2: Str(name),
                            3: Str(f"{v:.2f}"),
                            4: Str("12.00"),
                            5: Int(round(v * 100)),
                            6: (OPAQUE, b"\x9f\x78\x04" + struct.pack(">f", v)),
                            100: Int(0),
                        },
                    )
                ],
            )
        for k, (mount, size, pct) in enumerate(h.disks, start=1):
            used = int(size * pct / 100)
            put_table(
                d,
                (*b, 9, 1),
                [
                    (
                        (k,),
                        {
                            1: Int(k),
                            2: Str(mount),
                            3: Str(f"/dev/sda{k}"),
                            4: Int(-1),
                            5: Int(10),
                            6: Int(size),
                            7: Int(size - used),
                            8: Int(used),
                            9: Int(round(pct)),
                            100: Int(0),
                        },
                    )
                ],
            )
        if h.sensors:
            put_table(
                d,
                (*b, 13, 16, 2, 1),
                [((k,), {1: Int(k), 2: Str(name), 3: G32(c * 1000)}) for k, (name, c) in enumerate(h.sensors, start=1)],
            )
        return d

    def hr() -> dict[Oid, Value]:
        b = OI("1.3.6.1.2.1.25")
        d: dict[Oid, Value] = {
            (*b, 1, 1, 0): Ticks(snap.uptime(node.name) * 100),
            (*b, 1, 5, 0): G32(1),
            (*b, 1, 6, 0): G32(180 + int(40 * h01("procs", node.name))),
            (*b, 2, 2, 0): Int(h.mem_kb),
        }
        st = []
        ram = "1.3.6.1.2.1.25.2.1.2"
        virt = "1.3.6.1.2.1.25.2.1.3"
        other = "1.3.6.1.2.1.25.2.1.1"
        disk = "1.3.6.1.2.1.25.2.1.4"
        st.append((1, ram, "Physical memory", 1024, h.mem_kb, used_hr))
        st.append((3, virt, "Virtual memory", 1024, h.mem_kb + h.swap_kb, used_hr + swap_used))
        st.append((6, other, "Memory buffers", 1024, h.mem_kb, buffers))
        st.append((7, other, "Cached memory", 1024, cached, cached))
        st.append((10, virt, "Swap space", 1024, h.swap_kb, swap_used))
        for k, (mount, size, pct) in enumerate(h.disks):
            st.append((31 + k, disk, mount, 4096, size // 4, size // 4 * pct / 100))
        put_table(
            d,
            (*b, 2, 3, 1),
            [
                ((i,), {1: Int(i), 2: ObjId(t), 3: Str(desc), 4: Int(unit), 5: Int(size), 6: Int(used)})
                for i, t, desc, unit, size, used in st
            ],
        )
        for c in range(h.cores):
            load = max(0, min(100, round(cpu + (h01("core", node.name, c, snap.b) - 0.5) * 10)))
            put_table(d, (*b, 3, 3, 1), [((196608 + c,), {1: ObjId("0.0"), 2: Int(load)})])
        return d

    return [(OI("1.3.6.1.4.1.2021"), ucd), (OI("1.3.6.1.2.1.25"), hr)]


# ---------------------------------------------------------------------------
# Devices


Section = tuple[Oid, Callable[[], dict[Oid, Value]]]


class Device:
    """One vendor's agent: how it names things and which MIBs it has."""

    objid = "1.3.6.1.4.1.8072.3.2.10"
    services = 72
    forwarding = False
    xtable = True
    max_size = 1472
    contexts: tuple[str, ...] = ("",)

    def __init__(self, agent: SnmpAgent, node: Node) -> None:
        self.agent = agent
        self.node = node

    def sys_name(self) -> str:
        return self.node.hostname or self.node.name

    def rows(self, snap: Snapshot) -> list[IfRow]:
        raise NotImplementedError

    def arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        return []

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        return []

    def stack(self, rows: list[IfRow]) -> list[tuple[int, int]] | None:
        return None

    def valid_context(self, context: str) -> bool:
        return context in self.contexts

    def mib(self, snap: Snapshot, context: str) -> Mib:
        rows = self.rows(snap)
        n = self.node
        secs: list[Section] = [
            (OI("1.3.6.1.2.1.1"), system_section(snap, n, sys_descr(n), self.objid, self.sys_name(), self.services)),
            (
                OI("1.3.6.1.2.1.4"),
                ip_section(rows, self.forwarding, self.arp(snap, rows), 255 if n.vendor == "Cisco" else 64),
            ),
            (OI("1.3.6.1.6.3.10.2.1"), engine_section(self.agent, snap)),
            *if_sections(rows, self.xtable, self.stack(rows)),
        ]
        secs += self.extra(snap, rows, context)
        return Mib(secs)

    # helpers
    def l3_arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        by_port = {r.port: r for r in rows if r.port}
        out = []
        for ip, m, iface in snap.arp(self.node.name):
            r = by_port.get(iface)
            if r:
                out.append((r.index, ip, m, 3))
        for r in rows:  # its own addresses (static entries, as IOS and RouterOS list them)
            for a in r.ips:
                out.append((r.index, a.split("/")[0], r.mac or self.node.mac, 4))
        return out

    def fdb_by_port(self, snap: Snapshot) -> list[tuple[str, str, int]]:
        """The world's MAC table with a LAG member's entries moved to its LAG."""
        out = []
        for m, pname, vlan in snap.fdb(self.node.name):
            p = self.node.ports.get(pname)
            if p is not None and p.lag:
                pname = p.lag
            out.append((m, pname, vlan))
        return out


class CiscoIOSXE(Device):
    """Catalyst 9300 stack (IOS-XE 17.12): SNMPv3 only, per-VLAN BRIDGE-MIB contexts."""

    objid = "1.3.6.1.4.1.9.1.2502"  # ciscoC9300-48UXM (CISCO-PRODUCTS-MIB)
    services = 2  # "no ip routing": a layer 2 switch with a management SVI
    max_size = 1500

    def __init__(self, agent: SnmpAgent, node: Node) -> None:
        super().__init__(agent, node)
        self.vlans = sorted({v for p in node.ports.values() for v in (p.untagged, *p.tagged) if v})
        self.contexts = ("", *(f"vlan-{v}" for v in self.vlans))
        phys = [p for p in node.ports.values() if p.kind in ("ethernet", "sfp")]
        self.index: dict[str, int] = {"Gi0/0": 1}
        for m in (1, 2):
            for k, p in enumerate(q for q in phys if q.name[2:].startswith(f"{m}/")):
                self.index[p.name] = 9 + (m - 1) * 56 + k
        nxt = itertools.count(121)
        self.index["Null0"] = next(nxt)
        self.index["Vl1"] = next(nxt)
        for p in node.ports.values():
            if p.kind in ("vlan", "lag"):
                self.index[p.name] = next(nxt)
        # LLDP port numbers are internal (not the ifIndex)
        self.lldp_num = {p.name: k for k, p in enumerate(phys, start=1)}
        self.bridge_ports = {
            name: k
            for k, name in enumerate(
                [p.name for p in phys if not p.lag] + [p.name for p in node.ports.values() if p.kind == "lag"], start=1
            )
        }

    def sys_name(self) -> str:
        return f"{self.node.name}.acme.local"

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        out = [
            soft_row(
                snap, n, 1, "Gi0/0", descr="GigabitEthernet0/0", mac=acme.offset_mac(n.mac, 0x7F), oper=2, speed=1000
            )
        ]
        for p in n.ports.values():
            i = self.index[p.name]
            if p.kind in ("ethernet", "sfp"):
                r = port_row(
                    snap, n, p.name, i, descr=cisco_long(p.name), type=6, mac=p.mac or acme.offset_mac(n.mac, i)
                )
            elif p.kind == "lag":
                first = n.ports[p.members[0]]
                r = port_row(
                    snap,
                    n,
                    p.name,
                    i,
                    descr=cisco_long(p.name),
                    type=53,
                    mac=first.mac or acme.offset_mac(n.mac, self.index[first.name]),
                )
                if r.oper != 1:
                    r.oper = 7
            else:  # Vl10
                r = soft_row(
                    snap,
                    n,
                    i,
                    p.name,
                    descr=cisco_long(p.name),
                    type=53,
                    mac=p.mac,
                    speed=1000,
                    port=p.name,
                    ips=p.ips,
                    alias="MGMT",
                )
            out.append(r)
        out.append(soft_row(snap, n, self.index["Null0"], "Nu0", descr="Null0", type=1, oper=1, rate=0, speed=10000))
        out.append(
            soft_row(
                snap,
                n,
                self.index["Vl1"],
                "Vl1",
                descr="Vlan1",
                type=53,
                admin=False,
                oper=2,
                mac=acme.offset_mac(n.mac, 0x41),
                speed=1000,
            )
        )
        return sorted(out, key=lambda r: r.index)

    def arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        return self.l3_arp(snap, rows)

    def valid_context(self, context: str) -> bool:
        return context in self.contexts

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        by_port = {r.port: r for r in rows if r.port}
        vlan = int(context.split("-", 1)[1]) if context.startswith("vlan-") else 1
        ports = []
        for name, bp in self.bridge_ports.items():
            p = n.ports[name]
            if p.carries(vlan) or (vlan == 1 and p.untagged == 1):
                ports.append((bp, self.index[name]))
        fdb = [
            (m, self.bridge_ports[pn], v)
            for m, pn, v in self.fdb_by_port(snap)
            if v == vlan and pn in self.bridge_ports
        ]
        cpu = snap.cpu(n.name)
        mem = snap.mem(n.name)
        pool = 1_369_520_128
        used = int(pool * mem / 100)

        def vendor() -> dict[Oid, Value]:
            d: dict[Oid, Value] = {}
            # CISCO-PROCESS-MIB cpmCPUTotalTable (one row: the active switch)
            c5s = cpu + (h01("c5s", snap.b) - 0.5) * 8
            put_table(
                d,
                OI("1.3.6.1.4.1.9.9.109.1.1.1.1"),
                [
                    (
                        (1,),
                        {
                            2: Int(1000),
                            3: G32(max(1, c5s)),
                            4: G32(cpu),
                            5: G32(cpu),
                            6: G32(max(1, c5s)),
                            7: G32(cpu),
                            8: G32(cpu),
                            12: G32(8_041_000 * mem / 100),
                            13: G32(8_041_000 * (1 - mem / 100)),
                        },
                    )
                ],
            )
            # CISCO-MEMORY-POOL-MIB
            put_table(
                d,
                OI("1.3.6.1.4.1.9.9.48.1.1.1"),
                [
                    (
                        (1,),
                        {
                            2: Str("Processor"),
                            3: Int(0),
                            4: Int(1),
                            5: G32(used),
                            6: G32(pool - used),
                            7: G32((pool - used) * 0.82),
                        },
                    ),
                    ((2,), {2: Str("lsmpi_io"), 3: Int(0), 4: Int(1), 5: G32(6_294_304), 6: G32(208), 7: G32(208)}),
                ],
            )
            # CISCO-ENVMON-MIB temperatures, three sensors per stack member
            rows_t = []
            for m in (1, 2):
                for k, (sensor, off, thr) in enumerate((("Inlet", -14, 56), ("Outlet", -4, 125), ("HotSpot", 6, 125))):
                    t = snap.temperature(n.name, f"sw{m}-{sensor.lower()}") + off
                    rows_t.append(
                        (
                            (m * 1000 + 10 + k,),
                            {
                                2: Str(f"Switch {m} - {sensor} Temp Sensor"),
                                3: Int(round(t)),
                                4: Int(thr),
                                5: Int(0),
                                6: Int(1),
                            },
                        )
                    )
            put_table(d, OI("1.3.6.1.4.1.9.9.13.1.3.1"), rows_t)
            # CISCO-VTP-MIB vtpVlanTable: the VLANs (pollers use it to pick the vlan-<id> contexts)
            names = {1: "default"}
            for (site, vid), v in snap.design.vlans.items():
                if site == "hq":
                    names[vid] = v.name
            put_table(
                d,
                OI("1.3.6.1.4.1.9.9.46.1.3.1.1"),
                [
                    ((1, vid), {2: Int(1), 3: Int(1), 4: Str(names.get(vid, f"VLAN{vid:04d}"))})
                    for vid in sorted(set(self.vlans) | {1})
                ],
            )
            return d

        stack = n.extra.get("stack", [])
        ent = [(1, {2: Str("c93xx Switch Stack"), 5: Int(11), 7: Str("c93xx Switch Stack"), 11: Str(""), 13: Str("")})]
        for m, s in enumerate(stack, start=1):
            model, serial = s.split()
            ent.append(
                (
                    m * 1000,
                    {
                        2: Str(f"Cisco Catalyst 9300 Series {model} Switch"),
                        4: Int(1),
                        5: Int(3),
                        7: Str(f"Switch {m}"),
                        8: Str("V02"),
                        10: Str(n.version),
                        11: Str(serial),
                        12: Str("Cisco Systems Inc"),
                        13: Str(model),
                    },
                )
            )
        local = sorted((self.lldp_num[p], p) for p in self.lldp_num)
        return [
            (OI("1.3.6.1.2.1.17"), bridge_section(acme.offset_mac(n.mac, 0x80), ports, fdb, qbridge=False)),
            (OI("1.3.6.1.2.1.47"), entity_section(ent)),
            (OI("1.0.8802.1.1.2"), lldp_section(snap, n, local, by_port)),
            (OI("1.3.6.1.4.1.9"), vendor),
        ]


class ArubaOS(Device):
    """ArubaOS-Switch 2930F (WC.16.11)."""

    objid = "1.3.6.1.4.1.11.2.3.7.11.181.18"  # hpSwitchJL256A
    services = 74

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        out = []
        for p in n.ports.values():
            if p.kind in ("ethernet", "sfp"):
                out.append(port_row(snap, n, p.name, int(p.name), type=6))
            elif p.kind == "lag":
                r = port_row(snap, n, p.name, 289, type=161, mac=n.mac)
                out.append(r)
        vlans = sorted({v for p in n.ports.values() for v in (p.untagged, *p.tagged) if v})
        mgmt = next(p for p in n.ports.values() if p.kind == "vlan")
        for vid in vlans:
            name = "DEFAULT_VLAN" if vid == 1 else snap.design.vlans[("hq", vid)].name
            kw: dict[str, Any] = {"descr": name, "type": 53, "mac": n.mac, "speed": 0}
            if vid == mgmt.vlan:
                kw.update(port=mgmt.name, ips=mgmt.ips)
            out.append(soft_row(snap, n, 1000 + vid, f"VLAN{vid}", rate=300 if vid == mgmt.vlan else 40, **kw))
        return sorted(out, key=lambda r: r.index)

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        by_port = {r.port: r for r in rows if r.port}
        bports = [(r.index, r.index) for r in rows if r.type in (6, 161) and not n.ports[r.port or ""].lag]
        valid = {r.port: r.index for r in rows if r.port}
        fdb = [(m, valid[p], v) for m, p, v in self.fdb_by_port(snap) if p in valid]
        names = {1: "DEFAULT_VLAN"}
        for p in n.ports.values():
            for v in (p.untagged, *p.tagged):
                if v and v != 1:
                    names[v] = snap.design.vlans[("hq", v)].name
        cpu = snap.cpu(n.name)
        mem = snap.mem(n.name)
        total = 337_985_536

        def vendor() -> dict[Oid, Value]:
            d: dict[Oid, Value] = {OI("1.3.6.1.4.1.11.2.14.11.5.1.9.6.1.0"): Int(round(cpu))}  # hpSwitchCpuStat
            free = int(total * (1 - mem / 100))
            put_table(
                d,
                OI("1.3.6.1.4.1.11.2.14.11.5.1.1.2.1.1.1"),
                [
                    (
                        (1,),
                        {
                            1: Int(1),
                            2: Int(1),
                            3: Int(1),
                            4: Int(1),
                            5: G32(total),
                            6: G32(free),
                            7: G32(total - free),
                        },
                    )
                ],
            )
            return d

        ent = [
            (
                1,
                {
                    2: Str(f"Aruba JL256A 2930F-48G-PoE+-4SFP+ Switch, revision {n.version}"),
                    5: Int(3),
                    7: Str("Chassis"),
                    10: Str(n.version),
                    11: Str(n.serial),
                    12: Str("Aruba"),
                    13: Str("JL256A"),
                },
            )
        ]
        local = [(int(p.name), p.name) for p in n.ports.values() if p.kind in ("ethernet", "sfp")]
        return [
            (OI("1.3.6.1.2.1.17"), bridge_section(n.mac, bports, fdb, qbridge=True, vlan_names=names)),
            (OI("1.3.6.1.2.1.47"), entity_section(ent)),
            (OI("1.0.8802.1.1.2"), lldp_section(snap, n, local, by_port)),
            (OI("1.3.6.1.4.1.11"), vendor),
        ]


class Junos(Device):
    """EX2300 (Junos 23.4, ELS): the bridge is made of logical units."""

    objid = "1.3.6.1.4.1.2636.1.1.1.2.132"  # jnxProductNameEX2300
    services = 4
    forwarding = True

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        out = [
            soft_row(snap, n, 6, "lo0", type=24, mtu=65535, rate=20),
            soft_row(snap, n, 16, "lo0.0", type=24, mtu=65535, rate=20, ips=("127.0.0.1/32",)),
        ]
        for p in n.ports.values():
            i = junos_index(p.name)
            r = port_row(snap, n, p.name, i, type=6, mtu=1514)
            out.append(r)
            unit = IfRow(
                index=i + 1,
                name=f"{p.name}.0",
                descr=f"{p.name}.0",
                type=53,
                mtu=1500,
                speed=r.speed,
                oper=r.oper,
                admin=r.admin,
                rx=int(r.rx * 0.98),
                tx=int(r.tx * 0.98),
                rxp=int(r.rxp * 0.98),
                txp=int(r.txp * 0.98),
                last_change=r.last_change,
            )
            if p.name == "me0":
                unit.ips, r.ips = p.ips, ()
                unit.port = None
            out.append(unit)
        out.append(soft_row(snap, n, 502, "irb", type=53, mtu=1514, rate=0, mac=acme.offset_mac(n.mac, 0x50)))
        return sorted(out, key=lambda r: r.index)

    def stack(self, rows: list[IfRow]) -> list[tuple[int, int]]:
        out = []
        idx = {r.name: r.index for r in rows}
        for r in rows:
            if "." in r.name and r.name.split(".")[0] in idx:
                out += [(0, r.index), (r.index, idx[r.name.split(".")[0]])]
            elif "." not in r.name:
                out.append((r.index, 0))
        return out

    def arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        return gateway_arp(snap, rows)

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        by_port = {r.port: r for r in rows if r.port}
        bridge = [p for p in n.ports.values() if p.name != "me0"]
        bports = {p.name: k for k, p in enumerate(bridge, start=1)}
        vlans = sorted({p.untagged for p in bridge if p.untagged})
        fdb_id = {v: k for k, v in enumerate(vlans, start=3)}  # internal VLAN indexes, not VLAN ids
        fdb = [(m, bports[p], fdb_id.get(v, 2)) for m, p, v in snap.fdb(n.name) if p in bports]
        # The firewalls' DMZ interfaces talk all day: the world's DMZ table misses them (see the report)
        seen = {m for m, _, _ in fdb}
        for pname in ("ge-0/0/0", "ge-0/0/1"):
            peer = snap.peer(n.name, pname)
            if peer and snap.port_up(n.name, pname) and peer[1].mac and peer[1].mac not in seen:
                fdb.append((peer[1].mac, bports[pname], fdb_id.get(n.ports[pname].untagged or 0, 2)))
        cpu = snap.cpu(n.name)
        mem = snap.mem(n.name)
        temp = snap.temperature(n.name)
        names = {fdb_id[v]: snap.design.vlans[("hq", v)].name for v in vlans if ("hq", v) in snap.design.vlans}

        def vendor() -> dict[Oid, Value]:
            b = OI("1.3.6.1.4.1.2636.3.1")
            d: dict[Oid, Value] = {(*b, 2, 0): Str("Juniper EX2300-24T Switch"), (*b, 3, 0): Str(n.serial)}
            comp = [
                ((1, 1, 0, 0), "midplane", 0, 0, 0),
                ((2, 1, 0, 0), "Power Supply 0", 0, 0, 0),
                ((4, 1, 1, 0), "Fan Tray 0 Fan 0", 0, 0, 0),
                ((7, 1, 0, 0), "FPC: EX2300-24T @ 0/*/*", round(temp), round(cpu * 0.6), round(mem * 0.9)),
                ((8, 1, 1, 0), "PIC: 24x10/100/1000 Base-T @ 0/0/*", 0, 0, 0),
                ((8, 1, 2, 0), "PIC: 4x10G SFP/SFP+ @ 0/1/*", 0, 0, 0),
                ((9, 1, 0, 0), "Routing Engine 0", round(temp - 3), round(cpu), round(mem)),
            ]
            put_table(
                d,
                (*b, 13, 1),
                [
                    (
                        idx,
                        {
                            5: Str(desc),
                            6: Int(2),
                            7: G32(t),
                            8: G32(c),
                            11: G32(buf),
                            10: G32(2048 if idx[0] == 9 else 0),
                        },
                    )
                    for idx, desc, t, c, buf in comp
                ],
            )
            return d

        return [
            (
                OI("1.3.6.1.2.1.17"),
                bridge_section(
                    acme.offset_mac(n.mac, 0x60),
                    [(bp, junos_index(p) + 1) for p, bp in bports.items()],
                    fdb,
                    qbridge=True,
                    vlan_names=names,
                ),
            ),
            (
                OI("1.0.8802.1.1.2"),
                lldp_section(snap, n, [(junos_index(p.name), p.name) for p in n.ports.values()], by_port),
            ),
            (OI("1.3.6.1.4.1.2636"), vendor),
        ]


class RouterOS(Device):
    """CRS518 on RouterOS 7: MIKROTIK-MIB, HOST-RESOURCES, no LLDP-MIB."""

    objid = "1.3.6.1.4.1.14988.1"
    services = 78
    forwarding = True

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        out = []
        for k, p in enumerate(n.ports.values(), start=1):
            t = {"lag": 161, "bridge": 209, "vlan": 135}.get(p.kind, 6)
            if p.kind in ("bridge", "vlan"):
                out.append(
                    soft_row(
                        snap,
                        n,
                        k,
                        p.name,
                        type=t,
                        mac=p.mac,
                        port=p.name,
                        ips=p.ips,
                        alias=p.descr,
                        rate=900 if p.kind == "vlan" else 4000,
                        speed=0,
                        mtu=1500,
                    )
                )
            else:
                r = port_row(snap, n, p.name, k, type=t, mtu=1500)
                out.append(r)
        return out

    def arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        return self.l3_arp(snap, rows)

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        idx = {r.port: r.index for r in rows if r.port}
        bridge = n.ports["bridge"]
        bports = {name: k for k, name in enumerate((m for m in bridge.members if not n.ports[m].lag), start=1)}
        fdb = [(m, bports[p], v) for m, p, v in self.fdb_by_port(snap) if p in bports]
        names = {
            v: snap.design.vlans[("hq", v)].name
            for p in n.ports.values()
            for v in (p.untagged, *p.tagged)
            if v and ("hq", v) in snap.design.vlans
        }
        cpu = snap.cpu(n.name)
        mem = snap.mem(n.name)
        fw = snap.firmware(n.name)
        temp = snap.temperature(n.name)

        def hr() -> dict[Oid, Value]:
            b = OI("1.3.6.1.2.1.25")
            total = 2_097_152  # kB
            d: dict[Oid, Value] = {(*b, 1, 1, 0): Ticks(snap.uptime(n.name) * 100), (*b, 2, 2, 0): Int(total)}
            put_table(
                d,
                (*b, 2, 3, 1),
                [
                    (
                        (65536,),
                        {
                            1: Int(65536),
                            2: ObjId("1.3.6.1.2.1.25.2.1.2"),
                            3: Str("main memory"),
                            4: Int(1024),
                            5: Int(total),
                            6: Int(total * mem / 100),
                        },
                    ),
                    (
                        (131072,),
                        {
                            1: Int(131072),
                            2: ObjId("1.3.6.1.2.1.25.2.1.4"),
                            3: Str("system disk"),
                            4: Int(1024),
                            5: Int(131072),
                            6: Int(41_820),
                        },
                    ),
                ],
            )
            for c in range(4):
                put_table(
                    d,
                    (*b, 3, 3, 1),
                    [
                        (
                            (c + 1,),
                            {
                                1: ObjId("0.0"),
                                2: Int(max(0, min(100, round(cpu + (h01("core", n.name, c, snap.b) - 0.5) * 8)))),
                            },
                        )
                    ],
                )
            return d

        def vendor() -> dict[Oid, Value]:
            b = OI("1.3.6.1.4.1.14988.1.1")
            d: dict[Oid, Value] = {
                (*b, 3, 8, 0): Int(0),  # mtxrHlActiveFan
                (*b, 4, 1, 0): Str("Q8VN-J3PK"),
                (*b, 4, 3, 0): Int(6),
                (*b, 4, 4, 0): Str(n.version),
                (*b, 7, 1, 0): Str(""),
                (*b, 7, 3, 0): Str(n.serial),
                (*b, 7, 4, 0): Str(fw["current"]),
                (*b, 7, 5, 0): Str(""),
                (*b, 7, 6, 0): Str("2024-11-26 12:34:12"),
                (*b, 7, 7, 0): Str(fw["latest"]),
                (*b, 7, 8, 0): Str(n.model),
                (*b, 7, 9, 0): Str("CRS518-16XS-2XQ-RM"),
            }
            # RouterOS 7 health: mtxrGaugeTable (name, value, unit: 1 °C, 2 rpm, 6 status)
            gauges = [
                ("fan1-speed", 6420 + int(h01("fan", snap.b) * 300), 2),
                ("fan2-speed", 6390 + int(h01("fan2", snap.b) * 300), 2),
                ("psu1-state", 1, 6),
                ("psu2-state", 1, 6),
                ("cpu-temperature", round(temp + 9), 1),
                ("switch-temperature", round(temp), 1),
            ]
            put_table(
                d,
                (*b, 3, 100, 1),
                [
                    ((k,), {1: Int(k), 2: Str(name), 3: G32(v), 4: Int(u)})
                    for k, (name, v, u) in enumerate(gauges, start=1)
                ],
            )
            # mtxrNeighborTable: what LLDP/CDP/MNDP discovered
            rows_n = []
            for k, nb in enumerate(snap.lldp(n.name), start=1):
                a = advert(nb.node, nb.port, nb.mgmt_ip)
                rows_n.append(
                    (
                        (k,),
                        {
                            1: Int(k),
                            2: Ip(nb.mgmt_ip or "0.0.0.0"),
                            3: Mac(nb.port.mac or nb.node.mac),
                            4: Str(nb.node.version),
                            5: Str(nb.node.vendor or ""),
                            6: Str(a.sys_name),
                            7: Str(""),
                            8: Int(idx.get(nb.local_port, 0)),
                        },
                    )
                )
            put_table(d, (*b, 11, 1, 1), rows_n)
            return d

        return [
            (
                OI("1.3.6.1.2.1.17"),
                bridge_section(
                    bridge.mac, [(bp, idx[p]) for p, bp in bports.items()], fdb, qbridge=True, vlan_names=names
                ),
            ),
            (OI("1.3.6.1.2.1.25"), hr),
            (OI("1.3.6.1.4.1.14988"), vendor),
        ]


class LinuxHost(Device):
    """A Linux VM with net-snmp (Ubuntu 24.04)."""

    services = 72
    max_size = 16384

    def health(self) -> LinuxHealth:
        n = self.node
        return LinuxHealth(
            cores=n.cores or 4,
            mem_kb=(n.mem_mb or 8192) * 1024,
            swap_kb=4_194_300,
            disks=[("/", (n.disk_gb or 200) * 1_000_000, 31.0 + 6 * h01("disk", n.name)), ("/boot", 2_000_000, 9.0)],
        )

    def nic(self) -> str:
        return "ens18"

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        own = next(iter(n.ports))
        r = port_row(
            snap,
            n,
            own,
            2,
            name=self.nic(),
            descr=self.nic(),
            alias="",
            ips=(f"{n.ip}/{ipaddress.ip_network(snap.design.vlans[('hq', n.vlan)].subnet).prefixlen}",),
        )
        lo = soft_row(snap, n, 1, "lo", type=24, mtu=65536, rate=900, speed=10, ips=("127.0.0.1/8",))
        return [lo, r]

    def arp(self, snap: Snapshot, rows: list[IfRow]) -> list[tuple[int, str, str, int]]:
        return gateway_arp(snap, rows)

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        return netsnmp_sections(snap, self.node, self.health())


class NasLinux(LinuxHost):
    """A NAS (Synology DSM / QNAP QTS): onboard 1 GbE ports, the 10 GbE card on eth4, VLAN subinterfaces."""

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        eth4 = n.ports["eth4"]
        out = [soft_row(snap, n, 1, "lo", type=24, mtu=65536, rate=1500, speed=10, ips=("127.0.0.1/8",))]
        for k in range(4):
            out.append(soft_row(snap, n, 2 + k, f"eth{k}", oper=2, speed=1000, mac=acme.offset_mac(n.mac, k + 1)))
        main = port_row(snap, n, "eth4", 6, ips=tuple(a for a in eth4.ips if self._vlan_of(snap, a) == eth4.untagged))
        out.append(main)
        out.append(soft_row(snap, n, 7, "eth5", oper=2, speed=10000, mac=acme.offset_mac(n.mac, 5)))
        shares = {20: 0.18, 100: 0.6, 110: 0.2}
        for k, vid in enumerate(eth4.tagged):
            ips = tuple(a for a in eth4.ips if self._vlan_of(snap, a) == vid)
            f = shares.get(vid, 0.1)
            out.append(
                IfRow(
                    index=8 + k,
                    name=f"eth4.{vid}",
                    descr=f"eth4.{vid}",
                    mac=eth4.mac,
                    speed=main.speed,
                    oper=main.oper,
                    rx=int(main.rx * f),
                    tx=int(main.tx * f),
                    rxp=int(main.rxp * f),
                    txp=int(main.txp * f),
                    last_change=main.last_change,
                    ips=ips,
                )
            )
        return out

    @staticmethod
    def _vlan_of(snap: Snapshot, addr: str) -> int | None:
        net = ipaddress.ip_interface(addr).network
        for (site, vid), v in snap.design.vlans.items():
            if site == "hq" and v.subnet and ipaddress.ip_network(v.subnet) == net:
                return vid
        return None


class SynologyDSM(NasLinux):
    def health(self) -> LinuxHealth:
        return LinuxHealth(cores=4, mem_kb=16_166_000, swap_kb=6_918_000, disks=[("/", 2_385_528, 41.0)])

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        secs = netsnmp_sections(snap, n, self.health())
        disks = int(n.extra.get("disks", 8))
        used_pct = snap.nas_used_pct()
        total = int(float(n.extra.get("volume_tb", 64)) * 1e12)
        fw = snap.firmware(n.name)

        def hr_with_volume() -> dict[Oid, Value]:
            d = dict(secs[1][1]())
            idx = 51
            put_table(
                d,
                OI("1.3.6.1.2.1.25.2.3.1"),
                [
                    (
                        (idx,),
                        {
                            1: Int(idx),
                            2: ObjId("1.3.6.1.2.1.25.2.1.4"),
                            3: Str("/volume1"),
                            4: Int(4096),
                            5: Int(min(total // 4096, 2**31 - 1)),
                            6: Int(min(int(total * used_pct / 100) // 4096, 2**31 - 1)),
                        },
                    )
                ],
            )
            return d

        def vendor() -> dict[Oid, Value]:
            b = OI("1.3.6.1.4.1.6574")
            d: dict[Oid, Value] = {
                (*b, 1, 1, 0): Int(1),
                (*b, 1, 2, 0): Int(round(snap.temperature(n.name, "system"))),
                (*b, 1, 3, 0): Int(1),
                (*b, 1, 4, 1, 0): Int(1),
                (*b, 1, 4, 2, 0): Int(1),
                (*b, 1, 5, 1, 0): Str(n.model),
                (*b, 1, 5, 2, 0): Str(n.serial),
                (*b, 1, 5, 3, 0): Str(f"DSM {n.version}"),
                (*b, 1, 5, 4, 0): Int(1 if fw["pending"] else 2),
                (*b, 1, 6, 0): Int(1),
            }
            put_table(
                d,
                (*b, 2, 1, 1),
                [
                    (
                        (k,),
                        {
                            1: Int(k),
                            2: Str(f"Disk {k + 1}"),
                            3: Str("HAT5300-16T"),
                            4: Str("SATA"),
                            5: Int(1),
                            6: Int(round(snap.temperature(n.name, f"disk{k + 1}"))),
                            7: Int(1),
                            8: Int(0),
                            9: Int(0),
                            10: Int(0),
                            11: Int(0),
                            12: Str("Drive 1"),
                            13: Int(1),
                        },
                    )
                    for k in range(disks)
                ],
            )
            put_table(
                d,
                (*b, 3, 1, 1),
                [
                    (
                        (0,),
                        {1: Int(0), 2: Str("Volume 1"), 3: Int(1), 4: C64(total * (1 - used_pct / 100)), 5: C64(total)},
                    )
                ],
            )
            return d

        return [secs[0], (OI("1.3.6.1.2.1.25"), hr_with_volume), (OI("1.3.6.1.4.1.6574"), vendor)]


class QnapQTS(NasLinux):
    def health(self) -> LinuxHealth:
        return LinuxHealth(cores=4, mem_kb=16_281_000, swap_kb=8_388_600, disks=[("/", 409_600, 63.0)])

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        secs = netsnmp_sections(snap, n, self.health())
        cpu = snap.cpu(n.name)
        mem = snap.mem(n.name)
        total_gb = 15.5
        disks = int(n.extra.get("disks", 8))

        def cf(t: float) -> str:
            return f"{round(t)} C/{round(t * 9 / 5 + 32)} F"

        def vendor() -> dict[Oid, Value]:
            b = OI("1.3.6.1.4.1.24681.1.2")
            d: dict[Oid, Value] = {
                (*b, 1, 0): Str(f"{cpu:.1f} %"),
                (*b, 2, 0): Str(f"{total_gb:.1f} GB"),
                (*b, 3, 0): Str(f"{total_gb * (1 - mem / 100):.1f} GB"),
                (*b, 4, 0): Ticks(snap.uptime(n.name) * 100),
                (*b, 5, 0): Str(cf(snap.temperature(n.name, "cpu"))),
                (*b, 6, 0): Str(cf(snap.temperature(n.name, "system"))),
                (*b, 10, 0): Int(disks),
                (*b, 12, 0): Str(n.model),
                (*b, 13, 0): Str(n.hostname),
                (*b, 14, 0): Int(3),
                (*b, 16, 0): Int(1),
            }
            put_table(
                d,
                (*b, 11, 1),
                [
                    (
                        (k,),
                        {
                            1: Int(k),
                            2: Str(f"HDD{k}"),
                            3: Str(cf(snap.temperature(n.name, f"disk{k}") - 4)),
                            4: Int(0),
                            5: Str("ST16000NE000-2RW103"),
                            6: Str("14.55 TB"),
                            7: Str("GOOD"),
                        },
                    )
                    for k in range(1, disks + 1)
                ],
            )
            put_table(
                d,
                (*b, 15, 1),
                [
                    ((k,), {1: Int(k), 2: Str(f"System FAN {k}"), 3: Str(f"{1180 + int(h01('qfan', k) * 200)} RPM")})
                    for k in (1, 2, 3)
                ],
            )
            put_table(
                d,
                (*b, 17, 1),
                [
                    (
                        (1,),
                        {
                            1: Int(1),
                            2: Str("[Volume Backup, Pool 1]"),
                            3: Str("EXT4"),
                            4: Str("98.12 TB"),
                            5: Str("41.37 TB"),
                            6: Str("Ready"),
                        },
                    )
                ],
            )
            return d

        ent = [
            (
                1,
                {
                    2: Str(n.model),
                    5: Int(3),
                    7: Str(n.model),
                    10: Str(n.version),
                    11: Str(n.serial),
                    12: Str("QNAP"),
                    13: Str(n.model),
                },
            )
        ]
        return [*secs, (OI("1.3.6.1.2.1.47"), entity_section(ent)), (OI("1.3.6.1.4.1.24681"), vendor)]


class ApcNmc(Device):
    """APC Smart-UPS with a Network Management Card 3: MIB-II (no ifXTable), UPS-MIB, PowerNet-MIB."""

    objid = "1.3.6.1.4.1.318.1.3.27"  # smartUPS
    services = 72
    xtable = False

    def rows(self, snap: Snapshot) -> list[IfRow]:
        n = self.node
        r = port_row(snap, n, "eth0", 1, ips=(f"{n.ip}/24",), alias="")
        return [r]

    def extra(self, snap: Snapshot, rows: list[IfRow], context: str) -> list[Section]:
        n = self.node
        x = n.extra
        load = round(float(x.get("load_pct", 38)) - 6 + snap.cpu(n.name) * 0.4)
        runtime = max(5, round(float(x.get("runtime_min", 27)) * 38 / max(load, 1)))
        batt_t = round(25.5 + 2 * h01("ups-t", snap.b), 1)
        cap = int(x.get("battery_pct", 100))
        volts, hz = 220, 60

        def ups() -> dict[Oid, Value]:
            b = OI("1.3.6.1.2.1.33.1")
            d: dict[Oid, Value] = {
                (*b, 1, 1, 0): Str("APC"),
                (*b, 1, 2, 0): Str(n.model),
                (*b, 1, 3, 0): Str("UPS 16.5 (ID1003)"),
                (*b, 1, 4, 0): Str(n.version.split()[-1]),
                (*b, 1, 5, 0): Str(n.name),
                (*b, 2, 1, 0): Int(2),
                (*b, 2, 2, 0): Int(0),
                (*b, 2, 3, 0): Int(runtime),
                (*b, 2, 4, 0): Int(cap),
                (*b, 2, 5, 0): Int(2190),
                (*b, 2, 7, 0): Int(round(batt_t)),
                (*b, 3, 1, 0): C32(2),
                (*b, 3, 2, 0): Int(1),
                (*b, 4, 1, 0): Int(3),
                (*b, 4, 2, 0): Int(hz * 10),
                (*b, 4, 3, 0): Int(1),
            }
            put_table(d, (*b, 3, 3, 1), [((1,), {1: Int(1), 2: Int(hz * 10), 3: Int(volts)})])
            put_table(
                d,
                (*b, 4, 4, 1),
                [((1,), {1: Int(1), 2: Int(volts), 3: Int(round(load * 2.1)), 4: Int(round(load * 50)), 5: Int(load)})],
            )
            return d

        def powernet() -> dict[Oid, Value]:
            b = OI("1.3.6.1.4.1.318.1.1.1")
            d: dict[Oid, Value] = {
                (*b, 1, 1, 1, 0): Str(n.model),
                (*b, 1, 1, 2, 0): Str(n.name),
                (*b, 1, 2, 1, 0): Str("UPS 16.5 (ID1003)"),
                (*b, 1, 2, 2, 0): Str("04/18/2023"),
                (*b, 1, 2, 3, 0): Str(n.serial),
                (*b, 1, 2, 5, 0): Str("SRT5KXLI"),
                (*b, 2, 1, 1, 0): Int(2),
                (*b, 2, 1, 2, 0): Ticks(0),
                (*b, 2, 1, 3, 0): Str("04/18/2023"),
                (*b, 2, 2, 1, 0): G32(cap),
                (*b, 2, 2, 2, 0): G32(round(batt_t)),
                (*b, 2, 2, 3, 0): Ticks(runtime * 6000),
                (*b, 2, 2, 4, 0): Int(1),
                (*b, 2, 3, 1, 0): G32(cap * 10),
                (*b, 2, 3, 2, 0): G32(batt_t * 10),
                (*b, 3, 2, 1, 0): G32(volts),
                (*b, 3, 2, 4, 0): G32(hz),
                (*b, 3, 3, 1, 0): G32(volts * 10),
                (*b, 4, 1, 1, 0): Int(2),
                (*b, 4, 2, 1, 0): G32(volts),
                (*b, 4, 2, 2, 0): G32(hz),
                (*b, 4, 2, 3, 0): G32(load),
                (*b, 4, 2, 4, 0): G32(round(load * 0.21)),
                (*b, 4, 3, 1, 0): G32(volts * 10),
                (*b, 4, 3, 3, 0): G32(load * 10),
                (*b, 7, 2, 3, 0): Int(1),  # upsAdvTestDiagnosticsResults: ok
            }
            return d

        return [(OI("1.3.6.1.2.1.33"), ups), (OI("1.3.6.1.4.1.318"), powernet)]


def device_for(agent: SnmpAgent, node: Node) -> Device:
    kinds: dict[str, type[Device]] = {
        "Cisco": CiscoIOSXE,
        "Aruba": ArubaOS,
        "Juniper": Junos,
        "MikroTik": RouterOS,
        "Synology": SynologyDSM,
        "QNAP": QnapQTS,
        "APC": ApcNmc,
    }
    return kinds.get(node.vendor, LinuxHost)(agent, node)


# ---------------------------------------------------------------------------
# USM (RFC 3414, RFC 7860, RFC 3826; key extensions as gosnmp / net-snmp do them)

AUTH_HASH = {
    "md5": "md5",
    "sha": "sha1",
    "sha1": "sha1",
    "sha224": "sha224",
    "sha256": "sha256",
    "sha384": "sha384",
    "sha512": "sha512",
}
AUTH_LEN = {"md5": 12, "sha1": 12, "sha224": 16, "sha256": 24, "sha384": 32, "sha512": 48}
PRIV_LEN = {"des": 16, "aes": 16, "aes128": 16, "aes192": 24, "aes256": 32, "aes192c": 24, "aes256c": 32}

USM_STATS = "1.3.6.1.6.3.15.1.1"
UNSUPPORTED_SEC_LEVELS, NOT_IN_TIME_WINDOWS, UNKNOWN_USER_NAMES = 1, 2, 3
UNKNOWN_ENGINE_IDS, WRONG_DIGESTS, DECRYPTION_ERRORS = 4, 5, 6
UNKNOWN_CONTEXTS = OI("1.3.6.1.6.3.12.1.5.0")
TIME_WINDOW = 150


def password_to_key(hname: str, password: bytes) -> bytes:
    if not password:
        raise ValueError("empty password")
    n = 1_048_576
    return hashlib.new(hname, (password * (n // len(password) + 1))[:n]).digest()


def localize_key(hname: str, password: bytes, engine: bytes) -> bytes:
    ku = password_to_key(hname, password)
    return hashlib.new(hname, ku + engine + ku).digest()


def priv_key(hname: str, priv: str, password: bytes, engine: bytes) -> bytes:
    key = localize_key(hname, password, engine)
    if priv in ("aes192", "aes256"):  # Blumenthal
        key += hashlib.new(hname, key).digest()
    elif priv in ("aes", "aes128", "aes192c", "aes256c"):  # Reeder
        key += localize_key(hname, key, engine)
    return key[: PRIV_LEN[priv]]


def auth_digest(hname: str, key: bytes, msg: bytes) -> bytes:
    return hmac.new(key, msg, hname).digest()[: AUTH_LEN[hname]]


def encrypt(priv: str, key: bytes, boots: int, etime: int, salt: bytes, plain: bytes) -> bytes:
    if priv == "des":
        iv = bytes(a ^ b for a, b in zip(key[8:16], salt, strict=True))
        plain += b"\0" * (-len(plain) % 8)
        enc = Cipher(TripleDES(key[:8] * 3), modes.CBC(iv)).encryptor()
        return enc.update(plain) + enc.finalize()
    iv = struct.pack(">II", boots, etime) + salt
    enc = Cipher(algorithms.AES(key), CFB(iv)).encryptor()
    return enc.update(plain) + enc.finalize()


def decrypt(priv: str, key: bytes, boots: int, etime: int, salt: bytes, data: bytes) -> bytes:
    if priv == "des":
        if len(data) % 8 or len(salt) != 8:
            raise ValueError("bad DES ciphertext")
        iv = bytes(a ^ b for a, b in zip(key[8:16], salt, strict=True))
        dec = Cipher(TripleDES(key[:8] * 3), modes.CBC(iv)).decryptor()
        return dec.update(data) + dec.finalize()
    if len(salt) != 8:
        raise ValueError("bad AES salt")
    iv = struct.pack(">II", boots, etime) + salt
    dec = Cipher(algorithms.AES(key), CFB(iv)).decryptor()
    return dec.update(data) + dec.finalize()


@dataclass
class Usm:
    user: bytes
    auth: str  # hashlib name or "none"
    priv: str  # des | aes | aes192 | aes256 | aes192c | aes256c | none
    auth_key: bytes = b""
    priv_key: bytes = b""

    @classmethod
    def from_config(cls, v3: dict[str, Any], engine: bytes) -> Usm:
        auth = AUTH_HASH.get(str(v3.get("auth", "none")).lower(), "none")
        priv = str(v3.get("priv", "none")).lower()
        if priv in ("", "none") or auth == "none":
            priv = "none"
        u = cls(str(v3["user"]).encode(), auth, priv)
        if auth != "none":
            u.auth_key = localize_key(auth, str(v3["auth_pass"]).encode(), engine)
        if priv != "none":
            u.priv_key = priv_key(auth, priv, str(v3["priv_pass"]).encode(), engine)
        return u

    @property
    def flags(self) -> int:
        return (1 if self.auth != "none" else 0) | (2 if self.priv != "none" else 0)


def build_v3(
    msg_id: int,
    max_size: int,
    flags: int,
    engine: bytes,
    boots: int,
    etime: int,
    user: bytes,
    scoped: bytes,
    usm: Usm | None,
    salt: bytes = b"",
) -> bytes:
    """A v3 message; scoped is the encoded ScopedPDU (encrypted here when flags ask for privacy)."""
    priv_params = b""
    body = scoped
    if flags & 2 and usm is not None:
        priv_params = salt
        body = tlv(OCTETS, encrypt(usm.priv, usm.priv_key, boots, etime, salt, scoped))
    alen = AUTH_LEN[usm.auth] if flags & 1 and usm is not None else 0
    prefix = tlv(OCTETS, engine) + enc_int(boots) + enc_int(etime) + tlv(OCTETS, user)
    auth_tlv = tlv(OCTETS, b"\0" * alen)
    inner = prefix + auth_tlv + tlv(OCTETS, priv_params)
    sp_seq = tlv(SEQUENCE, inner)
    sp_oct = tlv(OCTETS, sp_seq)
    gd = tlv(SEQUENCE, enc_int(msg_id) + enc_int(max_size) + tlv(OCTETS, bytes([flags])) + enc_int(3))
    content = enc_int(3) + gd + sp_oct + body
    msg = tlv(SEQUENCE, content)
    if alen and usm is not None:
        pos = (
            (len(msg) - len(content))
            + len(enc_int(3))
            + len(gd)
            + (len(sp_oct) - len(sp_seq))
            + (len(sp_seq) - len(inner))
            + len(prefix)
            + (len(auth_tlv) - alen)
        )
        digest = auth_digest(usm.auth, usm.auth_key, msg)
        msg = msg[:pos] + digest + msg[pos + alen :]
    return msg


@dataclass
class V3Message:
    msg_id: int
    max_size: int
    flags: int
    model: int
    engine: bytes
    boots: int
    etime: int
    user: bytes
    auth_span: tuple[int, int]
    priv_params: bytes
    data: tuple[int, int, int]  # msgData TLV


def parse_v3(buf: bytes, items: list[tuple[int, int, int]]) -> V3Message:
    gd = children(buf, items[1][1], items[1][2])
    flags = buf[gd[2][1] : gd[2][2]]
    _, ss, se = dec_tlv(buf, items[2][1])  # the USM SEQUENCE inside the OCTET STRING
    sp = children(buf, ss, se)
    return V3Message(
        msg_id=dec_int(buf[gd[0][1] : gd[0][2]]),
        max_size=dec_int(buf[gd[1][1] : gd[1][2]]),
        flags=flags[0] if flags else 0,
        model=dec_int(buf[gd[3][1] : gd[3][2]]),
        engine=bytes(buf[sp[0][1] : sp[0][2]]),
        boots=dec_int(buf[sp[1][1] : sp[1][2]]),
        etime=dec_int(buf[sp[2][1] : sp[2][2]]),
        user=bytes(buf[sp[3][1] : sp[3][2]]),
        auth_span=(sp[4][1], sp[4][2]),
        priv_params=bytes(buf[sp[5][1] : sp[5][2]]),
        data=items[3],
    )


# ---------------------------------------------------------------------------
# The agent

ENTERPRISE = {"Cisco": 9, "Aruba": 11, "Juniper": 2636, "MikroTik": 14988, "APC": 318}


class SnmpAgent(UdpService):
    def __init__(self, ctx: Any, name: str, spec: dict[str, Any]) -> None:
        super().__init__(ctx, name, spec)
        cred = configs.snmp_credential(name, ctx.secrets)
        self.v3cfg: dict[str, Any] | None = cred["v3"]
        self.community = str(cred["community"]).encode()
        self.v2c = self.v3cfg is None  # the v3 agent (core-sw01) has v1/v2c turned off
        node = ctx.world.d.nodes[spec["node"]]
        self.dev = device_for(self, node)
        ent = ENTERPRISE.get(node.vendor, 8072)
        if ent == 8072:  # net-snmp: format 128 + 4 random bytes + boot time, like a fresh install
            self.engine_id = bytes.fromhex("80001f8880") + hashlib.sha256(node.name.encode()).digest()[:8]
        else:  # RFC 3411 format 3: the enterprise and a MAC
            self.engine_id = struct.pack(">I", 0x80000000 | ent) + b"\x03" + mac_bytes(node.mac)
        self._usm: Usm | None = None
        self._salt = itertools.count(int.from_bytes(os.urandom(6), "big"))
        self._cache: dict[str, tuple[float, float, Mib]] = {}
        self.stats = {k: 0 for k in range(1, 7)}

    @property
    def usm(self) -> Usm | None:
        if self._usm is None and self.v3cfg is not None:
            self._usm = Usm.from_config(self.v3cfg, self.engine_id)
        return self._usm

    def engine_clock(self, snap: Snapshot) -> tuple[int, int]:
        boot = snap.boot(self.node)
        boots = 3 + max(0, int((boot - 1_780_000_000) // 604_800))
        return boots, max(int(snap.t - boot), 0)

    def mib(self, snap: Snapshot, context: str = "") -> Mib:
        """The MIB at this instant (kept a few real seconds: a walk sees one consistent table)."""
        hit = self._cache.get(context)
        boot = snap.boot(self.node)
        if hit and hit[1] == boot and 0 <= snap.t - hit[0] <= max(1.0, 3 * snap.speed):
            return hit[2]
        mib = self.dev.mib(snap, context)
        self._cache[context] = (snap.t, boot, mib)
        return mib

    # -- UDP --
    def datagram(self, data: bytes, addr: tuple[str, int], snap: Snapshot) -> bytes | None:
        try:
            tag, s, e = dec_tlv(data, 0)
            if tag != SEQUENCE:
                return None
            items = children(data, s, e)
            version = dec_int(data[items[0][1] : items[0][2]])
            if version in (0, 1):
                return self._community(data, items, version, snap)
            if version == 3:
                return self._v3(data, items, snap)
        except (ValueError, IndexError):  # snmpInASNParseErrs: dropped
            return None
        return None

    def _community(self, data: bytes, items: list[tuple[int, int, int]], version: int, snap: Snapshot) -> bytes | None:
        if not self.v2c:
            return None
        community = data[items[1][1] : items[1][2]]
        if not hmac.compare_digest(community, self.community):
            return None  # snmpInBadCommunityNames: no answer
        pdu = dec_pdu(data, items[2])
        resp = self.respond(pdu, self.mib(snap), v1=version == 0, limit=self.dev.max_size)
        if resp is None:
            return None
        return tlv(SEQUENCE, enc_int(version) + tlv(OCTETS, community) + resp)

    def respond(self, pdu: Pdu, mib: Mib, v1: bool, limit: int) -> bytes | None:
        """The Response PDU for a request (GET, GETNEXT, GETBULK; SET is refused)."""
        if pdu.tag == GET:
            out = []
            for k, (oid, _) in enumerate(pdu.varbinds, start=1):
                v = mib.get(oid)
                if v1 and (v[0] in (NO_SUCH_OBJECT, NO_SUCH_INSTANCE) or v[0] == COUNTER64):
                    return self._error(pdu, 2, k)
                out.append(enc_varbind(oid, v))
            return self._fit(pdu, out, limit)
        if pdu.tag == GETNEXT:
            out = []
            for k, (oid, _) in enumerate(pdu.varbinds, start=1):
                nxt = self._next(mib, oid, v1)
                if nxt is None:
                    if v1:
                        return self._error(pdu, 2, k)
                    out.append(enc_varbind(oid, (END_OF_MIB, None)))
                else:
                    out.append(enc_varbind(*nxt))
            return self._fit(pdu, out, limit)
        if pdu.tag == GETBULK and not v1:
            n = max(0, min(pdu.a, len(pdu.varbinds)))
            reps = max(0, pdu.b)
            out: list[bytes] = []
            size = 0
            budget = limit - 60
            for oid, _ in pdu.varbinds[:n]:
                nxt = mib.next(oid)
                vb = enc_varbind(*nxt) if nxt else enc_varbind(oid, (END_OF_MIB, None))
                out.append(vb)
                size += len(vb)
            cur = [oid for oid, _ in pdu.varbinds[n:]]
            for _ in range(reps if cur else 0):
                row = []
                done = True
                for k, oid in enumerate(cur):
                    nxt = mib.next(oid)
                    if nxt is None:
                        row.append(enc_varbind(oid, (END_OF_MIB, None)))
                    else:
                        done = False
                        cur[k] = nxt[0]
                        row.append(enc_varbind(*nxt))
                rs = sum(len(x) for x in row)
                if size + rs > budget and len(out) > n:
                    break
                out += row
                size += rs
                if done:
                    break
            return self._fit(pdu, out, limit)
        if pdu.tag == SET:
            return self._error(pdu, 2 if v1 else 6, 1)
        return None

    @staticmethod
    def _next(mib: Mib, oid: Oid, v1: bool) -> tuple[Oid, Value] | None:
        nxt = mib.next(oid)
        while v1 and nxt is not None and nxt[1][0] == COUNTER64:  # RFC 3584: v1 skips Counter64
            nxt = mib.next(nxt[0])
        return nxt

    @staticmethod
    def _error(pdu: Pdu, status: int, index: int) -> bytes:
        vbs = [enc_varbind(oid, (NULL, None)) for oid, _ in pdu.varbinds]
        return enc_pdu(RESPONSE, pdu.reqid, status, index, vbs)

    def _fit(self, pdu: Pdu, vbs: list[bytes], limit: int) -> bytes:
        resp = enc_pdu(RESPONSE, pdu.reqid, 0, 0, vbs)
        if len(resp) + 40 > limit:
            return enc_pdu(RESPONSE, pdu.reqid, 1, 0, [])  # tooBig
        return resp

    # -- v3 --
    def _report(
        self, m: V3Message, stat: int | Oid, reqid: int, flags: int, snap: Snapshot, context: bytes = b""
    ) -> bytes | None:
        if not m.flags & 4:  # not reportable
            return None
        oid = stat if isinstance(stat, tuple) else (*OI(USM_STATS), stat, 0)
        if isinstance(stat, int):
            self.stats[stat] += 1
        count = self.stats.get(stat, 1) if isinstance(stat, int) else 1
        pdu = enc_pdu(REPORT, reqid, 0, 0, [enc_varbind(oid, C32(count))])
        scoped = tlv(SEQUENCE, tlv(OCTETS, self.engine_id) + tlv(OCTETS, context) + pdu)
        boots, etime = self.engine_clock(snap)
        usm = self.usm if flags & 1 else None
        return build_v3(m.msg_id, 65507, flags & 1, self.engine_id, boots, etime, m.user, scoped, usm)

    def _plain_reqid(self, data: bytes, m: V3Message) -> int:
        """The request id of an unencrypted scoped PDU (0 when it cannot be read)."""
        if m.flags & 2 or m.data[0] != SEQUENCE:
            return 0
        try:
            sc = children(data, m.data[1], m.data[2])
            return dec_pdu(data, sc[2]).reqid
        except (ValueError, IndexError):
            return 0

    def _v3(self, data: bytes, items: list[tuple[int, int, int]], snap: Snapshot) -> bytes | None:
        usm = self.usm
        if usm is None:
            return None  # v3 not enabled on this agent: unknown version, dropped
        m = parse_v3(data, items)
        if m.model != 3:
            return None
        boots, etime = self.engine_clock(snap)
        if m.engine != self.engine_id:
            return self._report(m, UNKNOWN_ENGINE_IDS, self._plain_reqid(data, m), 0, snap)
        if m.user != usm.user:
            return self._report(m, UNKNOWN_USER_NAMES, self._plain_reqid(data, m), 0, snap)
        if (m.flags & 3) != usm.flags:
            return self._report(m, UNSUPPORTED_SEC_LEVELS, self._plain_reqid(data, m), 0, snap)
        if m.flags & 1:
            s, e = m.auth_span
            if e - s != AUTH_LEN[usm.auth]:
                return self._report(m, WRONG_DIGESTS, 0, 0, snap)
            zeroed = data[:s] + b"\0" * (e - s) + data[e:]
            if not hmac.compare_digest(auth_digest(usm.auth, usm.auth_key, zeroed), data[s:e]):
                return self._report(m, WRONG_DIGESTS, 0, 0, snap)
            if m.boots != boots or abs(m.etime - etime) > TIME_WINDOW:
                return self._report(m, NOT_IN_TIME_WINDOWS, 0, 1, snap)
        tag, s, e = m.data
        try:
            if m.flags & 2:
                if tag != OCTETS:
                    raise ValueError("encrypted PDU expected")
                plain = decrypt(usm.priv, usm.priv_key, m.boots, m.etime, m.priv_params, data[s:e])
                st, ss, se = dec_tlv(plain, 0)
                buf, scoped = plain, (st, ss, se)
            else:
                buf, scoped = data, (tag, s, e)
            if scoped[0] != SEQUENCE:
                raise ValueError("scoped PDU expected")
            sc = children(buf, scoped[1], scoped[2])
            context = bytes(buf[sc[1][1] : sc[1][2]])
            pdu = dec_pdu(buf, sc[2])
        except (ValueError, IndexError):
            return self._report(m, DECRYPTION_ERRORS, 0, m.flags & 1, snap)
        ctx_name = context.decode(errors="replace")
        if not self.dev.valid_context(ctx_name):
            return self._report(m, UNKNOWN_CONTEXTS, pdu.reqid, m.flags & 1, snap, context)
        limit = min(max(m.max_size, 484), 65507) - 120
        resp = self.respond(pdu, self.mib(snap, ctx_name), v1=False, limit=min(limit, self.dev.max_size))
        if resp is None:
            return None
        scoped_out = tlv(SEQUENCE, tlv(OCTETS, self.engine_id) + tlv(OCTETS, context) + resp)
        salt = b""
        if m.flags & 2:
            n = next(self._salt)
            salt = struct.pack(">II", boots, n & 0xFFFFFFFF) if usm.priv == "des" else struct.pack(">Q", n)
        return build_v3(m.msg_id, 65507, m.flags & 3, self.engine_id, boots, etime, usm.user, scoped_out, usm, salt)
