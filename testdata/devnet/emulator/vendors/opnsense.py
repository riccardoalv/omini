"""OPNsense 26.7 REST API of HQ's HA pair (fw-hq-01 / fw-hq-02, Deciso DEC2752).

Answers the endpoints the Omini OPNsense plugin reads, in the shapes OPNsense's
controllers return (snake_case paths, as on 25.7+), with HTTP basic auth
(API key / secret). Everything that moves comes from the world snapshot:
ports and counters, CARP state, ARP, Kea leases (HA hot-standby: both nodes
serve the same lease database), gateways, tunnels, health and firmware.
"""

from __future__ import annotations

import base64
import hashlib
import ipaddress
import json
import time
import uuid
from collections.abc import Callable
from typing import Any

from ..model import Node, Port
from ..server import HttpService, Request, Response
from ..world import WEEK0, Snapshot, h01

# --- static dressing (consistent with acme.py) --------------------------------------------------------------------

FQDN = "{name}.acme.local"
FREEBSD = "FreeBSD 14.3-RELEASE-p4"
OPENSSL = "OpenSSL 3.0.17"
LOCAL_OFFSET = -3 * 3600

FIBER_GW = "203.0.113.1"  # the ISP's PPPoE concentrator
LTE_GW = "192.168.8.1"  # lte-gw01
BRANCH_ENDPOINT = "198.51.100.20:51820"  # br-gw01's WAN
WG_PORT = 51820

# IT staff with a road-warrior WireGuard peer (they connect at night).
ROAD_WARRIORS = [
    ("Thiago Fernandes (notebook)", "10.255.1.2/32", "203.0.113.171"),
    ("André Rodrigues (notebook)", "10.255.1.3/32", "203.0.113.188"),
    ("Bruno Silva (celular)", "10.255.1.4/32", "203.0.113.203"),
]

# (id, description, locked); "ddclient" is stopped on the CARP backup (it has no WAN address to publish).
SERVICES = [
    ("configd", "System Configuration Daemon", 1),
    ("cron", "Cron", 0),
    ("ddclient", "Dynamic DNS client", 0),
    ("dpinger", "Gateway Monitoring Daemon", 0),
    ("kea", "Kea DHCP", 0),
    ("login", "Users and Groups", 1),
    ("monit", "System Monitoring", 0),
    ("ntpd", "Network Time Daemon", 0),
    ("openssh", "Secure Shell Daemon", 0),
    ("openvpn/1", "OpenVPN server: Legacy remote access", 0),
    ("pf", "Packet Filter", 1),
    ("strongswan", "IPsec VPN", 0),
    ("syslog-ng", "Syslog-ng Daemon", 0),
    ("unbound", "Unbound DNS", 0),
    ("wireguard", "WireGuard", 0),
]

STATE_LIMIT = 1_631_000  # pf's default: 10 % of RAM


def road_warrior_sessions(name: str, t: float) -> list[tuple[float, float]]:
    """A road warrior's night sessions (start, end) on weekdays of the last two weeks, started by t.
    The schedule repeats weekly, like the rest of the world."""
    today = int((t - WEEK0) // 86400)
    out = []
    for day in range(today - 14, today + 1):
        wd = day % 7
        if wd >= 5 or h01("rw", name, wd) < 0.35:
            continue
        start = WEEK0 + day * 86400 + (19.5 + h01("rw-start", name, wd) * 3) * 3600
        end = start + (0.5 + h01("rw-dur", name, wd) * 2) * 3600
        if start <= t:
            out.append((start, end))
    return out


def _uuid(*parts: Any) -> str:
    return str(uuid.uuid5(uuid.NAMESPACE_URL, "devnet/opnsense/" + "/".join(map(str, parts))))


def _wg_key(*parts: Any) -> str:
    return base64.b64encode(hashlib.sha256("/".join(map(str, parts)).encode()).digest()).decode()


def _ctime(t: float) -> str:
    """OPNsense's date format, local time: 'Tue Oct 6 10:00:00 -03 2026'."""
    lt = time.gmtime(t + LOCAL_OFFSET)
    return f"{time.strftime('%a %b', lt)} {lt.tm_mday} {time.strftime('%H:%M:%S', lt)} -03 {lt.tm_year}"


def _uptime(seconds: float) -> str:
    s = max(int(seconds), 0)
    d, s = divmod(s, 86400)
    h, s = divmod(s, 3600)
    m, s = divmod(s, 60)
    return f"{d} days, {h:02d}:{m:02d}:{s:02d}"


def _media(speed: int | None, sfp: bool) -> str:
    if not speed:
        return "autoselect"
    if sfp:
        return f"{speed // 1000}Gbase-SR <full-duplex>"
    if speed >= 10000:
        return f"{speed // 1000}Gbase-T <full-duplex>"
    if speed in (2500, 5000):
        return f"{speed}Base-T <full-duplex>"
    return f"{speed}baseT <full-duplex>"


class OPNsense(HttpService):
    """One firewall of the pair (self.node)."""

    # -- plumbing ---------------------------------------------------------------------------------------------------

    def handle(self, req: Request, snap: Snapshot) -> Response:
        if req.path in ("/", "/index.php", "/ui/"):
            return Response.html_title("Login | OPNsense")
        if not req.path.startswith("/api/"):
            return Response.html_title("Page not found | OPNsense", status=404)
        if not self._authorized(req):
            return Response.json({"status": 401, "message": "Authentication Failed"}, 401)
        route = ROUTES.get(req.path.rstrip("/"))
        if route is None:
            return Response.json({"errorMessage": "Endpoint not found", "errorTitle": "Error"}, 404)
        if req.method not in ("GET", "HEAD"):
            return Response.json({"errorMessage": "Method not allowed"}, 405)
        out = route(self, snap)
        if isinstance(out, Response):
            return out
        return Response.json(out)

    def _authorized(self, req: Request) -> bool:
        auth = req.headers.get("authorization", "")
        if not auth.lower().startswith("basic "):
            return False
        try:
            key, _, secret = base64.b64decode(auth[6:].strip()).decode().partition(":")
        except Exception:
            return False
        return key == self.secrets.get("api_key") and secret == self.secrets.get("api_secret")

    @property
    def fw(self) -> str:
        return self.node

    def _n(self, snap: Snapshot) -> Node:
        return snap.node(self.fw)

    def _index(self) -> int:
        return 0 if self.fw.endswith("01") else 1

    def _master(self, snap: Snapshot) -> bool:
        return snap.carp_state(self.fw) == "MASTER"

    # -- interfaces -------------------------------------------------------------------------------------------------

    def _assigned(self, n: Node) -> list[tuple[str, str, str]]:
        """(device, identifier, description) of the assigned interfaces, in config order."""
        out = [
            ("pppoe0", "wan", "WAN_FIBER"),
            ("vlan0.10", "lan", "MGMT"),
            ("igc1", "opt1", "WAN_LTE"),
            ("igc2", "opt2", "DMZ"),
            ("igc3", "opt3", "SYNC"),
            ("wg0", "opt4", "WG_HQ"),
        ]
        k = 5
        for p in sorted(n.ports.values(), key=lambda p: p.vlan or 0):
            if p.kind == "vlan" and p.vlan != 10:
                out.append((p.name, f"opt{k}", p.descr.replace("-", "_")))
                k += 1
        return out

    def _counters(self, snap: Snapshot, p: Port) -> tuple[int, int, int, int, int, int]:
        """rx/tx bytes, rx/tx packets, rx/tx errors of a port."""
        if p.kind == "wg":
            rx, tx = self._wg_bytes(snap)
            return rx, tx, rx // 180, tx // 180, 0, 0
        st = snap.port(self.fw, p.name)
        return st.rx_bytes, st.tx_bytes, st.rx_packets, st.tx_packets, st.rx_errors, st.tx_errors

    def _wg_up(self, snap: Snapshot) -> bool:
        # Snapshot.tunnel("wg-branch") and port(fw, "wg0") recurse into each other: read the schedule directly.
        return bool(snap.w.tunnel_up["wg-branch"][snap.b])

    def _wg_bytes(self, snap: Snapshot) -> tuple[int, int]:
        w = snap.w
        key = (self.fw, "wg0")
        boot = snap.boot(self.fw)
        out = []
        for d, arrs in (("rx", w.rx), ("tx", w.tx)):
            arr = arrs.get(key)
            if arr is None:
                out.append(0)
                continue
            v = (w.integral(arr, (d, *key), snap.t) - w.integral(arr, (d, *key), boot)) / snap.speed
            out.append(int(max(v, 0)))
        return out[0], out[1]

    def _up(self, snap: Snapshot, p: Port) -> bool:
        if p.kind == "wg":  # wg0 stays up while the tunnel is down (no handshakes, that is all)
            return snap.up(self.fw)
        return snap.port_up(self.fw, p.name)

    def _status(self, snap: Snapshot, p: Port) -> str:
        if p.kind in ("ethernet", "sfp"):
            return "up" if snap.port_up(self.fw, p.name) else "no carrier"
        if p.kind == "ppp":
            return "up" if snap.port_up(self.fw, p.name) else "down"
        if p.kind == "wg":
            return "up"
        if p.kind == "lag":
            return "up" if snap.port_up(self.fw, p.name) else "no carrier"
        return "up"

    def _carp(self, snap: Snapshot, p: Port) -> list[dict[str, Any]]:
        """The CARP VIP of a VLAN interface (vhid = VLAN id) or of the DMZ port."""
        vid = p.vlan if p.kind == "vlan" else (90 if p.name == "igc2" else None)
        if vid is None:
            return []
        net = ipaddress.ip_interface(p.ips[0]).network
        vip = str(net.network_address + 1)
        return [
            {
                "vhid": str(vid),
                "advbase": "1",
                "advskew": "0" if self._index() == 0 else "100",
                "status": snap.carp_state(self.fw),
                "subnet": vip,
                "subnet_bits": net.prefixlen,
                "mode": "carp",
            }
        ]

    def _row(self, snap: Snapshot, p: Port, ident: str, descr: str) -> dict[str, Any]:
        up = self._up(snap, p)
        speed = snap.port_speed(self.fw, p.name) if up and p.kind != "wg" else None
        status = self._status(snap, p)
        physical = p.kind in ("ethernet", "sfp")
        hw = p.mac or "00:00:00:00:00:00"
        if p.name == "ax1":  # a lagg member takes the lagg's MAC; its own stays in macaddr_hw
            hw = _offset(p.mac, 1)
        mac = p.mac or "00:00:00:00:00:00"
        ipv4: list[dict[str, Any]] = []
        ipv6: list[dict[str, Any]] = []
        gateways: list[str] = []
        link_type = "none"
        if p.kind == "ppp":
            link_type = "pppoe"
            if up:  # the CARP backup keeps its dial-up interfaces disconnected
                ipv4 = [{"ipaddr": a, "subnetbits": 32} for a in p.ips]
                gateways = [FIBER_GW]
        else:
            ipv4 = [{"ipaddr": a, "subnetbits": int(a.split("/")[1])} for a in p.ips if not a.startswith("127.")]
            for c in self._carp(snap, p):
                ipv4.append(
                    {"ipaddr": f"{c['subnet']}/{c['subnet_bits']}", "subnetbits": c["subnet_bits"], "vhid": c["vhid"]}
                )
            if p.ips and p.kind in ("vlan", "ethernet") and p.mac:
                ipv6 = [{"ipaddr": f"fe80::{_eui64(p.mac)}%{p.name}/64", "subnetbits": 64, "link-local": True}]
            if p.name == "igc1":
                gateways = [LTE_GW]
                link_type = "static"
            elif p.ips:
                link_type = "static"
        row: dict[str, Any] = {
            "device": p.name,
            "identifier": ident,
            "description": descr or "Unassigned Interface",
            "enabled": bool(ident),
            "status": status,
            "macaddr": mac,
            "macaddr_hw": hw,
            "is_physical": physical,
            "mtu": str(p.mtu if p.kind != "ppp" else 1492),
            "link_type": link_type,
            "ipv4": ipv4,
            "ipv6": ipv6,
            "gateways": gateways,
            "flags": ["up", "broadcast", "running", "simplex", "multicast"] if status == "up" else ["broadcast"],
            "carp": self._carp(snap, p),
        }
        if physical:
            row["media"] = _media(speed, p.kind == "sfp") if up else "autoselect"
            row["media_raw"] = f"Ethernet autoselect ({row['media']})" if up else "Ethernet autoselect"
            row["supported_media"] = (
                ["autoselect", "10Gbase-SR\tfull-duplex", "1000baseSX\tfull-duplex"]
                if p.kind == "sfp"
                else [
                    "autoselect",
                    "2500Base-T\tfull-duplex",
                    "1000baseT\tfull-duplex",
                    "1000baseT",
                    "100baseTX\tfull-duplex",
                    "100baseTX",
                    "10baseT/UTP\tfull-duplex",
                    "10baseT/UTP",
                ]
            )
            if p.kind == "sfp":
                row["sfp"] = self._sfp(snap, p)
        elif p.kind == "lag":
            row["media"] = "autoselect"
            row["media_raw"] = "Ethernet autoselect"
            row["laggproto"] = "lacp"
            row["laggport"] = {
                m: {"flags": ["active", "collecting", "distributing"]} for m in p.members if snap.port_up(self.fw, m)
            }
        elif p.kind == "vlan":
            row["media"] = ""
            row["vlan"] = {"tag": str(p.vlan), "proto": "802.1q", "pcp": "0", "parent": p.parent}
        if p.kind != "vlan":  # VLAN counters come from the traffic report
            rx, tx, rxp, txp, rxe, txe = self._counters(snap, p)
            line = (speed or 0) * 1_000_000 if p.kind in ("ethernet", "sfp", "lag") else 0
            row["statistics"] = {
                "device": p.name,
                "driver": p.name.rstrip("0123456789"),
                "index": str(p.index),
                "flags": "0x8843",
                "link state": "2" if up else "1",
                "mtu": row["mtu"],
                "metric": "0",
                "line rate": f"{line} bit/s",
                "packets received": str(rxp),
                "input errors": str(rxe),
                "packets transmitted": str(txp),
                "output errors": str(txe),
                "collisions": "0",
                "bytes received": str(rx),
                "bytes transmitted": str(tx),
                "multicasts received": str(rxp // 400),
                "multicasts transmitted": str(txp // 900),
                "input queue drops": "0",
                "send queue drops": "0",
                "uptime": str(snap.uptime(self.fw)),
            }
        return row

    def _sfp(self, snap: Snapshot, p: Port) -> dict[str, Any]:
        """A 10GBASE-SR SFP+ as legacy_interfaces_details() reads it from `ifconfig -v` (identity, temperature,
        voltage: plain SFP modules print no per-lane power)."""
        temp = 31.0 + h01("sfp", self.fw, p.name) * 4 + 6 * snap.cpu(self.fw) / 100
        return {
            "plugged": "SFP/SFP+/SFP28 10G Base-SR (LC)",
            "vendor": "FS",
            "part_number": "SFP-10GSR-85",
            "serial_number": f"F2306{int(h01('sn', self.fw, p.name) * 1e7):07d}",
            "manufacturing_date": "2023-06-14",
            "temperature": f"{temp:.2f} C",
            "voltage": f"{3.28 + h01('v', self.fw, p.name) * 0.04:.2f} ",
        }

    def _ports(self, snap: Snapshot) -> list[tuple[Port, str, str]]:
        n = self._n(snap)
        assigned = {dev: (ident, descr) for dev, ident, descr in self._assigned(n)}
        order = ["igc0", "igc1", "igc2", "igc3", "ax0", "ax1", "lagg0"]
        order += sorted((p.name for p in n.ports.values() if p.kind == "vlan"), key=lambda x: int(x.split(".")[1]))
        order += ["pppoe0", "wg0", "enc0", "lo0"]
        out = []
        for name in order:
            ident, descr = assigned.get(name, ("", ""))
            out.append((n.ports[name], ident, descr))
        return out

    def interfaces_info(self, snap: Snapshot) -> dict[str, Any]:
        rows = []
        for p, ident, descr in self._ports(snap):
            if p.kind == "loopback":
                rows.append(
                    {
                        "device": "lo0",
                        "identifier": "lo0",
                        "description": "Loopback",
                        "status": "up",
                        "ipv4": [{"ipaddr": "127.0.0.1/8"}],
                        "ipv6": [{"ipaddr": "::1/128"}],
                    }
                )
                continue
            if p.kind == "ipsec":
                rows.append(
                    {
                        "device": "enc0",
                        "identifier": "",
                        "description": "Unassigned Interface",
                        "status": "up",
                        "ipv4": [],
                        "ipv6": [],
                    }
                )
                continue
            rows.append(self._row(snap, p, ident, descr))
        rows.append(
            {
                "device": "pfsync0",
                "identifier": "",
                "description": "Unassigned Interface",
                "status": "up",
                "ipv4": [],
                "ipv6": [],
            }
        )
        rows.append(
            {
                "device": "pflog0",
                "identifier": "",
                "description": "Unassigned Interface",
                "status": "up",
                "ipv4": [],
                "ipv6": [],
            }
        )
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    def vlan_settings(self, snap: Snapshot) -> dict[str, Any]:
        n = self._n(snap)
        rows = []
        for p in sorted(n.ports.values(), key=lambda p: p.vlan or 0):
            if p.kind != "vlan":
                continue
            rows.append(
                {
                    "uuid": _uuid("vlan", p.vlan),
                    "if": p.parent,
                    "tag": str(p.vlan),
                    "pcp": "0",
                    "proto": "",
                    "descr": p.descr,
                    "vlanif": p.name,
                }
            )
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    def traffic(self, snap: Snapshot) -> dict[str, Any]:
        n = self._n(snap)
        out = {}
        for dev, ident, descr in self._assigned(n):
            rx, tx, rxp, txp, rxe, txe = self._counters(snap, n.ports[dev])
            out[ident] = {
                "device": dev,
                "name": descr,
                "bytes received": str(rx),
                "bytes transmitted": str(tx),
                "packets received": str(rxp),
                "packets transmitted": str(txp),
                "input errors": str(rxe),
                "output errors": str(txe),
                "collisions": "0",
                "send queue drops": "0",
            }
        return {"interfaces": out, "time": round(snap.t, 4)}

    # -- ARP and DHCP -----------------------------------------------------------------------------------------------

    def arp(self, snap: Snapshot) -> dict[str, Any]:
        n = self._n(snap)
        names = {lease.mac: lease.hostname for lease in snap.leases("hq")}
        descr = {dev: d for dev, _i, d in self._assigned(n)}
        rows = []
        for ip, m, intf in snap.arp(self.fw):
            rows.append(
                {
                    "mac": m,
                    "ip": ip,
                    "intf": intf,
                    "intf_description": descr.get(intf, ""),
                    "expired": False,
                    "expires": 60 + int(h01("arp", ip, snap.b) * 1140),
                    "permanent": False,
                    "type": "ethernet",
                    "manufacturer": "",
                    "hostname": names.get(m, ""),
                }
            )
        for p in n.ports.values():  # the firewall's own addresses
            if not p.mac or p.kind in ("ppp", "loopback"):
                continue
            for a in p.ips:
                rows.append(
                    {
                        "mac": p.mac,
                        "ip": a.split("/")[0],
                        "intf": p.name,
                        "intf_description": descr.get(p.name, ""),
                        "expired": False,
                        "permanent": True,
                        "type": "ethernet",
                        "manufacturer": "Intel Corporate",
                        "hostname": "",
                    }
                )
        rows.sort(key=lambda r: ipaddress.ip_address(r["ip"]))
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    def _subnets(self, snap: Snapshot) -> list[tuple[int, Any]]:
        """Kea subnets: every routed HQ VLAN with an address pool, (subnet id, Vlan)."""
        vlans = sorted(
            (v for (site, _), v in snap.design.vlans.items() if site == "hq" and v.dhcp and v.subnet),
            key=lambda v: v.id,
        )
        return list(enumerate(vlans, start=1))

    def kea_leases(self, snap: Snapshot) -> dict[str, Any]:
        n = self._n(snap)
        ids = {v.id: (sid, v) for sid, v in self._subnets(snap)}
        assigned = {dev: (ident, d) for dev, ident, d in self._assigned(n)}
        rows = []
        for lease in snap.leases("hq"):
            sid, _v = ids.get(lease.vlan, (0, None))
            dev = "vlan0.10" if lease.vlan == 10 else f"vlan0.{lease.vlan}"
            ident, d = assigned.get(dev, ("", ""))
            rows.append(
                {
                    "address": lease.ip,
                    "hwaddr": lease.mac,
                    "client_id": "01:" + lease.mac,
                    "valid_lifetime": str(int(lease.end - lease.start)),
                    "expire": str(int(lease.end)),
                    "subnet_id": str(sid),
                    "fqdn_fwd": "0",
                    "fqdn_rev": "0",
                    "hostname": lease.hostname or "",
                    "state": "0",
                    "user_context": "",
                    "pool_id": "0",
                    "if": dev,
                    "if_descr": d,
                    "if_name": ident,
                    "is_reserved": "1" if lease.static else "0",
                }
            )
        return {
            "total": len(rows),
            "rowCount": len(rows),
            "current": 1,
            "rows": rows,
            "interfaces": {ident: d for _dev, ident, d in self._assigned(n)},
        }

    def kea_subnets(self, snap: Snapshot) -> dict[str, Any]:
        rows = []
        for sid, v in self._subnets(snap):
            a, b = v.dhcp
            rows.append(
                {
                    "uuid": _uuid("kea-subnet", v.id),
                    "subnet": v.subnet,
                    "next_server": "",
                    "option_data_autocollect": "1",
                    "pools": f"{a}-{b}",
                    "description": v.name,
                    "subnet_id": str(sid),
                }
            )
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    # -- gateways and VPNs ------------------------------------------------------------------------------------------

    def gateways(self, snap: Snapshot) -> dict[str, Any]:
        fiber = snap.gateway("fiber")
        if not self._master(snap):
            f = {
                "name": "WAN_FIBER_PPPOE",
                "address": "~",
                "status": "none",
                "status_translated": "Pending",
                "loss": "~",
                "delay": "~",
                "stddev": "~",
                "monitor": "~",
            }
        elif not fiber["up"]:
            f = {
                "name": "WAN_FIBER_PPPOE",
                "address": "~",
                "status": "down",
                "status_translated": "Offline",
                "loss": "100.0 %",
                "delay": "0.0 ms",
                "stddev": "0.0 ms",
                "monitor": "~",
            }
        else:
            f = {
                "name": "WAN_FIBER_PPPOE",
                "address": FIBER_GW,
                "status": "none",
                "status_translated": "Online",
                "loss": f"{fiber['loss_pct']:.1f} %",
                "delay": f"{fiber['latency_ms']:.1f} ms",
                "stddev": f"{0.2 + h01('sd', snap.b) * 0.5:.1f} ms",
                "monitor": FIBER_GW,
            }
        lte = snap.gateway("lte")
        delay, loss = lte["latency_ms"] or 0.0, lte["loss_pct"] or 0.0
        if not lte["up"]:
            status, text = "down", "Offline"
        elif delay >= 200 and loss >= 10:
            status, text = "delay+loss", "Latency, Packetloss"
        elif loss >= 10:
            status, text = "loss", "Packetloss"
        elif delay >= 200:
            status, text = "delay", "Latency"
        else:
            status, text = "none", "Online"
        lt = {
            "name": "WAN_LTE",
            "address": LTE_GW,
            "status": status,
            "status_translated": text,
            "loss": f"{loss:.1f} %",
            "delay": f"{delay:.1f} ms",
            "stddev": f"{1.5 + h01('sd-lte', snap.b) * (20 if status != 'none' else 4):.1f} ms",
            "monitor": LTE_GW,
        }
        return {"items": [f, lt], "status": "ok"}

    def wireguard(self, snap: Snapshot) -> dict[str, Any]:
        master = self._master(snap)
        rows: list[dict[str, Any]] = [
            {
                "if": "wg0",
                "type": "interface",
                "public-key": _wg_key("wg0", "hq"),
                "listen-port": str(WG_PORT),
                "fwmark": "off",
                "endpoint": str(WG_PORT),
                "status": "up",
                "name": "WG_HQ",
                "latest-handshake-age": None,
                "latest-handshake-epoch": None,
                "peer-status": "offline",
                "ifname": "WG_HQ",
            }
        ]

        def peer(name: str, key: str, endpoint: str, allowed: str, hs: float, rx: int, tx: int, keepalive: str) -> None:
            online = bool(hs) and snap.t - hs < 300
            rows.append(
                {
                    "if": "wg0",
                    "type": "peer",
                    "public-key": key,
                    "endpoint": endpoint,
                    "allowed-ips": allowed,
                    "latest-handshake": int(hs),
                    "transfer-rx": rx,
                    "transfer-tx": tx,
                    "persistent-keepalive": keepalive,
                    "name": name,
                    "latest-handshake-age": int(snap.t - hs) if hs else None,
                    "latest-handshake-epoch": _iso_local(hs) if hs else None,
                    "peer-status": "online" if online else "offline",
                    "ifname": "WG_HQ",
                }
            )

        # The site-to-site peer: HQ initiates (its endpoint is configured), only the CARP master holds the session.
        if master:
            up = self._wg_up(snap)
            s = snap.s
            hs = snap.t - (s % 90) if up else snap.t - (s % 86400 - 16 * 3600)
            rx, tx = self._wg_bytes(snap)
        else:
            hs, rx, tx = 0.0, 0, 0
        peer(
            "Filial Campinas",
            _wg_key("peer", "br-gw01"),
            BRANCH_ENDPOINT,
            "10.255.0.2/32,10.20.0.0/16",
            hs,
            rx,
            tx,
            "25",
        )
        for name, addr, ip in ROAD_WARRIORS:
            sessions = road_warrior_sessions(name, snap.t) if master else []
            boot = snap.boot(self.fw)
            hs, rx, tx, endpoint = 0.0, 0, 0, "(none)"
            for start, end in sessions:
                if end < boot:
                    continue
                stop = min(snap.t, end)
                hs = stop - ((stop - start) % 120)
                rx += int((stop - max(start, boot)) * (15_000 + h01("rw-rx", name) * 65_000))
                tx += int((stop - max(start, boot)) * (60_000 + h01("rw-tx", name) * 340_000))
                endpoint = f"{ip}:{40000 + int(h01('rw-port', name, start) * 20000)}"
            peer(name, _wg_key("peer", name), endpoint, addr, hs, rx, tx, "off")
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    def openvpn(self, snap: Snapshot) -> dict[str, Any]:
        """A legacy remote-access server left enabled, with nobody connected."""
        sid = _uuid("openvpn", "legacy")
        rows = [
            {
                "socket": f"/var/etc/openvpn/instance-{sid}.sock",
                "status": "ok",
                "type": "server",
                "id": sid,
                "description": "Legacy remote access",
                "common_name": None,
                "real_address": None,
                "virtual_address": None,
                "virtual_ipv6_address": None,
                "bytes_received": None,
                "bytes_sent": None,
                "connected_since": None,
                "connected_since__time_t_": None,
                "username": None,
                "client_id": None,
                "peer_id": None,
                "data_channel_cipher": None,
                "is_client": None,
                "timestamp": None,
                "service_id": "openvpn/1",
            }
        ]
        return {"total": 1, "rowCount": 1, "current": 1, "rows": rows}

    def ipsec(self, snap: Snapshot) -> dict[str, Any]:
        tun = snap.tunnel("ipsec-store")
        up = bool(tun["up"]) and self._master(snap)
        ikeid = _uuid("ipsec", "loja-santos")
        local = "203.0.113.45"
        row = {
            "local-addrs": local,
            "remote-addrs": "%any",
            "children": "",
            "local-id": "hq.acme.example",
            "remote-id": "loja-santos.acme.example",
            "version": "IKEv2",
            "routed": True,
            "local-class": "pre-shared key",
            "remote-class": "pre-shared key",
            "ikeid": ikeid,
            "phase1desc": "Loja Santos",
            "name": ikeid,
            "connected": up,
            "install-time": int(snap.s % 3600) if up else None,
            "bytes-in": tun["rx_bytes"] if up else 0,
            "bytes-out": tun["tx_bytes"] if up else 0,
            "packets-in": tun["rx_bytes"] // 700 if up else 0,
            "packets-out": tun["tx_bytes"] // 700 if up else 0,
        }
        return {"total": 1, "rowCount": 1, "current": 1, "rows": [row]}

    # -- system -----------------------------------------------------------------------------------------------------

    def system_information(self, snap: Snapshot) -> dict[str, Any]:
        fw = snap.firmware(self.fw)
        return {
            "name": FQDN.format(name=self.fw),
            "versions": [f"OPNsense {fw['current']}-amd64", FREEBSD, OPENSSL],
            "updates": "Click to check for updates.",
        }

    def system_resources(self, snap: Snapshot) -> dict[str, Any]:
        total = 16_826_978_304
        used = int(total * snap.mem(self.fw) / 100)
        arc = int(total * 0.06)
        return {
            "memory": {
                "total": str(total),
                "total_frmt": str(total // 2**20),
                "used": used,
                "used_frmt": str(used // 2**20),
                "arc": str(arc),
                "arc_frmt": str(arc // 2**20),
                "arc_txt": f"ARC size {arc // 2**20} MB",
            }
        }

    def _load(self, snap: Snapshot) -> list[float]:
        cores = self._n(snap).extra.get("cores", 8)
        cpu = snap.cpu(self.fw)
        return [round(cores * cpu / 100 * f, 2) for f in (1.0, 0.92, 0.85)]

    def system_time(self, snap: Snapshot) -> dict[str, Any]:
        boot = snap.boot(self.fw)
        return {
            "uptime": _uptime(snap.t - boot),
            "datetime": _ctime(snap.t),
            "boottime": _ctime(boot),
            "loadavg": ", ".join(f"{v:.2f}" for v in self._load(snap)),
        }

    def system_swap(self, snap: Snapshot) -> dict[str, Any]:
        return {"swap": [{"device": "/dev/gpt/swapfs", "total": "8388608", "used": "0"}]}

    def system_disk(self, snap: Snapshot) -> dict[str, Any]:
        pool = 225_485_783_040
        week = snap.s / (7 * 86400)
        ds = [
            ("zroot/ROOT/default", "/", 4_310_000_000 + int(h01("root", self.fw) * 2e8)),
            ("zroot/var/log", "/var/log", 2_900_000_000 + int(week * 1.4e9)),
            ("zroot/tmp", "/tmp", 3_100_000),
            ("zroot/var/db", "/var/db", 610_000_000 + int(week * 2.5e8)),
            ("zroot/usr/home", "/home", 1_200_000),
        ]
        used = sum(u for *_, u in ds)
        free = pool - used
        rows = [
            {
                "device": d,
                "type": "zfs",
                "total_bytes": u + free,
                "used_bytes": u,
                "available_bytes": free,
                "used_pct": round(u * 100 / (u + free)),
                "mountpoint": m,
            }
            for d, m, u in ds
        ]
        rows.insert(
            1,
            {
                "device": "/dev/gpt/efiboot0",
                "type": "msdosfs",
                "total_bytes": 268_419_072,
                "used_bytes": 1_327_104,
                "available_bytes": 267_091_968,
                "used_pct": 0,
                "mountpoint": "/boot/efi",
            },
        )
        rows.append(
            {
                "device": "devfs",
                "type": "devfs",
                "total_bytes": 0,
                "used_bytes": 0,
                "available_bytes": 0,
                "used_pct": 100,
                "mountpoint": "/dev",
            }
        )
        return {"devices": rows}

    def system_temperature(self, snap: Snapshot) -> list[dict[str, Any]]:
        cores = self._n(snap).extra.get("cores", 8)
        rows = [
            {
                "device": f"dev.cpu.{i}.temperature",
                "device_seq": str(i),
                "temperature": f"{snap.temperature(self.fw, f'cpu{i}'):.1f}",
                "type": "cpu",
                "type_translated": "CPU",
            }
            for i in range(cores)
        ]
        rows.append(
            {
                "device": "hw.acpi.thermal.tz0.temperature",
                "device_seq": "0",
                "temperature": f"{27.9 + h01('tz', self.fw) * 3:.1f}",
                "type": "zone",
                "type_translated": "Zone",
            }
        )
        return rows

    def cpu_stream(self, snap: Snapshot) -> Response:
        cpu = snap.cpu(self.fw)
        avg = round(4 + h01("cpu-avg", self.fw) * 6, 1)

        def event(total: float) -> str:
            user = round(total * 0.55, 1)
            sys_ = round(total * 0.3, 1)
            intr = round(total - user - sys_, 1)
            fields = {"total": total, "user": user, "nice": 0, "sys": sys_, "intr": intr, "idle": round(100 - total, 1)}
            return f"data: {json.dumps(fields, separators=(',', ':'))}\n\n"

        return Response.text(
            event(avg) + event(cpu), ctype="text/event-stream", headers=[("Cache-Control", "no-cache")]
        )

    def firmware_status(self, snap: Snapshot) -> dict[str, Any]:
        fw = snap.firmware(self.fw)
        cur, latest = fw["current"], fw["latest"]
        day0 = snap.t - snap.s % 86400  # local midnight
        check = day0 + 9 * 3600 + 3 if snap.s % 86400 >= 9 * 3600 else day0 - 86400 + 9 * 3600 + 3
        if fw["pending"]:
            pkgs = [
                {"name": name, "current_version": cur, "new_version": latest, "repository": "OPNsense"}
                for name in ("opnsense", "kernel", "base")
            ]
            check_info = {
                "connection": "ok",
                "download_size": "212.4MiB",
                "last_check": _ctime(check),
                "new_packages": [],
                "product_version": latest,
                "reinstall_packages": [],
                "repository": "ok",
                "updates": "3",
                "upgrade_needs_reboot": "1" if fw["needs_reboot"] else "0",
                "upgrade_packages": pkgs,
                "downgrade_packages": [],
                "remove_packages": [],
            }
            msg = "There are 3 updates available, total download size is 212.4MiB. This update requires a reboot."
            status = "update"
        else:
            check_info = {
                "connection": "ok",
                "download_size": "",
                "last_check": _ctime(check),
                "new_packages": [],
                "product_version": cur,
                "reinstall_packages": [],
                "repository": "ok",
                "updates": "0",
                "upgrade_needs_reboot": "0",
                "upgrade_packages": [],
                "downgrade_packages": [],
                "remove_packages": [],
            }
            msg = "There are no updates available on the selected mirror."
            status = "none"
        return {
            "status_msg": msg,
            "status": status,
            "product": {
                "product_version": cur,
                "product_latest": latest,
                "product_name": "OPNsense",
                "product_nickname": "Visionary Viper",
                "product_series": "26.7",
                "product_check": check_info,
            },
        }

    def services(self, snap: Snapshot) -> dict[str, Any]:
        backup = not self._master(snap)
        rows = [
            {
                "id": sid,
                "locked": locked,
                "running": 0 if (backup and sid == "ddclient") else 1,
                "description": d,
                "name": sid.split("/")[0],
            }
            for sid, d, locked in SERVICES
        ]
        return {"total": len(rows), "rowCount": len(rows), "current": 1, "rows": rows}

    def pf_info(self, snap: Snapshot) -> dict[str, Any]:
        biz = float(snap.w.business[snap.b])
        states = int(7_500 + 64_000 * biz + h01("states", snap.b) * 3_000)  # pfsync: the backup holds them too
        up = snap.uptime(self.fw)
        rate = 900 + 9_000 * biz
        return {
            "info": {
                "uptime": _uptime(up).replace(",", ""),
                "state-table": {
                    "current-entries": {"total": states},
                    "searches": {"total": int(up * rate * 40), "rate": round(rate * 40, 1)},
                    "inserts": {"total": int(up * rate * 0.6), "rate": round(rate * 0.6, 1)},
                    "removals": {"total": int(up * rate * 0.6) - states, "rate": round(rate * 0.6, 1)},
                },
                "counters": {"match": {"total": int(up * rate * 0.7), "rate": round(rate * 0.7, 1)}},
            }
        }

    def pf_memory(self, snap: Snapshot) -> dict[str, Any]:
        return {"memory": {"states": STATE_LIMIT, "src-nodes": STATE_LIMIT, "frags": 5000, "table-entries": 1_000_000}}

    def empty(self, snap: Snapshot) -> dict[str, Any]:
        """dnsmasq is installed (core since 25.7) but disabled: its searches answer with no rows."""
        return {"total": 0, "rowCount": 0, "current": 1, "rows": []}


def _offset(m: str, n: int) -> str:
    v = int(m.replace(":", ""), 16) + n
    s = f"{v:012x}"
    return ":".join(s[i : i + 2] for i in range(0, 12, 2))


def _eui64(m: str) -> str:
    b = bytes.fromhex(m.replace(":", ""))
    e = bytes([b[0] ^ 2, b[1], b[2], 0xFF, 0xFE, b[3], b[4], b[5]])
    return ":".join(f"{int.from_bytes(e[i : i + 2], 'big'):x}" for i in range(0, 8, 2))


def _iso_local(t: float) -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime(t + LOCAL_OFFSET))


ROUTES: dict[str, Callable[[OPNsense, Snapshot], Any]] = {
    "/api/diagnostics/system/system_information": OPNsense.system_information,
    "/api/diagnostics/system/system_resources": OPNsense.system_resources,
    "/api/diagnostics/system/system_time": OPNsense.system_time,
    "/api/diagnostics/system/system_swap": OPNsense.system_swap,
    "/api/diagnostics/system/system_disk": OPNsense.system_disk,
    "/api/diagnostics/system/system_temperature": OPNsense.system_temperature,
    "/api/diagnostics/cpu_usage/stream": OPNsense.cpu_stream,
    "/api/interfaces/overview/interfaces_info/1": OPNsense.interfaces_info,
    "/api/interfaces/overview/interfaces_info": OPNsense.interfaces_info,
    "/api/interfaces/vlan_settings/search_item": OPNsense.vlan_settings,
    "/api/diagnostics/traffic/interface": OPNsense.traffic,
    "/api/diagnostics/interface/search_arp": OPNsense.arp,
    "/api/kea/leases4/search": OPNsense.kea_leases,
    "/api/kea/dhcpv4/search_subnet": OPNsense.kea_subnets,
    "/api/dnsmasq/leases/search": OPNsense.empty,
    "/api/dnsmasq/settings/search_range": OPNsense.empty,
    "/api/routes/gateway/status": OPNsense.gateways,
    "/api/core/firmware/status": OPNsense.firmware_status,
    "/api/core/service/search": OPNsense.services,
    "/api/wireguard/service/show": OPNsense.wireguard,
    "/api/openvpn/service/search_sessions": OPNsense.openvpn,
    "/api/ipsec/sessions/search_phase1": OPNsense.ipsec,
    "/api/diagnostics/firewall/pf_statistics/info": OPNsense.pf_info,
    "/api/diagnostics/firewall/pf_statistics/memory": OPNsense.pf_memory,
}
