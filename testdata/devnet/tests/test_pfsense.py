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


def networks_of(d: dict) -> dict[int, dict]:
    return {n["vlan"]: n for n in d.get("networks") or []}


def test_reads_the_lab_network(runs: dict) -> None:
    d = device(runs["first"])
    nets = networks_of(d)
    assert list(nets) == [1]  # the WAN is never a network
    lan = nets[1]
    assert (lan["interface"], lan["device"], lan["gateway"], lan["access"]) == (
        "lan",
        "vtnet1",
        "192.168.200.1/24",
        "all",  # pfSense's default LAN rules
    )
    assert lan["dhcp"] == {
        "enabled": True,
        "from": "192.168.200.100",
        "to": "192.168.200.199",
        "reserve_in_range": False,
    }
    names = {r["name"] for r in d["dhcp_reservations"]}
    assert {"lab-k3s-01", "lab-k3s-02", "lab-k3s-03"} <= names


def test_creates_changes_and_removes_a_network(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        want = {
            "vlan": 77,
            "name": "LABNET",
            "gateway": "10.77.0.1/24",
            "dhcp": {"enabled": True, "from": "10.77.0.100", "to": "10.77.0.199"},
            "access": "internet",
        }
        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "network.set", "value": want})
        assert "error" not in out, out.get("error")
        d = device(out)
        got = networks_of(d)[77]
        assert (got["interface"], got["device"], got["parent"]) == ("opt1", "vtnet1.77", "vtnet1")
        assert (got["gateway"], got["access"], got["dhcp"]["from"]) == ("10.77.0.1/24", "internet", "10.77.0.100")
        assert [(v["id"], v["interface"]) for v in d["vlans"]] == [(77, "vtnet1.77")]
        assert iface(d, "vtnet1.77")["ips"] == ["10.77.0.1/24"]

        want["access"] = "local"
        want["dhcp"] = {"enabled": False}
        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "network.set", "value": want})
        assert "error" not in out, out.get("error")
        got = networks_of(device(out))[77]
        assert (got["access"], got["dhcp"]["enabled"]) == ("local", False)

        out = emu.collect(
            "pfsense", "apply", change={"device": "x", "kind": "network.set", "value": {**want, "mdns": True}}
        )
        assert "Avahi" in out["error"]

        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "network.delete", "value": {"vlan": 77}})
        assert "error" not in out, out.get("error")
        d = device(out)
        assert 77 not in networks_of(d) and not d.get("vlans")
        assert networks_of(d)[1]["access"] == "all"  # the LAN's rules were never touched

        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "network.delete", "value": {"vlan": 1}})
        assert "LAN" in out["error"]


def test_fixes_an_address(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        value = {"mac": "02:00:00:00:77:01", "ip": "192.168.200.77", "name": "printer"}
        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "reservation.set", "value": value})
        assert "error" not in out, out.get("error")
        assert value in device(out)["dhcp_reservations"]
        out = emu.collect(
            "pfsense", "apply", change={"device": "x", "kind": "reservation.delete", "value": {"mac": value["mac"]}}
        )
        assert "error" not in out, out.get("error")
        assert not any(r["mac"] == value["mac"] for r in device(out)["dhcp_reservations"])

        # pfSense keeps fixed addresses out of the DHCP range, and says so.
        dhcp = networks_of(device(out))[1]["dhcp"]
        assert dhcp["reserve_in_range"] is False
        inside = {**value, "ip": dhcp["from"]}
        out = emu.collect("pfsense", "apply", change={"device": "x", "kind": "reservation.set", "value": inside})
        assert "outside it" in out["error"]


def apply(emu: Emulator, kind: str, value: dict) -> dict:
    out = emu.collect("pfsense", "apply", change={"device": "x", "kind": kind, "value": value})
    assert "error" not in out, out.get("error")
    return device(out)


def test_reads_names_pauses_and_no_lagg(runs: dict) -> None:
    d = device(runs["first"])
    assert d["dns_domain"] == "lab.acme.local"
    assert not d.get("dns_records")  # no host override in the design
    assert not d.get("paused")
    assert not any(i.get("lag") for i in d["interfaces"])  # a VM: no LAGG


def test_local_dns_names(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        d = apply(emu, "dns.set", {"name": "k3s.lab.acme.local", "ip": "192.168.200.21"})
        assert d["dns_records"] == [{"name": "k3s.lab.acme.local", "ip": "192.168.200.21"}]
        d = apply(emu, "dns.set", {"name": "k3s.lab.acme.local", "ip": "192.168.200.22"})
        assert d["dns_records"] == [{"name": "k3s.lab.acme.local", "ip": "192.168.200.22"}]
        d = apply(emu, "dns.delete", {"name": "k3s.lab.acme.local"})
        assert not d.get("dns_records")


def test_wake_on_lan(world: World) -> None:
    kali = world.nodes["lab-kali"]
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        apply(emu, "wol.send", {"mac": kali.mac, "ip": kali.ip})
        apply(emu, "wol.send", {"mac": kali.mac})  # no address: the LAN
        out = emu.collect(
            "pfsense",
            "apply",
            change={"device": "x", "kind": "wol.send", "value": {"mac": kali.mac, "ip": "10.10.120.50"}},
        )
        assert "no network of pfSense holds 10.10.120.50" in out["error"]  # the WAN's network


def test_pause_and_resume_internet(world: World) -> None:
    kali = world.nodes["lab-kali"]
    with Emulator(at=TUESDAY_10, apis=["pfsense"], world=world) as emu:
        d = apply(emu, "pause.set", {"mac": kali.mac, "ips": [kali.ip], "name": "lab-kali"})
        assert d["paused"] == [{"mac": kali.mac, "ips": [kali.ip], "name": "lab-kali"}]
        assert networks_of(d)[1]["access"] == "all"  # the block is no access preset
        d = apply(emu, "pause.set", {"mac": kali.mac, "ips": ["192.168.200.98"], "name": "lab-kali"})
        assert d["paused"][0]["ips"] == ["192.168.200.98"]  # its new address replaces the old one
        d = apply(emu, "pause.delete", {"mac": kali.mac})
        assert not d.get("paused")

        out = emu.collect(
            "pfsense",
            "apply",
            change={"device": "x", "kind": "pause.set", "value": {"mac": kali.mac, "ips": ["192.168.200.1"]}},
        )
        assert "pfSense's own address" in out["error"]
