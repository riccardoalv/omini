"""The branch office's OC200 (Omada Open API) against the real omini-plugin-omada."""

from __future__ import annotations

import json
import ssl
import tempfile
import urllib.request
from pathlib import Path
from typing import Any

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("omada") is None, reason="omini-plugin-omada not found")

TUE = at_local(1, 10, 30)  # Tuesday 10:30, office hours
SUN = at_local(6, 4, 0)  # Sunday 04:00, empty office
TUNNEL_DOWN = at_local(1, 16, 5)  # the WireGuard tunnel to HQ drops daily at 16:00 for 12 minutes


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def run(world: World) -> dict[str, Any]:
    """One emulator, three instants, one state folder (the token is kept between collections)."""
    with tempfile.TemporaryDirectory() as state, Emulator(at=TUE, apis=["omada"], world=world) as emu:
        out: dict[str, Any] = {}
        out["tue"] = emu.collect("omada", state_dir=Path(state))
        out["test"] = emu.collect("omada", action="test", state_dir=Path(state))
        out["direct"] = direct_checks(emu)
        bad = {**emu.config("omada"), "client_secret": "0" * 32}
        out["bad"] = run_plugin("omada", bad, "test")
        emu.set_time(TUNNEL_DOWN)
        out["down"] = emu.collect("omada", state_dir=Path(state))
        emu.set_time(SUN)  # days later: the cached token has expired on the controller's clock
        out["sun"] = emu.collect("omada", state_dir=Path(state))
    return out


def token_requests(result: dict[str, Any]) -> int:
    return result["_stderr"].count("POST https://127.0.0.1") if "_stderr" in result else 0


def test_token_reused_then_renewed(run: dict[str, Any]) -> None:
    assert token_requests(run["tue"]) == 1
    assert token_requests(run["test"]) == 0  # the token kept in the state folder is still valid
    assert token_requests(run["sun"]) == 1  # expired (-44112): the plugin asks for a new one


def devices(result: dict[str, Any]) -> dict[str, dict[str, Any]]:
    assert "error" not in result, result.get("error")
    return {d["name"]: d for d in result["devices"]}


def iface(dev: dict[str, Any], name: str) -> dict[str, Any]:
    return next(i for i in dev["interfaces"] if i["name"] == name)


# -- the API itself, without the plugin --------------------------------------


def call(url: str, method: str = "GET", body: Any = None, token: str | None = None) -> tuple[int, Any]:
    ctx = ssl.create_default_context()
    ctx.check_hostname = False
    ctx.verify_mode = ssl.CERT_NONE
    req = urllib.request.Request(url, method=method, data=json.dumps(body).encode() if body is not None else None)
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", f"AccessToken={token}")
    try:
        with urllib.request.urlopen(req, context=ctx, timeout=10) as r:
            raw, status = r.read(), r.status
    except urllib.error.HTTPError as e:
        raw, status = e.read(), e.code
    try:
        return status, json.loads(raw)
    except ValueError:
        return status, raw.decode()


def direct_checks(emu: Emulator) -> dict[str, Any]:
    base = emu.urls["omada"]
    sec = emu.secrets["omada"]
    out: dict[str, Any] = {}
    out["root"] = call(base + "/")[1]
    out["info"] = call(base + "/api/info")[1]
    oid = out["info"]["result"]["omadacId"]
    tok_url = base + "/openapi/authorize/token?grant_type=client_credentials"
    out["bad_secret"] = call(tok_url, "POST", {"omadacId": oid, "client_id": sec["client_id"], "client_secret": "x"})
    out["bad_grant"] = call(base + "/openapi/authorize/token?grant_type=password", "POST", {})
    good = call(
        tok_url, "POST", {"omadacId": oid, "client_id": sec["client_id"], "client_secret": sec["client_secret"]}
    )
    out["token"] = good[1]
    token = good[1]["result"]["accessToken"]
    api = f"{base}/openapi/v1/{oid}"
    out["no_token"] = call(api + "/sites?page=1&pageSize=10")[1]
    out["bad_token"] = call(api + "/sites?page=1&pageSize=10", token="AT-nope")[1]
    out["no_page"] = call(api + "/sites", token=token)[1]
    sites = call(api + "/sites?page=1&pageSize=10", token=token)[1]
    out["sites"] = sites
    sid = sites["result"]["data"][0]["siteId"]
    out["devices_p1"] = call(api + f"/sites/{sid}/devices?page=1&pageSize=2", token=token)[1]
    out["devices_p2"] = call(api + f"/sites/{sid}/devices?page=2&pageSize=2", token=token)[1]
    sw = emu.world.nodes["br-sw01"].mac.replace(":", "-").upper()
    out["stat"] = call(api + f"/sites/{sid}/stat/switches/{sw}", token=token)[1]
    out["unknown"] = call(api + f"/sites/{sid}/nothing-here", token=token)
    return out


def test_api_shapes(run: dict[str, Any]) -> None:
    d = run["direct"]
    assert "<title>Omada Controller</title>" in d["root"]
    assert d["info"]["errorCode"] == 0 and d["info"]["result"]["controllerVer"].startswith("5.15.")
    assert d["bad_secret"][1]["errorCode"] == -44106
    assert d["bad_grant"][1]["errorCode"] == -44111
    tok = d["token"]["result"]
    assert tok["expiresIn"] == 7200 and tok["tokenType"] == "bearer" and tok["accessToken"].startswith("AT-")
    assert d["no_token"]["errorCode"] == -44113 and d["bad_token"]["errorCode"] == -44113
    assert d["no_page"]["errorCode"] == -1001  # page and pageSize are required
    site = d["sites"]["result"]["data"][0]
    assert site["name"] == acme.APIS["omada"]["site"] == "Filial Campinas"
    p1, p2 = d["devices_p1"]["result"], d["devices_p2"]["result"]
    assert p1["totalRows"] == p2["totalRows"] == 3  # br-sw01, br-ap01, br-ap02 (no gateway, not the OC200)
    assert len(p1["data"]) == 2 and len(p2["data"]) == 1
    assert {r["name"] for r in p1["data"] + p2["data"]} == {"br-sw01", "br-ap01", "br-ap02"}
    ports = {p["port"]: p for p in d["stat"]["result"]["ports"]}
    assert len(ports) == 28
    assert ports[23]["portStatus"]["poe"] is True and ports[23]["portStatus"]["poePower"] > 10  # EAP670
    assert ports[2]["portStatus"]["poe"] is True  # a Yealink phone
    assert ports[20]["portStatus"]["poe"] is False  # the printer has its own power supply
    assert ports[28]["type"] == 3 and ports[28]["profileName"] == "Trunk 10 30,32,40,80" and ports[28]["pvid"] == 10
    assert ports[20]["profileName"] == "USERS" and ports[2]["profileName"] == "VOICE"
    assert d["stat"]["result"]["uplink"]["port"] == 28
    assert d["unknown"][0] == 404 and d["unknown"][1]["errorCode"] == -1600


# -- the plugin ----------------------------------------------------------------


def test_devices_and_models(run: dict[str, Any], world: World) -> None:
    devs = devices(run["tue"])
    assert set(devs) == {"br-sw01", "br-ap01", "br-ap02"}  # no Omada gateway; the OC200 is not adopted
    sw, ap1 = devs["br-sw01"], devs["br-ap01"]
    assert sw["key"] == world.nodes["br-sw01"].mac and sw["role"] == "switch" and sw["vendor"] == "TP-Link"
    assert sw["model"] == "SG3428MP" and sw["host"] == "10.20.10.10" and sw["serial"] == "2235210001234"
    assert sw["os_version"].startswith("2.0.12 ")
    assert ap1["model"] == devs["br-ap02"]["model"] == "EAP670" and ap1["role"] == "ap"
    assert devs["br-ap02"]["host"] == "10.20.10.12"
    snap = world.at(TUE)
    assert sw["uptime_s"] == snap.uptime("br-sw01")
    assert sw["cpu_pct"] == round(snap.cpu("br-sw01")) and ap1["mem_pct"] == round(snap.mem("br-ap01"))
    assert "Connected to Omada Controller 5.15." in run["test"]["message"]
    assert '"Filial Campinas": 1 switch, 2 access points' in run["test"]["message"]


def test_switch_ports(run: dict[str, Any], world: World) -> None:
    sw = devices(run["tue"])["br-sw01"]
    assert len(sw["interfaces"]) == 28
    assert iface(sw, "Port 1")["speed_mbps"] == 100  # the OC200's Fast Ethernet port
    up28 = iface(sw, "Port 28")
    assert up28["connector"] == "sfp" and up28["up"] and up28["speed_mbps"] == 10000
    assert up28["transceiver"]["rx_power_dbm"] < 0 and 30 < up28["transceiver"]["temperature_c"] < 60
    assert iface(sw, "Port 25")["connector"] == "sfp" and iface(sw, "Port 25")["up"] is False
    assert iface(sw, "Port 23")["speed_mbps"] == 1000 and iface(sw, "Port 23")["connector"] == "rj45"
    assert iface(sw, "Port 22")["up"] is False
    st = world.at(TUE).port("br-sw01", "Port 28")
    assert up28["rx_bytes"] == st.rx_bytes and up28["tx_bytes"] == st.tx_bytes
    sun = devices(run["sun"])["br-sw01"]
    assert iface(sun, "Port 28")["rx_bytes"] > up28["rx_bytes"]
    assert iface(sun, "Port 23")["tx_bytes"] > iface(sw, "Port 23")["tx_bytes"]


def test_lldp_and_uplinks(run: dict[str, Any]) -> None:
    devs = devices(run["tue"])
    nb = {n["local_port"]: n for n in devs["br-sw01"]["neighbors"]}
    assert nb["Port 28"]["remote_name"] == "br-gw01" and nb["Port 28"]["remote_port"] == "sfp-sfpplus1"
    assert nb["Port 23"]["remote_name"] == "br-ap01" and nb["Port 24"]["remote_name"] == "br-ap02"
    assert sum(1 for n in nb.values() if n["remote_port"] == "LAN") == 8  # Yealink phones
    for ap, port in (("br-ap01", "Port 23"), ("br-ap02", "Port 24")):
        up = devs[ap]["neighbors"][0]
        assert up["local_port"] == "ETH1" and up["remote_name"] == "br-sw01" and up["remote_port"] == port
        assert up["remote_mac"] == devs["br-sw01"]["key"]
        assert iface(devs[ap], "ETH1")["speed_mbps"] == 1000


def test_clients_office_hours_vs_night(run: dict[str, Any], world: World) -> None:
    tue, sun = devices(run["tue"]), devices(run["sun"])
    for ap in ("br-ap01", "br-ap02"):
        want = world.at(TUE).wifi_clients(ap)
        got = tue[ap].get("wireless_clients") or []
        assert len(got) == len(want) > 5
        assert {c["mac"] for c in got} == {a.client.mac for a in want}
        assert {c["ssid"] for c in got} <= {"Acme", "Acme-Visitantes"}
        assert all(c["signal_dbm"] < -30 and c["band"] in ("2.4ghz", "5ghz") for c in got)
        assert not sun[ap].get("wireless_clients")  # Sunday 04:00: nobody in the office
    # Wired clients: 8 phones, printer, meeting-room TV, the controller and the router behind Port 28.
    assert len(tue["br-sw01"]["fdb"]) == 12 and len(sun["br-sw01"]["fdb"]) == 12
    assert len(tue["br-ap01"]["hosts"]) + len(tue["br-ap02"]["hosts"]) > 20


def test_wired_client_on_its_port(run: dict[str, Any], world: World) -> None:
    sw = devices(run["tue"])["br-sw01"]
    prn = world.nodes["br-prn01"]
    fdb = {e["mac"]: e for e in sw["fdb"]}
    assert fdb[prn.mac] == {"mac": prn.mac, "port": "Port 20", "vlan": 30}
    host = next(h for h in sw["hosts"] if h["mac"] == prn.mac)
    assert host["ip"] == prn.ip and "br-prn01" in host["hostnames"] and host["vendor"] == "Brother"
    oc = world.nodes["br-oc200"]
    assert fdb[oc.mac]["port"] == "Port 1" and fdb[oc.mac]["vlan"] == 10


def test_firmware_upgrade(run: dict[str, Any]) -> None:
    devs = devices(run["tue"])
    fw = devs["br-ap02"]["firmware"]
    assert fw["update_available"] is True and fw["latest"].startswith("1.1.6") and fw["current"].startswith("1.1.3")
    assert devs["br-ap01"]["firmware"]["update_available"] is False
    assert devs["br-sw01"]["firmware"]["update_available"] is False


def test_tunnel_down_and_wrong_secret(run: dict[str, Any]) -> None:
    assert "error" in run["down"] and not run["down"].get("devices")
    assert "rejected the client ID or secret" in run["bad"]["error"]


def test_disable_and_enable_a_port() -> None:
    with Emulator(at=TUE, apis=["omada"], world=World()) as emu:
        sw = devices(emu.collect("omada"))["br-sw01"]
        assert all(i["enabled"] for i in sw["interfaces"])
        change = {"device": sw["key"], "kind": "port.enabled", "port": "Port 20", "value": False}
        p20 = iface(devices(emu.collect("omada", "apply", change=change))["br-sw01"], "Port 20")
        assert (p20["enabled"], p20["up"]) == (False, False)  # the printer there drops
        assert iface(devices(emu.collect("omada"))["br-sw01"], "Port 20")["enabled"] is False

        change["value"] = True
        p20 = iface(devices(emu.collect("omada", "apply", change=change))["br-sw01"], "Port 20")
        assert p20["enabled"] is True and p20["up"] is True
        assert all(i["enabled"] for i in devices(emu.collect("omada"))["br-sw01"]["interfaces"])

        # Only switch ports: an access point's port is refused, nothing changes.
        ap = devices(emu.collect("omada"))["br-ap01"]
        bad = {"device": ap["key"], "kind": "port.enabled", "port": "ETH1", "value": False}
        assert "only switch ports" in emu.collect("omada", "apply", change=bad)["error"]


def test_a_vlans_wifi() -> None:
    with Emulator(at=TUE, apis=["omada"], world=World()) as emu:

        def wifi(res: dict) -> dict[str, dict]:
            assert "error" not in res, res.get("error")
            ap = next(d for d in res["devices"] if d.get("role") == "ap")
            return {w["ssid"]: w for w in ap.get("wifi_networks") or []}

        before = wifi(emu.collect("omada"))
        assert before and all(w.get("vlan") for w in before.values())
        change = {
            "device": "x",
            "kind": "wifi.set",
            "value": {"ssid": "Filial-Lab", "vlan": 77, "password": "correct horse"},
        }
        got = wifi(emu.collect("omada", "apply", change=change))
        assert got["Filial-Lab"]["vlan"] == 77
        change = {"device": "x", "kind": "wifi.set", "value": {"ssid": "Filial-Lab", "enabled": False}}
        assert wifi(emu.collect("omada", "apply", change=change))["Filial-Lab"]["enabled"] is False
        change = {"device": "x", "kind": "wifi.delete", "value": {"ssid": "Filial-Lab"}}
        assert "Filial-Lab" not in wifi(emu.collect("omada", "apply", change=change))


def test_vlans_of_the_site_and_its_switch_ports() -> None:
    with Emulator(at=TUE, apis=["omada"], world=World()) as emu:

        def switch(res: dict) -> dict:
            return devices(res)["br-sw01"]

        def vlans(dev: dict) -> dict[int, str]:
            return {v["id"]: v.get("name") for v in dev.get("vlans") or []}

        def port(dev: dict, name: str) -> tuple[int | None, list[int]]:
            v = iface(dev, name).get("vlans") or {}
            return v.get("untagged"), v.get("tagged") or []

        sw = switch(emu.collect("omada"))
        assert vlans(sw) == {1: "Default", 10: "MGMT", 30: "USERS", 32: "WIFI-CORP", 40: "VOICE", 80: "GUEST"}
        assert port(sw, "Port 20") == (30, [])
        assert port(sw, "Port 23") == (10, [32, 80])
        assert port(sw, "Port 28") == (10, [30, 32, 40, 80])
        assert port(sw, "Port 25") == (1, [])

        def apply(kind: str, value: dict, port: str | None = None) -> dict:
            change = {"device": sw["key"], "kind": kind, "value": value}
            if port:
                change["port"] = port
            return emu.collect("omada", "apply", change=change)

        # A new VLAN-only network, then renamed.
        assert vlans(switch(apply("vlan.set", {"id": 50, "name": "LAB"})))[50] == "LAB"
        assert vlans(switch(apply("vlan.set", {"id": 50, "name": "IOT"})))[50] == "IOT"

        # The printer's port to the new VLAN (the controller's own profile for it).
        assert port(switch(apply("port.vlans", {"untagged": 50, "tagged": None}, "Port 20")), "Port 20") == (50, [])
        # A trunk the site has no profile for: one made by Omini.
        sw2 = switch(apply("port.vlans", {"untagged": 30, "tagged": [40, 50]}, "Port 21"))
        assert port(sw2, "Port 21") == (30, [40, 50])
        assert port(sw2, "Port 22") == (30, []) and port(sw2, "Port 23") == (10, [32, 80])  # untouched

        # A VLAN still the native network of a port cannot go; once free, it can.
        assert "native network" in apply("vlan.delete", {"id": 50})["error"]
        assert port(switch(apply("port.vlans", {"untagged": 30}, "Port 20")), "Port 20") == (30, [])
        assert port(switch(apply("port.vlans", {"untagged": 30}, "Port 21")), "Port 21") == (30, [])
        assert 50 not in vlans(switch(apply("vlan.delete", {"id": 50})))

        # The Default LAN is not a VLAN-only network: refused.
        assert "gateway network" in apply("vlan.delete", {"id": 1})["error"]
        assert "always has a native" in apply("port.vlans", {"untagged": None, "tagged": [30]}, "Port 20")["error"]
