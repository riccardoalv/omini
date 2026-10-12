"""The MikroTik RouterOS REST emulator (br-gw01, sw-stor-01) against the real omini-plugin-mikrotik."""

from __future__ import annotations

import base64
import json
import re
import ssl
import urllib.error
import urllib.request

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("mikrotik") is None, reason="omini-plugin-mikrotik not found")

BR = "mikrotik-br-gw01"
STOR = "mikrotik-sw-stor-01"
TUESDAY_10_30 = at_local(1, 10, 30)
TUESDAY_10_45 = at_local(1, 10, 45)
MONDAY_10 = at_local(0, 10, 0)
SATURDAY_10 = at_local(5, 10, 0)
MONDAY_16_05 = at_local(0, 16, 5)  # wg_branch_down: the branch is unreachable from HQ
MONDAY_16_30 = at_local(0, 16, 30)
SUNDAY_2_05 = at_local(6, 2, 5)  # pve01 reboots (02:00, 8 min): its guests run on pve02


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def runs(world: World) -> dict[str, dict]:
    out: dict[str, dict] = {}
    with Emulator(at=TUESDAY_10_30, apis=[BR, STOR], world=world) as emu:
        out["br"] = emu.collect(BR)
        out["stor"] = emu.collect(STOR)
        out["test"] = emu.collect(BR, action="test")
        bad = dict(emu.config(BR), password="wrong-" + emu.config(BR)["password"])
        out["bad"] = run_plugin("mikrotik", bad)
        emu.set_time(TUESDAY_10_45)
        out["br2"] = emu.collect(BR)
        emu.set_time(MONDAY_10)
        out["stor_mon"] = emu.collect(STOR)
        emu.set_time(SATURDAY_10)
        out["stor_sat"] = emu.collect(STOR)
        emu.set_time(SUNDAY_2_05)
        out["stor_sun"] = emu.collect(STOR)
        emu.set_time(MONDAY_16_05)
        out["br_down"] = emu.collect(BR)
        emu.set_time(MONDAY_16_30)
        out["peers"] = {"rows": rest(emu, BR, "/rest/interface/wireguard/peers")}
        out["root"] = {"page": page(emu, BR, "/")}
    return out


def ctx() -> ssl.SSLContext:
    c = ssl.create_default_context()
    c.check_hostname = False
    c.verify_mode = ssl.CERT_NONE
    return c


def rest(emu: Emulator, api: str, path: str, user: str | None = None, password: str | None = None) -> object:
    cfg = emu.config(api)
    token = base64.b64encode(f"{user or cfg['username']}:{password or cfg['password']}".encode()).decode()
    req = urllib.request.Request(emu.urls[api] + path, headers={"Authorization": f"Basic {token}"})
    with urllib.request.urlopen(req, context=ctx(), timeout=10) as r:
        return json.loads(r.read())


def page(emu: Emulator, api: str, path: str) -> str:
    with urllib.request.urlopen(emu.urls[api] + path, context=ctx(), timeout=10) as r:
        return r.read().decode()


def device(out: dict) -> dict:
    assert "devices" in out, out
    assert len(out["devices"]) == 1
    return out["devices"][0]


def iface(d: dict, name: str) -> dict:
    return next(i for i in d["interfaces"] if i["name"] == name)


# --------------------------------------------------------------------------- br-gw01


def test_branch_router(runs: dict, world: World) -> None:
    d = device(runs["br"])
    node = world.nodes["br-gw01"]
    snap = world.at(TUESDAY_10_30)
    assert d["key"] == node.mac  # ether1's factory MAC
    assert d["name"] == "br-gw01"
    assert d["role"] == "router"
    assert d["vendor"] == "MikroTik"
    assert d["model"] == "RB5009UPr+S+"
    assert d["serial"] == "HG109WS8R5N"
    assert d["os_version"] == "RouterOS 7.16.2"
    assert d["uptime_s"] == snap.uptime("br-gw01")
    assert d["cpu_pct"] == round(snap.cpu("br-gw01"))
    assert abs(d["mem_pct"] - snap.mem("br-gw01")) < 0.2
    assert d["firmware"] == {"current": "7.16.2", "latest": "7.16.3", "update_available": True, "updates": 1}
    temps = {t["sensor"]: t for t in d["temperatures"]}
    assert temps["cpu-temperature"]["kind"] == "cpu"
    assert abs(temps["cpu-temperature"]["celsius"] - snap.temperature("br-gw01")) <= 0.5
    assert d["storage"][0]["total_bytes"] == 1024**3


def test_branch_ports(runs: dict, world: World) -> None:
    d = device(runs["br"])
    names = [i["name"] for i in d["interfaces"]]
    assert names[:9] == ["ether1", *(f"ether{p}" for p in range(2, 9)), "sfp-sfpplus1"]
    wan = iface(d, "ether1")
    assert wan["up"] and wan["wan"] and wan["speed_mbps"] == 1000 and wan["connector"] == "rj45"
    assert wan["ips"] == ["198.51.100.20/29"]
    assert wan["description"] == "WAN Vivo Fibra"
    assert not iface(d, "ether2")["up"]  # nothing plugged in the LAN ports
    assert iface(d, "ether2")["vlans"] == {"untagged": 30}
    up = iface(d, "sfp-sfpplus1")
    assert up["up"] and up["speed_mbps"] == 10000 and up["connector"] == "sfp" and up["description"] == "br-sw01"
    assert up["vlans"] == {"untagged": 10, "tagged": [30, 32, 40, 80]}
    assert up["transceiver"]["part"] == "SFP-10GSR-85" and up["transceiver"]["wavelength_nm"] == 850
    br = iface(d, "bridge")
    assert br["members"] == sorted([f"ether{p}" for p in range(2, 9)] + ["sfp-sfpplus1"])
    assert br["speed_mbps"] == 10000
    wg = iface(d, "wg-hq")
    assert wg["type"] == "tunnel" and wg["ips"] == ["10.255.0.2/30"]
    vlans = {v["id"]: v for v in d["vlans"]}
    assert sorted(vlans) == [10, 30, 32, 40, 80]
    assert vlans[32]["subnet"] == "10.20.32.0/24" and vlans[32]["interface"] == "vlan32"
    assert iface(d, "vlan40")["parent"] == "bridge" and iface(d, "vlan40")["vlan"] == 40
    assert d["gateways"] == [{"name": "Vivo Fibra", "interface": "ether1", "address": "198.51.100.17", "status": "up"}]
    # Counters move between two collections 15 minutes apart.
    d2 = device(runs["br2"])
    for name in ("ether1", "sfp-sfpplus1", "vlan32"):
        assert iface(d2, name)["rx_bytes"] > iface(d, name)["rx_bytes"]
        assert iface(d2, name)["tx_bytes"] > iface(d, name)["tx_bytes"]


def test_branch_clients(runs: dict, world: World) -> None:
    d = device(runs["br"])
    snap = world.at(TUESDAY_10_30)
    leases = snap.leases("branch")
    assert len(d["dhcp_leases"]) == len(leases) >= 30
    got = {lease["mac"]: lease for lease in d["dhcp_leases"]}
    prn = world.nodes["br-prn01"]
    assert got[prn.mac]["ip"] == prn.ip and got[prn.mac]["hostname"] == "br-prn01"
    assert {lease["ip"].rsplit(".", 2)[1] for lease in d["dhcp_leases"]} >= {"30", "32", "40"}
    arp = snap.arp("br-gw01")
    assert len(d["arp"]) == len(arp)
    ont = next(a for a in d["arp"] if a["ip"] == "198.51.100.17")
    assert ont == {"ip": "198.51.100.17", "mac": world.nodes["br-ont"].mac, "interface": "ether1"}
    sw = next(a for a in d["arp"] if a["ip"] == "10.20.10.10")
    assert sw["mac"] == world.nodes["br-sw01"].mac and sw["interface"] == "vlan10"
    # The bridge's MAC table: everything behind br-sw01 (ether1 is routed, not in the bridge).
    fdb = d["fdb"]
    assert fdb and {e["port"] for e in fdb} == {"sfp-sfpplus1"}
    assert len(fdb) == len([e for e in snap.fdb("br-gw01") if e[1] == "sfp-sfpplus1"])
    (nb,) = d["neighbors"]
    assert nb["local_port"] == "sfp-sfpplus1" and nb["protocol"] == "lldp"
    assert nb["remote_name"] == "br-sw01" and nb["remote_port"] == "Port 28" and nb["remote_ip"] == "10.20.10.10"
    assert nb["remote_mac"] == world.nodes["br-sw01"].mac


def test_test_action_and_credentials(runs: dict) -> None:
    assert runs["test"]["message"] == "Connected to br-gw01 (RouterOS 7.16.2, RB5009UPr+S+)"
    assert "rejected the username or password" in runs["bad"]["error"]
    assert "RouterOS router configuration page" in runs["root"]["page"]


def test_wireguard_outage(runs: dict) -> None:
    # 16:00–16:10 the tunnel to HQ is down: Omini (at HQ) cannot reach the branch router at all.
    assert "devices" not in runs["br_down"]
    assert runs["br_down"]["error"]
    # Back up at 16:30: the peer has handshaken again (the plugin does not read WireGuard; the API has it).
    (peer,) = runs["peers"]["rows"]
    assert peer["interface"] == "wg-hq" and peer["current-endpoint-address"] == "203.0.113.45"
    assert re.fullmatch(r"(1m)?\d{1,2}s", peer["last-handshake"])  # handshakes every 90 s


# --------------------------------------------------------------------------- sw-stor-01


def test_storage_switch(runs: dict, world: World) -> None:
    d = device(runs["stor"])
    node = world.nodes["sw-stor-01"]
    assert d["key"] == node.mac and d["name"] == "sw-stor-01"
    assert d["model"] == "CRS518-16XS-2XQ" and d["serial"] == "HFB09RQ1N7K"
    # RouterOS 7.16.3 is out, and RouterBOOT was never upgraded after 7.16.2: two pending updates.
    assert d["firmware"]["latest"] == "7.16.3" and d["firmware"]["updates"] == 2
    assert d["ips"] == ["10.10.10.6"]
    for p in range(1, 17):
        i = iface(d, f"sfp28-{p}")
        assert i["connector"] == "sfp"
    assert iface(d, "qsfp28-1-1")["connector"] == "qsfp" and not iface(d, "qsfp28-1-1")["up"]
    assert iface(d, "sfp28-1")["speed_mbps"] == 25000
    assert iface(d, "sfp28-1")["transceiver"]["part"] == "S28-PC01"  # DAC to pve01
    assert "rx_power_dbm" not in {k for k, v in iface(d, "sfp28-1")["transceiver"].items() if v is not None}
    assert iface(d, "sfp28-7")["speed_mbps"] == 10000 and iface(d, "sfp28-7")["description"] == "nas01"
    assert iface(d, "sfp28-15")["speed_mbps"] == 10000 and not iface(d, "sfp28-9")["up"]
    for k, pve in enumerate(("pve01", "pve02", "pve03")):
        bond = iface(d, f"bond{k + 1}")
        assert bond["type"] == "lag" and bond["up"] and bond["description"] == pve
        assert bond["members"] == [f"sfp28-{k * 2 + 1}", f"sfp28-{k * 2 + 2}"]
        assert 100 in bond["vlans"]["tagged"] and 102 in bond["vlans"]["tagged"]
    core = iface(d, "bond-core")
    assert core["members"] == ["sfp28-15", "sfp28-16"]
    assert core["vlans"] == {"untagged": 10, "tagged": [20, 30, 40, 50, 60, 70, 100, 110, 120]}
    assert "sfp28-1" not in iface(d, "bridge")["members"]  # bond slaves: their bond is the bridge port
    assert {"bond1", "bond-core", "sfp28-7"} <= set(iface(d, "bridge")["members"])
    vlans = {v["id"] for v in d["vlans"]}
    assert {10, 20, 100, 101, 102, 110, 120} <= vlans
    assert iface(d, "vlan10")["ips"] == ["10.10.10.6/24"]


def test_storage_mac_table_and_neighbors(runs: dict, world: World) -> None:
    d = device(runs["stor"])
    snap = world.at(TUESDAY_10_30)
    expected = snap.fdb("sw-stor-01")
    assert len(d["fdb"]) == len(expected) > 150
    dc01 = world.nodes["dc01"]  # a Windows VM on pve01, VLAN 20
    assert {"mac": dc01.mac, "port": "bond1", "vlan": 20} in d["fdb"]
    assert {e["port"] for e in d["fdb"]} >= {"bond1", "bond2", "bond3", "bond-core", "sfp28-7", "sfp28-8"}
    nbs = {n["local_port"]: n for n in d["neighbors"]}
    assert sorted(nbs) == sorted([f"sfp28-{p}" for p in range(1, 7)] + ["sfp28-15", "sfp28-16"])
    assert nbs["sfp28-3"]["remote_name"] == "pve02.acme.local" and nbs["sfp28-3"]["remote_port"] == "ens1f0np0"
    assert nbs["sfp28-16"]["remote_name"] == "core-sw01" and nbs["sfp28-16"]["remote_port"] == "Te2/1/6"
    assert "CAT9K_IOSXE" in nbs["sfp28-16"]["remote_platform"]
    # The switch only resolves its gateway (HQ's CARP address) and its poller.
    assert {a["ip"] for a in d["arp"]} == {"10.10.10.1", world.nodes["omini"].ip}


def test_failing_fiber(runs: dict, world: World) -> None:
    mon, sat = device(runs["stor_mon"]), device(runs["stor_sat"])
    rx_mon = iface(mon, "sfp28-16")["transceiver"]["rx_power_dbm"]
    rx_sat = iface(sat, "sfp28-16")["transceiver"]["rx_power_dbm"]
    assert rx_mon == pytest.approx(world.at(MONDAY_10).fiber_rx_dbm(), abs=0.01)
    assert rx_sat == pytest.approx(world.at(SATURDAY_10).fiber_rx_dbm(), abs=0.01)
    assert rx_mon > -6 and rx_sat < -11  # degraded by ~7.5 dB over the week
    healthy = iface(sat, "sfp28-15")["transceiver"]["rx_power_dbm"]
    assert -3.5 < healthy < -2  # its twin in bond-core is fine
    # CRC errors keep growing on the bad fiber only; the link is still up at 10G.
    assert iface(sat, "sfp28-16")["rx_errors"] > iface(mon, "sfp28-16")["rx_errors"] > 0
    assert iface(sat, "sfp28-15")["rx_errors"] == 0
    assert iface(sat, "sfp28-16")["up"] and iface(sat, "sfp28-16")["speed_mbps"] == 10000
    # RouterOS does not report the module's alarm thresholds: the plugin has no rx_power_low_dbm to compare with.
    assert iface(sat, "sfp28-16")["transceiver"].get("rx_power_low_dbm") is None


def test_proxmox_reboot_moves_guests(runs: dict, world: World) -> None:
    d = device(runs["stor_sun"])
    assert not iface(d, "bond1")["up"] and not iface(d, "sfp28-1")["up"]  # pve01 is rebooting
    assert iface(d, "bond2")["up"]
    dc01 = world.nodes["dc01"]
    assert {"mac": dc01.mac, "port": "bond2", "vlan": 20} in d["fdb"]  # live-migrated to pve02
    assert not any(n["local_port"] in ("sfp28-1", "sfp28-2") for n in d["neighbors"])


def test_rest_api_shapes(world: World) -> None:
    with Emulator(at=TUESDAY_10_30, apis=[STOR], world=world) as emu:
        one = rest(emu, STOR, "/rest/interface/bond-core")
        assert one["type"] == "bond" and one["running"] == "true"
        ethers = rest(emu, STOR, "/rest/interface?type=bond&.proplist=name,type")
        assert ethers == [{"name": n, "type": "bond"} for n in ("bond1", "bond2", "bond3", "bond-core")]
        bonds = rest(emu, STOR, "/rest/interface/bonding")
        assert bonds[0]["mode"] == "802.3ad" and bonds[0]["slaves"] == "sfp28-1,sfp28-2"
        with pytest.raises(urllib.error.HTTPError) as e:
            rest(emu, STOR, "/rest/interface/wifi")
        assert e.value.code == 400
        with pytest.raises(urllib.error.HTTPError) as e:
            rest(emu, STOR, "/rest/system/resource", password="nope")
        assert e.value.code == 401
        assert json.loads(e.value.read()) == {"error": 401, "message": "Unauthorized"}
    assert acme.APIS[STOR]["node"] == "sw-stor-01"


def test_disable_and_enable_a_port() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=World()) as emu:
        key = device(emu.collect(BR))["key"]
        change = {"device": key, "kind": "port.enabled", "port": "sfp-sfpplus1", "value": False}
        sfp = iface(device(emu.collect(BR, "apply", change=change)), "sfp-sfpplus1")
        assert (sfp["enabled"], sfp["up"]) == (False, False)  # br-sw01 behind it drops
        assert iface(device(emu.collect(BR)), "sfp-sfpplus1")["enabled"] is False
        assert rest(emu, BR, "/rest/interface/ethernet/sfp-sfpplus1")["disabled"] == "true"

        change["value"] = True
        sfp = iface(device(emu.collect(BR, "apply", change=change)), "sfp-sfpplus1")
        assert sfp["enabled"] is True and sfp["up"] is True
        ports = device(emu.collect(BR))["interfaces"]
        assert all(p["enabled"] for p in ports)

        # A port the router does not have: an error, nothing changed.
        out = emu.collect(BR, "apply", change=dict(change, port="ether42"))
        assert "ether42" in out["error"]


# --------------------------------------------------------------------------- VLAN and network changes


def network(d: dict, vlan: int) -> dict | None:
    return next((n for n in d.get("networks") or [] if n["vlan"] == vlan), None)


def test_branch_networks_are_read(runs: dict) -> None:
    d = device(runs["br"])
    assert [n["vlan"] for n in d["networks"]] == [10, 30, 32, 40, 80]  # never the WAN nor wg-hq
    users = network(d, 30)
    assert users == {
        "vlan": 30,
        "name": "USERS",
        "interface": "vlan30",
        "device": "vlan30",
        "parent": "bridge",
        "gateway": "10.20.30.1/24",
        "dhcp": {"enabled": True, "from": "10.20.30.100", "to": "10.20.30.200"},
        "access": "custom",  # defconf firewall, no rule of its own
        "mdns": False,
    }
    assert network(d, 10)["dhcp"] == {"enabled": False}
    # The storage switch has a default route too, but through its own management VLAN: no networks.
    assert "networks" not in device(runs["stor"])


def test_branch_network_set_and_delete() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=World()) as emu:
        key = device(emu.collect(BR))["key"]
        want = {
            "vlan": 50,
            "name": "CAMERAS",
            "gateway": "10.20.50.1/24",
            "dhcp": {"enabled": True, "from": "10.20.50.100", "to": "10.20.50.199"},
            "access": "internet",
            "mdns": True,
            "parent": "",
        }
        out = emu.collect(BR, "apply", change={"device": key, "kind": "network.set", "value": want})
        d = device(out)
        assert network(d, 50) == {
            **{k: v for k, v in want.items() if k != "parent"},
            "interface": "vlan50",
            "device": "vlan50",
            "parent": "bridge",
        }
        assert {"id": 50, "name": "CAMERAS", "interface": "vlan50", "subnet": "10.20.50.0/24"} in d["vlans"]
        # What RouterOS now has: the bridge tagged in VLAN 50, the DHCP server, Omini's rules before defconf's drops.
        entry = next(e for e in rest(emu, BR, "/rest/interface/bridge/vlan") if e["vlan-ids"] == "50")
        assert entry["tagged"] == "bridge"
        server = next(s for s in rest(emu, BR, "/rest/ip/dhcp-server") if s["interface"] == "vlan50")
        assert server["address-pool"] == "omini-vlan50"
        comments = [r["comment"] for r in rest(emu, BR, "/rest/ip/firewall/filter")]
        drop_lan = comments.index("defconf: drop all not coming from LAN")
        assert comments[drop_lan - 3 : drop_lan] == [
            "Omini: CAMERAS DNS",
            "Omini: CAMERAS DNS",
            "Omini: CAMERAS not to the router",
        ]
        assert rest(emu, BR, "/rest/ip/dns")["mdns-repeat-ifaces"] == "vlan50"

        # In place: a network already there changes access and DHCP range, its rules are replaced.
        users = dict(network(d, 30), access="local", dhcp={"enabled": True, "from": "10.20.30.50", "to": "10.20.30.99"})
        d = device(emu.collect(BR, "apply", change={"device": key, "kind": "network.set", "value": users}))
        assert network(d, 30)["access"] == "local" and network(d, 30)["dhcp"]["from"] == "10.20.30.50"

        out = emu.collect(BR, "apply", change={"device": key, "kind": "network.delete", "value": {"vlan": 50}})
        d = device(out)
        assert network(d, 50) is None
        assert not any(r.get("in-interface") == "vlan50" for r in rest(emu, BR, "/rest/ip/firewall/filter"))
        assert rest(emu, BR, "/rest/ip/dns")["mdns-repeat-ifaces"] == ""
        # A network that is not there cannot be removed.
        out = emu.collect(BR, "apply", change={"device": key, "kind": "network.delete", "value": {"vlan": 50}})
        assert "serves no network on VLAN 50" in out["error"]


def test_branch_port_vlans_and_reservations() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=World()) as emu:
        key = device(emu.collect(BR))["key"]
        trunk = {"device": key, "kind": "port.vlans", "port": "ether2", "value": {"untagged": 30, "tagged": [40]}}
        d = device(emu.collect(BR, "apply", change=trunk))
        assert iface(d, "ether2")["vlans"] == {"untagged": 30, "tagged": [40]}
        port = next(p for p in rest(emu, BR, "/rest/interface/bridge/port") if p["interface"] == "ether2")
        assert port["frame-types"] == "admit-all"
        access = dict(trunk, value={"untagged": 32})
        d = device(emu.collect(BR, "apply", change=access))
        assert iface(d, "ether2")["vlans"] == {"untagged": 32}
        assert iface(d, "sfp-sfpplus1")["vlans"] == {"untagged": 10, "tagged": [30, 32, 40, 80]}  # untouched

        res = {"mac": "02:00:00:aa:bb:01", "ip": "10.20.30.250", "name": "printer-2"}
        d = device(emu.collect(BR, "apply", change={"device": key, "kind": "reservation.set", "value": res}))
        assert res in d["dhcp_reservations"]
        lease = next(r for r in rest(emu, BR, "/rest/ip/dhcp-server/lease") if r["mac-address"] == "02:00:00:AA:BB:01")
        assert lease["server"] == "dhcp-vlan30" and lease["dynamic"] == "false"
        d = device(
            emu.collect(BR, "apply", change={"device": key, "kind": "reservation.delete", "value": {"mac": res["mac"]}})
        )
        assert res not in (d.get("dhcp_reservations") or [])


def test_storage_switch_vlans() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[STOR], world=World()) as emu:
        key = device(emu.collect(STOR))["key"]
        add = {"device": key, "kind": "vlan.set", "value": {"id": 130, "name": "BACKUP"}}
        d = device(emu.collect(STOR, "apply", change=add))
        assert {"id": 130, "name": "BACKUP"} in d["vlans"]
        trunk = {
            "device": key,
            "kind": "port.vlans",
            "port": "sfp28-8",
            "value": {"untagged": None, "tagged": [100, 130]},
        }
        d = device(emu.collect(STOR, "apply", change=trunk))
        assert iface(d, "sfp28-8")["vlans"] == {"tagged": [100, 130]}
        out = emu.collect(STOR, "apply", change={"device": key, "kind": "vlan.delete", "value": {"id": 10}})
        assert "has an interface on this device (vlan10)" in out["error"]  # its management VLAN
        back = dict(trunk, value={"untagged": 100, "tagged": None})
        emu.collect(STOR, "apply", change=back)
        d = device(emu.collect(STOR, "apply", change={"device": key, "kind": "vlan.delete", "value": {"id": 130}}))
        assert 130 not in {v["id"] for v in d["vlans"]}
        assert iface(d, "sfp28-8")["vlans"] == {"untagged": 100}


# --------------------------------------------------------------------------- DNS, Wake-on-LAN, pause, STP, LAG, backup


def test_branch_dns_names_and_wake() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=World()) as emu:
        d = device(emu.collect(BR))
        key = d["key"]
        assert d["dns_domain"] == "cps.acme.local"  # what the branch's DHCP hands out
        assert d["dns_records"] == [{"name": "br-gw01.cps.acme.local", "ip": "10.20.10.1"}]

        name = {"name": "printer.cps.acme.local", "ip": "10.20.30.250"}
        d = device(emu.collect(BR, "apply", change={"device": key, "kind": "dns.set", "value": name}))
        assert name in d["dns_records"]
        row = next(r for r in rest(emu, BR, "/rest/ip/dns/static") if r.get("name") == name["name"])
        assert row["comment"] == "Omini" and row["address"] == "10.20.30.250"
        moved = dict(name, ip="10.20.30.251")
        d = device(emu.collect(BR, "apply", change={"device": key, "kind": "dns.set", "value": moved}))
        assert moved in d["dns_records"] and name not in d["dns_records"]
        d = device(
            emu.collect(BR, "apply", change={"device": key, "kind": "dns.delete", "value": {"name": name["name"]}})
        )
        assert [r["name"] for r in d["dns_records"]] == ["br-gw01.cps.acme.local"]

        prn = World().nodes["br-prn01"]
        wake = {"mac": prn.mac, "ip": prn.ip}
        assert "devices" in emu.collect(BR, "apply", change={"device": key, "kind": "wol.send", "value": wake})
        out = emu.collect(BR, "apply", change={"device": key, "kind": "wol.send", "value": dict(wake, ip="172.31.9.9")})
        assert "no network that holds 172.31.9.9" in out["error"]
        # Without an address: the LAN, which the branch router does not have (only VLANs).
        out = emu.collect(BR, "apply", change={"device": key, "kind": "wol.send", "value": {"mac": prn.mac}})
        assert "no LAN" in out["error"]


def test_branch_pause_internet() -> None:
    world = World()
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=world) as emu:
        key = device(emu.collect(BR))["key"]
        conns = rest(emu, BR, "/rest/ip/firewall/connection")
        ip = conns[0]["src-address"].split(":")[0]
        lease = next(lease for lease in world.at(TUESDAY_10_30).leases("branch") if lease.ip == ip)
        want = {"mac": lease.mac, "ips": [ip], "name": "Notebook"}
        d = device(emu.collect(BR, "apply", change={"device": key, "kind": "pause.set", "value": want}))
        assert d["paused"] == [want]
        rules = rest(emu, BR, "/rest/ip/firewall/filter")
        forward = [r for r in rules if r["chain"] == "forward"]
        assert forward[0]["src-address-list"] == "omini_paused" and forward[0]["dst-address-list"] == "!omini-private"
        assert forward[0]["comment"] == "Omini: paused devices"
        lists = rest(emu, BR, "/rest/ip/firewall/address-list")
        assert {a["address"] for a in lists if a["list"] == "omini-private"} == {
            "10.0.0.0/8",
            "172.16.0.0/12",
            "192.168.0.0/16",
        }
        assert [(a["address"], a["comment"]) for a in lists if a["list"] == "omini_paused"] == [
            (ip, f"Omini {lease.mac} Notebook")
        ]
        # Its open connection is cut; the others stay.
        left = rest(emu, BR, "/rest/ip/firewall/connection")
        assert not any(c["src-address"].startswith(ip + ":") for c in left) and len(left) == len(conns) - 1

        d = device(
            emu.collect(BR, "apply", change={"device": key, "kind": "pause.delete", "value": {"mac": lease.mac}})
        )
        assert "paused" not in d
        assert not any(r.get("src-address-list") == "omini_paused" for r in rest(emu, BR, "/rest/ip/firewall/filter"))

    with Emulator(at=TUESDAY_10_30, apis=[STOR], world=world) as emu:
        key = device(emu.collect(STOR))["key"]
        out = emu.collect(STOR, "apply", change={"device": key, "kind": "pause.set", "value": want})
        assert "does not route" in out["error"]  # its default route is its management VLAN's


def test_stp_and_lags(runs: dict, world: World) -> None:
    stor = device(runs["stor"])
    core = world.nodes["core-sw01"]
    assert stor["stp_root"] == f"4096.{core.mac}"  # the core switch, behind bond-core
    assert iface(stor, "bond-core")["stp"] == {"state": "forwarding", "role": "root"}
    assert iface(stor, "sfp28-7")["stp"] == {"state": "forwarding", "role": "designated"}
    assert iface(stor, "sfp28-9")["stp"] == {"state": "disabled", "role": "disabled"}  # nothing plugged
    assert "stp" not in iface(stor, "sfp28-15")  # a bond member: its bond is the bridge port
    assert iface(stor, "sfp28-15")["lag"] == iface(stor, "sfp28-16")["lag"] == "bond-core"
    assert iface(stor, "sfp28-1")["lag"] == "bond1" and "lag" not in iface(stor, "sfp28-7")
    assert iface(stor, "bond-core")["type"] == "lag"
    br = device(runs["br"])
    bridge_mac = iface(br, "bridge")["mac"]
    assert br["stp_root"] == f"32768.{bridge_mac}"  # the branch router is its own root
    assert iface(br, "sfp-sfpplus1")["stp"] == {"state": "forwarding", "role": "designated"}


def test_backup_is_the_export() -> None:
    with Emulator(at=TUESDAY_10_30, apis=[BR], world=World()) as emu:
        key = device(emu.collect(BR))["key"]
        (b,) = emu.collect(BR, "backup")["backups"]
        assert b["device"] == key and b["format"] == "text"
        text = b["content"]
        assert text.startswith("# software id = ") and " by RouterOS " not in text
        assert "/ip dns static add address=10.20.10.1 name=br-gw01.cps.acme.local ttl=1d\n" in text
        assert "private-key" not in text  # no sensitive values
        # Later, nothing changed: the same backup (the export's time stamp is left out).
        emu.set_time(TUESDAY_10_45)
        assert emu.collect(BR, "backup")["backups"] == [b]
        name = {"name": "nas.cps.acme.local", "ip": "10.20.30.20"}
        emu.collect(BR, "apply", change={"device": key, "kind": "dns.set", "value": name})
        (after,) = emu.collect(BR, "backup")["backups"]
        assert after["content"] != text and "name=nas.cps.acme.local" in after["content"]
