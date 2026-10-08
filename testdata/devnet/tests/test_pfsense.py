"""The pfSense REST API emulator (lab-fw01) against the real omini-plugin-pfsense."""

from __future__ import annotations

import json
import ssl
import urllib.error
import urllib.request

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("pfsense") is None, reason="omini-plugin-pfsense not found")

TUESDAY_10 = at_local(1, 10, 0)
TUESDAY_10_15 = at_local(1, 10, 15)
SUNDAY_REBOOT = at_local(6, 2, 25)  # pve02 reboots at 02:20 for 8 minutes; lab-fw01 goes down with it


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def runs(world: World) -> dict[str, dict]:
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        first = emu.collect("pfsense")
        emu.set_time(TUESDAY_10_15)
        second = emu.collect("pfsense")
        test = emu.collect("pfsense", action="test")
    return {"first": first, "second": second, "test": test}


def device(out: dict) -> dict:
    assert "devices" in out, out
    assert len(out["devices"]) == 1
    return out["devices"][0]


def iface(d: dict, name: str) -> dict:
    return next(i for i in d["interfaces"] if i["name"] == name)


def test_device(runs: dict, world: World) -> None:
    d = device(runs["first"])
    node = world.nodes["lab-fw01"]
    assert d["key"] == node.ports["vtnet1"].mac  # the LAN MAC
    assert d["name"] == "lab-fw01.lab.acme.local"
    assert d["role"] == "firewall"
    assert d["vendor"] == "pfSense"
    assert d["os_version"] == "pfSense CE 2.8.0"
    assert d["firmware"]["current"] == "pfSense CE 2.8.0"
    assert d.get("model") is None  # a KVM guest identifies as plain "pfSense"
    snap = world.at(TUESDAY_10)
    assert abs(d["uptime_s"] - snap.uptime("lab-fw01")) <= 1
    assert d["cpu_pct"] == snap.cpu("lab-fw01")
    assert d["swap_pct"] == 0
    assert len(d["load_avg"]) == 3
    assert not d.get("temperatures")  # no sensor in a VM
    assert sorted(d["macs"]) == sorted([node.ports["vtnet0"].mac, node.ports["vtnet1"].mac])
    assert d["ips"] == ["10.10.120.10", "192.168.200.1"]
    services = {s["name"]: s for s in d["services"]}
    assert {"unbound", "kea-dhcp4", "ntpd", "sshd", "dpinger"} <= set(services)
    assert all(s["running"] for s in services.values())
    assert d["firewall_states"]["limit"] == 407000
    assert 0 < d["firewall_states"]["current"] < 2000
    assert not d.get("vpn_peers")
    assert not d.get("vlans")


def test_interfaces_and_gateway(runs: dict) -> None:
    d = device(runs["first"])
    assert [i["name"] for i in d["interfaces"]] == ["vtnet0", "vtnet1"]
    wan, lan = iface(d, "vtnet0"), iface(d, "vtnet1")
    assert wan["description"] == "WAN" and wan["wan"] is True and wan["up"] is True
    assert wan["ips"] == ["10.10.120.10/24"]
    assert lan["description"] == "LAN" and not lan.get("wan")
    assert lan["ips"] == ["192.168.200.1/24"]
    for i in (wan, lan):
        assert i["type"] == "other"  # virtio: no jack, no real speed
        assert i.get("speed_mbps") is None and i.get("connector") is None
        assert i["media"] == "10Gbase-T <full-duplex>"
    [gw] = d["gateways"]
    assert gw["name"] == "WANGW"
    assert gw["interface"] == "vtnet0"
    assert gw["address"] == "10.10.120.1"
    assert gw["status"] == "up"
    assert 0 < gw["rtt_ms"] < 2
    assert gw["loss_pct"] == 0


def test_counters_grow(runs: dict, world: World) -> None:
    a, b = device(runs["first"]), device(runs["second"])
    for name in ("vtnet0", "vtnet1"):
        assert iface(b, name)["rx_bytes"] > iface(a, name)["rx_bytes"] > 0
        assert iface(b, name)["tx_bytes"] > iface(a, name)["tx_bytes"] > 0
    st = world.at(TUESDAY_10).port("lab-fw01", "vtnet0")
    assert iface(a, "vtnet0")["rx_bytes"] == st.rx_bytes


def test_arp_and_leases(runs: dict, world: World) -> None:
    d = device(runs["first"])
    arp = {e["ip"]: e for e in d["arp"]}
    for name in ("lab-k3s-01", "lab-k3s-02", "lab-k3s-03", "lab-kali"):
        n = world.nodes[name]
        assert arp[n.ip]["mac"] == n.mac
        assert arp[n.ip]["interface"] == "vtnet1"
    assert arp["10.10.120.1"]["mac"] == acme.carp_mac(120)
    assert arp["10.10.120.1"]["interface"] == "vtnet0"
    assert arp["10.10.120.2"]["mac"] == world.nodes["fw-hq-01"].ports["lagg0"].mac
    assert "192.168.200.1" not in arp  # its own (permanent) entries are skipped
    leases = {lease["ip"]: lease for lease in d["dhcp_leases"]}
    for name in ("lab-k3s-01", "lab-k3s-02", "lab-k3s-03", "lab-kali"):
        n = world.nodes[name]
        assert leases[n.ip]["mac"] == n.mac
        assert leases[n.ip]["hostname"] == name
    [pool] = d["dhcp_pools"]
    assert pool["network"] == "LAN" and pool["total"] == 100


def test_test_action(runs: dict) -> None:
    assert runs["test"]["message"] == "Connected to lab-fw01 (pfSense CE 2.8.0)"


def test_unreachable_while_pve02_reboots(world: World) -> None:
    with Emulator(at=SUNDAY_REBOOT, apis=["pfsense"], world=world) as emu:
        out = emu.collect("pfsense")
        assert "error" in out and "devices" not in out
        emu.set_time(at_local(6, 2, 40))  # back with pve02 (it does not migrate): just booted
        d = device(emu.collect("pfsense"))
    assert 0 < d["uptime_s"] < 15 * 60
    assert iface(d, "vtnet0")["rx_bytes"] < 10**9  # counters restarted at boot


def test_wrong_key_and_raw_api(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        url = emu.urls["pfsense"]
        cfg = dict(emu.config("pfsense"), api_key="0" * 48)
        from emulator.testing import run_plugin

        out = run_plugin("pfsense", cfg)
        assert "rejected the API key" in out["error"]

        ctx = ssl.create_default_context()
        ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE

        def get(path: str, key: str | None) -> tuple[int, str]:
            req = urllib.request.Request(url + path, headers={"X-API-Key": key} if key else {})
            try:
                with urllib.request.urlopen(req, context=ctx) as r:
                    return r.status, r.read().decode()
            except urllib.error.HTTPError as e:
                return e.code, e.read().decode()

        status, body = get("/", None)
        assert status == 200 and "<title>pfSense - Login</title>" in body
        status, body = get("/api/v2/status/system", None)
        assert status == 401 and json.loads(body)["response_id"] == "AUTH_AUTHENTICATION_FAILED"
        key = emu.secrets["pfsense"]["api_key"]
        status, body = get("/api/v2/diagnostics/arp_table?limit=2&offset=1", key)
        page = json.loads(body)["data"]
        assert status == 200 and len(page) == 2 and page[0]["id"] == 1
        status, body = get("/api/v2/status/interfaces?name=lan", key)
        assert [r["hwif"] for r in json.loads(body)["data"]] == ["vtnet1"]
        assert get("/api/v2/status/wireguard/tunnels", key)[0] == 424
        assert get("/api/v2/status/nothing", key)[0] == 404
