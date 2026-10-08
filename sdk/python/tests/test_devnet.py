"""The simulated development network (testdata/devnet) answers like real plugins.

Each devnet plugin is run the way Omini runs plugins (request on stdin, answer
on stdout) and every device it returns must validate against the SDK models.
The network is a pure function of the time: same instant, same answer;
counters only grow (except across a reboot) and the scheduled scenarios
happen when they should.
"""

from __future__ import annotations

import importlib.util
import json
import subprocess
import sys
from pathlib import Path
from types import ModuleType

import pytest

from omini_sdk.models import Device, PluginManifest, PluginResponse

DEVNET = Path(__file__).resolve().parents[3] / "testdata" / "devnet"
AT = 1_791_460_800  # 2026-10-08T12:00:00Z: no scheduled event is on
PLUGIN_DIRS = sorted(p for p in DEVNET.iterdir() if (p / "plugin.yaml").is_file())


def _load() -> ModuleType:
    spec = importlib.util.spec_from_file_location("devnet", DEVNET / "_shared" / "devnet.py")
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    sys.modules["devnet"] = module  # dataclasses look their module up while it loads
    spec.loader.exec_module(module)
    return module


devnet = _load()


def manifest(path: Path) -> PluginManifest:
    """The top-level scalar keys of plugin.yaml (the devnet manifests use nothing else)."""
    values: dict[str, object] = {}
    for line in path.read_text().splitlines():
        if line and not line[0].isspace() and ":" in line:
            key, _, value = line.partition(":")
            value = value.strip().strip('"')
            if value and value != ">-":
                values[key] = int(value) if value.isdigit() else value
    return PluginManifest.model_validate(values)


def run(plugin_dir: Path, action: str = "collect") -> PluginResponse:
    request = {"protocol": 1, "action": action, "config": {}, "state_dir": "/nonexistent"}
    proc = subprocess.run(
        [sys.executable, str(plugin_dir / "main.py")],
        input=json.dumps(request),
        capture_output=True,
        text=True,
        cwd=plugin_dir,
        timeout=60,
        check=False,
    )
    assert proc.stdout, proc.stderr
    return PluginResponse.model_validate_json(proc.stdout)


def test_every_plugin_folder_is_a_devnet_plugin():
    assert sorted(p.name for p in PLUGIN_DIRS) == sorted(devnet.PLUGINS)
    for p in PLUGIN_DIRS:
        m = manifest(p / "plugin.yaml")
        assert m.id == p.name
        assert m.protocol == 1
        assert (p / m.entrypoint).is_file()
        assert (p / "requirements.txt").read_text().strip() == ""


@pytest.mark.parametrize("plugin_dir", PLUGIN_DIRS, ids=lambda p: p.name)
def test_plugin_answers_like_omini_expects(plugin_dir: Path):
    for action in ("collect", "test"):
        resp = run(plugin_dir, action)
        if resp.error:  # only the branch fails, while its tunnel is down
            assert plugin_dir.name == "devnet-branch"
            assert "IPsec" in resp.error
            continue
        if action == "collect":
            assert resp.devices
        else:
            assert resp.message and resp.message.startswith("Simulated")


SCHEDULE = [
    AT,
    AT + 240,  # the phone has roamed
    AT + 1500,  # the branch tunnel is down
    AT - AT % 14400 + 6000,  # the fiber is down
    AT + 7 * 86400 + 1234,
]


@pytest.mark.parametrize("at", SCHEDULE)
def test_devices_validate_and_are_deterministic(at: float):
    for plugin in devnet.PLUGINS:
        try:
            first = devnet.devices(plugin, at)
        except devnet.Unreachable:
            assert plugin == "devnet-branch"
            continue
        assert first == devnet.devices(plugin, at)
        keys = [Device.model_validate(d).key for d in first]
        assert len(keys) == len(set(keys)), plugin


def test_one_device_per_key_except_the_merged_router():
    owners: dict[str, list[str]] = {}
    for plugin in devnet.PLUGINS:
        for d in devnet.devices(plugin, AT):
            owners.setdefault(d["key"], []).append(plugin)
    shared = {k: v for k, v in owners.items() if len(v) > 1}
    # The lab router is a VM: the hypervisor and the router's own plugin both report it.
    assert shared == {devnet.gmac(301): ["devnet-pve", "devnet-lab"]}


def test_every_mac_is_unique():
    stations = devnet.hq_stations() + devnet.lab_stations() + devnet.infra_stations()
    macs = [s.mac for s in stations]
    assert len(macs) == len(set(macs))
    managed = {s.mac for s in stations if s.managed}
    clients = {s.mac for s in stations if not s.managed}
    assert not managed & clients


def interfaces(at: float) -> dict[tuple[str, str], tuple[int, dict]]:
    out = {}
    for plugin in devnet.PLUGINS:
        try:
            found = devnet.devices(plugin, at)
        except devnet.Unreachable:
            continue
        for d in found:
            for i in d.get("interfaces", []):
                out[(d["key"], i["name"])] = (d.get("uptime_s", 0), i)
    return out


def test_counters_grow():
    before, after = interfaces(AT), interfaces(AT + 30)
    grew = 0
    for k, (uptime, i) in before.items():
        if "rx_bytes" not in i or k not in after:
            continue
        uptime2, j = after[k]
        assert uptime2 == uptime + 30, k
        for c in ("rx_bytes", "tx_bytes"):
            assert j[c] >= i[c], (k, c)
            grew += j[c] > i[c]
    assert grew > 300


def test_counters_reset_when_a_device_reboots():
    # The marketing router reboots every 4 hours.
    boot = devnet.boot("mkt-router", AT, 4 * 3600) + 4 * 3600
    (old,) = devnet.devices("devnet-dnat", boot - 10)
    (new,) = devnet.devices("devnet-dnat", boot + 20)
    assert new["uptime_s"] == 20 < old["uptime_s"]
    assert new["interfaces"][0]["rx_bytes"] < old["interfaces"][0]["rx_bytes"]


def wifi_parent(at: float, mac: str) -> str | None:
    for ap in devnet.devices("devnet-wifi", at):
        if any(c["mac"] == mac for c in ap.get("wireless_clients", [])):
            return ap["name"]
    return None


def test_the_phone_roams_between_two_access_points():
    phone = devnet.mac("88:66:5a", "roamer")
    assert wifi_parent(AT, phone) == "ap-office1"
    assert wifi_parent(AT + 240, phone) == "ap-office2"
    assert wifi_parent(AT + 480, phone) == "ap-office1"


def test_a_laptop_and_a_vm_leave_and_come_back():
    laptop = devnet.mac("3c:fd:fe", "lt", 3)
    hour = AT - AT % 3600
    assert wifi_parent(hour + 300, laptop) == "ap-office1"
    assert wifi_parent(hour + 900, laptop) is None
    (fw,) = devnet.devices("devnet-fw", hour + 900)
    assert laptop not in {a["mac"] for a in fw["arp"]}
    assert laptop in {lease["mac"] for lease in fw["dhcp_leases"]}  # leases outlive devices

    names = lambda at: {d["name"] for d in devnet.devices("devnet-pve", at)}  # noqa: E731
    two_hours = AT - AT % 7200
    assert "win11-vdi" in names(two_hours + 2600)
    assert "win11-vdi" not in names(two_hours + 2800)


def gateway(at: float, name: str) -> dict:
    (fw,) = devnet.devices("devnet-fw", at)
    return next(g for g in fw["gateways"] if g["name"] == name)


def test_gateways_degrade_and_go_down():
    hour = AT - AT % 3600
    assert gateway(hour + 100, "FIBER_PPPOE")["status"] == "up"
    assert gateway(hour + 2500, "FIBER_PPPOE")["status"] == "degraded"
    assert gateway(hour + 1300, "LTE_DHCP")["status"] == "degraded"
    assert gateway(AT - AT % 14400 + 6000, "FIBER_PPPOE")["status"] == "down"


def test_the_branch_drops_with_its_tunnel():
    at = AT + 1500
    with pytest.raises(devnet.Unreachable, match="IPsec"):
        devnet.devices("devnet-branch", at)
    (fw,) = devnet.devices("devnet-fw", at)
    peer = next(p for p in fw["vpn_peers"] if p["name"] == "branch-office")
    assert peer["connected"] is False


def test_the_nas_fills_up_then_is_cleaned():
    def used(at: float) -> float:
        (nas,) = (d for d in devnet.devices("devnet-storage", at) if d["name"] == "nas-01")
        vol = nas["storage"][0]
        return vol["used_bytes"] / vol["total_bytes"]

    start = AT - (AT - devnet.BASE) % 21600
    assert 0.85 < used(start) < used(start + 10800) < used(start + 21599) < 0.98
    assert used(start + 21600) < 0.87


def test_the_network_is_big():
    (fw,) = devnet.devices("devnet-fw", AT)
    assert len(fw["vlans"]) == 8
    wifi = sum(len(ap.get("wireless_clients", [])) for ap in devnet.devices("devnet-wifi", AT))
    assert wifi >= 50
    assert len(fw["arp"]) >= 120
    assert sum(len(devnet.devices(p, AT)) for p in devnet.PLUGINS) >= 35
