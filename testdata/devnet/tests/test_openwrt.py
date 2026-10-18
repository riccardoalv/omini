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
    # Its IPv4 addresses, and its IPv6 ones next to them (IPv6 is first-class).
    assert {ip for ip in dev["ips"] if ":" not in ip} == {"10.30.1.1", "10.30.80.1", "100.64.23.45"}
    assert any(":" in ip for ip in dev["ips"])
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
        # Outside the ACL: other uci configs (rpcd's uci check), other ubus methods and any other file (rpcd's path
        # check).
        assert rpc(url, sid, "uci", "get", {"config": "rpcd"})["result"] == [openwrt.PERMISSION_DENIED]
        assert rpc(url, sid, "uci", "commit", {"config": "network"})["error"]["code"] == -32002
        assert rpc(url, sid, "uci", "get", {"config": "network", "section": "lan", "option": "proto"})["result"] == [
            0,
            {"value": "static"},
        ]
        assert rpc(url, sid, "file", "read", {"path": "/etc/shadow"})["result"] == [openwrt.PERMISSION_DENIED]
        assert rpc(url, sid, "system", "board")["result"][1]["release"]["target"] == "mediatek/filogic"
        dump = rpc(url, sid, "network.interface", "dump")["result"][1]["interface"]
        assert {i["interface"] for i in dump} == {"guest", "lan", "loopback", "wan", "wan6"}
    assert acme.APIS[API]["node"] == NODE


def test_disable_and_enable_a_port() -> None:
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        key = device(emu.collect(API))["key"]
        change = {"device": key, "kind": "port.enabled", "port": "lan3", "value": False}
        lan3 = ifaces(device(emu.collect(API, "apply", change=change)))["lan3"]
        assert (lan3["enabled"], lan3["up"]) == (False, False)  # the store PC there drops
        by = ifaces(device(emu.collect(API)))
        assert by["lan3"]["enabled"] is False and "speed_mbps" not in by["lan3"]
        assert by["lan2"]["enabled"] is True and by["lan2"]["up"] is True
        # Confirmed: rpcd's rollback timer does not bring it back.
        emu.set_time(SAT_15 + 120)
        assert ifaces(device(emu.collect(API)))["lan3"]["enabled"] is False

        change["value"] = True
        lan3 = ifaces(device(emu.collect(API, "apply", change=change)))["lan3"]
        assert lan3["enabled"] is True and lan3["up"] is True
        ports = ifaces(device(emu.collect(API)))
        assert all(ports[p]["enabled"] for p in openwrt.PORTS)
        assert "enabled" not in ports["eth0"]  # the switch's CPU port is not offered


def test_an_unconfirmed_apply_is_rolled_back() -> None:
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        url, sec = emu.urls[API], emu.secrets[API]
        sid = rpc(
            url, openwrt.NULL_SESSION, "session", "login", {"username": sec["username"], "password": sec["password"]}
        )["result"][1]["ubus_rpc_session"]
        rpc(url, sid, "uci", "add", {"config": "network", "type": "device", "values": {"name": "lan1", "enabled": "0"}})
        assert rpc(url, sid, "uci", "apply", {"rollback": True, "timeout": 30})["result"] == [0]
        assert rpc(url, sid, "network.device", "status", {"name": "lan1"})["result"][1]["present"] is False
        # Omini's own apply waits: another change is pending confirmation.
        out = emu.collect(API, "apply", change={"device": "x", "kind": "port.enabled", "port": "lan2", "value": False})
        assert "another change waiting to be confirmed" in out["error"]
        emu.set_time(SAT_15 + 31)  # not confirmed: rpcd restores the file and the port comes back
        assert ifaces(device(emu.collect(API)))["lan1"]["enabled"] is True


def test_a_read_only_acl_maps_but_does_not_change(monkeypatch: pytest.MonkeyPatch) -> None:
    # The login without "list write": only the read block of the README's ACL file.
    acl = json.loads(json.dumps(openwrt.ACL))
    acl["ubus"]["uci"] = ["get"]
    acl["uci"] = {"network": ["read"]}
    monkeypatch.setattr(openwrt, "ACL", acl)
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        dev = device(emu.collect(API))
        assert ifaces(dev)["lan3"]["enabled"] is True
        change = {"device": dev["key"], "kind": "port.enabled", "port": "lan3", "value": False}
        out = emu.collect(API, "apply", change=change)
        assert "the ACL of user 'omini' does not allow changes (ubus uci revert)" in out["error"]
        assert "list write 'omini'" in out["error"]
        assert ifaces(device(emu.collect(API)))["lan3"]["enabled"] is True


def test_a_wifi_on_the_lan() -> None:
    with Emulator(at=THU_16, apis=[API], world=World()) as emu:

        def wifi(res: dict) -> dict[str, dict]:
            assert "error" not in res, res.get("error")
            return {w["ssid"]: w for w in res["devices"][0].get("wifi_networks") or []}

        before = wifi(emu.collect(API))
        assert before
        change = {
            "device": "x",
            "kind": "wifi.set",
            "value": {"ssid": "Loja-Lab", "vlan": 1, "password": "correct horse"},
        }
        got = wifi(emu.collect(API, "apply", change=change))
        assert "Loja-Lab" in got and got["Loja-Lab"].get("vlan") is None  # the LAN
        change = {"device": "x", "kind": "wifi.delete", "value": {"ssid": "Loja-Lab"}}
        assert "Loja-Lab" not in wifi(emu.collect(API, "apply", change=change))


def apply(emu: Emulator, kind: str, value: Any, port: str | None = None) -> dict[str, Any]:
    change: dict[str, Any] = {"device": "x", "kind": kind, "value": value}
    if port:
        change["port"] = port
    return emu.collect(API, "apply", change=change)


def test_vlans_need_bridge_vlan_filtering() -> None:
    # The store router's bridges do not filter VLANs: the plugin says how to turn it on, and changes nothing.
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        before = device(emu.collect(API))
        assert "vlans" not in before and all("vlans" not in i for i in before["interfaces"])
        for kind, value, port in (
            ("vlan.set", {"id": 30, "name": "Cameras"}, None),
            ("port.vlans", {"untagged": 1, "tagged": [30]}, "lan4"),
            ("network.set", {"vlan": 30, "gateway": "10.30.30.1/24", "access": "local"}, None),
        ):
            out = apply(emu, kind, value, port)
            assert "bridge br-lan does not filter VLANs" in out["error"]
            assert "Network → Interfaces → Devices → br-lan" in out["error"]
        after = device(emu.collect(API))
        assert (after["networks"], after["interfaces"][0]) == (before["networks"], before["interfaces"][0])
        assert "vlans" not in after


def test_the_lan_and_its_fixed_addresses() -> None:
    world = World()
    snap = world.at(SAT_15)
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        dev = device(emu.collect(API))
        [lan] = dev["networks"]  # the guest bridge has no VLAN; the WAN is not a network
        assert lan == {
            "vlan": 1,
            "name": "lan",
            "interface": "lan",
            "device": "br-lan",
            "parent": "br-lan",
            "gateway": "10.30.1.1/24",
            "dhcp": {"enabled": True, "from": "10.30.1.100", "to": "10.30.1.200"},
            "access": "custom",
        }
        fixed = {r["mac"]: r for r in dev["dhcp_reservations"]}
        for name in ("pos-01", "pos-02"):
            n = snap.node(name)
            assert fixed[n.mac]["ip"] == n.ip

        lan["dhcp"] = {"enabled": True, "from": "10.30.1.50", "to": "10.30.1.99"}
        got = device(apply(emu, "network.set", lan))["networks"][0]
        assert got["dhcp"] == {"enabled": True, "from": "10.30.1.50", "to": "10.30.1.99"}
        assert got["access"] == "custom"  # custom: the firewall is left alone

        res = {"mac": "02:00:00:aa:bb:01", "ip": "10.30.1.40", "name": "Label printer 2"}
        fixed = {r["mac"]: r for r in device(apply(emu, "reservation.set", res))["dhcp_reservations"]}
        assert fixed["02:00:00:aa:bb:01"] == {"mac": "02:00:00:aa:bb:01", "ip": "10.30.1.40", "name": "label-printer-2"}
        # Confirmed: rpcd's rollback timer does not undo it.
        emu.set_time(SAT_15 + 120)
        dev = device(emu.collect(API))
        assert dev["networks"][0]["dhcp"]["from"] == "10.30.1.50"
        assert "02:00:00:aa:bb:01" in {r["mac"] for r in dev["dhcp_reservations"]}
        fixed = device(apply(emu, "reservation.delete", {"mac": "02:00:00:aa:bb:01"}))["dhcp_reservations"]
        assert "02:00:00:aa:bb:01" not in {r["mac"] for r in fixed}
        assert "discovery between VLANs" in apply(emu, "network.set", {**lan, "mdns": True})["error"]
        assert "LAN cannot be removed" in apply(emu, "network.delete", {"vlan": 1})["error"]


def test_a_vlan_from_the_switch_port_to_the_router() -> None:
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        # The admin turns bridge VLAN filtering on in LuCI: VLAN 1 untagged on every LAN port, the LAN on br-lan.1.
        url, sec = emu.urls[API], emu.secrets[API]
        sid = rpc(
            url, openwrt.NULL_SESSION, "session", "login", {"username": sec["username"], "password": sec["password"]}
        )["result"][1]["ubus_rpc_session"]
        ports = [f"lan{k}:u*" for k in range(1, 6)]
        rpc(
            url,
            sid,
            "uci",
            "add",
            {"config": "network", "type": "bridge-vlan", "values": {"device": "br-lan", "vlan": "1", "ports": ports}},
        )
        rpc(url, sid, "uci", "set", {"config": "network", "section": "lan", "values": {"device": "br-lan.1"}})
        assert rpc(url, sid, "uci", "apply", {"rollback": True, "timeout": 30})["result"] == [0]
        assert rpc(url, sid, "uci", "confirm", {})["result"] == [0]

        dev = device(emu.collect(API))
        assert [v["id"] for v in dev["vlans"]] == [1]
        assert ifaces(dev)["lan4"]["vlans"] == {"untagged": 1}
        assert "vlans" not in ifaces(dev)["eth1"]  # the WAN is no bridge port

        dev = device(apply(emu, "vlan.set", {"id": 30, "name": "Cameras"}))
        assert {"id": 30, "name": "Cameras"}.items() <= next(v for v in dev["vlans"] if v["id"] == 30).items()
        dev = device(apply(emu, "port.vlans", {"untagged": 30, "tagged": None}, "lan4"))
        assert ifaces(dev)["lan4"]["vlans"] == {"untagged": 30}
        assert ifaces(dev)["lan5"]["vlans"] == {"untagged": 1}  # the other ports as they were
        dev = device(apply(emu, "port.vlans", {"untagged": 1, "tagged": [30]}, "lan5"))
        assert ifaces(dev)["lan5"]["vlans"] == {"untagged": 1, "tagged": [30]}

        cams = {
            "vlan": 30,
            "name": "Cameras",
            "gateway": "10.30.30.1/24",
            "dhcp": {"enabled": True, "from": "10.30.30.100", "to": "10.30.30.200"},
            "access": "local",
        }
        nets = {n["vlan"]: n for n in device(apply(emu, "network.set", cams))["networks"]}
        assert nets[30] == {
            **cams,
            "name": "cameras",
            "interface": "cameras",
            "device": "br-lan.30",
            "parent": "br-lan",
        }
        assert nets[1]["device"] == "br-lan.1"
        nets = {n["vlan"]: n for n in device(apply(emu, "network.set", {**cams, "access": "internet"}))["networks"]}
        assert nets[30]["access"] == "internet"
        assert "uses VLAN 30: delete its network first" in apply(emu, "vlan.delete", {"id": 30})["error"]

        emu.set_time(SAT_15 + 120)  # confirmed: nothing comes back
        dev = device(apply(emu, "network.delete", {"vlan": 30}))
        assert [n["vlan"] for n in dev["networks"]] == [1]
        assert 30 in [v["id"] for v in dev["vlans"]]  # its ports still use it: the VLAN stays
        apply(emu, "port.vlans", {"untagged": 1}, "lan4")
        apply(emu, "port.vlans", {"untagged": 1}, "lan5")
        dev = device(apply(emu, "vlan.delete", {"id": 30}))
        assert [v["id"] for v in dev["vlans"]] == [1]
        assert all(ifaces(dev)[f"lan{k}"]["vlans"] == {"untagged": 1} for k in range(1, 6))
        # The firewall and DHCP are back as they were.
        fw = rpc(url, sid, "uci", "get", {"config": "firewall"})["result"][1]["values"]
        assert not [s for s in fw.values() if "cameras" in (s.get("name"), s.get("src"))]
        dhcp = rpc(url, sid, "uci", "get", {"config": "dhcp"})["result"][1]["values"]
        assert "cameras" not in dhcp


def test_local_dns_names() -> None:
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        dev = device(emu.collect(API))
        assert dev["dns_domain"] == "lan" and "dns_records" not in dev
        dev = device(apply(emu, "dns.set", {"name": "nvr.lan", "ip": "10.30.1.30"}))
        assert dev["dns_records"] == [{"name": "nvr.lan", "ip": "10.30.1.30"}]
        dev = device(apply(emu, "dns.set", {"name": "nvr.lan", "ip": "10.30.1.31"}))
        assert dev["dns_records"] == [{"name": "nvr.lan", "ip": "10.30.1.31"}]
        emu.set_time(SAT_15 + 120)  # confirmed: rpcd's rollback timer does not undo it
        assert device(emu.collect(API))["dns_records"] == [{"name": "nvr.lan", "ip": "10.30.1.31"}]
        assert "dns_records" not in device(apply(emu, "dns.delete", {"name": "nvr.lan"}))
        assert "not a DNS name" in apply(emu, "dns.set", {"name": "a b", "ip": "10.30.1.30"})["error"]


def test_pause_a_device() -> None:
    world = World()
    pc = world.at(SAT_15).node("pc-loja")
    with Emulator(at=SAT_15, apis=[API], world=world) as emu:
        url, sec = emu.urls[API], emu.secrets[API]
        assert "paused" not in device(emu.collect(API))
        value = {"mac": pc.mac, "ips": [pc.ip], "name": "Store PC"}
        dev = device(apply(emu, "pause.set", value))
        assert dev["paused"] == [{"mac": pc.mac, "ips": [pc.ip], "name": "Store PC"}]
        sid = rpc(
            url, openwrt.NULL_SESSION, "session", "login", {"username": sec["username"], "password": sec["password"]}
        )["result"][1]["ubus_rpc_session"]
        fw = rpc(url, sid, "uci", "get", {"config": "firewall"})["result"][1]["values"]
        rules = sorted((s["src"], s["dest"], s["target"]) for s in fw.values() if s.get("name") == "Omini pause")
        assert rules == [("guest", "wan", "REJECT"), ("lan", "wan", "REJECT")]  # every zone but the WAN's
        # By MAC, IPv4 and IPv6 alike (towards the WAN zone: local IPv6 stays reachable).
        for r in (s for s in fw.values() if s.get("name") == "Omini pause"):
            assert r["src_mac"] == [pc.mac] and r["family"] == "any" and r["proto"] == "all"
            assert r["omini"] == [f"{pc.mac} {pc.ip} Store PC"] and "ipset" not in r
        assert not [s for s in fw.values() if s[".type"] == "ipset"]

        assert "is the router itself" in apply(emu, "pause.set", {"mac": pc.mac, "ips": ["10.30.1.1"]})["error"]
        emu.set_time(SAT_15 + 120)
        assert device(emu.collect(API))["paused"][0]["mac"] == pc.mac
        assert "paused" not in device(apply(emu, "pause.delete", {"mac": pc.mac}))
        fw = rpc(url, sid, "uci", "get", {"config": "firewall"})["result"][1]["values"]
        assert not [s for s in fw.values() if s[".type"] == "ipset" or s.get("name") == "Omini pause"]


def test_wake_on_lan(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr(openwrt, "WAKES", [])
    with Emulator(at=SUN_15, apis=[API], world=World()) as emu:
        device(apply(emu, "wol.send", {"mac": "02:00:00:aa:bb:07", "ip": "10.30.80.7"}))
        device(apply(emu, "wol.send", {"mac": "02:00:00:aa:bb:08"}))
        assert openwrt.WAKES == [("br-guest", "02:00:00:aa:bb:07"), ("br-lan", "02:00:00:aa:bb:08")]
        assert "holds 10.99.0.7" in apply(emu, "wol.send", {"mac": "02:00:00:aa:bb:07", "ip": "10.99.0.7"})["error"]
        monkeypatch.setattr(openwrt, "ETHERWAKE_INSTALLED", False)
        assert "etherwake is not installed" in apply(emu, "wol.send", {"mac": "02:00:00:aa:bb:07"})["error"]
    assert len(openwrt.WAKES) == 2


def test_wake_on_lan_needs_the_acl(monkeypatch: pytest.MonkeyPatch) -> None:
    acl = json.loads(json.dumps(openwrt.ACL))
    del acl["file"][openwrt.ETHERWAKE]
    monkeypatch.setattr(openwrt, "ACL", acl)
    with Emulator(at=SUN_15, apis=[API], world=World()) as emu:
        err = apply(emu, "wol.send", {"mac": "02:00:00:aa:bb:07"})["error"]
        assert "does not allow running etherwake" in err and '"/usr/bin/etherwake": ["list", "exec"]' in err


def test_backup() -> None:
    with Emulator(at=SAT_15, apis=[API], world=World()) as emu:
        key = device(emu.collect(API))["key"]
        out = emu.collect(API, "backup")
        assert "error" not in out, out.get("error")
        [b] = out["backups"]
        assert b["device"] == key and b["format"] == "text"
        text = b["content"]
        for package in ("network", "wireless", "dhcp", "firewall", "system"):
            assert f"package {package}\n" in text
        assert "config interface 'lan'\n\toption device 'br-lan'\n\toption proto 'static'\n" in text
        assert "option hostname 'store-rt01'" in text
        assert "emulated-key" not in text and "option key " not in text  # the Wi-Fi keys stay out
        # Only configuration: an hour later, with other clients and counters, the same text.
        emu.set_time(SAT_15 + 3600)
        assert emu.collect(API, "backup")["backups"][0]["content"] == text
        apply(emu, "dns.set", {"name": "nvr.lan", "ip": "10.30.1.30"})
        after = emu.collect(API, "backup")["backups"][0]["content"]
        assert after != text and "config domain\n\toption name 'nvr.lan'\n\toption ip '10.30.1.30'\n" in after
