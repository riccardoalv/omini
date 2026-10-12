"""The AdGuard Home emulator (the add-on on homeassistant) against the real omini-plugin-adguard."""

from __future__ import annotations

import base64
import json
import urllib.error
import urllib.request
from typing import Any

import pytest

from emulator.testing import Emulator, plugin_dir
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("adguard") is None, reason="omini-plugin-adguard not found")

TUESDAY_10 = at_local(1, 10, 0)
WEDNESDAY_10 = at_local(2, 10, 0)
SUNDAY_REBOOT = at_local(6, 2, 25)  # pve02 reboots at 02:20 for 8 minutes; homeassistant stops with it


@pytest.fixture(scope="module")
def world() -> World:
    return World()


def device(out: dict[str, Any]) -> dict[str, Any]:
    assert "error" not in out, out.get("error")
    [d] = out["devices"]
    return d


def records(d: dict[str, Any]) -> list[tuple[str, str]]:
    return [(r["name"], r["ip"]) for r in d.get("dns_records") or []]


def test_collect_and_test(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["adguard"], world=world) as emu:
        d = device(emu.collect("adguard"))
        test = emu.collect("adguard", action="test")
    ha, nvr = world.nodes["homeassistant"], world.nodes["nvr01"]
    assert (d["key"], d["role"], d["vendor"], d["model"], d["os_version"]) == (
        "127.0.0.1",
        "server",
        "AdGuard",
        "AdGuard Home",
        "0.107.57",
    )
    assert d["ips"] == [ha.ip]
    # The IoT names; the CNAME and the wildcard rewrites are not local names.
    assert records(d) == [("homeassistant.acme.local", ha.ip), ("nvr01.acme.local", nvr.ip)]
    # Its DHCP server is off: HQ's firewalls hand out the addresses.
    assert "dhcp_leases" not in d and "dns_domain" not in d
    assert [(s["name"], s["running"]) for s in d["services"]] == [("DNS", True), ("DHCP", False)]
    assert test == {"message": "Connected to AdGuard Home 0.107.57, 2 local names", "_stderr": test["_stderr"]}


def test_dns_set_change_and_delete(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["adguard"], world=world) as emu:
        ch = {
            "device": "127.0.0.1",
            "kind": "dns.set",
            "value": {"name": "printer-iot.acme.local", "ip": "10.10.50.30"},
        }
        d = device(emu.collect("adguard", "apply", change=ch))
        assert ("printer-iot.acme.local", "10.10.50.30") in records(d)
        # The same name with another address is changed, not duplicated.
        ch["value"] = {"name": "printer-iot.acme.local", "ip": "10.10.50.31"}
        d = device(emu.collect("adguard", "apply", change=ch))
        assert [r for r in records(d) if r[0] == "printer-iot.acme.local"] == [
            ("printer-iot.acme.local", "10.10.50.31")
        ]
        # And it stays: a new run reads it back.
        assert ("printer-iot.acme.local", "10.10.50.31") in records(device(emu.collect("adguard")))
        d = device(
            emu.collect(
                "adguard",
                "apply",
                change={"device": "127.0.0.1", "kind": "dns.delete", "value": {"name": "printer-iot.acme.local"}},
            )
        )
        assert all(r[0] != "printer-iot.acme.local" for r in records(d))


def test_backup_is_deterministic(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["adguard"], world=world) as emu:
        first = emu.collect("adguard", "backup")
        # A day later the filter lists updated and other IoT devices are online: the same configuration.
        emu.set_time(WEDNESDAY_10)
        second = emu.collect("adguard", "backup")
        emu.collect(
            "adguard",
            "apply",
            change={"device": "127.0.0.1", "kind": "dns.set", "value": {"name": "x.acme.local", "ip": "10.10.50.99"}},
        )
        third = emu.collect("adguard", "backup")
    assert "error" not in first, first.get("error")
    [b] = first["backups"]
    assert (b["device"], b["format"]) == ("127.0.0.1", "json")
    data = json.loads(b["content"])
    assert {"status", "rewrites", "dns", "filtering", "clients", "dhcp", "access"} <= set(data)
    assert emu.secrets["adguard"]["password"] not in b["content"]
    assert second["backups"] == first["backups"]
    assert third["backups"] != first["backups"]


def test_wrong_password_and_reboot(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["adguard"], world=world) as emu:
        url = emu.urls["adguard"]
        bad = base64.b64encode(b"omini:wrong").decode()
        req = urllib.request.Request(url + "/control/status", headers={"Authorization": f"Basic {bad}"})
        with pytest.raises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(req, timeout=5)
        assert e.value.code == 401 and "Basic" in e.value.headers["WWW-Authenticate"]
        emu.set_time(SUNDAY_REBOOT)
        out = emu.collect("adguard")
    assert "error" in out
