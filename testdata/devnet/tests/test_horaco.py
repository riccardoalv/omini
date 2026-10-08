"""The Horaco switch emulator, read by the real omini-plugin-horaco."""

from __future__ import annotations

import threading
import time
import urllib.request
from typing import Any

import pytest

from emulator.testing import Emulator, plugin_dir
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("horaco") is None, reason="omini-plugin-horaco not found")

TUE_1030 = at_local(1, 10, 30)
LATER = TUE_1030 + 300
SW = "sw-wh-01"


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def runs(world: World) -> dict[str, Any]:
    with Emulator(at=TUE_1030, apis=["horaco"], world=world) as emu:
        first = emu.collect("horaco")
        emu.set_time(LATER)
        second = emu.collect("horaco")
    return {"first": first, "second": second}


def device(out: dict[str, Any]) -> dict[str, Any]:
    assert "error" not in out, out
    [dev] = out["devices"]
    return dev


def ifaces(dev: dict[str, Any]) -> dict[str, dict[str, Any]]:
    return {i["name"]: i for i in dev["interfaces"]}


def test_device(runs, world):
    dev = device(runs["first"])
    snap = world.at(TUE_1030)
    node = snap.node(SW)
    assert dev["key"] == node.mac
    assert (dev["name"], dev["role"], dev["vendor"], dev["model"], dev["os_version"]) == (
        "sw-wh-01",
        "switch",
        "Horaco",
        "HC-SWTGW218AS",
        "V200.1.7",
    )
    assert dev["ips"] == ["10.10.10.8"]
    assert dev["macs"] == [node.mac]
    assert dev["uptime_s"] == snap.uptime(SW)


def test_ports_speeds_and_connectors(runs):
    ports = ifaces(device(runs["first"]))
    assert sorted(ports, key=lambda n: int(n.split()[1])) == [f"Port {n}" for n in range(1, 11)]
    p9 = ports["Port 9"]  # fiber uplink to core-sw01
    assert (p9["up"], p9["speed_mbps"], p9["connector"], p9["duplex"]) == (True, 10000, "sfp", "full")
    assert (ports["Port 10"]["up"], ports["Port 10"]["connector"]) == (False, "sfp")
    for name in ("Port 1", "Port 2"):  # the Halo units (their eth0 runs at 1G)
        assert (ports[name]["up"], ports[name]["speed_mbps"], ports[name]["connector"]) == (True, 1000, "rj45")
    assert ports["Port 3"]["up"] is True  # label printer
    for name in ("Port 7", "Port 8"):
        assert (ports[name]["up"], ports[name].get("speed_mbps"), ports[name]["connector"]) == (False, None, "rj45")


def test_counters_match_the_world_and_grow(runs, world):
    a, b = ifaces(device(runs["first"])), ifaces(device(runs["second"]))
    snap = world.at(TUE_1030)
    for name in ("Port 1", "Port 2", "Port 9"):
        ps = snap.port(SW, name)
        assert (a[name]["rx_bytes"], a[name]["tx_bytes"]) == (ps.rx_bytes, ps.tx_bytes)
        assert b[name]["rx_bytes"] > a[name]["rx_bytes"] and b[name]["tx_bytes"] > a[name]["tx_bytes"]
    assert a["Port 9"]["rx_bytes"] > 2**32  # past 32 bits: the "hi-lo" split is read back
    assert a["Port 7"]["rx_bytes"] == 0


def test_mac_table_across_pages(runs, world):
    dev = device(runs["first"])
    snap = world.at(TUE_1030)
    fdb = {(e["mac"], e["port"], e["vlan"]) for e in dev["fdb"]}
    want = {(m, p, v) for m, p, v in snap.fdb(SW)}
    assert len(want) > 30  # more than one page of 30
    # The plugin keeps one entry per MAC and port (a firewall's MAC in six VLANs counts once).
    assert {(m, p) for m, p, _ in fdb} == {(m, p) for m, p, _ in want}
    assert fdb <= want
    mac = {n: snap.node(n).mac for n in ("halo-wh-01", "halo-wh-02", "halo-wh-03", "prn-wh-zebra", "core-sw01")}
    assert (mac["halo-wh-01"], "Port 1", 34) in fdb
    assert (mac["halo-wh-02"], "Port 2", 34) in fdb
    assert (mac["halo-wh-03"], "Port 2", 34) in fdb  # meshed behind halo-wh-02
    assert (mac["prn-wh-zebra"], "Port 3", 70) in fdb
    assert (mac["core-sw01"], "Port 9", 10) in fdb
    # Wi-Fi clients of the Halo units are learned on the units' ports.
    clients = {"Port 1": snap.wifi_clients("halo-wh-01"), "Port 2": snap.wifi_clients("halo-wh-02")}
    clients["Port 2"] += snap.wifi_clients("halo-wh-03")
    seen = 0
    for port, assocs in clients.items():
        for a in assocs:
            m = a.client.mac
            assert (m, port, 34) in fdb, (a.client.name, port)
            seen += 1
    assert seen >= 6


def test_wrong_password(world):
    with Emulator(at=TUE_1030, apis=["horaco"], world=world) as emu:
        cfg = dict(emu.config("horaco"), password="not-the-password")
        from emulator.testing import run_plugin

        out = run_plugin("horaco", cfg)
    assert "rejected the username or password" in out.get("error", ""), out


def test_web_pages_and_a_busy_switch(world):
    busy = at_local(1, 10, 17) + 1  # the web server drops every connection for a few seconds
    with Emulator(at=TUE_1030, apis=["horaco"], world=world) as emu:
        url = emu.urls["horaco"]
        root = urllib.request.urlopen(url + "/", timeout=5).read().decode()
        assert "<title>Login</title>" in root and 'name ="login"' in root
        # Without a session a page sends the browser to the login page.
        page = urllib.request.urlopen(url + "/info.cgi", timeout=5).read().decode()
        assert 'location.replace("/login.cgi")' in page
        missing = urllib.request.Request(url + "/nothing.cgi")
        with pytest.raises(urllib.error.HTTPError):
            urllib.request.urlopen(missing, timeout=5)

        emu.set_time(busy)
        out: dict[str, Any] = {}
        t = threading.Thread(target=lambda: out.update(emu.collect("horaco")))
        t.start()
        time.sleep(1.5)  # the plugin's first tries are dropped...
        emu.set_time(busy + 60)  # ... then the switch answers again
        t.join(120)
    dev = device(out)
    assert dev["model"] == "HC-SWTGW218AS" and len(dev["fdb"]) > 30


def test_halo_reboot_at_dawn(world):
    """halo-wh-02 reboots every day at 04:00: Port 2 is down for two minutes."""
    t = at_local(2, 4, 1)
    with Emulator(at=t, apis=["horaco"], world=world) as emu:
        dev = device(emu.collect("horaco"))
    snap = world.at(t)
    ports = ifaces(dev)
    assert ports["Port 2"]["up"] is False and ports["Port 1"]["up"] is True
    fdb = {(e["mac"], e["port"]) for e in dev["fdb"]}
    assert (snap.node("halo-wh-02").mac, "Port 2") not in fdb
    assert (snap.node("halo-wh-01").mac, "Port 1") in fdb
