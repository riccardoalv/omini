"""The real OPNsense plugin against the emulated HA pair fw-hq-01 / fw-hq-02.

Instants are taken in the coming simulated week: the plugin drops Kea leases
whose expiry is before the real clock, so a frozen instant in the past would
see none.
"""

from __future__ import annotations

import ssl
import time
import urllib.request
from datetime import datetime
from typing import Any

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.vendors.opnsense import ROAD_WARRIORS, road_warrior_sessions
from emulator.world import WEEK0, World, at_local, week_index

pytestmark = pytest.mark.skipif(plugin_dir("opnsense") is None, reason="omini-plugin-opnsense not found")

FW1, FW2 = "opnsense-fw-hq-01", "opnsense-fw-hq-02"
WEEK = week_index(time.time()) + 1
TUE_1030 = at_local(1, 10, 30, week=WEEK)
TUE_1035 = at_local(1, 10, 35, week=WEEK)
TUE_1115 = at_local(1, 11, 15, week=WEEK)  # LTE degraded
TUE_1410 = at_local(1, 14, 10, week=WEEK)  # fiber down
TUE_1605 = at_local(1, 16, 5, week=WEEK)  # WireGuard to the branch down
SUN_0400 = at_local(6, 4, 0, week=WEEK)

ROUTED = [(v, name, subnet) for v, name, subnet, routed, _p, _ in acme.HQ_VLANS if subnet and routed and v != 90]


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def emu(world: World):
    with Emulator(at=TUE_1030, apis=[FW1, FW2], world=world) as e:
        yield e


_cache: dict[tuple[str, float], dict[str, Any]] = {}


def device(emu: Emulator, api: str, t: float) -> dict[str, Any]:
    key = (api, t)
    if key not in _cache:
        emu.set_time(t)
        out = emu.collect(api)
        assert "error" not in out, out.get("error")
        [dev] = out["devices"]
        _cache[key] = dev
    return _cache[key]


def ports(dev: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {i["name"]: i for i in dev["interfaces"]}


def local(iso: str) -> tuple[int, int]:
    """An ISO time from the plugin → (hour, minute) in São Paulo."""
    t = datetime.fromisoformat(iso.replace("Z", "+00:00")).timestamp()
    s = (t - WEEK0) % 86400
    return int(s // 3600), int(s % 3600 // 60)


def test_master_firewall(emu: Emulator) -> None:
    dev = device(emu, FW1, TUE_1030)
    assert (dev["name"], dev["role"], dev["vendor"]) == ("fw-hq-01.acme.local", "firewall", "OPNsense")
    assert dev["os_version"].startswith("OPNsense 26.7.")
    assert dev["uptime_s"] > 86400 and 0 < dev["cpu_pct"] < 100 and 25 < dev["mem_pct"] < 60
    p = ports(dev)
    vlan_ifaces = {f"vlan0.{v}" for v, _, _ in ROUTED}
    assert set(p) == {"igc0", "igc1", "igc2", "igc3", "ax0", "ax1", "lagg0", "pppoe0", "wg0"} | vlan_ifaces
    for name in ("igc0", "igc1", "igc2", "igc3"):
        assert (p[name]["type"], p[name]["speed_mbps"], p[name]["connector"]) == ("ethernet", 1000, "rj45")
    for name in ("ax0", "ax1"):
        assert (p[name]["speed_mbps"], p[name]["connector"]) == (10000, "sfp")
        tr = p[name]["transceiver"]
        assert (tr["vendor"], tr["part"], tr["type"]) == ("FS", "SFP-10GSR-85", "10G Base-SR")
        assert 25 < tr["temperature_c"] < 50
    assert (p["lagg0"]["type"], p["lagg0"]["speed_mbps"]) == ("lag", 20000)
    # VLAN interfaces on the LAN trunk, each with the node's address and the CARP VIP (x.x.x.1).
    for vid, _name, subnet in ROUTED:
        i = p[f"vlan0.{vid}"]
        net, bits = subnet.split("/")
        base = net.rsplit(".", 1)[0]
        assert (i["type"], i["vlan"], i["parent"]) == ("vlan", vid, "lagg0")
        assert i["ips"] == [f"{base}.2/{bits}", f"{base}.1/{bits}"]
    assert "10.10.90.1/24" in p["igc2"]["ips"]  # the DMZ's VIP
    assert [(v["id"], v["name"], v["subnet"]) for v in dev["vlans"]] == ROUTED
    # WANs: PPPoE on the master, LTE on igc1.
    assert (p["pppoe0"]["type"], p["pppoe0"]["wan"], p["pppoe0"]["ips"]) == ("tunnel", True, ["203.0.113.45/32"])
    assert p["igc1"]["wan"] and p["igc1"]["ips"] == ["192.168.8.10/24"]
    gw = {g["name"]: g for g in dev["gateways"]}
    assert (gw["WAN_FIBER_PPPOE"]["status"], gw["WAN_FIBER_PPPOE"]["interface"]) == ("up", "pppoe0")
    assert gw["WAN_FIBER_PPPOE"]["rtt_ms"] < 10
    assert (gw["WAN_LTE"]["status"], gw["WAN_LTE"]["interface"]) == ("up", "igc1")
    # Health and extras.
    assert dev["firewall_states"]["limit"] == 1_631_000 and dev["firewall_states"]["current"] > 20_000
    assert len(dev["temperatures"]) == 9 and dev["storage"][0]["mount"] == "/"
    assert len(dev["load_avg"]) == 3 and dev["swap_pct"] == 0.0
    services = {s["name"]: s["running"] for s in dev["services"]}
    assert {"kea", "unbound", "wireguard", "strongswan", "openssh", "ntpd", "dpinger"} <= set(services)
    assert all(services.values())
    pools = {pool["network"]: pool for pool in dev["dhcp_pools"]}
    assert pools["10.10.30.0/24"]["total"] == 151 and pools["10.10.32.0/23"]["used"] > 50


def test_backup_firewall(emu: Emulator, world: World) -> None:
    master, backup = device(emu, FW1, TUE_1030), device(emu, FW2, TUE_1030)
    snap = world.at(TUE_1030)
    assert snap.carp_state("fw-hq-02") == "BACKUP"
    assert backup["name"] == "fw-hq-02.acme.local" and backup["key"] != master["key"]
    p = ports(backup)
    # "Disconnect dialup interfaces" on the CARP backup: the PPPoE has no address, no speed.
    assert p["pppoe0"]["wan"] and not p["pppoe0"]["up"]
    assert p["pppoe0"].get("ips") is None and p["pppoe0"].get("speed_mbps") is None
    assert p["vlan0.30"]["ips"] == ["10.10.30.3/24", "10.10.30.1/24"]
    gw = {g["name"]: g for g in backup["gateways"]}
    assert gw["WAN_FIBER_PPPOE"]["status"] == "unknown" and gw["WAN_LTE"]["status"] == "up"
    # The backup forwards nothing: a small ARP table; Kea HA serves the same leases on both nodes.
    assert len(backup["arp"]) == len([a for a in snap.arp("fw-hq-02")]) < 50 < len(master["arp"])
    assert len(backup["dhcp_leases"]) == len(master["dhcp_leases"])
    stopped = [s["name"] for s in backup["services"] if not s["running"]]
    assert stopped == ["ddclient"]
    assert not any(peer["connected"] for peer in backup["vpn_peers"])


def test_counters_grow(emu: Emulator) -> None:
    a, b = ports(device(emu, FW1, TUE_1030)), ports(device(emu, FW1, TUE_1035))
    for name in ("pppoe0", "lagg0", "ax0", "vlan0.32", "igc2"):
        assert b[name]["rx_bytes"] > a[name]["rx_bytes"] and b[name]["tx_bytes"] > a[name]["tx_bytes"], name
    # Office hours: several Mbit/s through the LAN trunk.
    rate = (b["lagg0"]["rx_bytes"] - a["lagg0"]["rx_bytes"]) * 8 / 300 / 1e6
    assert 5 < rate < 20000


def test_tables_follow_the_office_hours(emu: Emulator, world: World) -> None:
    tue, sun = device(emu, FW1, TUE_1030), device(emu, FW1, SUN_0400)
    s_tue, s_sun = world.at(TUE_1030), world.at(SUN_0400)
    assert len(tue["arp"]) == len(s_tue.arp("fw-hq-01"))
    assert len(sun["arp"]) == len(s_sun.arp("fw-hq-01"))
    assert len(tue["arp"]) > len(sun["arp"]) + 80
    assert len(tue["dhcp_leases"]) == len(s_tue.leases("hq"))
    assert len(sun["dhcp_leases"]) == len(s_sun.leases("hq"))
    assert len(tue["dhcp_leases"]) > 2 * len(sun["dhcp_leases"])

    def wifi(dev: dict[str, Any]) -> int:
        return sum(1 for lease in dev["dhcp_leases"] if lease["ip"].startswith(("10.10.32.", "10.10.33.")))

    assert wifi(tue) > 80 and wifi(sun) < 10
    # A specific lease and ARP entry, as the world has them.
    lease = next(x for x in s_tue.leases("hq") if x.hostname and x.vlan == 30)
    got = {x["mac"]: x for x in tue["dhcp_leases"]}[lease.mac]
    assert (got["ip"], got["hostname"]) == (lease.ip, lease.hostname)
    ip, mac, intf = s_tue.arp("fw-hq-01")[0]
    assert {"ip": ip, "mac": mac, "interface": intf} in tue["arp"]


def test_fiber_down(emu: Emulator) -> None:
    dev = device(emu, FW1, TUE_1410)
    gw = {g["name"]: g for g in dev["gateways"]}
    assert gw["WAN_FIBER_PPPOE"]["status"] == "down" and gw["WAN_FIBER_PPPOE"].get("address") is None
    assert gw["WAN_LTE"]["status"] == "up"
    p = ports(dev)
    assert not p["pppoe0"]["up"] and p["pppoe0"].get("ips") is None
    assert p["igc0"]["up"]  # the ONT is still linked: the PPPoE session is what failed


def test_lte_degraded(emu: Emulator) -> None:
    gw = {g["name"]: g for g in device(emu, FW1, TUE_1115)["gateways"]}
    assert gw["WAN_LTE"]["status"] == "degraded" and gw["WAN_LTE"]["rtt_ms"] > 150
    assert gw["WAN_FIBER_PPPOE"]["status"] == "up"


def test_firmware(emu: Emulator) -> None:
    pending = device(emu, FW1, TUE_1030)["firmware"]
    assert pending["update_available"] and pending["needs_reboot"] and pending["updates"] == 3
    cur = int(pending["current"].rsplit(".", 1)[1])
    assert pending["latest"] == f"26.7.{cur + 1}"
    assert local(pending["checked_at"]) == (9, 0)
    # Updated (and rebooted) on Saturday night.
    done = device(emu, FW1, SUN_0400)
    assert not done["firmware"]["update_available"] and done["firmware"].get("needs_reboot") is None
    assert done["firmware"]["current"] == pending["latest"]
    assert done["uptime_s"] < 6 * 3600


def test_vpn_peers(emu: Emulator) -> None:
    def peers(t: float) -> dict[str, dict[str, Any]]:
        return {p["name"]: p for p in device(emu, FW1, t)["vpn_peers"]}

    morning, down = peers(TUE_1030), peers(TUE_1605)
    branch = morning["Filial Campinas"]
    assert branch["connected"] and branch["protocol"] == "wireguard" and branch["endpoint"] == "198.51.100.20:51820"
    assert branch["rx_bytes"] > 0 and branch["tx_bytes"] > 0
    assert not down["Filial Campinas"]["connected"]
    assert local(down["Filial Campinas"]["last_handshake"]) == (16, 0)
    store = morning["Loja Santos"]
    assert (store["protocol"], store["connected"]) == ("ipsec", True)
    # Road warriors (IT staff) only connect at night; OpenVPN's legacy server has nobody connected.
    for name, addr, _ip in ROAD_WARRIORS:
        assert morning[name]["address"] == addr and not morning[name]["connected"]
    assert not any(p["protocol"] == "openvpn" for p in morning.values())


def test_road_warrior_at_night(emu: Emulator) -> None:
    name, t = next(
        (rw[0], s + 900)
        for rw in ROAD_WARRIORS
        for d in range(5)
        for s, e in road_warrior_sessions(rw[0], at_local(d, 23, 59, week=WEEK))
        if s >= at_local(d, 0, 0, week=WEEK) and e - s > 1800
    )
    peer = {p["name"]: p for p in device(emu, FW1, t)["vpn_peers"]}[name]
    assert peer["connected"] and peer["endpoint"].startswith("203.0.113.") and peer["tx_bytes"] > 0


def test_wrong_secret(emu: Emulator) -> None:
    cfg = dict(emu.config(FW1), api_secret="not-the-secret")
    out = run_plugin("opnsense", cfg, "test")
    assert "rejected the API key or secret" in out.get("error", "")
    out = run_plugin("opnsense", emu.config(FW1), "test")
    assert out.get("message", "").startswith("Connected to fw-hq-01.acme.local (OPNsense 26.7.")


def test_login_page(emu: Emulator) -> None:
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    emu.set_time(TUE_1030)
    with urllib.request.urlopen(emu.urls[FW2] + "/", context=ctx, timeout=10) as r:
        assert "<title>Login | OPNsense</title>" in r.read().decode()
