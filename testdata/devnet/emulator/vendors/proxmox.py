"""Proxmox VE 9.0 REST API (`/api2/json`): the "acme-pve" cluster of pve01, pve02 and pve03.

The API is served by pve01 (:8006); like the real cluster, any node answers for
all of them (pmxcfs). Requests carry an API token
(`Authorization: PVEAPIToken=omini@pve!omini=<uuid>`); a wrong one gets 401.
The token has the PVEAuditor role on / (privilege separation on): it reads
everything the plugin needs, but not the update list (`Sys.Modify`, 403),
like a real auditor token. `Proxmox.PRIVILEGES` can be widened by tests.

What comes from the world:
- nodes online/offline (`snap.up`: each node reboots on Sunday night, pve01 at
  02:00, pve02 at 02:20, pve03 at 02:40, 8 minutes each; while pve01 is down
  the whole API is unreachable, the hub drops it); an offline node is listed
  "offline" and its paths answer 595 like the proxy of a real cluster;
- guests on each node (`snap.guests`: they live-migrate to the next node five
  minutes before their node reboots and come back after it), plus one stopped
  guest per node (a template, a retired VM, a test container) that the plugin
  must leave out;
- guest NIC counters from the host's tap/veth ports (`snap.port(host, tapNiM)`:
  the tap's rx is what the guest sent, so `in` = tap tx and `out` = tap rx, as
  PVE's netstat reports them), CPU, memory, uptime;
- node CPU, memory, uptime; storage usage (the NFS ISO share on nas01 follows
  `snap.nas_used_pct`, the PBS datastore grows with the nightly backups).

Vendor dressing (consistent with acme.py): /etc/network/interfaces of each node
(eno1/eno2 → bond0 → vmbr0 with `hwaddress`, ens1f0np0/ens1f1np1 → bond1 →
vmbr1 VLAN-aware with jumbo frames, VLAN interfaces for corosync, storage, Ceph
and backup; pve02 also has vmbr2, the lab bridge without ports), the guests'
configurations, the QEMU guest agent's answers (Windows and Linux; wsus01's
agent is enabled but its service is not running; the pfSense VM has none) and
the containers' interfaces.
"""

from __future__ import annotations

import ipaddress
import re
from typing import Any

from .. import acme
from ..model import Node
from ..server import HttpService, Request, Response
from ..world import REBOOTS, Snapshot, h01, wsec

GIB = 1024**3
TIB = 1024**4
VERSION = "9.0.10"
REPOID = "deb1ca707ec72a89"
KERNEL = "6.14.11-2-pve"
KVERSION = f"Linux {KERNEL} #1 SMP PREEMPT_DYNAMIC PMX 6.14.11-2 (2025-09-12T09:46Z)"
QEMU_MACHINE = "pc-q35-10.0+pve1"
TOKEN_ROLE = "PVEAuditor"
AUDITOR = frozenset(
    {"Sys.Audit", "VM.Audit", "Datastore.Audit", "VM.GuestAgent.Audit", "Pool.Audit", "SDN.Audit", "Mapping.Audit"}
)

# Stopped guests (never migrated): node → (vmid, name, kind, template, ostype, cores, mem MiB, disk GiB).
STOPPED = {
    "pve01": [(9000, "tpl-debian12", "qemu", True, "l26", 2, 2048, 32)],
    "pve02": [(111, "old-erp", "qemu", False, "win10", 4, 16384, 250)],
    "pve03": [(206, "ct-test", "lxc", False, "debian", 1, 512, 8)],
}
# Guests whose agent is enabled in the config but whose service does not run.
AGENT_DOWN = {"wsus01"}
# Linux guests' NIC names inside the VM (q35 + virtio: ens18), when different.
VM_IFNAME = {"homeassistant": "enp0s18", "lab-kali": "eth0"}
OSTYPE = {"win2022": "win11", "l26": "l26", "other": "other", "debian": "debian", "ubuntu": "ubuntu"}

# Pending updates, once the nightly `apt update` of Wednesday finds them; installed at the Sunday reboot.
UPDATES = [
    ("pve-manager", "Proxmox Virtual Environment Management Tools", VERSION, "9.0.11", "admin", "amd64"),
    ("proxmox-kernel-6.14", "Latest Proxmox Kernel Image", "6.14.11-2", "6.14.11-3", "admin", "all"),
    ("pve-qemu-kvm", "Full virtualization on x86 hardware", "10.0.2-4", "10.0.2-5", "admin", "amd64"),
    ("libpve-storage-perl", "Proxmox VE storage management library", "9.0.13", "9.0.14", "perl", "all"),
    (
        "ceph-common",
        "common utilities to mount and interact with a ceph storage cluster",
        "19.2.3-pve1",
        "19.2.3-pve2",
        "admin",
        "amd64",
    ),
]
UPDATES_FOUND = 2 * 86400 + 6 * 3600  # Wednesday 06:00 (local)


def ok(data: Any) -> Response:
    return Response.json(
        {"data": data},
        headers=[("Cache-Control", "max-age=0"), ("Pragma", "no-cache"), ("Server", "pve-api-daemon/3.0")],
    )


def fail(status: int, message: str) -> Response:
    """PVE puts the reason in the status line and answers `{"data": null}` (plus `message` in newer versions)."""
    return Response.json({"data": None, "message": message + "\n"}, status, headers=[("Server", "pve-api-daemon/3.0")])


def link_local(mac: str) -> str:
    """The EUI-64 IPv6 link-local address of a MAC."""
    b = [int(x, 16) for x in mac.split(":")]
    b[0] ^= 0x02
    eui = [*b[:3], 0xFF, 0xFE, *b[3:]]
    return str(ipaddress.IPv6Address(bytes([0xFE, 0x80] + [0] * 6 + eui)))


def kind_of(g: Node) -> str:
    return "lxc" if g.kind == "lxc" else "qemu"


class Proxmox(HttpService):
    PRIVILEGES: frozenset[str] = AUDITOR

    # ------------------------------------------------------------- helpers

    @property
    def nodes(self) -> list[str]:
        return list(self.spec.get("nodes") or [self.node])

    def authorized(self, req: Request) -> bool:
        auth = req.headers.get("authorization", "")
        m = re.match(r"^PVEAPIToken[= ](\S+?)[=:](\S+)$", auth.strip())
        if not m:
            return False
        return m.group(1) == self.secrets.get("token_id") and m.group(2) == self.secrets.get("token_secret")

    def prefix(self, g: Node, vlan: int | None) -> int:
        if vlan is None:
            return 24
        v = self.ctx.world.d.vlans.get((g.site, vlan)) or self.ctx.world.d.vlans.get(("hq", vlan))
        if v and v.subnet:
            return ipaddress.ip_network(v.subnet).prefixlen
        return 24

    def gateway(self, g: Node, vlan: int | None) -> str | None:
        if vlan == acme.LAB_LAN:
            return "192.168.200.1"
        v = self.ctx.world.d.vlans.get(("hq", vlan)) if vlan else None
        return v.gateway if v else None

    def guest_nics(self, g: Node) -> list[dict[str, Any]]:
        """net0..netN of a guest: MAC, bridge, tag, address, tap/veth name."""
        out = []
        for i, p in enumerate(g.ports.values()):
            vlan = p.untagged
            if vlan == acme.LAB_LAN:
                bridge, tag = "vmbr2", None
            elif vlan == 10:
                bridge, tag = "vmbr0", None
            else:
                bridge, tag = "vmbr1", vlan
            ip = None
            if p.ips:
                ip = p.ips[0]
            elif g.ip and i == 0:
                ip = f"{g.ip}/{self.prefix(g, vlan)}"
            dev = f"{'veth' if g.kind == 'lxc' else 'tap'}{g.vmid}i{i}"
            out.append(
                {"index": i, "mac": p.mac or g.mac, "bridge": bridge, "tag": tag, "vlan": vlan, "ip": ip, "dev": dev}
            )
        return out

    def counters(self, snap: Snapshot, host: str, g: Node) -> list[tuple[dict[str, Any], int, int]]:
        """(nic, bytes received by the guest, bytes sent by the guest) from the host's taps."""
        out = []
        ports = snap.node(host).ports
        for nic in self.guest_nics(g):
            if nic["dev"] not in ports:
                out.append((nic, 0, 0))
                continue
            st = snap.port(host, nic["dev"])
            out.append((nic, st.tx_bytes, st.rx_bytes))
        return out

    def running(self, snap: Snapshot, node: str) -> list[Node]:
        return [g for g in snap.guests(node) if snap.present(g.name)]

    def find_guest(self, snap: Snapshot, node: str, kind: str, vmid: int) -> Node | tuple | None:
        for g in self.running(snap, node):
            if g.vmid == vmid and kind_of(g) == kind:
                return g
        for row in STOPPED.get(node, []):
            if row[0] == vmid and row[2] == kind:
                return row
        return None

    # ------------------------------------------------------------- answers

    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path
        if path in ("/", "/index.html", ""):
            return Response.html_title(f"{self.node} - Proxmox Virtual Environment")
        if not path.startswith("/api2/json"):
            return Response.text("", 501, "text/plain")
        if not self.authorized(req):
            return fail(401, "invalid token value!" if req.headers.get("authorization") else "No ticket")
        sub = path[len("/api2/json") :].rstrip("/") or "/"
        if req.method not in ("GET", "HEAD"):
            return fail(403, "Permission check failed")
        return self.route(sub, snap)

    def route(self, sub: str, snap: Snapshot) -> Response:
        if sub == "/version":
            return ok({"version": VERSION, "release": "9.0", "repoid": REPOID[:8], "console": "xtermjs"})
        if sub == "/nodes":
            return ok([self.node_row(snap, n) for n in self.nodes])
        if sub == "/cluster/status":
            return ok(self.cluster_status(snap))
        if sub == "/cluster":
            return ok(
                [
                    {"name": n}
                    for n in (
                        "acme",
                        "backup",
                        "ceph",
                        "firewall",
                        "ha",
                        "log",
                        "options",
                        "replication",
                        "resources",
                        "status",
                        "tasks",
                    )
                ]
            )
        m = re.match(r"^/nodes/([^/]+)(/.*)?$", sub)
        if not m:
            return fail(501, f"Method 'GET {sub}' not implemented")
        node, rest = m.group(1), m.group(2) or ""
        if node not in self.nodes:
            return fail(
                500,
                f"hostname lookup '{node}' failed - failed to get address info for: {node}: Name or service not known",
            )
        if not snap.up(node):
            return fail(595, f"Connection refused - no route to host {self.ctx.world.nodes[node].ip}:8006")
        if rest == "":
            return ok(
                [
                    {"name": x}
                    for x in (
                        "apt",
                        "capabilities",
                        "ceph",
                        "disks",
                        "firewall",
                        "hardware",
                        "lxc",
                        "network",
                        "netstat",
                        "qemu",
                        "status",
                        "storage",
                        "tasks",
                        "version",
                    )
                ]
            )
        if rest == "/status":
            return self.need("Sys.Audit") or ok(self.node_status(snap, node))
        if rest == "/version":
            return ok({"version": VERSION, "release": "9.0", "repoid": REPOID[:8]})
        if rest == "/network":
            return self.need("Sys.Audit") or ok(self.network(node))
        if rest == "/storage":
            return ok(self.storage(snap, node) if "Datastore.Audit" in self.PRIVILEGES else [])
        if rest == "/apt/update":
            return self.need("Sys.Modify") or ok(self.updates(snap, node))
        if rest == "/netstat":
            return self.need("Sys.Audit") or ok(self.netstat(snap, node))
        if rest in ("/qemu", "/lxc"):
            if "VM.Audit" not in self.PRIVILEGES:
                return ok([])  # PVE filters guest lists by permission
            return ok(self.guest_list(snap, node, rest[1:]))
        m = re.match(r"^/(qemu|lxc)/(\d+)(/.*)?$", rest)
        if m:
            return self.guest(snap, node, m.group(1), int(m.group(2)), m.group(3) or "")
        return fail(501, f"Method 'GET /nodes/{node}{rest}' not implemented")

    def need(self, priv: str) -> Response | None:
        if priv in self.PRIVILEGES:
            return None
        return fail(403, f"Permission check failed (/nodes, {priv})")

    # -- cluster and nodes --

    def node_row(self, snap: Snapshot, name: str) -> dict[str, Any]:
        n = snap.node(name)
        fp = ":".join(f"{int(h01('fp', name, k) * 256):02X}" for k in range(32))
        row: dict[str, Any] = {"node": name, "id": f"node/{name}", "type": "node", "level": "", "ssl_fingerprint": fp}
        if not snap.up(name):
            row["status"] = "offline"
            return row
        st = self.node_status(snap, name)
        row.update(
            {
                "status": "online",
                "cpu": st["cpu"],
                "maxcpu": n.cores,
                "mem": st["memory"]["used"],
                "maxmem": st["memory"]["total"],
                "disk": st["rootfs"]["used"],
                "maxdisk": st["rootfs"]["total"],
                "uptime": st["uptime"],
            }
        )
        return row

    def cluster_status(self, snap: Snapshot) -> list[dict[str, Any]]:
        online = [n for n in self.nodes if snap.up(n)]
        out: list[dict[str, Any]] = [
            {
                "type": "cluster",
                "id": "cluster",
                "name": self.spec.get("cluster", "acme-pve"),
                "nodes": len(self.nodes),
                "quorate": int(len(online) * 2 > len(self.nodes)),
                "version": 3,
            }
        ]
        for k, name in enumerate(self.nodes):
            out.append(
                {
                    "type": "node",
                    "id": f"node/{name}",
                    "name": name,
                    "ip": snap.node(name).ip,
                    "nodeid": k + 1,
                    "local": int(name == self.node),
                    "online": int(name in online),
                    "level": "",
                }
            )
        return out

    def node_status(self, snap: Snapshot, name: str) -> dict[str, Any]:
        n = snap.node(name)
        cpu = snap.cpu(name) / 100
        total = n.mem_mb * 1024 * 1024
        used = int(total * snap.mem(name) / 100)
        swap_total = 8 * GIB
        swap_used = int(swap_total * 0.004 * (1 + 3 * h01("swap", name, snap.b)))
        root_total, root_used = self.rootfs(snap, name)
        load1 = cpu * n.cores * 0.55
        load = [load1, load1 * (0.92 + 0.1 * h01("l5", name, snap.b)), load1 * (0.88 + 0.1 * h01("l15", name, snap.b))]
        sockets = int(n.extra.get("sockets", 1))
        return {
            "uptime": snap.uptime(name),
            "cpu": round(cpu, 6),
            "wait": round(0.0008 + 0.004 * h01("iowait", name, snap.b), 6),
            "idle": 0,
            "loadavg": [f"{x:.2f}" for x in load],
            "kversion": KVERSION,
            "pveversion": f"pve-manager/{n.version or VERSION}/{REPOID}",
            "cpuinfo": {
                "model": n.extra.get("cpu", ""),
                "cores": n.cores // 2 // sockets,
                "cpus": n.cores,
                "sockets": sockets,
                "mhz": f"{2400 + h01('mhz', name, snap.b) * 900:.3f}",
                "hvm": "1",
                "flags": "fpu vme de pse tsc msr pae mce cx8 apic sep mtrr pge mca cmov pat pse36 clflush "
                "dts acpi mmx fxsr sse sse2 ss ht tm pbe syscall nx pdpe1gb rdtscp lm constant_tsc "
                "vmx avx512f avx512dq aes avx2",
                "user_hz": 100,
            },
            "memory": {"total": total, "used": used, "free": total - used, "available": total - int(used * 0.93)},
            "swap": {"total": swap_total, "used": swap_used, "free": swap_total - swap_used},
            "rootfs": {
                "total": root_total,
                "used": root_used,
                "free": root_total - root_used,
                "avail": root_total - root_used - int(root_total * 0.05),
            },
            "ksm": {"shared": int(6 * GIB * (0.6 + 0.4 * h01("ksm", name)))},
            "boot-info": {"mode": "efi", "secureboot": 1},
            "current-kernel": {
                "sysname": "Linux",
                "release": KERNEL,
                "machine": "x86_64",
                "version": "#1 SMP PREEMPT_DYNAMIC PMX 6.14.11-2 (2025-09-12T09:46Z)",
            },
        }

    def rootfs(self, snap: Snapshot, name: str) -> tuple[int, int]:
        # The BOSS-N1 mirror (480 GB): a 96 GiB root LV, the rest is local-lvm.
        total = int(94.3 * GIB)
        used = int(total * (0.13 + 0.04 * h01("root", name)) + (wsec(snap.t) / 86400) * 0.12 * GIB)
        return total, used

    def network(self, name: str) -> list[dict[str, Any]]:
        n = self.ctx.world.nodes[name]
        mgmt_gw = self.ctx.world.d.vlans[("hq", 10)].gateway
        rows: list[dict[str, Any]] = []
        prio = iter(range(3, 40))
        for p in ("eno1", "eno2", "ens1f0np0", "ens1f1np1"):
            row = {
                "iface": p,
                "type": "eth",
                "method": "manual",
                "method6": "manual",
                "families": ["inet"],
                "active": 1,
                "exists": 1,
                "priority": next(prio),
            }
            if p.startswith("ens"):
                row["mtu"] = "9000"
            rows.append(row)
        rows.append(
            {
                "iface": "bond0",
                "type": "bond",
                "method": "manual",
                "method6": "manual",
                "families": ["inet"],
                "slaves": "eno1 eno2",
                "bond_mode": "802.3ad",
                "bond-xmit-hash-policy": "layer2+3",
                "bond_miimon": "100",
                "active": 1,
                "autostart": 1,
                "priority": next(prio),
            }
        )
        rows.append(
            {
                "iface": "bond1",
                "type": "bond",
                "method": "manual",
                "method6": "manual",
                "families": ["inet"],
                "slaves": "ens1f0np0 ens1f1np1",
                "bond_mode": "802.3ad",
                "bond-xmit-hash-policy": "layer3+4",
                "bond_miimon": "100",
                "mtu": "9000",
                "active": 1,
                "autostart": 1,
                "priority": next(prio),
            }
        )
        ip0 = n.ports["vmbr0"].ips[0]
        rows.append(
            {
                "iface": "vmbr0",
                "type": "bridge",
                "method": "static",
                "method6": "manual",
                "families": ["inet"],
                "address": ip0.split("/")[0],
                "netmask": ip0.split("/")[1],
                "cidr": ip0,
                "gateway": mgmt_gw,
                "bridge_ports": "bond0",
                "bridge_stp": "off",
                "bridge_fd": "0",
                "options": [f"hwaddress {n.ports['vmbr0'].mac}"],
                "comments": "Management\n",
                "active": 1,
                "autostart": 1,
                "priority": next(prio),
            }
        )
        rows.append(
            {
                "iface": "vmbr1",
                "type": "bridge",
                "method": "manual",
                "method6": "manual",
                "families": ["inet"],
                "bridge_ports": "bond1",
                "bridge_stp": "off",
                "bridge_fd": "0",
                "bridge_vlan_aware": 1,
                "bridge_vids": "2-4094",
                "mtu": "9000",
                "comments": "VM trunk (VLAN aware)\n",
                "active": 1,
                "autostart": 1,
                "priority": next(prio),
            }
        )
        if "vmbr2" in n.ports:
            rows.append(
                {
                    "iface": "vmbr2",
                    "type": "bridge",
                    "method": "manual",
                    "method6": "manual",
                    "families": ["inet"],
                    "bridge_ports": "none",
                    "bridge_stp": "off",
                    "bridge_fd": "0",
                    "comments": "Lab LAN (no uplink)\n",
                    "active": 1,
                    "autostart": 1,
                    "priority": next(prio),
                }
            )
        for p in n.ports.values():
            if p.kind != "vlan":
                continue
            cidr = p.ips[0]
            row = {
                "iface": p.name,
                "type": "vlan",
                "method": "static",
                "method6": "manual",
                "families": ["inet"],
                "address": cidr.split("/")[0],
                "netmask": cidr.split("/")[1],
                "cidr": cidr,
                "vlan-raw-device": p.parent,
                "vlan-id": str(p.vlan),
                "active": 1,
                "autostart": 1,
                "priority": next(prio),
            }
            if p.descr:
                row["comments"] = p.descr + "\n"
            if p.parent == "vmbr1":
                row["mtu"] = "9000"
            rows.append(row)
        return rows

    def storage(self, snap: Snapshot, name: str) -> list[dict[str, Any]]:
        world = self.ctx.world
        root_total, root_used = self.rootfs(snap, name)
        lvm_total = int(348.4 * GIB)
        lvm_used = int(lvm_total * (0.02 + 0.03 * h01("lvm", name)))
        # Ceph: 3 nodes × 4 × 3.84 TB NVMe, 3 replicas.
        ceph_total = int(3 * 4 * 3.84e12 / 3 * 0.95)
        guests_gb = sum(g.disk_gb for g in world.nodes.values() if g.host)
        ceph_used = int(guests_gb * GIB * 0.58 + 0.4 * TIB * (wsec(snap.t) / (7 * 86400)))
        cephfs_total = int(ceph_total * 0.31)
        cephfs_used = int(0.42 * TIB)
        nas_total = int(64e12 * 0.93)
        nas_used = int(nas_total * snap.nas_used_pct() / 100)
        pbs_total = int(32 * TIB)
        day = wsec(snap.t) / 86400
        pbs_used = int(pbs_total * (0.47 + 0.011 * min(day, 6.1)))  # GC + prune run on Sunday morning
        pbs_up = any(snap.host_of("pbs01") == h and snap.up(h) for h in self.nodes)

        def row(sid: str, typ: str, content: str, shared: int, total: int, used: int, active: bool = True) -> dict:
            r: dict[str, Any] = {
                "storage": sid,
                "type": typ,
                "content": content,
                "shared": shared,
                "enabled": 1,
                "active": int(active),
            }
            if active:
                r.update(
                    {
                        "total": total,
                        "used": used,
                        "avail": max(total - used, 0),
                        "used_fraction": round(used / total, 6) if total else 0,
                    }
                )
            else:
                r.update({"total": 0, "used": 0, "avail": 0})
            return r

        return [
            row("ceph-vm", "rbd", "images,rootdir", 1, ceph_total, ceph_used),
            row("cephfs", "cephfs", "vztmpl,iso,backup,snippets", 1, cephfs_total, cephfs_used),
            row("local", "dir", "iso,vztmpl,backup", 0, root_total, root_used),
            row("local-lvm", "lvmthin", "images,rootdir", 0, lvm_total, lvm_used),
            row("nas-iso", "nfs", "iso,vztmpl", 1, nas_total, nas_used, snap.up("nas01")),
            row("pbs01", "pbs", "backup", 1, pbs_total, pbs_used, pbs_up),
        ]

    def updates(self, snap: Snapshot, name: str) -> list[dict[str, Any]]:
        s = wsec(snap.t)
        reboot = next(((d * 86400 + h * 3600 + mins * 60) for d, h, mins in REBOOTS.get(name, [])), 7 * 86400)
        if not (UPDATES_FOUND <= s < reboot):
            return []
        return [
            {
                "Package": pkg,
                "Title": title,
                "Description": title + ".",
                "OldVersion": old,
                "Version": new,
                "Origin": "Proxmox",
                "Priority": "optional",
                "Section": section,
                "Arch": arch,
            }
            for pkg, title, old, new, section, arch in UPDATES
        ]

    def netstat(self, snap: Snapshot, name: str) -> list[dict[str, Any]]:
        out = []
        for g in self.running(snap, name):
            for nic, rx, tx in self.counters(snap, name, g):
                out.append({"dev": f"net{nic['index']}", "vmid": str(g.vmid), "in": str(rx), "out": str(tx)})
        return out

    # -- guests --

    def guest_list(self, snap: Snapshot, node: str, kind: str) -> list[dict[str, Any]]:
        out = []
        for g in self.running(snap, node):
            if kind_of(g) != kind:
                continue
            maxmem = g.mem_mb * 1024 * 1024
            maxdisk = g.disk_gb * GIB
            rx = tx = 0
            for _nic, r, t in self.counters(snap, node, g):
                rx, tx = rx + r, tx + t
            uptime = snap.uptime(g.name)
            row: dict[str, Any] = {
                "vmid": g.vmid,
                "name": g.name,
                "status": "running",
                "cpu": round(snap.cpu(g.name) / 100, 6),
                "cpus": g.cores,
                "mem": int(maxmem * snap.mem(g.name) / 100),
                "maxmem": maxmem,
                "maxdisk": maxdisk,
                "uptime": uptime,
                "netin": rx,
                "netout": tx,
                "diskread": int(uptime * 40_000 * (1 + h01("dr", g.name))),
                "diskwrite": int(uptime * 25_000 * (1 + h01("dw", g.name))),
            }
            if kind == "qemu":
                row.update(
                    {
                        "disk": 0,
                        "pid": 1000 + int(h01("pid", g.name, snap.boot(g.name)) * 900000),
                        "qmpstatus": "running",
                        "serial": 1,
                    }
                )
                if g.name.startswith("lab-"):
                    row["tags"] = "lab"
            else:
                row.update(
                    {
                        "type": "lxc",
                        "disk": int(maxdisk * (0.18 + 0.4 * h01("disk", g.name))),
                        "swap": 0,
                        "maxswap": 512 * 1024 * 1024,
                    }
                )
                if g.name.startswith("lab-"):
                    row["tags"] = "k3s;lab"
            out.append(row)
        for vmid, gname, gkind, template, _ostype, cores, mem, disk in STOPPED.get(node, []):
            if gkind != kind:
                continue
            row = {
                "vmid": vmid,
                "name": gname,
                "status": "stopped",
                "cpu": 0,
                "cpus": cores,
                "mem": 0,
                "maxmem": mem * 1024 * 1024,
                "disk": 0,
                "maxdisk": disk * GIB,
                "uptime": 0,
                "netin": 0,
                "netout": 0,
                "diskread": 0,
                "diskwrite": 0,
            }
            if template:
                row["template"] = 1
            if kind == "lxc":
                row.update({"type": "lxc", "swap": 0, "maxswap": 512 * 1024 * 1024})
            out.append(row)
        return out

    def guest(self, snap: Snapshot, node: str, kind: str, vmid: int, rest: str) -> Response:
        if "VM.Audit" not in self.PRIVILEGES:
            return fail(403, f"Permission check failed (/vms/{vmid}, VM.Audit)")
        g = self.find_guest(snap, node, kind, vmid)
        if g is None:
            conf = "qemu-server" if kind == "qemu" else "lxc"
            return fail(500, f"Configuration file 'nodes/{node}/{conf}/{vmid}.conf' does not exist")
        if rest == "/config":
            return ok(self.config(g, kind) if isinstance(g, Node) else self.stopped_config(g))
        if rest in ("", "/status"):
            return ok(
                [{"subdir": x} for x in ("config", "status", "agent", "rrd")]
                if kind == "qemu"
                else [{"subdir": x} for x in ("config", "status", "interfaces", "rrd")]
            )
        if not isinstance(g, Node):
            if rest.startswith("/agent") or rest == "/interfaces":
                return fail(500, f"VM {vmid} is not running")
            return fail(501, f"Method 'GET /nodes/{node}/{kind}/{vmid}{rest}' not implemented")
        if kind == "qemu" and rest == "/agent/network-get-interfaces":
            if "VM.GuestAgent.Audit" not in self.PRIVILEGES:
                return fail(403, f"Permission check failed (/vms/{vmid}, VM.GuestAgent.Audit)")
            if not self.has_agent(g):
                return fail(500, "No QEMU guest agent configured")
            if g.name in AGENT_DOWN:
                return fail(500, "QEMU guest agent is not running")
            return ok({"result": self.agent_interfaces(snap, node, g)})
        if kind == "lxc" and rest == "/interfaces":
            return ok(self.lxc_interfaces(g))
        return fail(501, f"Method 'GET /nodes/{node}/{kind}/{vmid}{rest}' not implemented")

    def has_agent(self, g: Node) -> bool:
        return g.extra.get("ostype") != "other"

    def config(self, g: Node, kind: str) -> dict[str, Any]:
        ostype = OSTYPE.get(g.extra.get("ostype", "l26"), "l26")
        digest = f"{int(h01('digest', g.name) * 2**160):040x}"
        uuid = f"{int(h01('uuid', g.name) * 2**128):032x}"
        uuid = f"{uuid[:8]}-{uuid[8:12]}-{uuid[12:16]}-{uuid[16:20]}-{uuid[20:]}"
        nics = self.guest_nics(g)
        if kind == "lxc":
            cfg: dict[str, Any] = {
                "hostname": g.hostname or g.name,
                "ostype": ostype,
                "arch": "amd64",
                "cores": g.cores,
                "memory": g.mem_mb,
                "swap": 512,
                "rootfs": f"ceph-vm:vm-{g.vmid}-disk-0,size={g.disk_gb}G",
                "unprivileged": 1,
                "features": "nesting=1" + (",keyctl=1" if g.name.startswith("lab-k3s") else ""),
                "onboot": 1,
                "digest": digest,
            }
            for nic in nics:
                parts = [f"name=eth{nic['index']}", f"bridge={nic['bridge']}", "firewall=1"]
                gw = self.gateway(g, nic["vlan"])
                if gw and nic["index"] == 0:
                    parts.append(f"gw={gw}")
                parts.append(f"hwaddr={nic['mac'].upper()}")
                parts.append(f"ip={nic['ip']}" if nic["ip"] else "ip=dhcp")
                if nic["tag"]:
                    parts.append(f"tag={nic['tag']}")
                parts.append("type=veth")
                cfg[f"net{nic['index']}"] = ",".join(parts)
            return cfg
        windows = ostype.startswith("win")
        cfg = {
            "name": g.name,
            "ostype": ostype,
            "cores": g.cores,
            "sockets": 1,
            "memory": str(g.mem_mb),
            "cpu": "x86-64-v2-AES",
            "numa": 0,
            "scsihw": "virtio-scsi-single",
            "boot": "order=scsi0;ide2;net0",
            "scsi0": f"ceph-vm:vm-{g.vmid}-disk-0,discard=on,iothread=1,size={g.disk_gb}G",
            "ide2": "none,media=cdrom",
            "onboot": 1,
            "smbios1": f"uuid={uuid}",
            "vmgenid": f"{uuid[::-1][:8]}-1b2c-4d5e-8f90-{uuid[-12:]}",
            "meta": "creation-qemu=10.0.2,ctime=1755000000",
            "digest": digest,
        }
        if self.has_agent(g):
            cfg["agent"] = "1" if not windows else "1,fstrim_cloned_disks=1"
        if windows:
            cfg.update(
                {
                    "bios": "ovmf",
                    "machine": QEMU_MACHINE,
                    "efidisk0": f"ceph-vm:vm-{g.vmid}-disk-1,efitype=4m,pre-enrolled-keys=1,size=528K",
                    "tpmstate0": f"ceph-vm:vm-{g.vmid}-disk-2,size=4M,version=v2.0",
                }
            )
        if g.name.startswith("lab-"):
            cfg["tags"] = "lab"
        for nic in nics:
            parts = [f"virtio={nic['mac'].upper()}", f"bridge={nic['bridge']}", "firewall=1"]
            if nic["tag"]:
                parts.append(f"tag={nic['tag']}")
            cfg[f"net{nic['index']}"] = ",".join(parts)
        return cfg

    def stopped_config(self, row: tuple) -> dict[str, Any]:
        vmid, name, kind, template, ostype, cores, mem, disk = row
        m = acme.mac("proxmox", name).upper()
        if kind == "lxc":
            return {
                "hostname": name,
                "ostype": ostype,
                "arch": "amd64",
                "cores": cores,
                "memory": mem,
                "rootfs": f"ceph-vm:subvol-{vmid}-disk-0,size={disk}G",
                "unprivileged": 1,
                "net0": f"name=eth0,bridge=vmbr1,firewall=1,hwaddr={m},ip=dhcp,tag=20,type=veth",
                "digest": f"{int(h01('digest', name) * 2**160):040x}",
            }
        cfg = {
            "name": name,
            "ostype": ostype,
            "cores": cores,
            "sockets": 1,
            "memory": str(mem),
            "scsi0": f"ceph-vm:{'base' if template else 'vm'}-{vmid}-disk-0,size={disk}G",
            "net0": f"virtio={m},bridge=vmbr1,firewall=1,tag=20",
            "boot": "order=scsi0",
            "digest": f"{int(h01('digest', name) * 2**160):040x}",
        }
        if template:
            cfg.update({"template": 1, "agent": "1", "ide2": "ceph-vm:cloudinit,media=cdrom"})
        return cfg

    def agent_interfaces(self, snap: Snapshot, node: str, g: Node) -> list[dict[str, Any]]:
        windows = OSTYPE.get(g.extra.get("ostype", ""), "").startswith("win")
        stats = {nic["index"]: (rx, tx) for nic, rx, tx in self.counters(snap, node, g)}

        def statistics(rx: int, tx: int) -> dict[str, int]:
            return {
                "rx-bytes": rx,
                "rx-packets": rx // 900,
                "rx-errs": 0,
                "rx-dropped": 0,
                "tx-bytes": tx,
                "tx-packets": tx // 900,
                "tx-errs": 0,
                "tx-dropped": 0,
            }

        out: list[dict[str, Any]] = []
        if not windows:
            out.append(
                {
                    "name": "lo",
                    "hardware-address": "00:00:00:00:00:00",
                    "ip-addresses": [
                        {"ip-address": "127.0.0.1", "ip-address-type": "ipv4", "prefix": 8},
                        {"ip-address": "::1", "ip-address-type": "ipv6", "prefix": 128},
                    ],
                    "statistics": statistics(48_000, 48_000),
                }
            )
        for nic in self.guest_nics(g):
            rx, tx = stats.get(nic["index"], (0, 0))
            addrs = []
            if nic["ip"]:
                ip, plen = nic["ip"].split("/")
                addrs.append({"ip-address": ip, "ip-address-type": "ipv4", "prefix": int(plen)})
            addrs.append({"ip-address": link_local(nic["mac"]), "ip-address-type": "ipv6", "prefix": 64})
            if windows:
                name = "Ethernet" if nic["index"] == 0 else f"Ethernet {nic['index'] + 1}"
            else:
                name = VM_IFNAME.get(g.name, "ens18") if nic["index"] == 0 else f"ens{18 + nic['index']}"
            out.append(
                {"name": name, "hardware-address": nic["mac"], "ip-addresses": addrs, "statistics": statistics(rx, tx)}
            )
        if windows:
            out.append(
                {
                    "name": "Loopback Pseudo-Interface 1",
                    "ip-addresses": [
                        {"ip-address": "::1", "ip-address-type": "ipv6", "prefix": 128},
                        {"ip-address": "127.0.0.1", "ip-address-type": "ipv4", "prefix": 8},
                    ],
                    "statistics": statistics(0, 0),
                }
            )
        if g.name == "docker01":
            for k, (dev, net) in enumerate((("docker0", "172.17.0.1"), ("br-4f1c2a9d7e01", "172.18.0.1"))):
                m = f"02:42:{int(h01('dk', k) * 256):02x}:{int(h01('dk2', k) * 256):02x}:3c:{k + 1:02x}"
                out.append(
                    {
                        "name": dev,
                        "hardware-address": m,
                        "ip-addresses": [
                            {"ip-address": net, "ip-address-type": "ipv4", "prefix": 16},
                            {"ip-address": link_local(m), "ip-address-type": "ipv6", "prefix": 64},
                        ],
                        "statistics": statistics(1_000_000 * (k + 1), 2_000_000 * (k + 1)),
                    }
                )
        return out

    def lxc_interfaces(self, g: Node) -> list[dict[str, Any]]:
        out: list[dict[str, Any]] = [
            {
                "name": "lo",
                "hwaddr": "00:00:00:00:00:00",
                "hardware-address": "00:00:00:00:00:00",
                "inet": "127.0.0.1/8",
                "inet6": "::1/128",
                "ip-addresses": [
                    {"ip-address": "127.0.0.1", "ip-address-type": "inet", "prefix": 8},
                    {"ip-address": "::1", "ip-address-type": "inet6", "prefix": 128},
                ],
            }
        ]
        for nic in self.guest_nics(g):
            ll = link_local(nic["mac"])
            row: dict[str, Any] = {
                "name": f"eth{nic['index']}",
                "hwaddr": nic["mac"],
                "hardware-address": nic["mac"],
                "inet6": f"{ll}/64",
                "ip-addresses": [],
            }
            if nic["ip"]:
                ip, plen = nic["ip"].split("/")
                row["inet"] = nic["ip"]
                row["ip-addresses"].append({"ip-address": ip, "ip-address-type": "inet", "prefix": int(plen)})
            row["ip-addresses"].append({"ip-address": ll, "ip-address-type": "inet6", "prefix": 64})
            out.append(row)
        if g.name.startswith("lab-k3s-"):
            k = int(g.name[-1])
            for dev, addr, plen in (("cni0", f"10.42.{k - 1}.1", 24), ("flannel.1", f"10.42.{k - 1}.0", 32)):
                m = f"{0x02 | (int(h01('k3s', dev, k) * 256) & 0xFC):02x}:{int(h01('k3', dev, k) * 256):02x}"
                m += f":5e:0a:{k:02x}:01"
                out.append(
                    {
                        "name": dev,
                        "hwaddr": m,
                        "hardware-address": m,
                        "inet": f"{addr}/{plen}",
                        "ip-addresses": [{"ip-address": addr, "ip-address-type": "inet", "prefix": plen}],
                    }
                )
        return out
