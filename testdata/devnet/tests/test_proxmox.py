"""The real Proxmox VE plugin against the emulated acme-pve cluster (pve01, pve02, pve03)."""

from __future__ import annotations

import json
import ssl
import urllib.error
import urllib.request

import pytest

from emulator import acme
from emulator.testing import Emulator, plugin_dir, run_plugin
from emulator.vendors import proxmox
from emulator.world import World, at_local

pytestmark = pytest.mark.skipif(plugin_dir("proxmox") is None, reason="omini-plugin-proxmox not found")

WED = at_local(2, 10, 0)  # Wednesday 10:00, an ordinary working day
NODES = ("pve01", "pve02", "pve03")
STOPPED = {"tpl-debian12", "old-erp", "ct-test"}


@pytest.fixture(scope="module")
def world() -> World:
    return World()


@pytest.fixture(scope="module")
def emu(world):
    with Emulator(at=WED, apis=["proxmox"], world=world) as e:
        yield e


def collect(emu: Emulator, t: float) -> dict[str, dict]:
    emu.set_time(t)
    out = emu.collect("proxmox")
    assert "error" not in out, out
    return {d["name"]: d for d in out["devices"]}


@pytest.fixture(scope="module")
def wed(emu):
    return collect(emu, WED)


def guests_under(devices: dict[str, dict], node: str) -> set[str]:
    return {n for n, d in devices.items() if any(nb.get("remote_name") == node for nb in d.get("neighbors") or [])}


def iface(dev: dict, name: str) -> dict:
    return next(i for i in dev["interfaces"] if i["name"] == name)


def test_nodes(wed, world):
    for k, name in enumerate(NODES):
        d = wed[name]
        n = world.nodes[name]
        assert d["key"] == n.ports["vmbr0"].mac == n.mac  # the hwaddress of vmbr0 (eno1's MAC)
        assert d["host"] == f"10.10.10.{11 + k}"
        assert d["role"] == "server" and d["vendor"] == "Proxmox"
        assert d["model"] == "Intel(R) Xeon(R) Silver 4314 CPU @ 2.40GHz"
        assert d["os_version"] == "Proxmox VE 9.0.10"
        assert d["cpu_count"] == 64 and d["mem_total_bytes"] == 512 * 1024**3
        assert 0 < d["cpu_pct"] < 100 and 50 < d["mem_pct"] < 80 and len(d["load_avg"]) == 3
        assert d["ips"] == [f"10.10.{net}.{11 + k}" for net in (10, 103, 100, 101, 102, 110)]
        [gw] = d["gateways"]
        assert (gw["interface"], gw["address"]) == ("vmbr0", "10.10.10.1")
        # PVEAuditor cannot read the update list (Sys.Modify): only the version.
        assert d["firmware"] == {"current": "9.0.10"}
        snap = world.at(WED)
        assert abs(d["uptime_s"] - snap.uptime(name)) < 5


def test_node_interfaces(wed):
    for name in NODES:
        ports = {i["name"]: i for i in wed[name]["interfaces"]}
        want = {
            "eno1",
            "eno2",
            "ens1f0np0",
            "ens1f1np1",
            "bond0",
            "bond1",
            "vmbr0",
            "vmbr1",
            "vmbr0.103",
            "vmbr1.100",
            "vmbr1.101",
            "vmbr1.102",
            "vmbr1.110",
        }
        assert set(ports) == want | ({"vmbr2"} if name == "pve02" else set())
        assert ports["bond0"]["type"] == "lag" and ports["bond0"]["members"] == ["eno1", "eno2"]
        assert ports["bond1"]["members"] == ["ens1f0np0", "ens1f1np1"]
        assert ports["vmbr0"]["type"] == "bridge" and ports["vmbr0"]["members"] == ["bond0"]
        assert ports["vmbr0"]["description"] == "Management"
        assert ports["vmbr1"]["members"] == ["bond1"]
        assert ports["eno1"]["type"] == "ethernet" and ports["eno1"]["up"] is True
        vlan = ports["vmbr1.100"]
        assert (vlan["type"], vlan["parent"], vlan["vlan"]) == ("vlan", "vmbr1", 100)
        assert ports["vmbr0.103"]["parent"] == "vmbr0" and ports["vmbr0.103"]["vlan"] == 103
    assert "members" not in iface(wed["pve02"], "vmbr2")  # the lab bridge has no ports


def test_node_storage(wed, world):
    disks = {s["mount"]: s for s in wed["pve01"]["storage"]}
    assert set(disks) == {"/", "local", "local-lvm", "ceph-vm", "cephfs", "nas-iso", "pbs01"}
    assert disks["ceph-vm"]["fs_type"] == "rbd" and disks["nas-iso"]["fs_type"] == "nfs"
    assert disks["pbs01"]["fs_type"] == "pbs" and disks["local-lvm"]["fs_type"] == "lvmthin"
    nas = disks["nas-iso"]
    assert nas["used_bytes"] / nas["total_bytes"] * 100 == pytest.approx(world.at(WED).nas_used_pct(), abs=0.01)
    assert all(0 < s["used_bytes"] < s["total_bytes"] for s in disks.values())


def test_every_running_guest_under_its_node(wed, world):
    snap = world.at(WED)
    for node in NODES:
        want = {g.name for g in snap.guests(node)}
        assert guests_under(wed, node) == want
    assert [len(guests_under(wed, n)) for n in NODES] == [7, 10, 4]
    assert len(wed) == 3 + 21
    assert not STOPPED & set(wed)  # stopped guests and templates are left out
    for node in NODES:
        for g in guests_under(wed, node):
            [nb] = wed[g]["neighbors"]
            assert nb["protocol"] == "other" and nb["remote_platform"] == "Proxmox VE"
            assert nb["remote_mac"] == wed[node]["key"] and nb["remote_ip"] == wed[node]["host"]
            assert wed[g]["role"] == "server" and wed[g]["key"] == world.nodes[g].mac


def test_guest_details(wed, world):
    dc01 = wed["dc01"]
    assert dc01["model"] == "QEMU/KVM virtual machine" and dc01["os_version"] == "Windows 11"  # ostype win11
    assert dc01["ips"] == ["10.10.20.10"] and dc01["host"] == "10.10.20.10"  # from the guest agent
    assert dc01["neighbors"][0]["remote_port"] == "vmbr1"
    assert dc01["cpu_count"] == 4 and dc01["mem_total_bytes"] == 8 * 1024**3 and "mem_pct" not in dc01
    assert iface(dc01, "net0")["ips"] == ["10.10.20.10/24"]

    docker = wed["docker01"]
    assert docker["macs"] == [world.nodes["docker01"].mac]
    assert docker["ips"] == ["10.10.20.50"]  # docker0 / br-* are not the VM's NICs

    k3s = wed["lab-k3s-01"]
    assert k3s["model"] == "LXC container" and k3s["os_version"] == "Ubuntu"
    assert k3s["neighbors"][0]["remote_name"] == "pve02" and k3s["neighbors"][0]["remote_port"] == "vmbr2"
    assert k3s["ips"] == ["192.168.200.11"] and k3s["key"] == world.nodes["lab-k3s-01"].mac

    omini = wed["omini"]
    assert omini["model"] == "LXC container" and omini["os_version"] == "Debian"
    assert omini["ips"] == ["10.10.10.250"] and omini["neighbors"][0]["remote_port"] == "vmbr0"
    assert omini["neighbors"][0]["remote_name"] == "pve01"
    assert 0 < omini["mem_pct"] < 100 and omini["storage"][0]["total_bytes"] == 16 * 1024**3

    fw = wed["lab-fw01"]  # pfSense: no guest agent, so no addresses
    assert "ips" not in fw and fw["neighbors"][0]["remote_port"] == "vmbr1"
    assert fw["macs"] == sorted(p.mac for p in world.nodes["lab-fw01"].ports.values())
    assert "ips" not in wed["wsus01"]  # agent enabled, its service not running
    assert wed["unifi01"]["neighbors"][0]["remote_port"] == "vmbr0"


def test_guest_counters_from_the_taps(wed, world):
    snap = world.at(WED)
    st = snap.port("pve01", "tap100i0")
    net0 = iface(wed["dc01"], "net0")
    assert (net0["rx_bytes"], net0["tx_bytes"]) == (st.tx_bytes, st.rx_bytes)  # the tap's tx is what dc01 received
    fw = {i["name"]: i for i in wed["lab-fw01"]["interfaces"]}
    assert fw["net1"]["rx_bytes"] == snap.port("pve02", "tap130i1").tx_bytes
    k3s = iface(wed["lab-k3s-01"], "net0")
    assert k3s["tx_bytes"] == snap.port("pve02", "veth202i0").rx_bytes


def test_counters_grow(emu, wed):
    later = collect(emu, WED + 600)
    for name in ("dc01", "erp-app01", "omini", "lab-k3s-02"):
        a, b = iface(wed[name], "net0"), iface(later[name], "net0")
        assert b["rx_bytes"] > a["rx_bytes"] and b["tx_bytes"] > a["tx_bytes"], name
    assert later["pve01"]["uptime_s"] == wed["pve01"]["uptime_s"] + 600


def test_live_migration_while_pve02_reboots(emu):
    # Sunday 02:17: pve02 reboots at 02:20; its HQ guests left for pve03 at 02:15.
    before = collect(emu, at_local(6, 2, 17))
    moved = {"dc02", "erp-db01", "file01", "mon01"}
    assert guests_under(before, "pve03") == moved | {"pbs01", "print01", "wsus01", "rproxy-int"}
    assert guests_under(before, "pve02") == {
        "homeassistant",
        "lab-fw01",
        "lab-k3s-01",
        "lab-k3s-02",
        "lab-k3s-03",
        "lab-kali",
    }
    assert len(guests_under(before, "pve01")) == 7
    # A migrated guest is on a new tap: its counters start again.
    assert iface(before["dc02"], "net0")["rx_bytes"] < 10 * 1024**3
    # 02:22: pve02 is down (offline in the cluster, skipped); the guests that stayed on it are gone.
    during = collect(emu, at_local(6, 2, 22))
    assert "pve02" not in during and {"pve01", "pve03"} <= set(during)
    assert guests_under(during, "pve03") == guests_under(before, "pve03")
    assert not {"homeassistant", "lab-fw01", "lab-k3s-01", "lab-kali"} & set(during)
    assert during["dc02"]["neighbors"][0]["remote_mac"] == during["pve03"]["key"]


def test_api_unreachable_while_pve01_reboots(emu):
    emu.set_time(at_local(6, 2, 4))
    out = emu.collect("proxmox")
    assert "error" in out and "devices" not in out


def test_pending_updates_with_sys_modify(emu, monkeypatch):
    monkeypatch.setattr(proxmox.Proxmox, "PRIVILEGES", proxmox.AUDITOR | {"Sys.Modify"})
    devices = collect(emu, at_local(3, 9, 0))  # Thursday: Wednesday's apt update found them
    fw = devices["pve02"]["firmware"]
    assert fw == {"current": "9.0.10", "latest": "9.0.11", "update_available": True, "updates": 5}


def test_wrong_token(emu):
    emu.set_time(WED)
    cfg = dict(emu.config("proxmox"), token_secret="00000000-0000-4000-8000-000000000000")
    out = run_plugin("proxmox", cfg)
    assert "rejected the API token" in out["error"]
    cfg = dict(emu.config("proxmox"), token_id="root@pam!omini")
    assert "rejected the API token" in run_plugin("proxmox", cfg)["error"]


def test_raw_api(emu):
    emu.set_time(WED)
    ctx = ssl._create_unverified_context()
    base = emu.urls["proxmox"]
    with urllib.request.urlopen(base + "/", context=ctx) as r:
        assert "<title>pve01 - Proxmox Virtual Environment</title>" in r.read().decode()

    def get(path: str, auth: bool = True) -> tuple[int, dict]:
        sec = emu.secrets["proxmox"]
        req = urllib.request.Request(base + "/api2/json" + path)
        if auth:
            req.add_header("Authorization", f"PVEAPIToken={sec['token_id']}={sec['token_secret']}")
        try:
            with urllib.request.urlopen(req, context=ctx) as r:
                return r.status, json.loads(r.read())
        except urllib.error.HTTPError as e:
            return e.code, json.loads(e.read() or b"null")

    assert get("/version", auth=False)[0] == 401
    status, body = get("/cluster/status")
    assert status == 200 and body["data"][0] == {
        "type": "cluster",
        "id": "cluster",
        "name": "acme-pve",
        "nodes": 3,
        "quorate": 1,
        "version": 3,
    }
    assert [n["ip"] for n in body["data"][1:]] == ["10.10.10.11", "10.10.10.12", "10.10.10.13"]
    rows = get("/nodes/pve01/qemu")[1]["data"]
    assert {r["name"]: r["status"] for r in rows}["tpl-debian12"] == "stopped"
    assert next(r for r in rows if r["name"] == "tpl-debian12")["template"] == 1
    cfg = get("/nodes/pve01/qemu/108/config")[1]["data"]
    assert cfg["net0"] == f"virtio={acme.mac('proxmox', 'docker01').upper()},bridge=vmbr1,firewall=1,tag=20"
    lxc = get("/nodes/pve02/lxc/202/config")[1]["data"]
    assert "bridge=vmbr2" in lxc["net0"] and "tag=" not in lxc["net0"]
    assert get("/nodes/pve01/qemu/101/config")[0] == 500  # dc02 runs on pve02
    assert get("/nodes/pve01/apt/update")[0] == 403
    assert get("/nodes/pve01/bogus")[0] == 501
    netstat = get("/nodes/pve02/netstat")[1]["data"]
    assert any(r["vmid"] == "130" and r["dev"] == "net1" for r in netstat)  # lab-fw01's LAN NIC
