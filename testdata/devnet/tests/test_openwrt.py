"""The real OpenWrt plugin against the emulated ubus API of store-rt01 (GL.iNet GL-MT6000, OpenWrt 24.10.2)."""

from __future__ import annotations

import json
import ssl
import urllib.request
from pathlib import Path
from typing import Any

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.vendors import openwrt
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("openwrt") is None, reason="omini-plugin-openwrt not found")

API = "openwrt"
NODE = "store-rt01"
SAT_15 = at_local(5, 15)  # Saturday 15:00: store open, customers on the guest Wi-Fi
SUN_15 = at_local(6, 15)  # Sunday 15:00: store closed
THU_16 = at_local(3, 16)  # Thursday 16:00: staff phones and a customer on Wi-Fi
MON_06 = at_local(0, 6)  # an hour after the weekly reboot (Monday 05:00, 2 minutes)
MON_0335 = at_local(0, 3, 35)  # the IPsec tunnel to HQ is down


@pytest.fixture(scope="module")
def world() -> World:
    return World()


def device(out: dict[str, Any]) -> dict[str, Any]:
    assert "error" not in out, out.get("error")
    assert len(out["devices"]) == 1
    return out["devices"][0]


def ifaces(dev: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {i["name"]: i for i in dev["interfaces"]}


def rpc(url: str, sid: str, obj: str, method: str, args: dict[str, Any] | None = None) -> dict[str, Any]:
    body = {"jsonrpc": "2.0", "id": 1, "method": "call", "params": [sid, obj, method, args or {}]}
    return json.loads(fetch(url + "/ubus", json.dumps(body).encode()))


def fetch(url: str, data: bytes | None = None) -> str:
    ctx = ssl.create_default_context()
    ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE
    req = urllib.request.Request(url, data, {"Content-Type": "application/json"} if data else {})
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
            return r.read().decode()
    except urllib.error.HTTPError as e:
        return e.read().decode()


def test_saturday_afternoon(world: World, tmp_path: Path) -> None:
    snap = world.at(SAT_15)
    node = snap.node(NODE)
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        dev = device(emu.collect(API, state_dir=tmp_path))
        # The saved session expires after 5 idle minutes: the plugin is refused, logs in again and goes on.
        emu.set_time(SAT_15 + 360)
        later = device(emu.collect(API, state_dir=tmp_path))
    assert (tmp_path / "session.json").exists()

    assert dev["name"] == NODE
    assert dev["key"] == node.mac
    assert dev["model"] == "GL.iNet GL-MT6000"
    assert dev["os_version"] == "OpenWrt 24.10.2"
    assert dev["role"] == "router"
    assert dev["uptime_s"] == snap.uptime(NODE)
    assert set(dev["ips"]) == {"10.30.1.1", "10.30.80.1", "100.64.23.45"}
    assert 20 < dev["mem_pct"] < 45
    assert dev["temperatures"][0]["sensor"] == "cpu-thermal" and dev["temperatures"][0]["kind"] == "cpu"
    assert abs(dev["temperatures"][0]["celsius"] - snap.temperature(NODE, "cpu")) < 0.1
    assert {s["mount"] for s in dev["storage"]} == {"/", "/tmp"}

    by = ifaces(dev)
    # WAN: the 2.5G port, linked at the Huawei modem's 1G; DHCP from the modem, default route through it.
    assert by["eth1"]["wan"] is True and by["eth1"]["speed_mbps"] == snap.port_speed(NODE, "eth1") == 1000
    assert "100.64.23.45/22" in by["eth1"]["ips"]
    gws = {g["name"]: g for g in dev["gateways"]}
    assert gws["wan"] == {"name": "wan", "interface": "eth1", "address": "100.64.20.1", "status": "up"}
    # LAN ports (DSA) with what is plugged in: POS terminals, store PC, camera, label printer.
    for k in range(1, 6):
        p = by[f"lan{k}"]
        assert p["type"] == "ethernet" and p["mac"] == node.mac
        assert p["up"] is True and p["speed_mbps"] == 1000
        assert p["rx_bytes"] == snap.port(NODE, f"lan{k}").rx_bytes
    assert by["br-lan"]["members"] == ["lan1", "lan2", "lan3", "lan4", "lan5", "phy0-ap0", "phy1-ap0"]
    assert by["br-lan"]["ips"] == ["10.30.1.1/24"] and by["br-lan"]["description"] == "lan"
    assert by["br-guest"]["members"] == ["phy0-ap1", "phy1-ap1"] and by["br-guest"]["ips"] == ["10.30.80.1/24"]
    assert {n for n, i in by.items() if i["type"] == "wireless"} == set(openwrt.WIFI_IFACES)

    # Counters grow between two readings six minutes apart.
    later_by = ifaces(later)
    assert later_by["eth1"]["rx_bytes"] > by["eth1"]["rx_bytes"]
    assert later_by["lan3"]["tx_bytes"] > by["lan3"]["tx_bytes"]
    assert later["uptime_s"] == dev["uptime_s"] + 360

    # DHCP leases and ARP: the POS terminals (static leases) are there.
    leases = {lease["mac"]: lease for lease in dev["dhcp_leases"]}
    arp = {(a["ip"], a["mac"]): a["interface"] for a in dev["arp"]}
    for name in ("pos-01", "pos-02"):
        n = snap.node(name)
        assert leases[n.mac]["ip"] == n.ip and leases[n.mac]["hostname"] == name
        assert arp[(n.ip, n.mac)] == "br-lan"
    pc = snap.node("pc-loja")
    assert leases[pc.mac]["hostname"] == "LOJA-SANTOS-PC"
    assert {lease.mac for lease in snap.leases("store")} == set(leases)
    assert arp[("100.64.20.1", snap.node("store-modem").mac)] == "eth1"
    assert len(dev["arp"]) == len(snap.arp(NODE))
    hosts = {(h["ip"], h["mac"]) for h in dev["hosts"]}
    assert (snap.node("pos-01").ip, snap.node("pos-01").mac) in hosts

    # Staff on "Acme-Loja" and customers on the guest Wi-Fi, with signal and link rates.
    want = {a.client.mac: a for a in snap.wifi_clients(NODE)}
    got = {c["mac"]: c for c in dev["wireless_clients"]}
    assert want and set(got) == set(want)
    for mac, c in got.items():
        a = want[mac]
        assert c["ssid"] == a.ssid
        assert c["interface"].startswith(a.ssid + " · ")
        assert c["signal_dbm"] == a.signal and c["tx_rate_mbps"] == a.tx_rate
        assert snap.node(a.client.name).site == "store"


def test_sunday_closed(world: World) -> None:
    snap = world.at(SUN_15)
    with Emulator(at=SUN_15, apis=[API], world=world) as emu:
        dev = device(emu.collect(API))
    by = ifaces(dev)
    # POS terminals and the store PC are off: their ports have no link; camera and printer stay.
    for port in ("lan1", "lan2", "lan3"):
        assert by[port]["up"] is False and "speed_mbps" not in by[port]
    for port in ("lan4", "lan5"):
        assert by[port]["up"] is True and by[port]["speed_mbps"] == 1000
    assert "wireless_clients" not in dev
    lease_macs = {lease["mac"] for lease in dev["dhcp_leases"]}
    assert snap.node("pos-01").mac not in lease_macs and snap.node("pos-02").mac not in lease_macs
    assert snap.node("cam-loja-01").mac in lease_macs and snap.node("prn-loja").mac in lease_macs
    arp_ips = {a["ip"] for a in dev["arp"]}
    assert snap.node("pos-01").ip not in arp_ips and snap.node("cam-loja-01").ip in arp_ips


def test_weekday_both_ssids(world: World) -> None:
    snap = world.at(THU_16)
    with Emulator(at=THU_16, apis=[API], world=world) as emu:
        dev = device(emu.collect(API))
    got = {c["mac"]: c for c in dev["wireless_clients"]}
    by_ssid: dict[str, set[str]] = {}
    for a in snap.wifi_clients(NODE):
        by_ssid.setdefault(a.ssid, set()).add(a.client.mac)
    assert set(by_ssid) == {"Acme-Loja", "Loja-Clientes"}
    for ssid, macs in by_ssid.items():
        assert {m for m, c in got.items() if c["ssid"] == ssid} == macs
    staff = {c["mac"] for c in got.values() if c["interface"] == "Acme-Loja · 5 GHz"}
    assert {snap.node(n).mac for n in ("moto-g85-Gabriela", "Galaxy-S25-de-Henrique")} <= staff


def test_after_weekly_reboot_and_tunnel_down(world: World) -> None:
    with Emulator(at=MON_06, apis=[API], world=world) as emu:
        dev = device(emu.collect(API))
        assert dev["uptime_s"] == world.at(MON_06).uptime(NODE) < 3600
        # 03:30-03:40 the IPsec tunnel to HQ is down: the router cannot be reached.
        emu.set_time(MON_0335)
        out = emu.collect(API)
    assert "error" in out and "devices" not in out


def test_wrong_password(world: World) -> None:
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        cfg = {**emu.config(API), "password": emu.secrets[API]["password"] + "x"}
        out = run_plugin("openwrt", cfg)
        raw = rpc(
            emu.urls[API],
            openwrt.NULL_SESSION,
            "session",
            "login",
            {"username": cfg["username"], "password": cfg["password"]},
        )
    assert out["error"] == "OpenWrt rejected the username or password"
    assert raw["result"] == [openwrt.PERMISSION_DENIED]


def test_acl_denied(world: World, monkeypatch: pytest.MonkeyPatch) -> None:
    # An admin who left DHCP leases out of the ACL and did not list /proc/net/arp.
    acl = json.loads(json.dumps(openwrt.ACL))
    acl["ubus"]["luci-rpc"] = ["getHostHints"]
    del acl["file"]["/proc/net/arp"]
    monkeypatch.setattr(openwrt, "ACL", acl)
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        msg = emu.collect(API, "test")
        dev = device(emu.collect(API))
    assert msg["message"] == (
        "Connected to store-rt01 (OpenWrt 24.10.2). Not allowed by the ACL: luci-rpc getDHCPLeases, "
        "file read /proc/net/arp."
    )
    assert "dhcp_leases" not in dev and "arp" not in dev
    assert dev["temperatures"] and dev["hosts"] and dev["wireless_clients"]


def test_raw_api(world: World) -> None:
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        url = emu.urls[API]
        assert "<title>store-rt01 - LuCI</title>" in fetch(url + "/")
        null = openwrt.NULL_SESSION
        assert rpc(url, null, "system", "board")["error"] == {"code": -32002, "message": "Access denied"}
        sec = emu.secrets[API]
        login = rpc(url, null, "session", "login", {"username": sec["username"], "password": sec["password"]})
        assert login["result"][0] == 0
        sid = login["result"][1]["ubus_rpc_session"]
        assert login["result"][1]["timeout"] == 300
        # Both SSIDs, on both radios.
        ssids = {}
        for ifname in openwrt.WIFI_IFACES:
            info = rpc(url, sid, "iwinfo", "info", {"device": ifname})["result"][1]
            ssids[ifname] = (info["ssid"], info["mode"], info["hwmodes"][-1])
        assert ssids == {
            "phy0-ap0": ("Acme-Loja", "Master", "ax"),
            "phy1-ap0": ("Acme-Loja", "Master", "ax"),
            "phy0-ap1": ("Loja-Clientes", "Master", "ax"),
            "phy1-ap1": ("Loja-Clientes", "Master", "ax"),
        }
        # Outside the ACL: uci (ubus level) and any other file (rpcd's path check).
        assert rpc(url, sid, "uci", "get", {"config": "wireless"})["error"]["code"] == -32002
        assert rpc(url, sid, "file", "read", {"path": "/etc/shadow"})["result"] == [openwrt.PERMISSION_DENIED]
        assert rpc(url, sid, "system", "board")["result"][1]["release"]["target"] == "mediatek/filogic"
        dump = rpc(url, sid, "network.interface", "dump")["result"][1]["interface"]
        assert {i["interface"] for i in dump} == {"guest", "lan", "loopback", "wan", "wan6"}
    assert acme.APIS[API]["node"] == NODE
