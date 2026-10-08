"""SNMP agents: BER, v1/v2c GET/GETNEXT/GETBULK, v3 (USM) with a minimal client, and what each agent serves."""

from __future__ import annotations

import hmac
import itertools
import socket
import struct
from collections.abc import Iterator
from typing import Any

import pytest

from emulator import acme
from emulator.testing import Emulator
from emulator.vendors import snmp as s
from emulator.world import at_local

WED_11 = at_local(2, 11, 0, week=1)  # nas_disk_hot is on
MON_11 = at_local(0, 11, 0, week=1)
SAT_2301 = at_local(5, 23, 1, week=1)  # sw-f2-01 is rebooting
SAT_2310 = at_local(5, 23, 10, week=1)

SNMP_APIS = [n for n, spec in acme.APIS.items() if spec["kind"] == "snmp"]
_reqid = itertools.count(1000)


# ---------------------------------------------------------------------------
# A minimal client


def send(addr: tuple[str, int], msg: bytes, timeout: float = 0.5) -> bytes | None:
    with socket.socket(socket.AF_INET, socket.SOCK_DGRAM) as sock:
        sock.settimeout(timeout)
        sock.sendto(msg, addr)
        try:
            return sock.recv(65535)
        except TimeoutError:
            return None


def v2_message(community: str, tag: int, oids: list[str], version: int = 1, a: int = 0, b: int = 0) -> bytes:
    pdu = s.enc_pdu(tag, next(_reqid), a, b, [s.enc_varbind(s.OI(o), (s.NULL, None)) for o in oids])
    return s.tlv(s.SEQUENCE, s.enc_int(version) + s.tlv(s.OCTETS, community.encode()) + pdu)


def v2_pdu(raw: bytes) -> s.Pdu:
    _, st, en = s.dec_tlv(raw, 0)
    return s.dec_pdu(raw, s.children(raw, st, en)[2])


def text(v: s.Value) -> str:
    return v[1].decode() if isinstance(v[1], bytes) else str(v[1])


class V2:
    def __init__(self, addr: tuple[str, int], community: str, version: int = 1) -> None:
        self.addr, self.community, self.version = addr, community, version

    def req(self, tag: int, oids: list[str], a: int = 0, b: int = 0) -> s.Pdu | None:
        raw = send(self.addr, v2_message(self.community, tag, oids, self.version, a, b))
        return v2_pdu(raw) if raw else None

    def get(self, *oids: str) -> list[s.Value]:
        pdu = self.req(s.GET, list(oids))
        assert pdu is not None, "no answer"
        return [v for _, v in pdu.varbinds]

    def walk(self, base: str, bulk: bool = True) -> dict[s.Oid, s.Value]:
        root = s.OI(base)
        out: dict[s.Oid, s.Value] = {}
        cur = root
        while True:
            pdu = (
                self.req(s.GETBULK, [".".join(map(str, cur))], 0, 25)
                if bulk
                else self.req(s.GETNEXT, [".".join(map(str, cur))])
            )
            assert pdu is not None and pdu.a == 0
            for oid, v in pdu.varbinds:
                if v[0] == s.END_OF_MIB or oid[: len(root)] != root:
                    return out
                assert oid > cur
                out[oid] = v
                cur = oid


class V3:
    """Discovery, then authenticated (and encrypted) requests, like gosnmp does."""

    def __init__(self, addr: tuple[str, int], cfg: dict[str, Any]) -> None:
        self.addr, self.cfg = addr, cfg
        self.msg_id = itertools.count(1)
        self.salt = itertools.count(77)
        self.engine, self.boots, self.etime = b"", 0, 0
        self.usm: s.Usm | None = None

    def discover(self) -> s.V3Message:
        scoped = s.tlv(s.SEQUENCE, s.tlv(s.OCTETS, b"") + s.tlv(s.OCTETS, b"") + s.enc_pdu(s.GET, 1, 0, 0, []))
        raw = send(self.addr, s.build_v3(next(self.msg_id), 65507, 4, b"", 0, 0, b"", scoped, None))
        assert raw is not None, "no discovery answer"
        m, pdu = self.parse(raw, check=False)
        assert pdu.tag == s.REPORT and pdu.varbinds[0][0] == s.OI("1.3.6.1.6.3.15.1.1.4.0")
        self.engine, self.boots, self.etime = m.engine, m.boots, m.etime
        self.usm = s.Usm.from_config(self.cfg, self.engine)
        return m

    def parse(self, raw: bytes, check: bool = True) -> tuple[s.V3Message, s.Pdu]:
        _, st, en = s.dec_tlv(raw, 0)
        m = s.parse_v3(raw, s.children(raw, st, en))
        if check and m.flags & 1:
            assert self.usm is not None
            a, b = m.auth_span
            zeroed = raw[:a] + b"\0" * (b - a) + raw[b:]
            assert hmac.compare_digest(s.auth_digest(self.usm.auth, self.usm.auth_key, zeroed), raw[a:b])
        tag, a, b = m.data
        buf = raw
        if m.flags & 2:
            assert self.usm is not None and tag == s.OCTETS
            buf = s.decrypt(self.usm.priv, self.usm.priv_key, m.boots, m.etime, m.priv_params, raw[a:b])
            tag, a, b = s.dec_tlv(buf, 0)
        sc = s.children(buf, a, b)
        return m, s.dec_pdu(buf, sc[2])

    def request(
        self,
        tag: int,
        oids: list[str],
        context: str = "",
        a: int = 0,
        b: int = 0,
        user: bytes | None = None,
        etime: int | None = None,
        usm: s.Usm | None = None,
    ) -> tuple[s.V3Message, s.Pdu] | None:
        usm = usm or self.usm
        assert usm is not None
        pdu = s.enc_pdu(tag, next(_reqid), a, b, [s.enc_varbind(s.OI(o), (s.NULL, None)) for o in oids])
        scoped = s.tlv(s.SEQUENCE, s.tlv(s.OCTETS, self.engine) + s.tlv(s.OCTETS, context.encode()) + pdu)
        n = next(self.salt)
        salt = struct.pack(">II", self.boots, n) if usm.priv == "des" else struct.pack(">Q", n)
        msg = s.build_v3(
            next(self.msg_id),
            65507,
            usm.flags | 4,
            self.engine,
            self.boots,
            self.etime if etime is None else etime,
            user or usm.user,
            scoped,
            usm,
            salt,
        )
        raw = send(self.addr, msg)
        return self.parse(raw) if raw else None


@pytest.fixture(scope="module")
def emu() -> Iterator[Emulator]:
    with Emulator(at=WED_11, apis=SNMP_APIS) as e:
        yield e


def v2(emu: Emulator, api: str, community: str | None = None, version: int = 1) -> V2:
    return V2(emu.addrs[api], community or emu.secrets["snmp"]["community"], version)


# ---------------------------------------------------------------------------
# BER


@pytest.mark.parametrize("v", [0, 1, 127, 128, 255, 256, -1, -128, -129, 2**31 - 1, 2**32 - 1, 2**63, 2**64 - 1])
def test_ber_integers_round_trip(v: int) -> None:
    for tag in (s.INTEGER, s.COUNTER64) if v >= 0 else (s.INTEGER,):
        raw = s.enc_value((tag, v))
        t, a, b = s.dec_tlv(raw, 0)
        assert t == tag and s.dec_value(t, raw[a:b]) == (tag, v)
    assert s.enc_int(128) == b"\x02\x02\x00\x80"
    assert s.enc_int(-1) == b"\x02\x01\xff"


def test_ber_oids_lengths_and_varbinds() -> None:
    for o in (
        "1.3.6.1.2.1.1.1.0",
        "1.0.8802.1.1.2.1.4.1.1.9.0.49.1",
        "1.3.6.1.4.1.2636.1.1.1.2.132",
        "1.3.6.1.2.1.17.7.1.2.2.1.2.10.0.11.134.255.128.1",
        "2.999.16384.4294967295",
    ):
        assert s.dec_oid(s.enc_oid(s.OI(o))) == s.OI(o)
    assert s.enc_oid(s.OI("1.3.6.1.4.1.14988")) == bytes.fromhex("2b06010401f50c")
    big = s.tlv(s.OCTETS, b"x" * 300)
    assert big[:4] == b"\x04\x82\x01\x2c" and s.dec_tlv(big, 0) == (s.OCTETS, 4, 304)
    vb = s.enc_varbind(s.OI("1.3.6.1.2.1.1.5.0"), s.Str("sw-f1-01"))
    _, a, b = s.dec_tlv(vb, 0)
    (_, oa, ob), (vt, va, vbe) = s.children(vb, a, b)
    assert s.dec_oid(vb[oa:ob]) == s.OI("1.3.6.1.2.1.1.5.0") and s.dec_value(vt, vb[va:vbe]) == s.Str("sw-f1-01")
    with pytest.raises(ValueError):
        s.dec_tlv(b"\x04\x05abc", 0)


def test_usm_keys_match_rfc3414_vectors() -> None:
    engine = bytes.fromhex("000000000000000000000002")
    assert s.localize_key("md5", b"maplesyrup", engine).hex() == "526f5eed9fcce26f8964c2930787d82b"
    assert s.localize_key("sha1", b"maplesyrup", engine).hex() == "6695febc9288e36282235fc7151f128497b38f3f"


# ---------------------------------------------------------------------------
# v1 / v2c


def test_v2c_get_system_group_of_each_vendor(emu: Emulator) -> None:
    want = {
        "snmp-sw-f1-01": (
            "Aruba JL256A 2930F-48G-PoE+-4SFP+ Switch, revision WC.16.11.0024",
            "1.3.6.1.4.1.11.2.3.7.11.",
            "sw-f1-01",
        ),
        "snmp-sw-dmz-01": (
            "Juniper Networks, Inc. ex2300-24t Ethernet Switch, kernel JUNOS 23.4R2-S3",
            "1.3.6.1.4.1.2636.1.1.1.2.",
            "sw-dmz-01",
        ),
        "snmp-sw-stor-01": ("RouterOS CRS518-16XS-2XQ", "1.3.6.1.4.1.14988.1", "sw-stor-01"),
        "snmp-nas01": ("Linux nas01 4.4.302+", "1.3.6.1.4.1.8072.3.2.10", "nas01"),
        "snmp-bkp-nas01": ("Linux bkp-nas01 5.10.60-qnap", "1.3.6.1.4.1.8072.3.2.10", "bkp-nas01"),
        "snmp-mon01": ("Linux mon01 6.8.0-85-generic", "1.3.6.1.4.1.8072.3.2.10", "mon01"),
    }
    for api, (descr, objid, name) in want.items():
        d, o, up, n, loc = v2(emu, api).get(
            "1.3.6.1.2.1.1.1.0", "1.3.6.1.2.1.1.2.0", "1.3.6.1.2.1.1.3.0", "1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.6.0"
        )
        assert text(d).startswith(descr), api
        assert ".".join(map(str, o[1])).startswith(objid.rstrip(".")), api
        assert up[0] == s.TICKS and up[1] > 0
        assert text(n) == name and text(loc) == "Acme HQ - Sala de servidores"
    ups = v2(emu, "snmp-ups-rack01", "public")
    assert text(ups.get("1.3.6.1.2.1.1.1.0")[0]).startswith("APC Web/SNMP Management Card")
    assert text(ups.get("1.3.6.1.4.1.318.1.1.1.1.1.1.0")[0]) == "Smart-UPS SRT 5000"
    assert ups.get("1.3.6.1.2.1.33.1.4.1.0")[0] == s.Int(3)  # upsOutputSource: normal


def test_get_exceptions_and_v1_errors(emu: Emulator) -> None:
    c = v2(emu, "snmp-sw-f1-01")
    missing, no_inst = c.get("1.3.6.1.4.1.99999.1.0", "1.3.6.1.2.1.2.2.1.2.9999")
    assert missing[0] == s.NO_SUCH_OBJECT and no_inst[0] == s.NO_SUCH_INSTANCE
    last = c.req(s.GETNEXT, ["1.3.6.1.6.3.10.2.1.4.0"])
    assert last is not None and last.varbinds[0][1][0] == s.END_OF_MIB
    v1 = v2(emu, "snmp-sw-f1-01", version=0)
    assert text(v1.get("1.3.6.1.2.1.1.5.0")[0]) == "sw-f1-01"
    err = v1.req(s.GET, ["1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.31.1.1.1.6.1"])  # Counter64: not in v1
    assert err is not None and (err.a, err.b) == (2, 2)
    nxt = v1.req(s.GETNEXT, ["1.3.6.1.2.1.31.1.1.1.5.99999"])  # skips the HC columns
    assert nxt is not None and nxt.varbinds[0][0][:11] == s.OI("1.3.6.1.2.1.31.1.1.1.14")
    assert v1.req(s.GETBULK, ["1.3.6.1.2.1.1"], 0, 5) is None
    refused = c.req(s.SET, ["1.3.6.1.2.1.1.5.0"])
    assert refused is not None and refused.a == 6


def test_wrong_community_and_v2c_on_the_v3_switch_get_no_answer(emu: Emulator) -> None:
    assert v2(emu, "snmp-sw-f1-01", "public").req(s.GET, ["1.3.6.1.2.1.1.5.0"]) is None
    assert v2(emu, "snmp-ups-rack01").req(s.GET, ["1.3.6.1.2.1.1.5.0"]) is None  # the UPS only knows "public"
    assert v2(emu, "snmp-core-sw01").req(s.GET, ["1.3.6.1.2.1.1.5.0"]) is None
    assert send(emu.addrs["snmp-sw-f1-01"], b"\x30\x03\x02\x01") is None  # garbage


def test_walk_getnext_equals_getbulk_and_aruba_interfaces(emu: Emulator) -> None:
    c = v2(emu, "snmp-sw-f1-01")
    names = c.walk("1.3.6.1.2.1.31.1.1.1.1")
    assert c.walk("1.3.6.1.2.1.31.1.1.1.1", bulk=False) == names
    by_index = {o[-1]: text(v) for o, v in names.items()}
    assert [by_index[i] for i in range(1, 53)] == [str(i) for i in range(1, 53)]
    assert by_index[289] == "Trk1" and by_index[1010] == "VLAN10"
    t = c.walk("1.3.6.1.2.1.2.2.1.3")
    assert t[s.OI("1.3.6.1.2.1.2.2.1.3.289")] == s.Int(161)
    hs = c.walk("1.3.6.1.2.1.31.1.1.1.15")
    assert hs[s.OI("1.3.6.1.2.1.31.1.1.1.15.49")][1] == 10000 and hs[s.OI("1.3.6.1.2.1.31.1.1.1.15.289")][1] == 20000
    alias = c.get("1.3.6.1.2.1.31.1.1.1.18.45")[0]
    assert text(alias) == "ap-f1-01"
    oper = c.walk("1.3.6.1.2.1.2.2.1.8")
    assert sum(1 for v in oper.values() if v[1] == 1) >= 40
    # sw-f2-01 port 46 has a bad cable: 100 Mbps
    assert v2(emu, "snmp-sw-f2-01").get("1.3.6.1.2.1.31.1.1.1.15.46")[0] == s.G32(100)


def test_counters_grow_and_32_bit_ones_wrap(emu: Emulator) -> None:
    c = v2(emu, "snmp-sw-stor-01")
    idx = next(o[-1] for o, v in c.walk("1.3.6.1.2.1.31.1.1.1.1").items() if text(v) == "sfp28-7")
    hc = f"1.3.6.1.2.1.31.1.1.1.10.{idx}"
    lo = f"1.3.6.1.2.1.2.2.1.16.{idx}"
    a64, a32 = c.get(hc, lo)
    assert a64[0] == s.COUNTER64 and a32[0] == s.COUNTER32
    assert a64[1] > 2**32 and a32[1] == a64[1] % 2**32
    emu.set_time(WED_11 + 300)
    try:
        b64, _ = c.get(hc, lo)
    finally:
        emu.set_time(WED_11)
    assert b64[1] > a64[1]


def test_lldp_fdb_and_arp_as_each_vendor_reports_them(emu: Emulator) -> None:
    aruba = v2(emu, "snmp-sw-f1-01")
    rem = aruba.walk("1.0.8802.1.1.2.1.4.1.1")
    names = {o[-2]: text(v) for o, v in rem.items() if o[10] == 9}  # lldpRemSysName by local port
    assert names[49] == "core-sw01.acme.local" and names[45] == "ap-f1-01"
    ports = {o[-2]: v for o, v in rem.items() if o[10] == 7}
    assert text(ports[49]) == "Te1/1/3"  # Cisco: port id subtype interfaceName
    phone = next(o for o, v in rem.items() if o[10] == 4 and o[-2] == 1)
    assert rem[phone] == s.Int(5)  # Yealink: chassis id is its address
    q = aruba.walk("1.3.6.1.2.1.17.7.1.2.2.1.2")
    assert len(q) > 200 and {o[13] for o in q} >= {10, 30, 32, 40}  # fdb id = VLAN id
    assert sum(1 for v in q.values() if v[1] == 289) > 150  # learned on the uplink trunk (Trk1)

    junos = v2(emu, "snmp-sw-dmz-01")
    base = junos.walk("1.3.6.1.2.1.17.1.4.1.2")
    ifnames = {o[-1]: text(v) for o, v in junos.walk("1.3.6.1.2.1.31.1.1.1.1").items()}
    assert {ifnames[v[1]] for v in base.values()} >= {"ge-0/0/0.0", "ge-0/0/10.0"}  # logical units
    assert "me0.0" not in {ifnames[v[1]] for v in base.values()}
    fdb = junos.walk("1.3.6.1.2.1.17.7.1.2.2.1.2")
    web01 = s.mac_oid(emu.world.d.nodes["web01"].mac)
    assert any(o[-6:] == web01 for o in fdb)
    assert {o[13] for o in fdb} == {3}  # an internal VLAN index, not 90
    rem = junos.walk("1.0.8802.1.1.2.1.4.1.1.9")
    assert {text(v) for v in rem.values()} >= {"web01.acme.com.br", "core-sw01.acme.local", "fw-hq-01"}

    mt = v2(emu, "snmp-sw-stor-01")
    assert mt.walk("1.0.8802.1.1.2") == {}  # RouterOS: no LLDP-MIB
    neigh = mt.walk("1.3.6.1.4.1.14988.1.1.11.1.1.6")
    assert {text(v) for v in neigh.values()} >= {"pve01.acme.local", "core-sw01.acme.local"}
    arp = mt.walk("1.3.6.1.2.1.4.22.1.2")
    assert len(arp) > 20
    assert any(o[-4:] == (10, 10, 10, 5) for o in arp)


def test_scenario_nas_disk_and_switch_reboot(emu: Emulator) -> None:
    nas = v2(emu, "snmp-nas01")
    disk3 = "1.3.6.1.4.1.6574.2.1.1.6.2"
    try:
        hot = nas.get(disk3)[0][1]
        emu.set_time(MON_11)
        cool = nas.get(disk3)[0][1]
        emu.set_time(SAT_2301)
        assert v2(emu, "snmp-sw-f2-01").req(s.GET, ["1.3.6.1.2.1.1.3.0"]) is None  # rebooting
        emu.set_time(SAT_2310)
        up = v2(emu, "snmp-sw-f2-01").get("1.3.6.1.2.1.1.3.0")[0][1]
    finally:
        emu.set_time(WED_11)
    assert hot >= 60 > cool
    assert 0 < up < 10 * 60 * 100
    model, serial, version, upgrade = nas.get(
        "1.3.6.1.4.1.6574.1.5.1.0", "1.3.6.1.4.1.6574.1.5.2.0", "1.3.6.1.4.1.6574.1.5.3.0", "1.3.6.1.4.1.6574.1.5.4.0"
    )
    assert (text(model), text(serial), text(version)) == ("RS1221+", "2290RQRV6D2X1", "DSM 7.2.2-72806 Update 4")
    assert upgrade == s.Int(2)
    mem_total, avail = nas.get("1.3.6.1.4.1.2021.4.5.0", "1.3.6.1.4.1.2021.4.6.0")
    assert 0 < avail[1] < mem_total[1]
    qnap = v2(emu, "snmp-bkp-nas01").get("1.3.6.1.4.1.24681.1.2.1.0", "1.3.6.1.4.1.24681.1.2.5.0")
    assert text(qnap[0]).endswith(" %") and " C/" in text(qnap[1])


# ---------------------------------------------------------------------------
# v3


def test_v3_discovery_authpriv_and_bridge_contexts(emu: Emulator) -> None:
    cfg = emu.secrets["snmp"]["v3"]
    c = V3(emu.addrs["snmp-core-sw01"], cfg)
    m = c.discover()
    assert m.engine.startswith(bytes.fromhex("8000000903")) and m.boots > 0 and m.etime > 0
    got = c.request(s.GET, ["1.3.6.1.2.1.1.5.0", "1.3.6.1.2.1.1.1.0"])
    assert got is not None
    resp, pdu = got
    assert resp.flags & 3 == 3 and pdu.tag == s.RESPONSE
    assert text(pdu.varbinds[0][1]) == "core-sw01.acme.local"
    assert text(pdu.varbinds[1][1]).startswith("Cisco IOS Software [Dublin], Catalyst L3 Switch Software")
    bulk = c.request(s.GETBULK, ["1.3.6.1.2.1.31.1.1.1.1"], a=0, b=20)
    assert bulk is not None
    assert [text(v) for _, v in bulk[1].varbinds][:3] == ["Gi0/0", "Tw1/0/1", "Tw1/0/2"]
    # BRIDGE-MIB per VLAN: the default context is VLAN 1 (nothing learned), vlan-30 has the desktops
    none = c.request(s.GETNEXT, ["1.3.6.1.2.1.17.4.3.1.2"])
    assert none is not None and none[1].varbinds[0][0][:11] != s.OI("1.3.6.1.2.1.17.4.3.1.2")
    users = c.request(s.GETBULK, ["1.3.6.1.2.1.17.4.3.1.2"], context="vlan-30", b=60)
    assert users is not None
    learned = [v for o, v in users[1].varbinds if o[:11] == s.OI("1.3.6.1.2.1.17.4.3.1.2")]
    assert len(learned) > 20 and all(v[0] == s.INTEGER and v[1] > 0 for v in learned)
    ports = c.request(s.GETBULK, ["1.3.6.1.2.1.17.1.4.1.2"], context="vlan-30", b=60)
    assert ports is not None
    by_bridge_port = {o[-1]: v[1] for o, v in ports[1].varbinds if o[:11] == s.OI("1.3.6.1.2.1.17.1.4.1.2")}
    assert {v[1] for v in learned} <= set(by_bridge_port)  # every learned port maps to an ifIndex
    bad_ctx = c.request(s.GET, ["1.3.6.1.2.1.1.5.0"], context="vlan-999")
    assert bad_ctx is not None and bad_ctx[1].tag == s.REPORT


def test_v3_reports_and_refusals(emu: Emulator) -> None:
    cfg = emu.secrets["snmp"]["v3"]
    c = V3(emu.addrs["snmp-core-sw01"], cfg)
    c.discover()
    late = c.request(s.GET, ["1.3.6.1.2.1.1.5.0"], etime=c.etime + 1000)
    assert late is not None and late[1].tag == s.REPORT
    assert late[1].varbinds[0][0] == s.OI("1.3.6.1.6.3.15.1.1.2.0") and late[0].etime == c.etime  # notInTimeWindows
    stranger = c.request(s.GET, ["1.3.6.1.2.1.1.5.0"], user=b"intruder")
    assert stranger is not None and stranger[1].varbinds[0][0] == s.OI("1.3.6.1.6.3.15.1.1.3.0")
    wrong = s.Usm.from_config({**cfg, "auth_pass": "not-the-password"}, c.engine)
    digest = c.request(s.GET, ["1.3.6.1.2.1.1.5.0"], usm=wrong)
    assert digest is not None and digest[1].varbinds[0][0] == s.OI("1.3.6.1.6.3.15.1.1.5.0")
    assert all(v[0] == s.COUNTER32 for _, v in digest[1].varbinds)
    # agents without v3 do not answer v3 at all
    other = V3(emu.addrs["snmp-sw-f1-01"], cfg)
    with pytest.raises(AssertionError, match="no discovery answer"):
        other.discover()


@pytest.mark.parametrize(
    ("auth", "priv"),
    [
        ("md5", "des"),
        ("sha", "aes"),
        ("sha224", "aes192"),
        ("sha384", "aes256"),
        ("sha512", "aes256c"),
        ("sha256", "none"),
    ],
)
def test_v3_every_auth_and_privacy_protocol(auth: str, priv: str) -> None:
    from emulator import configs

    sec = configs.generate()
    sec["snmp"]["v3"].update(auth=auth, priv=priv)
    with Emulator(at=WED_11, apis=["snmp-core-sw01"], secrets=sec) as e:
        c = V3(e.addrs["snmp-core-sw01"], sec["snmp"]["v3"])
        c.discover()
        got = c.request(s.GETNEXT, ["1.0.8802.1.1.2.1.4.1.1.9"])
        assert got is not None
        assert got[0].flags & 3 == (1 if priv == "none" else 3)
        assert got[1].tag == s.RESPONSE and text(got[1].varbinds[0][1])  # an LLDP neighbor's name
