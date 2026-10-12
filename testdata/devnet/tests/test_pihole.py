"""The Pi-hole v6 emulator (Docker on docker01) against the real omini-plugin-pihole."""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from typing import Any

import pytest

from emulator.testing import Emulator, plugin_dir
from emulator.vendors import pihole
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("pihole") is None, reason="omini-plugin-pihole not found")

TUESDAY_10 = at_local(1, 10, 0)
WEDNESDAY_10 = at_local(2, 10, 0)


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
    with Emulator(at=TUESDAY_10, apis=["pihole"], world=world) as emu:
        d = device(emu.collect("pihole"))
        test = emu.collect("pihole", action="test")
    docker01 = world.nodes["docker01"]
    n = world.nodes
    # Host networking: Pi-hole sees docker01's interface, so it is docker01 (by MAC).
    assert (d["key"], d["name"], d["role"], d["vendor"], d["model"], d["os_version"]) == (
        docker01.mac,
        "docker01",
        "server",
        "Pi-hole",
        "Pi-hole",
        "6.1.2",
    )
    assert d["macs"] == [docker01.mac] and d["ips"] == [docker01.ip]
    assert records(d) == sorted(
        [
            ("docker01.acme.local", docker01.ip),
            ("grafana.acme.local", docker01.ip),
            ("mon01.acme.local", n["mon01"].ip),
            ("netbox.acme.local", n["netbox"].ip),
            ("rproxy-int.acme.local", n["rproxy-int"].ip),
            ("zabbix.acme.local", n["mon01"].ip),
        ]
    )
    assert d["dns_domain"] == "acme.local"
    assert "dhcp_leases" not in d
    assert test["message"] == "Connected to Pi-hole 6.1.2, 6 local names"


def test_dns_set_change_and_delete(world: World) -> None:
    docker01 = world.nodes["docker01"]
    with Emulator(at=TUESDAY_10, apis=["pihole"], world=world) as emu:

        def apply(kind: str, value: dict[str, Any]) -> dict[str, Any]:
            return device(emu.collect("pihole", "apply", change={"device": docker01.mac, "kind": kind, "value": value}))

        d = apply("dns.set", {"name": "erp.acme.local", "ip": "10.10.20.30"})
        assert ("erp.acme.local", "10.10.20.30") in records(d)
        d = apply("dns.set", {"name": "erp.acme.local", "ip": "10.10.20.31"})
        assert [r for r in records(d) if r[0] == "erp.acme.local"] == [("erp.acme.local", "10.10.20.31")]
        # grafana shares a line with docker01: moving it keeps docker01's name where it was.
        d = apply("dns.set", {"name": "grafana.acme.local", "ip": "10.10.20.70"})
        assert ("grafana.acme.local", "10.10.20.70") in records(d)
        assert ("docker01.acme.local", docker01.ip) in records(d)
        assert ("grafana.acme.local", docker01.ip) not in records(d)
        d = apply("dns.delete", {"name": "erp.acme.local"})
        assert all(r[0] != "erp.acme.local" for r in records(d))
        # Every run signed out: a new one still gets a session (max 16).
        for _ in range(pihole.MAX_SESSIONS + 1):
            device(emu.collect("pihole"))


def test_backup_is_deterministic_and_has_no_secrets(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pihole"], world=world) as emu:
        first = emu.collect("pihole", "backup")
        emu.set_time(WEDNESDAY_10)  # the block list updated (new count and date)
        second = emu.collect("pihole", "backup")
    assert "error" not in first, first.get("error")
    [b] = first["backups"]
    assert (b["device"], b["format"]) == (world.nodes["docker01"].mac, "json")
    data = json.loads(b["content"])
    assert set(data) == {"config", "groups", "lists", "domains", "clients"}
    assert "BALLOON" not in b["content"] and "pwhash" not in b["content"]
    assert data["config"]["dns"]["domain"] == {"name": "acme.local", "local": True}
    assert second["backups"] == first["backups"]


def test_wrong_password(world: World) -> None:
    with Emulator(at=TUESDAY_10, apis=["pihole"], world=world) as emu:
        req = urllib.request.Request(
            emu.urls["pihole"] + "/api/auth",
            data=json.dumps({"password": "wrong"}).encode(),
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with pytest.raises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(req, timeout=5)
        assert e.value.code == 401
        assert json.loads(e.value.read())["session"]["valid"] is False
        req = urllib.request.Request(emu.urls["pihole"] + "/api/config")
        with pytest.raises(urllib.error.HTTPError) as e:
            urllib.request.urlopen(req, timeout=5)
        assert e.value.code == 401
