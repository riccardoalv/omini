"""The simulated world: deterministic, consistent and following the scenario of DESIGN.md."""

from __future__ import annotations

import gzip
from collections import Counter
from pathlib import Path

import pytest

from emulator import acme
from emulator.world import WEEK, World, at_local

OUI_DB = Path(__file__).resolve().parents[3] / "internal" / "oui" / "oui.tsv.gz"
TUE_1030 = at_local(1, 10, 30)
SUN_0400 = at_local(6, 4, 0)


@pytest.fixture(scope="module")
def world() -> World:
    return World()


def test_deterministic(world: World):
    other = World()
    a, b = world.at(TUE_1030), other.at(TUE_1030)
    assert [n.ip for n in world.nodes.values()] == [n.ip for n in other.nodes.values()]
    assert a.fdb("core-sw01") == b.fdb("core-sw01")
    assert a.port("fw-hq-01", "lagg0") == b.port("fw-hq-01", "lagg0")
    # The week repeats: the same instant a week later looks the same (except counters, which keep growing).
    c = world.at(TUE_1030 + WEEK)
    assert c.fdb("core-sw01") == a.fdb("core-sw01")
    assert c.port("core-sw01", "Po21").rx_bytes > a.port("core-sw01", "Po21").rx_bytes  # no reboot in between


def test_addresses_unique_except_the_designed_duplicate(world: World):
    ips = Counter(n.ip for n in world.nodes.values() if n.ip)
    dups = {ip for ip, k in ips.items() if k > 1}
    assert dups == {"10.10.30.150"}
    assert world.duplicate_ip[0] == "prn-f3-01"
    macs = Counter(n.mac for n in world.nodes.values() if n.mac)
    assert all(k == 1 for k in macs.values())


def test_ouis_are_real(world: World):
    """Every non-random MAC belongs to a maker in Omini's IEEE registry."""
    with gzip.open(OUI_DB, "rt") as f:
        known = {line.split("\t", 1)[0] for line in f}
    for n in world.nodes.values():
        if not n.mac or n.extra.get("private_mac"):
            continue
        assert n.mac.replace(":", "")[:6] in known, (n.name, n.mac)


def test_presence_follows_the_office_hours(world: World):
    tue, sun = world.at(TUE_1030), world.at(SUN_0400)
    laptops = [n for n in world.nodes.values() if n.kind == "laptop" and n.site == "hq"]
    assert sum(tue.present(n.name) for n in laptops) > 0.8 * len(laptops)
    assert sum(sun.present(n.name) for n in laptops) == 0
    assert sun.present("Galaxy-A15-Portaria") and not tue.present("Galaxy-A15-Portaria")
    assert all(tue.present(n) for n in ("core-sw01", "dc01", "cam-f1-01", "nvr01"))


def test_counters_grow_and_reset_on_reboot(world: World):
    a, b = world.at(TUE_1030), world.at(TUE_1030 + 300)
    pa, pb = a.port("core-sw01", "Po21"), b.port("core-sw01", "Po21")
    assert pb.rx_bytes > pa.rx_bytes and pb.tx_bytes > pa.tx_bytes
    # The rate between two readings is the designed rate (whatever the clock's speed).
    fast = world.at(TUE_1030, speed=60), world.at(TUE_1030 + 60 * 60, speed=60)
    rate = (fast[1].port("core-sw01", "Po21").rx_bytes - fast[0].port("core-sw01", "Po21").rx_bytes) / 60
    assert rate == pytest.approx(world.rx[("core-sw01", "Po21")][fast[0].b : fast[0].b + 12].mean(), rel=0.05)
    before, after = world.at(at_local(6, 1, 50)), world.at(at_local(6, 2, 30))
    assert after.uptime("pve01") < 30 * 60 < before.uptime("pve01")
    assert after.port("pve01", "bond1").rx_bytes < before.port("pve01", "bond1").rx_bytes


def test_traffic_is_consistent_across_a_cable(world: World):
    s = world.at(TUE_1030)
    for a, pa, b, pb in (
        ("core-sw01", "Po21", "sw-f1-01", "Trk1"),
        ("sw-stor-01", "bond1", "pve01", "bond1"),
        ("core-sw01", "Po11", "fw-hq-01", "lagg0"),
    ):
        x, y = s.port(a, pa), s.port(b, pb)
        assert x.rx_rate == pytest.approx(y.tx_rate, rel=1e-6)
        assert x.tx_rate == pytest.approx(y.rx_rate, rel=1e-6)


def test_scenario(world: World):
    tue = world.at(at_local(1, 14, 10))
    assert tue.active("fiber_down") and not tue.gateway("fiber")["up"]
    assert tue.port("fw-hq-01", "igc1").tx_rate > 1_000_000  # the 5G uplink carries the traffic
    assert not tue.port_up("fw-hq-01", "pppoe0")
    night = world.at(at_local(2, 2, 30))
    assert night.port("sw-stor-01", "sfp28-8").tx_rate > 1.1e9  # bkp-nas01's 10G port is saturated
    sat = world.at(at_local(5, 22, 31))
    assert sat.master() == "fw-hq-02" and sat.carp_state("fw-hq-01") == "INIT"
    sun = world.at(at_local(6, 2, 22))
    assert sun.host_of("dc02") == "pve03" and not sun.up("pve02")
    assert world.at(at_local(2, 4, 1)).up("halo-wh-03") is False  # meshed through the rebooting halo-wh-02
    assert world.at(at_local(0, 9, 0)).fiber_rx_dbm() > -6 > world.at(at_local(6, 20, 0)).fiber_rx_dbm()
    fw = world.at(at_local(2, 10, 0)).firmware("fw-hq-01")
    assert fw["pending"] and fw["needs_reboot"]
    assert not world.at(at_local(5, 23, 0)).firmware("fw-hq-01")["pending"]
    assert not world.at(at_local(1, 16, 5)).reachable("mikrotik-br-gw01")
    assert world.at(at_local(1, 16, 15)).reachable("mikrotik-br-gw01")


def test_roaming(world: World):
    room, day, start, _end, who = world.meetings[0]
    phone = next(c for c in world.d.wifi if world.nodes[c].owner in who and world.nodes[c].kind == "mobile")
    s = world.at(at_local(day, start, 10))
    if s.present(phone):
        from emulator.world import ROOMS

        assert s.ap_of(phone) == ROOMS[room]


def test_tables(world: World):
    s = world.at(TUE_1030)
    fdb = s.fdb("sw-f1-01")
    phone = next(n for n in world.nodes.values() if n.kind == "phone" and n.site == "hq")
    assert any(m == phone.mac and v == 40 for m, _, v in fdb) or not s.present(phone.name)
    arp = {ip for ip, _, _ in s.arp("fw-hq-01")}
    assert "10.10.20.10" in arp and "10.10.10.21" in arp
    leases = s.leases("hq")
    assert len(leases) > 150 and all(lease.end > s.t for lease in leases)
    assert {n.local_port for n in s.lldp("pve01")} == {"eno1", "eno2", "ens1f0np0", "ens1f1np1"}
    assert len(s.wifi_clients("ap-patio-01")) >= 1
    assert all(a.signal < -70 for a in s.wifi_clients("ap-patio-01"))


def test_apis_cover_their_nodes():
    for name, spec in acme.APIS.items():
        assert spec["node"] in World().nodes or pytest.fail(name)
