"""pfSense CE with the REST API package v2 (pfrest.org): the lab router lab-fw01.

lab-fw01 is a pfSense CE 2.8.0 VM on pve02 (it never migrates, so it is
unreachable while pve02 reboots on Sunday night; the hub handles that).
WAN vtnet0 has a static address in HQ's LAB VLAN (gateway: HQ's CARP VIP
10.10.120.1), LAN vtnet1 is the internal bridge of the lab guests, served by
Kea DHCP (range .100-.199, static mappings for the k3s nodes and for any
host whose address lies outside the range).

Every request is authenticated with the `X-API-Key` header and answered in
the package's envelope `{code, status, response_id, message, data, _links}`.
Plural endpoints take `limit` (0 = all), `offset`, `sort_by`/`sort_order` and
field filters (`?name=wan`), like the package. The WireGuard package is not
installed (424), and no OpenVPN or IPsec is configured (empty lists).
"""

from __future__ import annotations

import ipaddress
import time
from typing import Any

from .. import acme
from ..server import HttpService, Request, Response
from ..world import Snapshot, h01

NODE = "lab-fw01"
DOMAIN = "lab.acme.local"
WAN_GW = "10.10.120.1"  # HQ's CARP VIP in the LAB VLAN
WAN_VHID = 120
VERSION = {"version": "2.8.0-RELEASE", "base": "2.8.0", "patch": "0", "buildtime": "Wed May 28 17:38:46 UTC 2025"}
API_VERSION = "v2.6.4"
# pfSense's default state table size: 10 % of the RAM in states of ~1 kB (4 GB VM).
MAX_STATES = 407000
LAB_POOL = ("192.168.200.100", "192.168.200.199")
STATIC_MAPS = ("lab-k3s-01", "lab-k3s-02", "lab-k3s-03")  # fixed addresses registered in the resolver
SERVICES = [  # name, description (pfSense CE 2.8 with Kea, a static WAN and nothing else installed)
    ("dpinger", "Gateway Monitoring Daemon"),
    ("kea-dhcp4", "Kea DHCP Server"),
    ("ntpd", "NTP clock sync"),
    ("sshd", "Secure Shell Daemon"),
    ("syslogd", "System Logger Daemon"),
    ("unbound", "DNS Resolver"),
]
IFACES = [  # pfSense name, description, NIC
    ("wan", "WAN", "vtnet0"),
    ("lan", "LAN", "vtnet1"),
]
RESERVED = {"limit", "offset", "sort_by", "sort_order", "sort_flags", "reverse"}

NOT_FOUND_HTML = (
    "<html>\r\n<head><title>404 Not Found</title></head>\r\n<body>\r\n<center><h1>404 Not Found</h1></center>\r\n"
    "<hr><center>nginx</center>\r\n</body>\r\n</html>\r\n"
)


def envelope(
    data: Any, code: int = 200, status: str = "ok", response_id: str = "SUCCESS", message: str = ""
) -> Response:
    return Response.json(
        {"code": code, "status": status, "response_id": response_id, "message": message, "data": data, "_links": {}},
        code,
    )


def uptime_text(seconds: int) -> str:
    """pfSense's get_uptime(): '3 Days 04 Hours 12 Minutes 09 Seconds' (no days part under a day)."""
    days, rest = divmod(max(int(seconds), 0), 86400)
    h, rest = divmod(rest, 3600)
    m, s = divmod(rest, 60)

    def unit(n: int, word: str) -> str:
        return f"{word}{'' if n == 1 else 's'}"

    out = f"{h:02d} {unit(h, 'Hour')} {m:02d} {unit(m, 'Minute')} {s:02d} {unit(s, 'Second')}"
    return f"{days} {unit(days, 'Day')} {out}" if days else out


def link_local(mac: str) -> str:
    b = [int(x, 16) for x in mac.split(":")]
    b[0] ^= 0x02
    e = [*b[:3], 0xFF, 0xFE, *b[3:]]
    groups = [f"{(e[i] << 8 | e[i + 1]):x}" for i in range(0, 8, 2)]
    return "fe80::" + ":".join(groups)


def stamp(t: float) -> str:
    return time.strftime("%Y/%m/%d %H:%M:%S", time.gmtime(t))


class PfSense(HttpService):
    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path.rstrip("/") or "/"
        if not path.startswith("/api/"):
            if path in ("/", "/index.php"):
                return Response.html_title("pfSense - Login")
            return Response.text(NOT_FOUND_HTML, 404)
        route = ROUTES.get(path)
        if route is None:
            return Response.text(NOT_FOUND_HTML, 404)
        if req.method == "OPTIONS":
            return Response(200, b"", [("Allow", "GET, OPTIONS")])
        key = req.headers.get("x-api-key", "")
        if not key or key != self.secrets.get("api_key"):
            return envelope([], 401, "unauthorized", "AUTH_AUTHENTICATION_FAILED", "Authentication failed.")
        if req.method not in ("GET", "HEAD"):
            # The key belongs to a read-only user: every write privilege is missing.
            return envelope(
                [], 403, "forbidden", "FORBIDDEN", f"User does not have required privileges for {req.method} {path}."
            )
        fn, plural = route
        try:
            out = fn(self, snap)
        except Dependency as e:
            return envelope([], 424, "failed dependency", "MODEL_REQUIRES_PACKAGE", str(e))
        if not plural:
            return envelope(out)
        return self.page(req, out)

    # -- plural endpoints: filters, sorting, pagination --
    def page(self, req: Request, rows: list[dict[str, Any]]) -> Response:
        for k, vs in req.query.items():
            if k in RESERVED:
                continue
            name, _, op = k.partition("__")
            want = vs[0]
            if op not in ("", "exact", "contains", "startswith", "endswith"):
                continue

            def match(v: Any, op: str = op, want: str = want) -> bool:
                s = str(v).lower() if isinstance(v, bool) else str(v)
                if op == "contains":
                    return want in s
                if op == "startswith":
                    return s.startswith(want)
                if op == "endswith":
                    return s.endswith(want)
                return s == want

            rows = [r for r in rows if name in r and match(r[name])]
        sort_by = req.arg("sort_by")
        if sort_by:
            rows = sorted(
                rows,
                key=lambda r: (r.get(sort_by) is None, str(r.get(sort_by))),
                reverse=(req.arg("sort_order") or "").upper() == "SORT_DESC",
            )
        try:
            limit = int(req.arg("limit", "0") or 0)
            offset = int(req.arg("offset", "0") or 0)
        except ValueError:
            return envelope([], 400, "bad request", "FIELD_INVALID_TYPE", "Field `limit` must be of type `integer`.")
        if limit < 0 or offset < 0:
            return envelope(
                [],
                400,
                "bad request",
                "NUMERIC_RANGE_VALIDATOR_MINIMUM_CONSTRAINT",
                "Field `limit` must be greater than or equal to `0`.",
            )
        rows = rows[offset:]
        if limit:
            rows = rows[:limit]
        return envelope(rows)

    # -- data --
    def lab_node(self, snap: Snapshot) -> Any:
        return snap.node(NODE)

    def system_status(self, snap: Snapshot) -> dict[str, Any]:
        cpu = snap.cpu(NODE)
        mem = snap.mem(NODE)
        load1 = round(cpu / 100 * 2 * (0.8 + 0.4 * h01("load1", snap.b)), 2)
        return {
            "platform": "pfSense",
            "serial": "",
            "netgate_id": f"{int(h01('netgate-id', NODE) * 2**64):016x}{int(h01('nid2', NODE) * 2**16):04x}",
            "uptime": uptime_text(snap.uptime(NODE)),
            "bios_vendor": "SeaBIOS",
            "bios_version": "rel-1.16.3-0-ga6ed6b701f0a-prebuilt.qemu.org",
            "bios_date": "04/01/2014",
            "cpu_model": "QEMU Virtual CPU version 2.5+",
            "kernel_pti": False,
            "mds_mitigation": "inactive",
            # A KVM guest has no thermal sensor: pfSense reports none.
            "temp_c": None,
            "temp_f": None,
            "cpu_load_avg": [load1, round(load1 * 0.9, 2), round(load1 * 0.8, 2)],
            "cpu_count": self.lab_node(snap).cores or 2,
            "cpu_usage": cpu,
            "mbuf_usage": round(1.2 + h01("mbuf", NODE) * 0.6, 1),
            "mem_usage": round(mem),
            "swap_usage": 0,
            "disk_usage": round(9 + 2 * h01("disk", NODE)),
        }

    def version(self, snap: Snapshot) -> dict[str, Any]:
        return dict(VERSION)

    def hostname(self, snap: Snapshot) -> dict[str, Any]:
        return {"hostname": self.lab_node(snap).hostname or NODE, "domain": DOMAIN}

    def restapi_version(self, snap: Snapshot) -> dict[str, Any]:
        return {
            "current_version": API_VERSION,
            "latest_version": API_VERSION,
            "latest_version_release_date": "",
            "update_available": False,
            "install_version": None,
        }

    def interfaces(self, snap: Snapshot) -> list[dict[str, Any]]:
        n = self.lab_node(snap)
        out = []
        for i, (name, descr, nic) in enumerate(IFACES):
            p = n.ports[nic]
            st = snap.port(NODE, nic)
            ip, _, prefix = (p.ips[0] if p.ips else "").partition("/")
            out.append(
                {
                    "id": i,
                    "name": name,
                    "descr": descr,
                    "hwif": nic,
                    "macaddr": p.mac,
                    "mtu": str(p.mtu or 1500),
                    "enable": True,
                    "status": "up" if st.up else "no carrier",
                    "ipaddr": ip,
                    "subnet": prefix,
                    "linklocal": f"{link_local(p.mac)}%{nic}" if p.mac else "",
                    "ipaddrv6": "",
                    "subnetv6": "",
                    "inerrs": st.rx_errors,
                    "outerrs": st.tx_errors,
                    "collisions": 0,
                    "inbytes": st.rx_bytes,
                    "inbytespass": st.rx_bytes,
                    "outbytes": st.tx_bytes,
                    "outbytespass": st.tx_bytes,
                    "inpkts": st.rx_packets,
                    "inpktspass": st.rx_packets,
                    "outpkts": st.tx_packets,
                    "outpktspass": st.tx_packets,
                    "dhcplink": None,
                    # virtio-net always says 10Gbase-T, whatever the host has.
                    "media": "10Gbase-T <full-duplex>" if st.up else "",
                    "gateway": WAN_GW if name == "wan" and st.up else None,
                    "gatewayv6": None,
                }
            )
        return out

    def gateways(self, snap: Snapshot) -> list[dict[str, Any]]:
        # dpinger to HQ's CARP VIP over one Proxmox bridge and the core: a fraction of a millisecond. While
        # the CARP pair fails over the VIP moves and a few probes are lost.
        delay = round(0.28 + h01("pf-lat", snap.b) * 0.35, 3)
        loss = 0
        if snap.active("carp_failover") and snap.local()[2] in (28, 29, 36):
            delay, loss = round(delay + 4.2, 3), 3
        return [
            {
                "id": 0,
                "name": "WANGW",
                "srcip": "10.10.120.10",
                "monitorip": WAN_GW,
                "delay": delay,
                "stddev": round(0.04 + h01("pf-sd", snap.b) * 0.08, 3),
                "loss": loss,
                "status": "online",
                "substatus": "none",
            }
        ]

    def names(self, snap: Snapshot) -> dict[str, str]:
        """MAC → short host name, for the hosts the resolver knows (leases and static mappings)."""
        out = {}
        for m in self.static_maps(snap):
            out[m["mac"]] = m["hostname"]
        for lease in snap.leases("lab"):
            if lease.hostname:
                out[lease.mac] = lease.hostname
        return out

    def arp(self, snap: Snapshot) -> list[dict[str, Any]]:
        n = self.lab_node(snap)
        descr = {nic: d for _, d, nic in IFACES}
        names = self.names(snap)
        rows: list[tuple[str, str, str, bool]] = []
        for _, _, nic in IFACES:  # its own addresses: permanent entries
            p = n.ports[nic]
            for a in p.ips:
                rows.append((a.split("/")[0], p.mac or "", nic, True))
        rows += [(ip, m, iface, False) for ip, m, iface in snap.arp(NODE)]
        if snap.up("fw-hq-01") or snap.up("fw-hq-02"):
            rows.append((WAN_GW, acme.carp_mac(WAN_VHID), "vtnet0", False))
        rows.sort(key=lambda r: (list(descr).index(r[2]) if r[2] in descr else 9, ipaddress.ip_address(r[0])))
        out = []
        for i, (ip, m, iface, perm) in enumerate(rows):
            host = names.get(m, NODE if perm and iface == "vtnet1" else "")
            out.append(
                {
                    "id": i,
                    "hostname": host,
                    "ip_address": ip,
                    "mac_address": m,
                    "interface": descr.get(iface, iface),
                    "type": "ethernet",
                    "permanent": perm,
                    "dnsresolve": f"{host}.{DOMAIN}" if host else "",
                    "expires": "Permanent"
                    if perm
                    else f"Expires in {1200 - int(h01('arp', ip, snap.b) * 1100)} seconds",
                }
            )
        return out

    def static_maps(self, snap: Snapshot) -> list[dict[str, Any]]:
        """Static mappings: the k3s nodes, and DHCP hosts whose address lies outside the pool."""
        lo, hi = (ipaddress.ip_address(a) for a in LAB_POOL)
        hosts = [snap.node(h) for h in STATIC_MAPS]
        hosts += [
            n
            for n in snap.nodes.values()
            if n.site == "lab" and n.dhcp and n.ip and not lo <= ipaddress.ip_address(n.ip) <= hi
        ]
        out = []
        for i, h in enumerate(sorted({h.name: h for h in hosts}.values(), key=lambda h: ipaddress.ip_address(h.ip))):
            out.append(
                {
                    "parent_id": "lan",
                    "id": i,
                    "mac": h.mac,
                    "ipaddr": h.ip,
                    "hostname": h.hostname or h.name,
                    "descr": f"{h.kind.upper()} {h.vmid} on {h.host}" if h.vmid else "",
                    "cid": "",
                    "arp_table_static_entry": False,
                }
            )
        return out

    def leases(self, snap: Snapshot) -> list[dict[str, Any]]:
        online = {m for _, m, _ in snap.arp(NODE)}
        statics = self.static_maps(snap)
        static_macs = {m["mac"] for m in statics}
        out: list[dict[str, Any]] = []
        for lease in snap.leases("lab"):
            if lease.mac in static_macs or lease.static:
                continue
            out.append(
                {
                    "ip": lease.ip,
                    "mac": lease.mac,
                    "hostname": lease.hostname or None,
                    "if": "lan",
                    "starts": stamp(lease.start),
                    "ends": stamp(lease.end),
                    "active_status": "active",
                    "online_status": "active/online" if lease.mac in online else "idle/offline",
                    "descr": None,
                }
            )
        for m in statics:
            out.append(
                {
                    "ip": m["ipaddr"],
                    "mac": m["mac"],
                    "hostname": m["hostname"],
                    "if": "lan",
                    "starts": "",
                    "ends": "",
                    "active_status": "static",
                    "online_status": "active/online" if m["mac"] in online else "idle/offline",
                    "descr": m["descr"] or None,
                }
            )
        out.sort(key=lambda r: ipaddress.ip_address(r["ip"]))
        return [{"id": i, **r} for i, r in enumerate(out)]

    def dhcp_servers(self, snap: Snapshot) -> list[dict[str, Any]]:
        return [
            {
                "id": "lan",
                "interface": "lan",
                "enable": True,
                "range_from": LAB_POOL[0],
                "range_to": LAB_POOL[1],
                "domain": DOMAIN,
                "failover_peerip": "",
                "mac_allow": [],
                "mac_deny": [],
                "domainsearchlist": [],
                "defaultleasetime": 7200,
                "maxleasetime": 86400,
                "gateway": "",
                "dnsserver": [],
                "winsserver": [],
                "ntpserver": [],
                "staticarp": False,
                "ignorebootp": False,
                "ignoreclientuids": False,
                "nonak": False,
                "disablepingcheck": False,
                "dhcpleaseinlocaltime": False,
                "statsgraph": False,
                "denyunknown": None,
                "pool": [],
                "numberoptions": [],
                "staticmap": self.static_maps(snap),
            }
        ]

    def services(self, snap: Snapshot) -> list[dict[str, Any]]:
        return [
            {"id": i, "name": name, "action": None, "description": d, "enabled": True, "status": True}
            for i, (name, d) in enumerate(SERVICES)
        ]

    def states(self, snap: Snapshot) -> dict[str, Any]:
        rate = sum(snap.port(NODE, nic).rx_rate + snap.port(NODE, nic).tx_rate for _, _, nic in IFACES)
        hosts = len([1 for _, _, iface in snap.arp(NODE) if iface == "vtnet1"])
        current = int(38 + 22 * hosts + rate / 2500 + h01("states", snap.b) * 30)
        return {"maximumstates": MAX_STATES, "defaultmaximumstates": MAX_STATES, "currentstates": current}

    def empty(self, snap: Snapshot) -> list[Any]:
        return []

    def wireguard(self, snap: Snapshot) -> list[Any]:
        raise Dependency("The requested action requires the `pfSense-pkg-WireGuard` package but it is not installed.")


class Dependency(Exception):
    """An endpoint of a package that is not installed (the REST API answers 424)."""


ROUTES: dict[str, tuple[Any, bool]] = {
    "/api/v2/status/system": (PfSense.system_status, False),
    "/api/v2/system/version": (PfSense.version, False),
    "/api/v2/system/hostname": (PfSense.hostname, False),
    "/api/v2/system/restapi/version": (PfSense.restapi_version, False),
    "/api/v2/status/interfaces": (PfSense.interfaces, True),
    "/api/v2/status/gateways": (PfSense.gateways, True),
    "/api/v2/diagnostics/arp_table": (PfSense.arp, True),
    "/api/v2/status/dhcp_server/leases": (PfSense.leases, True),
    "/api/v2/services/dhcp_servers": (PfSense.dhcp_servers, True),
    "/api/v2/status/services": (PfSense.services, True),
    "/api/v2/interface/vlans": (PfSense.empty, True),
    "/api/v2/firewall/states/size": (PfSense.states, False),
    "/api/v2/status/openvpn/servers": (PfSense.empty, True),
    "/api/v2/status/openvpn/clients": (PfSense.empty, True),
    "/api/v2/status/ipsec/sas": (PfSense.empty, True),
    "/api/v2/status/wireguard/tunnels": (PfSense.wireguard, True),
}
