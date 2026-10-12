"""The real UniFi plugin against the emulated self-hosted UniFi Network Application (unifi01)."""

from __future__ import annotations

import json
import ssl
import urllib.error
import urllib.request

import pytest

from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("unifi") is None, reason="omini-plugin-unifi not found")

TUE_1030 = at_local(1, 10, 30)
TUE_1035 = at_local(1, 10, 35)
SUN_0400 = at_local(6, 4, 0)
WED_0301 = at_local(2, 3, 1)
WED_0305 = at_local(2, 3, 5)

APS = ["ap-f1-01", "ap-f1-02", "ap-cafe-01", "ap-f2-01", "ap-f2-02", "ap-f3-01", "ap-f3-02", "ap-mr-01", "ap-patio-01"]
MODELS = {"sw-f3-01": "USL48PB", "ap-f1-01": "UAP6MP", "ap-cafe-01": "U6ENT", "ap-mr-01": "U7PRO", "ap-patio-01": "U6M"}


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def runs(world: World) -> dict[float, dict]:
    out = {}
    with Emulator(at=TUE_1030, apis=["unifi"], world=world) as emu:
        for t in (TUE_1030, TUE_1035, SUN_0400, WED_0301, WED_0305):
            emu.set_time(t)
            res = emu.collect("unifi")
            assert "error" not in res, res.get("error")
            out[t] = {d["name"]: d for d in res["devices"]}
        emu.set_time(TUE_1030)
        cfg = dict(emu.config("unifi"), password="not-the-password")
        out["wrong"] = run_plugin("unifi", cfg)
        ctx = ssl.create_default_context()
        ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE
        with urllib.request.urlopen(emu.urls["unifi"] + "/", context=ctx, timeout=10) as r:
            out["page"] = (r.url, r.read().decode())
    return out


def test_devices_and_models(runs: dict, world: World) -> None:
    devs = runs[TUE_1030]
    assert set(devs) == {"sw-f3-01", *APS}
    for name, d in devs.items():
        n = world.nodes[name]
        assert d["key"] == n.mac
        assert d["host"] == n.ip
        assert d["vendor"] == "Ubiquiti"
        assert d["role"] == ("switch" if name == "sw-f3-01" else "ap")
        assert d["serial"] == n.mac.replace(":", "").upper()
    for name, code in MODELS.items():
        assert devs[name]["model"] == code
    assert devs["ap-f3-02"]["model"] == "UAP6MP"


def test_switch_ports_and_lldp(runs: dict, world: World) -> None:
    sw = runs[TUE_1030]["sw-f3-01"]
    ifs = {i["name"]: i for i in sw["interfaces"]}
    assert len([i for i in ifs.values() if i["type"] == "ethernet"]) == 52
    assert ifs["Port 49"]["speed_mbps"] == 10000 and ifs["Port 49"]["connector"] == "sfp"
    assert ifs["Port 49"]["transceiver"]["part"] == "UACC-OM-MM-10G-D"
    assert ifs["Port 50"]["up"] and ifs["Port 50"]["speed_mbps"] == 10000
    assert ifs["Port 51"]["up"] is False and "transceiver" not in ifs["Port 51"]
    assert ifs["Port 37"]["speed_mbps"] == 100  # camera
    assert ifs["Port 45"]["speed_mbps"] == 1000 and ifs["Port 45"]["connector"] == "rj45"
    assert ifs["Port 48"]["up"] is False
    nbs = {n["local_port"]: n for n in sw["neighbors"]}
    core = world.nodes["core-sw01"].mac
    assert nbs["Port 49"]["remote_mac"] == core and nbs["Port 50"]["remote_mac"] == core
    assert nbs["Port 45"]["remote_mac"] == world.nodes["ap-f3-01"].mac
    assert nbs["Port 47"]["remote_name"] == "ap-mr-01"
    snap = world.at(TUE_1030)
    assert len(nbs) == len(snap.lldp("sw-f3-01"))
    assert sw["firmware"]["update_available"] and sw["firmware"]["latest"] == "7.2.123.16565"
    assert sw["temperatures"] and 0 < sw["cpu_pct"] < 100
    # Counters grow in five minutes.
    later = {i["name"]: i for i in runs[TUE_1035]["sw-f3-01"]["interfaces"]}
    assert later["Port 49"]["rx_bytes"] > ifs["Port 49"]["rx_bytes"]
    assert later["Port 45"]["tx_bytes"] > ifs["Port 45"]["tx_bytes"]


def test_stp_and_the_lag_to_the_core(runs: dict) -> None:
    sw = runs[TUE_1030]["sw-f3-01"]
    ifs = {i["name"]: i for i in sw["interfaces"]}
    assert ifs["Port 49"]["lag"] == ifs["Port 50"]["lag"] == "LAG 49-50"
    lag = ifs["LAG 49-50"]
    assert lag["type"] == "lag" and lag["up"] is True and lag["speed_mbps"] == 20000
    assert [n for n, i in ifs.items() if i.get("lag")] == ["Port 49", "Port 50"]
    assert ifs["Port 45"]["stp"] == {"state": "forwarding"}
    assert ifs["Port 48"]["stp"] == {"state": "disabled"}  # nothing plugged in
    assert "stp_root" not in sw  # UniFi does not say which bridge is the root
    ap = runs[TUE_1030]["ap-f1-01"]
    assert not any(i.get("stp") or i.get("lag") for i in ap["interfaces"])


def test_wired_clients(runs: dict, world: World) -> None:
    sw = runs[TUE_1030]["sw-f3-01"]
    fdb = {(e["mac"], e["port"]) for e in sw["fdb"]}
    assert (world.nodes["prn-f3-01"].mac, "Port 30") in fdb
    assert (world.nodes["cam-f3-01"].mac, "Port 37") in fdb
    assert not any(p in ("Port 45", "Port 46", "Port 47", "Port 49", "Port 50") for _, p in fdb)
    # A desktop behind the phone on Port 1, both on that port.
    assert sum(1 for _, p in fdb if p == "Port 1") == 2
    hosts = {h["ip"]: h for h in sw["hosts"]}
    assert world.nodes["prn-f3-01"].ip in hosts
    assert any(h.get("hostnames") == ["ACME-NB-0101"] for h in hosts.values())


def test_mesh_uplink_and_weak_signal(runs: dict, world: World) -> None:
    patio = runs[TUE_1030]["ap-patio-01"]
    up = [n for n in patio["neighbors"] if n["local_port"] == "Wi-Fi uplink"]
    assert up and up[0]["remote_mac"] == world.nodes["ap-cafe-01"].mac and up[0]["remote_port"] == "5 GHz"
    eth = next(i for i in patio["interfaces"] if i["name"] == "Port 1")
    assert eth["up"] is False and eth["rx_bytes"] == 0
    assert patio["wireless_clients"]
    assert all(c["signal_dbm"] <= -75 for c in patio["wireless_clients"])
    # The wired APs on the Aruba switches: their LLDP neighbor.
    f1 = runs[TUE_1030]["ap-f1-01"]
    assert f1["neighbors"][0]["remote_mac"] == world.nodes["sw-f1-01"].mac and f1["neighbors"][0]["remote_port"] == "45"


def test_clients_per_ap(runs: dict, world: World) -> None:
    for t in (TUE_1030, SUN_0400):
        snap = world.at(t)
        for ap in APS:
            got = {c["mac"] for c in runs[t][ap].get("wireless_clients") or []}
            assert got == {a.client.mac for a in snap.wifi_clients(ap)}, (t, ap)
    busy = sum(len(runs[TUE_1030][ap].get("wireless_clients") or []) for ap in APS)
    quiet = sum(len(runs[SUN_0400][ap].get("wireless_clients") or []) for ap in APS)
    assert busy > 100 and quiet < busy / 3
    every = [c for ap in APS for c in runs[TUE_1030][ap].get("wireless_clients") or []]
    assert {c["ssid"] for c in every} == {"Acme", "Acme-IoT", "Acme-Visitantes"}
    assert all(c["band"] == "2.4ghz" for c in every if c["ssid"] == "Acme-IoT")
    assert any(c["band"] == "6ghz" for c in runs[TUE_1030]["ap-mr-01"]["wireless_clients"])
    assert any(c["mac"][1] in "26ae" for c in every if c["ssid"] == "Acme-Visitantes")  # a random-MAC guest
    vaps = {i["description"] for i in runs[TUE_1030]["ap-cafe-01"]["interfaces"] if i["type"] == "wireless"}
    assert "Acme · 6 GHz" in vaps and "Acme-IoT · 2.4 GHz" in vaps and "Acme-IoT · 5 GHz" not in vaps


def test_meeting_room_roaming(runs: dict, world: World) -> None:
    room = next(m for m in world.meetings if m[0] in ("paulista", "ibirapuera") and m[1] == 1 and m[2] <= 10.5 < m[3])
    snap = world.at(TUE_1030)
    roamers = [
        c
        for c, wa in world.d.wifi.items()
        if world.nodes[c].owner in room[4] and wa.home != "ap-mr-01" and snap.ap_of(c) == "ap-mr-01"
    ]
    assert roamers
    on_mr = {c["mac"] for c in runs[TUE_1030]["ap-mr-01"]["wireless_clients"]}
    for c in roamers:
        mac = world.nodes[c].mac
        assert mac in on_mr
        home = world.d.wifi[c].home
        assert mac not in {x["mac"] for x in runs[TUE_1030][home].get("wireless_clients") or []}


def test_wednesday_firmware_reboots(runs: dict, world: World) -> None:
    before = runs[TUE_1030]
    assert before["ap-f1-01"]["firmware"]["update_available"] is True
    assert before["ap-f1-01"]["firmware"]["latest"] == "7.0.95.16123"
    at301 = runs[WED_0301]
    assert "ap-f1-01" not in at301  # rebooting: disconnected (state 0), skipped by the plugin
    assert set(at301) == {"sw-f3-01", *APS} - {"ap-f1-01"}
    gone = {a.client.mac for a in world.at(at_local(2, 2, 59)).wifi_clients("ap-f1-01")}
    seen = {c["mac"] for d in at301.values() for c in d.get("wireless_clients") or []}
    assert gone and not gone & seen
    at305 = runs[WED_0305]
    # The cafeteria AP reboots, and the patio AP meshed to it loses the controller too.
    assert "ap-cafe-01" not in at305 and "ap-patio-01" not in at305
    assert at305["ap-f1-01"]["os_version"] == "7.0.95.16123"
    assert at305["ap-f1-01"]["firmware"]["update_available"] is False
    assert at305["ap-f1-01"]["uptime_s"] < 600
    assert at305["ap-f2-01"]["firmware"]["update_available"] is True


def test_web_page(runs: dict) -> None:
    url, body = runs["page"]
    assert "/manage/account/login" in url
    assert "<title>UniFi Network</title>" in body


def test_wrong_password(runs: dict) -> None:
    res = runs["wrong"]
    assert "devices" not in res
    assert "rejected the username or password" in res["error"]


def test_session_required() -> None:
    with Emulator(at=TUE_1030, apis=["unifi"]) as emu:
        ctx = ssl.create_default_context()
        ctx.check_hostname, ctx.verify_mode = False, ssl.CERT_NONE
        with pytest.raises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(emu.urls["unifi"] + "/api/s/default/stat/device", context=ctx, timeout=10)
        assert e.value.code == 401
        assert b"api.err.LoginRequired" in e.value.read()


def test_disable_and_enable_a_port() -> None:
    world = World()
    with Emulator(at=TUE_1030, apis=["unifi"], world=world) as emu:

        def switch(res: dict) -> dict:
            assert "error" not in res, res.get("error")
            return next(d for d in res["devices"] if d["name"] == "sw-f3-01")

        def ports(res: dict) -> dict[str, dict]:
            return {i["name"]: i for i in switch(res)["interfaces"]}

        key = switch(emu.collect("unifi"))["key"]
        change = {"device": key, "kind": "port.enabled", "port": "Port 30", "value": False}
        after = emu.collect("unifi", "apply", change=change)
        p30 = ports(after)["Port 30"]
        assert (p30["enabled"], p30["up"]) == (False, False)  # the printer there drops
        assert ports(emu.collect("unifi"))["Port 30"]["enabled"] is False
        assert ports(after)["Port 29"]["enabled"] is True

        change["value"] = True
        p30 = ports(emu.collect("unifi", "apply", change=change))["Port 30"]
        assert p30["enabled"] is True and p30["up"] is True
        assert all(p["enabled"] for p in ports(emu.collect("unifi")).values() if p["type"] != "lag")

        # A View Only account reads the map, but cannot change a port.
        emu.secrets["unifi"]["read_only"] = True
        res = emu.collect("unifi", "apply", change=dict(change, value=False))
        assert "Full Management or Site Admin" in res["error"]
        assert ports(emu.collect("unifi"))["Port 30"]["enabled"] is True


def test_a_vlans_wifi() -> None:
    with Emulator(at=TUE_1030, apis=["unifi"], world=World()) as emu:

        def wifi(res: dict) -> dict[str, dict]:
            assert "error" not in res, res.get("error")
            ap = next(d for d in res["devices"] if d.get("role") == "ap")
            return {w["ssid"]: w for w in ap.get("wifi_networks") or []}

        before = wifi(emu.collect("unifi"))
        assert before and all(w.get("vlan") for w in before.values() if w["ssid"] != "ACME-CORP")
        change = {
            "device": "x",
            "kind": "wifi.set",
            "value": {"ssid": "ACME-LAB", "vlan": 77, "password": "correct horse"},
        }
        got = wifi(emu.collect("unifi", "apply", change=change))
        assert got["ACME-LAB"]["vlan"] == 77
        change = {"device": "x", "kind": "wifi.delete", "value": {"ssid": "ACME-LAB"}}
        assert "ACME-LAB" not in wifi(emu.collect("unifi", "apply", change=change))


def test_a_switch_port_s_vlans_and_the_site_s_vlans() -> None:
    with Emulator(at=TUE_1030, apis=["unifi"], world=World()) as emu:

        def switch(res: dict) -> dict:
            assert "error" not in res, res.get("error")
            return next(d for d in res["devices"] if d["name"] == "sw-f3-01")

        def vlans(res: dict) -> dict[str, dict | None]:
            return {i["name"]: i.get("vlans") for i in switch(res)["interfaces"]}

        def apply(kind: str, value: dict, port: str | None = None) -> dict:
            change = {"device": key, "kind": kind, "value": value}
            if port:
                change["port"] = port
            return emu.collect("unifi", "apply", change=change)

        before = emu.collect("unifi")
        key = switch(before)["key"]
        site = {v["id"]: v["name"] for v in switch(before)["vlans"]}
        assert site[1] == "Default" and site[60] == "CAMERAS"
        got = vlans(before)
        assert got["Port 1"] == {"untagged": 30, "tagged": [40]}  # desk phone and PC
        assert got["Port 45"] == {"untagged": 1, "tagged": [32, 50, 80]}  # an AP
        assert got["Port 49"] is None  # the LAG to the core

        # An access port, then a trunk with no native VLAN; the others stay.
        after = vlans(apply("port.vlans", {"untagged": 60, "tagged": []}, "Port 48"))
        assert after["Port 48"] == {"untagged": 60, "tagged": []}
        assert after["Port 47"] == got["Port 47"]
        after = vlans(apply("port.vlans", {"untagged": None, "tagged": [30, 40]}, "Port 48"))
        assert after["Port 48"] == {"tagged": [30, 40]}  # no untagged VLAN
        assert vlans(emu.collect("unifi"))["Port 48"] == {"tagged": [30, 40]}

        # A new VLAN, renamed, carried by the port, then deleted.
        res = apply("vlan.set", {"id": 90, "name": "LAB"})
        assert {v["id"]: v["name"] for v in switch(res)["vlans"]}[90] == "LAB"
        res = apply("vlan.set", {"id": 90, "name": "LAB-2"})
        assert {v["id"]: v["name"] for v in switch(res)["vlans"]}[90] == "LAB-2"
        after = vlans(apply("port.vlans", {"untagged": 30, "tagged": [90]}, "Port 48"))
        assert after["Port 48"] == {"untagged": 30, "tagged": [90]}
        apply("port.vlans", {"untagged": 30, "tagged": []}, "Port 48")
        res = apply("vlan.delete", {"id": 90})
        assert 90 not in {v["id"] for v in switch(res)["vlans"]}

        # Refused: a VLAN with no network, the LAN, a network a Wi-Fi uses.
        assert "no network for VLAN 99" in apply("port.vlans", {"untagged": 99}, "Port 48")["error"]
        assert "site's LAN" in apply("vlan.delete", {"id": 1})["error"]
        assert "the Wi-Fi" in apply("vlan.delete", {"id": 50})["error"]
        assert "aggregate" in apply("port.vlans", {"untagged": 30}, "Port 49")["error"]


def test_backup() -> None:
    with Emulator(at=TUE_1030, apis=["unifi"], world=World()) as emu:
        first = emu.collect("unifi", "backup")
        assert "error" not in first, first.get("error")
        keys = {d["key"] for d in emu.collect("unifi")["devices"]}
        backups = {b["device"]: b for b in first["backups"]}
        assert set(backups) == keys and {b["format"] for b in backups.values()} == {"json"}
        content = {k: json.loads(b["content"]) for k, b in backups.items()}
        # The site's settings go with the switch (the gateway is not UniFi).
        holders = [k for k, c in content.items() if "site" in c]
        assert holders == [emu.world.nodes["sw-f3-01"].mac]
        site = content[holders[0]]["site"]
        assert {"networks", "wifi_networks", "port_profiles", "settings"} <= set(site)
        assert site["settings"]["ntp"]["ntp_server_1"] == "a.ntp.br"
        text = "".join(b["content"] for b in first["backups"])
        for secret in ("x_passphrase", "x_ssh_password", "Ub1-ssh-Acme!", "acme-ro-2026", "community"):
            assert secret not in text
        assert "uptime" not in text and "rx_bytes" not in text

        # Live status changes, the configuration does not: the same backup.
        emu.set_time(TUE_1035)
        assert emu.collect("unifi", "backup")["backups"] == first["backups"]

        # A port disabled through Omini changes the switch's backup only.
        change = {"device": holders[0], "kind": "port.enabled", "port": "Port 30", "value": False}
        assert "error" not in emu.collect("unifi", "apply", change=change)
        after = {b["device"]: b["content"] for b in emu.collect("unifi", "backup")["backups"]}
        assert after[holders[0]] != backups[holders[0]]["content"]
        assert all(after[k] == b["content"] for k, b in backups.items() if k != holders[0])
