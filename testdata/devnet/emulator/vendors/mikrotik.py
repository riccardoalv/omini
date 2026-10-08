"""MikroTik RouterOS v7 REST API (www-ssl service): br-gw01 (RB5009UPr+S+) and sw-stor-01 (CRS518-16XS-2XQ).

The REST API maps console menus to URLs (`/rest/interface/bridge/port`) and
answers JSON where every value is a string ("true", "1500", "1Gbps",
"1w2d3h4m5s"), like RouterOS 7.16. Authentication is HTTP basic on every
request (no session); a wrong username or password answers 401
`{"error":401,"message":"Unauthorized"}`. GET reads a menu (a list, or one
object for singleton menus like /system/resource); `GET /rest/<menu>/<id>`
reads one item by `.id` or name; query parameters filter (`?type=ether`) and
`.proplist` picks fields. POST runs a command: only `print` and the
read-only `/interface/ethernet/monitor` with `once` are accepted from Omini's
read-only user (any other command, PUT, PATCH or DELETE answers 403 "not
enough permissions"). A menu this RouterOS lacks (no `wifi`/`wireless`
package on these boxes) answers 400 "no such command or directory".

All topology data comes from the world snapshot: port state and counters,
the bridge host table (snap.fdb), ARP, DHCP leases of the branch, LLDP
neighbors, CPU/memory/temperatures. Vendor dressing (optics, health sensors,
RouterBOOT, package update status) lives here. sw-stor-01's sfp28-16 is the
failing fiber: its Rx power is snap.fiber_rx_dbm() (about -4.6 dBm on Monday,
-12 dBm by Saturday) and its CRC errors grow with it (the world's counters).
"""

from __future__ import annotations

import base64
import binascii
import hmac
import ipaddress
import json
import time
from typing import Any

from .. import acme
from ..model import Node, Port
from ..server import HttpService, Request, Response
from ..world import Snapshot, h01

VERSION = "7.16.2"
LATEST = "7.16.3"
BUILD_TIME = "2024-11-29 14:17:45"
WEBFIG_TITLE = "RouterOS router configuration page"
TIME_ZONE = "America/Sao_Paulo"

# Per box: what the hardware is (RouterBOARD facts, flash, RAM, sensors).
HARDWARE: dict[str, dict[str, Any]] = {
    "br-gw01": {
        "firmware-type": "70x0",
        "factory-firmware": "7.6",
        "current-firmware": "7.16.2",
        "upgrade-firmware": "7.16.2",
        "total-memory": 1024 * 1024 * 1024,
        "total-hdd-space": 1024 * 1024 * 1024,
        "cpu-frequency": "1400",
        "factory-software": "7.6",
    },
    # The CRS518 got RouterOS 7.16.2, but nobody ran /system/routerboard/upgrade: RouterBOOT is older.
    "sw-stor-01": {
        "firmware-type": "al2",
        "factory-firmware": "7.1.5",
        "current-firmware": "7.15.3",
        "upgrade-firmware": "7.16.2",
        "total-memory": 2 * 1024 * 1024 * 1024,
        "total-hdd-space": 128 * 1024 * 1024,
        "cpu-frequency": "1700",
        "factory-software": "7.1.5",
    },
}

# Optics: 10GBASE-SR modules on the fiber links, passive SFP28 DACs to the Proxmox nodes.
OPTIC = {"vendor": "FS", "part": "SFP-10GSR-85", "revision": "A", "wavelength": "850nm", "type": "SFP/SFP+/SFP28"}
DAC = {"vendor": "FS", "part": "S28-PC01", "revision": "A", "type": "SFP/SFP+/SFP28"}

NOT_FOUND = {"error": 400, "message": "Bad Request", "detail": "no such command or directory"}
FORBIDDEN = {"error": 403, "message": "Forbidden", "detail": "not enough permissions (9)"}
UNAUTHORIZED = {"error": 401, "message": "Unauthorized"}

WG_HQ_ENDPOINT = "203.0.113.45"  # fw-hq-01's public address (pppoe0)


# --------------------------------------------------------------------------- formatting


def b(value: Any) -> str:
    return "true" if value else "false"


def ros_mac(m: str | None) -> str:
    return (m or "00:00:00:00:00:00").upper()


def duration(seconds: float) -> str:
    """RouterOS durations: 1w2d3h4m5s, zero parts left out ("3h5s"), "0s" for nothing."""
    s = max(int(seconds), 0)
    out = ""
    for unit, size in (("w", 604800), ("d", 86400), ("h", 3600), ("m", 60), ("s", 1)):
        if s >= size:
            out += f"{s // size}{unit}"
            s %= size
    return out or "0s"


def local_time(t: float) -> str:
    return time.strftime("%Y-%m-%d %H:%M:%S", time.gmtime(t + acme.LOCAL_UTC_OFFSET))


def rate(mbps: int) -> str:
    if mbps >= 1000:
        g = mbps / 1000
        return f"{g:g}Gbps"
    return f"{mbps}Mbps"


def client_id(m: str) -> str:
    """DHCP client id as RouterOS prints it: 1:aa:bb:c:0:0:1 (no leading zeros)."""
    return "1:" + ":".join(format(int(x, 16), "x") for x in m.split(":"))


def speed_setting(p: Port) -> str:
    if p.kind == "sfp":
        if (p.speed or 0) >= 100000:
            return "100G-baseSR4-LR4"
        if (p.speed or 0) >= 25000:
            return "25G-baseSR-LR"
        return "10G-baseSR-LR"
    return "1G-baseT-full"


# --------------------------------------------------------------------------- the service


class MikroTik(HttpService):
    def __init__(self, *a: Any, **kw: Any) -> None:
        super().__init__(*a, **kw)
        self.box: Node = self.ctx.world.nodes[self.node]
        self.hw = HARDWARE.get(self.node, HARDWARE["br-gw01"])
        self.by_mac = {n.mac: n for n in self.ctx.world.nodes.values() if n.mac}
        self.ids = {name: f"*{p.index:X}" for name, p in self.box.ports.items()}
        self.ids["lo"] = "*FF"
        bridge = next((p for p in self.box.ports.values() if p.kind == "bridge"), None)
        # Bond slaves are not bridge ports in RouterOS: their bond is.
        self.bridge_ports = [m for m in (bridge.members if bridge else ()) if not self.box.ports[m].lag]
        self.bridge = bridge.name if bridge else None

    # -- HTTP --
    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path
        if not path.startswith("/rest"):
            if req.method in ("GET", "HEAD") and path in ("/", "/webfig/", "/index.html"):
                return Response.html_title(WEBFIG_TITLE)
            return Response.text(
                "<html><head><title>Error 404: Not Found</title></head><body>Not Found</body></html>", 404
            )
        if not self.authorized(req):
            return Response.json(UNAUTHORIZED, 401, [("WWW-Authenticate", 'Basic realm="RouterOS"')])
        sub = "/" + path[len("/rest") :].strip("/")
        if req.method == "GET":
            return self.get(sub, req, snap)
        if req.method == "POST":
            body = self.body(req)
            if body is None:
                return Response.json({"error": 400, "message": "Bad Request", "detail": "JSON parse error"}, 400)
            if sub == "/interface/ethernet/monitor":
                return self.monitor(body, snap)
            if sub.endswith("/print"):
                return self.get(sub[: -len("/print")], req, snap, body.get(".proplist"))
            return Response.json(FORBIDDEN, 403)
        if req.method in ("PUT", "PATCH", "DELETE"):
            return Response.json(FORBIDDEN, 403)
        return Response.json({"error": 405, "message": "Method Not Allowed"}, 405)

    def authorized(self, req: Request) -> bool:
        auth = req.headers.get("authorization", "")
        if not auth.lower().startswith("basic "):
            return False
        try:
            user, _, password = base64.b64decode(auth[6:].strip()).decode().partition(":")
        except (binascii.Error, UnicodeDecodeError):
            return False
        want_user = str(self.secrets.get("username", ""))
        want_pass = str(self.secrets.get("password", ""))
        return hmac.compare_digest(user, want_user) and hmac.compare_digest(password, want_pass) and bool(want_pass)

    @staticmethod
    def body(req: Request) -> dict[str, Any] | None:
        if not req.body:
            return {}
        try:
            data = json.loads(req.body)
        except ValueError:
            return None
        return data if isinstance(data, dict) else None

    def get(self, sub: str, req: Request, snap: Snapshot, proplist: Any = None) -> Response:
        menus = self.menus()
        fn = menus.get(sub)
        item = None
        if fn is None:
            parent, _, item = sub.rpartition("/")
            fn = menus.get(parent)
            if fn is None or not item:
                return Response.json(NOT_FOUND, 400)
        data = fn(snap)
        if item is not None:
            if not isinstance(data, list):
                return Response.json(NOT_FOUND, 400)
            hit = next((r for r in data if item in (r.get(".id"), r.get("name"))), None)
            if hit is None and item.startswith("*"):
                return Response.json({"error": 404, "message": "Not Found", "detail": "no such item"}, 404)
            if hit is None:  # not a name of the menu: a sub-menu this RouterOS lacks
                return Response.json(NOT_FOUND, 400)
            data = hit
        if isinstance(data, list):
            filters = {k: v[0] for k, v in req.query.items() if not k.startswith(".") and v}
            data = [r for r in data if all(r.get(k) == v for k, v in filters.items())]
        props = proplist if proplist is not None else req.arg(".proplist")
        if props:
            keep = [p.strip() for p in (props if isinstance(props, list) else str(props).split(",")) if p.strip()]
            if isinstance(data, list):
                data = [{k: r[k] for k in keep if k in r} for r in data]
            else:
                data = {k: data[k] for k in keep if k in data}
        return Response.json(data)

    def menus(self) -> dict[str, Any]:
        return {
            "/system/resource": self.resource,
            "/system/identity": lambda s: {"name": self.box.hostname or self.box.name},
            "/system/routerboard": self.routerboard,
            "/system/health": self.health,
            "/system/package/update": self.package_update,
            "/system/package": self.packages,
            "/system/clock": self.clock,
            "/interface": self.interfaces,
            "/interface/ethernet": self.ethernet,
            "/interface/bridge": self.bridges,
            "/interface/bridge/port": self.bridge_port_rows,
            "/interface/bridge/vlan": self.bridge_vlans,
            "/interface/bridge/host": self.bridge_hosts,
            "/interface/vlan": self.vlan_ifaces,
            "/interface/bonding": self.bonds,
            "/interface/pppoe-client": lambda s: [],
            "/interface/wireguard": self.wireguard,
            "/interface/wireguard/peers": self.wireguard_peers,
            "/ip/address": self.addresses,
            "/ip/route": self.routes,
            "/ip/arp": self.arp,
            "/ip/neighbor": self.neighbors,
            "/ip/dhcp-server": self.dhcp_servers,
            "/ip/dhcp-server/lease": self.leases,
            "/ip/pool": self.pools,
        }

    # -- system --
    def resource(self, snap: Snapshot) -> dict[str, str]:
        total = self.hw["total-memory"]
        disk = self.hw["total-hdd-space"]
        free_disk = int(disk * (0.72 + 0.04 * h01("flash", self.node)))
        up = snap.uptime(self.node)
        return {
            "uptime": duration(up),
            "version": f"{self.version(snap)} (stable)",
            "build-time": BUILD_TIME,
            "factory-software": self.hw["factory-software"],
            "free-memory": str(int(total * (1 - snap.mem(self.node) / 100))),
            "total-memory": str(total),
            "cpu": "ARM64",
            "cpu-count": "4",
            "cpu-frequency": self.hw["cpu-frequency"],
            "cpu-load": str(round(snap.cpu(self.node))),
            "free-hdd-space": str(free_disk),
            "total-hdd-space": str(disk),
            "write-sect-since-reboot": str(int(up / 20)),
            "write-sect-total": str(int(up / 20) + 1_204_331),
            "bad-blocks": "0",
            "architecture-name": "arm64",
            "board-name": self.box.model,
            "platform": "MikroTik",
        }

    def version(self, snap: Snapshot) -> str:
        return snap.firmware(self.node)["current"] or VERSION

    def routerboard(self, snap: Snapshot) -> dict[str, str]:
        return {
            "routerboard": "true",
            "board-name": self.box.model,
            "model": self.box.model,
            "serial-number": self.box.serial,
            "firmware-type": self.hw["firmware-type"],
            "factory-firmware": self.hw["factory-firmware"],
            "current-firmware": self.hw["current-firmware"],
            "upgrade-firmware": self.hw["upgrade-firmware"],
        }

    def health(self, snap: Snapshot) -> list[dict[str, str]]:
        n = self.node
        cpu = snap.temperature(n, "cpu")
        rows: list[tuple[str, str, str]]
        if n == "sw-stor-01":
            fan = int(5200 + 900 * snap.cpu(n) / 100 + h01("fan", n, snap.b) * 120)
            rows = [
                ("fan-state", "ok", ""),
                ("fan1-speed", str(fan), "RPM"),
                ("fan2-speed", str(fan + 60), "RPM"),
                ("fan3-speed", str(fan - 40), "RPM"),
                ("fan4-speed", str(fan + 20), "RPM"),
                ("psu1-state", "ok", ""),
                ("psu2-state", "ok", ""),
                ("cpu-temperature", f"{cpu:.0f}", "C"),
                ("switch-temperature", f"{snap.temperature(n, 'switch') + 6:.0f}", "C"),
                ("board-temperature1", f"{snap.temperature(n, 'board') - 8:.0f}", "C"),
            ]
        else:
            volts = 24.0 + h01("volt", n, snap.b) * 0.3
            watts = 11.5 + 4 * snap.cpu(n) / 100 + h01("watt", n, snap.b)
            rows = [
                ("voltage", f"{volts:.1f}", "V"),
                ("current", f"{watts / volts * 1000:.0f}", "mA"),
                ("power-consumption", f"{watts:.1f}", "W"),
                ("cpu-temperature", f"{cpu:.0f}", "C"),
                ("board-temperature1", f"{snap.temperature(n, 'board') - 9:.0f}", "C"),
            ]
        return [{".id": f"*{k + 1:X}", "name": name, "value": v, "type": t} for k, (name, v, t) in enumerate(rows)]

    def package_update(self, snap: Snapshot) -> dict[str, str]:
        cur = self.version(snap)
        fw = snap.firmware(self.node)
        latest = fw["latest"] if fw["pending"] else LATEST
        return {
            "channel": "stable",
            "installed-version": cur,
            "latest-version": latest,
            "status": "New version is available" if latest != cur else "System is already up to date",
        }

    def packages(self, snap: Snapshot) -> list[dict[str, str]]:
        cur = self.version(snap)
        names = ["routeros"]
        return [
            {
                ".id": f"*{k + 1:X}",
                "name": p,
                "version": cur,
                "build-time": BUILD_TIME,
                "scheduled": "",
                "disabled": "false",
            }
            for k, p in enumerate(names)
        ]

    def clock(self, snap: Snapshot) -> dict[str, str]:
        date, tm = local_time(snap.t).split(" ")
        return {
            "time": tm,
            "date": date,
            "time-zone-autodetect": "false",
            "time-zone-name": TIME_ZONE,
            "gmt-offset": "-03:00",
            "dst-active": "false",
        }

    # -- interfaces --
    def physical(self) -> list[Port]:
        return [p for p in self.box.ports.values() if p.kind in ("ethernet", "sfp")]

    def counters(self, snap: Snapshot, p: Port) -> dict[str, int | bool]:
        """Counters of any interface (wg is computed here: the world's port() recurses for wg ports)."""
        if p.kind == "wg":
            up = bool(snap.w.tunnel_up["wg-branch"][snap.b])
            rx = self._integral(snap, "rx", p.name)
            tx = self._integral(snap, "tx", p.name)
            return {"up": True, "link": up, "rx": rx, "tx": tx, "rxp": rx // 600, "txp": tx // 600, "err": 0}
        st = snap.port(self.node, p.name)
        rx, tx = st.rx_bytes, st.tx_bytes
        if p.kind in ("bridge", "vlan") and rx == 0 and tx == 0:
            if p.kind == "bridge" and self.node == "br-gw01":
                for q in self.box.ports.values():
                    if q.kind == "vlan":
                        s2 = snap.port(self.node, q.name)
                        rx, tx = rx + s2.rx_bytes, tx + s2.tx_bytes
            else:
                live = max(snap.t - snap.boot(self.node), 0)  # management traffic (REST, SNMP, LLDP)
                rx, tx = int(live * 2900), int(live * 6100)
        return {
            "up": st.up,
            "link": st.up,
            "rx": rx,
            "tx": tx,
            "rxp": st.rx_packets or rx // 800,
            "txp": st.tx_packets or tx // 800,
            "err": st.rx_errors,
        }

    def _integral(self, snap: Snapshot, which: str, port: str) -> int:
        w = snap.w
        arr = (w.rx if which == "rx" else w.tx).get((self.node, port))
        if arr is None:
            return 0
        key = (which, self.node, port)
        return int(max(w.integral(arr, key, snap.t) - w.integral(arr, key, snap.boot(self.node)), 0) / snap.speed)

    def link_downs(self, snap: Snapshot, p: Port) -> int:
        if self.node == "sw-stor-01" and p.name == "sfp28-16":
            day = snap.local()[0]
            return 1 + day * 2  # flaps more as the fiber degrades
        return 1 if snap.port(self.node, p.name).up else 0

    def interfaces(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        boot = snap.boot(self.node)
        bonded = {m for p in self.box.ports.values() if p.kind == "lag" for m in p.members}
        for p in self.box.ports.values():
            c = self.counters(snap, p)
            kind = {
                "ethernet": "ether",
                "sfp": "ether",
                "lag": "bond",
                "bridge": "bridge",
                "vlan": "vlan",
                "wg": "wg",
            }.get(p.kind, "ether")
            row = {".id": self.ids[p.name], "name": p.name}
            if kind == "ether":
                row["default-name"] = p.name
            mtu = "1420" if kind == "wg" else str(p.mtu)
            row.update(
                {
                    "type": kind,
                    "mtu": mtu,
                    "actual-mtu": mtu,
                }
            )
            if kind in ("ether", "bond", "bridge", "vlan"):
                row["l2mtu"] = "1588" if kind != "vlan" else "1584"
                row["mac-address"] = ros_mac(p.mac or self.box.mac)
            if kind == "ether":
                row["max-l2mtu"] = "9796" if p.kind == "sfp" else "9574"
            if c["link"] and kind in ("ether", "bond"):
                row["last-link-up-time"] = local_time(boot + 25 + p.index)
                row["link-downs"] = str(self.link_downs(snap, p))
            elif kind == "ether":
                row["link-downs"] = "0"
            row.update(
                {
                    "rx-byte": str(c["rx"]),
                    "tx-byte": str(c["tx"]),
                    "rx-packet": str(c["rxp"]),
                    "tx-packet": str(c["txp"]),
                    "rx-drop": "0",
                    "tx-drop": "0",
                    "tx-queue-drop": "0",
                    "rx-error": str(c["err"]),
                    "tx-error": "0",
                    "fp-rx-byte": str(c["rx"] if kind in ("ether", "bond") else 0),
                    "fp-tx-byte": "0",
                    "fp-rx-packet": str(c["rxp"] if kind in ("ether", "bond") else 0),
                    "fp-tx-packet": "0",
                    "running": b(c["up"]),
                    "slave": b(p.name in bonded or p.name in self.bridge_ports),
                    "disabled": "false",
                }
            )
            if p.descr:
                row["comment"] = p.descr
            out.append(row)
        out.append(
            {
                ".id": self.ids["lo"],
                "name": "lo",
                "type": "loopback",
                "mtu": "65536",
                "actual-mtu": "65536",
                "mac-address": "00:00:00:00:00:00",
                "rx-byte": "0",
                "tx-byte": "0",
                "rx-packet": "0",
                "tx-packet": "0",
                "rx-drop": "0",
                "tx-drop": "0",
                "tx-queue-drop": "0",
                "rx-error": "0",
                "tx-error": "0",
                "running": "true",
                "slave": "false",
                "disabled": "false",
            }
        )
        return out

    def ethernet(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for p in self.physical():
            row = {
                ".id": self.ids[p.name],
                "name": p.name,
                "default-name": p.name,
                "mtu": str(p.mtu),
                "l2mtu": "1588",
                "mac-address": ros_mac(p.mac),
                "orig-mac-address": ros_mac(p.mac),
                "arp": "enabled",
                "arp-timeout": "auto",
                "loop-protect": "default",
                "auto-negotiation": b(p.kind != "sfp"),
                "speed": speed_setting(p),
                "advertise": "10M-baseT-half,10M-baseT-full,100M-baseT-half,100M-baseT-full,1G-baseT-half,1G-baseT-full"
                if p.kind != "sfp"
                else "",
                "rx-flow-control": "off",
                "tx-flow-control": "off",
                "bandwidth": "unlimited/unlimited",
                "switch": "switch1",
                "running": b(snap.port(self.node, p.name).up),
                "slave": b(p.lag is not None or p.name in self.bridge_ports),
                "disabled": "false",
            }
            if p.kind == "sfp":
                row.update({"sfp-rate-select": "high", "sfp-shutdown-temperature": "95C", "sfp-ignore-rx-los": "false"})
            if p.poe:
                row.update({"poe-out": "auto-on", "poe-priority": "10", "poe-lldp-enabled": "false"})
            if p.descr:
                row["comment"] = p.descr
            out.append(row)
        return out

    def module(self, p: Port) -> dict[str, str] | None:
        """The optic or DAC in an SFP cage: by what the cable on it is."""
        if p.kind != "sfp":
            return None
        link = next(
            (
                lk
                for lk in self.ctx.world.d.links
                if (lk.a, lk.pa) == (self.node, p.name) or (lk.b, lk.pb) == (self.node, p.name)
            ),
            None,
        )
        if link is None:
            return None
        return DAC if link.kind == "dac" else OPTIC

    def monitor_row(self, snap: Snapshot, p: Port) -> dict[str, str]:
        st = snap.port(self.node, p.name)
        row: dict[str, str] = {"name": p.name}
        if st.up:
            row.update(
                {
                    "status": "link-ok",
                    "auto-negotiation": "done" if p.kind != "sfp" else "disabled",
                    "rate": rate(st.speed or p.speed or 1000),
                    "full-duplex": "true",
                    "tx-flow-control": "false",
                    "rx-flow-control": "false",
                }
            )
            if p.kind != "sfp":
                row["advertising"] = (
                    "10M-baseT-half,10M-baseT-full,100M-baseT-half,100M-baseT-full,1G-baseT-half,1G-baseT-full"
                )
                row["link-partner-advertising"] = row["advertising"]
        else:
            row.update({"status": "no-link", "auto-negotiation": "disabled" if p.kind == "sfp" else "incomplete"})
        if p.poe:
            row["poe-out-status"] = "powered-on" if st.up else "waiting-for-load"
        mod = self.module(p)
        if p.kind == "sfp":
            row["sfp-module-present"] = b(mod is not None)
        if mod is None:
            return row
        serial = f"{'F2210' if mod is OPTIC else 'G2309'}{int(h01('sfp', self.node, p.name) * 1e7):07d}"
        row.update(
            {
                "sfp-rx-loss": "false",
                "sfp-tx-fault": "false",
                "sfp-type": mod["type"],
                "sfp-connector-type": "LC" if mod is OPTIC else "copper-pigtail",
                "sfp-vendor-name": mod["vendor"],
                "sfp-vendor-part-number": mod["part"],
                "sfp-vendor-revision": mod["revision"],
                "sfp-vendor-serial": serial,
                "sfp-manufacturing-date": "23-04-11" if mod is OPTIC else "23-09-02",
            }
        )
        if mod is DAC:
            row["sfp-link-length-copper"] = "1m"
            row["sfp-wavelength"] = "0nm"
            return row
        jitter = (h01("dom", self.node, p.name, snap.b) - 0.5) * 0.12
        rx = -2.1 - 1.3 * h01("rx", self.node, p.name) + jitter
        if (self.node, p.name) == ("sw-stor-01", "sfp28-16"):
            rx = snap.fiber_rx_dbm()
        if not st.up:
            rx = -40.0
        row.update(
            {
                "sfp-link-length-om3": "300m",
                "sfp-wavelength": mod["wavelength"],
                "sfp-temperature": f"{31 + h01('t', self.node, p.name) * 9 + snap.cpu(self.node) / 20:.0f}C",
                "sfp-supply-voltage": f"{3.28 + h01('v', self.node, p.name) * 0.04:.3f}V",
                "sfp-tx-bias-current": f"{6 + h01('bias', self.node, p.name) * 2:.0f}mA",
                "sfp-tx-power": f"{-2.3 - h01('tx', self.node, p.name) * 0.6:.3f}dBm",
                "sfp-rx-power": f"{rx:.3f}dBm",
            }
        )
        return row

    def monitor(self, body: dict[str, Any], snap: Snapshot) -> Response:
        if "once" not in body and "duration" not in body:
            # Without `once` the console command keeps running; the REST API times it out.
            return Response.json({"error": 400, "message": "Bad Request", "detail": "Session closed"}, 400)
        numbers = [x.strip() for x in str(body.get("numbers") or "").split(",") if x.strip()]
        by_id = {self.ids[p.name]: p for p in self.physical()}
        by_name = {p.name: p for p in self.physical()}
        rows = []
        for x in numbers:
            p = by_name.get(x) or by_id.get(x)
            if p is None:
                return Response.json({"error": 400, "message": "Bad Request", "detail": "no such item"}, 400)
            rows.append(self.monitor_row(snap, p))
        return Response.json(rows)

    # -- bridge --
    def bridges(self, snap: Snapshot) -> list[dict[str, str]]:
        if not self.bridge:
            return []
        p = self.box.ports[self.bridge]
        return [
            {
                ".id": self.ids[p.name],
                "name": p.name,
                "mtu": "auto",
                "actual-mtu": "1500",
                "l2mtu": "1588",
                "arp": "enabled",
                "arp-timeout": "auto",
                "mac-address": ros_mac(p.mac),
                "auto-mac": "false",
                "admin-mac": ros_mac(p.mac),
                "protocol-mode": "rstp" if self.node == "br-gw01" else "mstp",
                "priority": "0x8000" if self.node == "br-gw01" else "0x4000",
                "fast-forward": "true",
                "igmp-snooping": "false",
                "vlan-filtering": "true",
                "ether-type": "0x8100",
                "pvid": "1",
                "frame-types": "admit-all",
                "ingress-filtering": "true",
                "dhcp-snooping": "false",
                "running": "true",
                "disabled": "false",
            }
        ]

    def bridge_port_rows(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for k, name in enumerate(self.bridge_ports):
            p = self.box.ports[name]
            pvid = p.untagged or 1
            frames = (
                "admit-only-vlan-tagged"
                if not p.untagged and p.tagged
                else "admit-only-untagged-and-priority-tagged"
                if not p.tagged
                else "admit-all"
            )
            up = snap.port(self.node, name).up
            out.append(
                {
                    ".id": f"*{k:X}",
                    "interface": name,
                    "bridge": self.bridge or "bridge",
                    "pvid": str(pvid),
                    "frame-types": frames,
                    "ingress-filtering": "true",
                    "hw": "true",
                    "hw-offload": "true",
                    "edge": "auto",
                    "point-to-point": "auto",
                    "learn": "auto",
                    "horizon": "none",
                    "status": "in-bridge" if up else "inactive",
                    "role": "designated-port" if up else "disabled-port",
                    "inactive": b(not up),
                    "disabled": "false",
                    "trusted": "false",
                }
            )
        return out

    def bridge_vlans(self, snap: Snapshot) -> list[dict[str, str]]:
        vids: dict[int, dict[str, list[str]]] = {}
        for name in self.bridge_ports:
            p = self.box.ports[name]
            vids.setdefault(p.untagged or 1, {"tagged": [], "untagged": []})["untagged"].append(name)
            for v in p.tagged:
                vids.setdefault(v, {"tagged": [], "untagged": []})["tagged"].append(name)
        cpu = {q.vlan for q in self.box.ports.values() if q.kind == "vlan" and q.parent == self.bridge}
        out = []
        for k, vid in enumerate(sorted(vids)):
            tagged = (["bridge"] if vid in cpu else []) + vids[vid]["tagged"]
            untagged = vids[vid]["untagged"] + (["bridge"] if vid == 1 else [])
            out.append(
                {
                    ".id": f"*{k + 1:X}",
                    "bridge": self.bridge or "bridge",
                    "vlan-ids": str(vid),
                    "tagged": "" if vid == 1 else ",".join(tagged),
                    "untagged": "" if vid == 1 else ",".join(untagged),
                    "current-tagged": ",".join(tagged),
                    "current-untagged": ",".join(untagged),
                    "dynamic": b(vid == 1),
                    "disabled": "false",
                }
            )
        return out

    def bridge_hosts(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        members = set(self.bridge_ports)
        k = 0
        for mac_, port, vid in snap.fdb(self.node):
            if port not in members:
                continue  # learned on a routed port (br-gw01's ether1), not in the bridge
            k += 1
            out.append(
                {
                    ".id": f"*{k:X}",
                    "mac-address": ros_mac(mac_),
                    "on-interface": port,
                    "bridge": self.bridge or "bridge",
                    "vid": str(vid),
                    "local": "false",
                    "dynamic": "true",
                    "external": "false",
                    "invalid": "false",
                }
            )
        for name in self.bridge_ports:
            p = self.box.ports[name]
            k += 1
            out.append(
                {
                    ".id": f"*{k:X}",
                    "mac-address": ros_mac(p.mac),
                    "on-interface": name,
                    "bridge": self.bridge or "bridge",
                    "vid": str(p.untagged or 1),
                    "local": "true",
                    "dynamic": "false",
                    "external": "false",
                    "invalid": "false",
                }
            )
        return out

    def vlan_ifaces(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for p in self.box.ports.values():
            if p.kind != "vlan":
                continue
            row = {
                ".id": self.ids[p.name],
                "name": p.name,
                "interface": p.parent or "bridge",
                "vlan-id": str(p.vlan),
                "mtu": str(p.mtu),
                "l2mtu": "1584",
                "mac-address": ros_mac(p.mac or self.box.mac),
                "arp": "enabled",
                "arp-timeout": "auto",
                "loop-protect": "default",
                "use-service-tag": "false",
                "running": "true",
                "disabled": "false",
            }
            if p.descr:
                row["comment"] = p.descr
            out.append(row)
        return out

    def bonds(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for p in self.box.ports.values():
            if p.kind != "lag":
                continue
            row = {
                ".id": self.ids[p.name],
                "name": p.name,
                "slaves": ",".join(p.members),
                "mode": "802.3ad",
                "transmit-hash-policy": "layer-3-and-4",
                "lacp-rate": "30secs",
                "lacp-user-key": "0",
                "link-monitoring": "mii",
                "mii-interval": "100ms",
                "min-links": "0",
                "mtu": str(p.mtu),
                "mac-address": ros_mac(p.mac),
                "arp": "enabled",
                "arp-timeout": "auto",
                "running": b(snap.port(self.node, p.name).up),
                "disabled": "false",
            }
            if p.descr:
                row["comment"] = p.descr
            out.append(row)
        return out

    # -- WireGuard (br-gw01 → HQ) --
    def wireguard(self, snap: Snapshot) -> list[dict[str, str]]:
        return [
            {
                ".id": self.ids[p.name],
                "name": p.name,
                "listen-port": "13231",
                "mtu": "1420",
                "public-key": base64.b64encode(h01("wgpub", self.node).hex().encode()[:32]).decode(),
                "running": "true",
                "disabled": "false",
                "comment": p.descr,
            }
            for p in self.box.ports.values()
            if p.kind == "wg"
        ]

    def wireguard_peers(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for k, p in enumerate(q for q in self.box.ports.values() if q.kind == "wg"):
            up = bool(snap.w.tunnel_up["wg-branch"][snap.b])
            s = snap.s % 86400
            ago = s % 90 if up else s - 16 * 3600  # handshakes every 90 s; none since 16:00 while it is down
            c = self.counters(snap, p)
            out.append(
                {
                    ".id": f"*{k + 1:X}",
                    "interface": p.name,
                    "name": "fw-hq",
                    "comment": "Matriz (fw-hq CARP)",
                    "public-key": base64.b64encode(h01("wgpub", "fw-hq").hex().encode()[:32]).decode(),
                    "endpoint-address": WG_HQ_ENDPOINT,
                    "endpoint-port": "51820",
                    "current-endpoint-address": WG_HQ_ENDPOINT if up else "",
                    "current-endpoint-port": "51820" if up else "0",
                    "allowed-address": "10.10.0.0/16,10.30.0.0/16,10.255.0.1/32",
                    "persistent-keepalive": "25s",
                    "rx": str(c["rx"]),
                    "tx": str(c["tx"]),
                    "last-handshake": duration(max(ago, 1)),
                    "responder": "false",
                    "disabled": "false",
                }
            )
        return out

    # -- IP --
    def addresses(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        k = 0
        for p in self.box.ports.values():
            for a in p.ips:
                k += 1
                net = ipaddress.ip_interface(a).network
                row = {
                    ".id": f"*{k:X}",
                    "address": a,
                    "network": str(net.network_address),
                    "interface": p.name,
                    "actual-interface": p.name,
                    "invalid": "false",
                    "dynamic": "false",
                    "disabled": "false",
                }
                if p.descr:
                    row["comment"] = p.descr
                out.append(row)
        return out

    def routes(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        k = 0x80000000

        def add(row: dict[str, str]) -> None:
            nonlocal k
            k += 1
            out.append({".id": f"*{k:X}", "routing-table": "main", "disabled": "false", **row})

        if self.node == "br-gw01":
            up = snap.port(self.node, "ether1").up
            add(
                {
                    "dst-address": "0.0.0.0/0",
                    "gateway": "198.51.100.17",
                    "immediate-gw": "198.51.100.17%ether1",
                    "distance": "1",
                    "scope": "30",
                    "target-scope": "10",
                    "vrf-interface": "ether1",
                    "active": b(up),
                    "inactive": b(not up),
                    "static": "true",
                    "comment": "Vivo Fibra",
                }
            )
            for dst in ("10.10.0.0/16", "10.30.0.0/16"):
                add(
                    {
                        "dst-address": dst,
                        "gateway": "wg-hq",
                        "immediate-gw": "wg-hq",
                        "distance": "1",
                        "scope": "30",
                        "target-scope": "10",
                        "active": "true",
                        "inactive": "false",
                        "static": "true",
                        "comment": "HQ via WireGuard",
                    }
                )
        else:
            add(
                {
                    "dst-address": "0.0.0.0/0",
                    "gateway": "10.10.10.1",
                    "immediate-gw": "10.10.10.1%vlan10",
                    "distance": "1",
                    "scope": "30",
                    "target-scope": "10",
                    "vrf-interface": "vlan10",
                    "active": "true",
                    "inactive": "false",
                    "static": "true",
                }
            )
        for p in self.box.ports.values():
            for a in p.ips:
                net = ipaddress.ip_interface(a).network
                up = p.kind in ("vlan", "bridge", "wg") or snap.port(self.node, p.name).up
                add(
                    {
                        "dst-address": str(net),
                        "gateway": p.name,
                        "immediate-gw": p.name,
                        "distance": "0",
                        "scope": "10",
                        "local-address": f"{a.split('/')[0]}%{p.name}",
                        "active": b(up),
                        "inactive": b(not up),
                        "dynamic": "true",
                        "connect": "true",
                    }
                )
        return out

    def leased(self, snap: Snapshot) -> dict[str, Any]:
        return {lease.mac: lease for lease in snap.leases(self.box.site)} if self.node == "br-gw01" else {}

    def arp(self, snap: Snapshot) -> list[dict[str, str]]:
        leased = self.leased(snap)
        table = snap.arp(self.node)
        if self.box.kind == "switch":
            # A switch only resolves who it talks to: its gateway (the CARP address) and its pollers (Omini).
            # snap.arp() lists the whole subnet, and has no CARP VIP: both fixed here.
            v = self.ctx.world.d.vlans.get((self.box.site, self.box.vlan or 0))
            table = [(ip, m, i) for ip, m, i in table if ip == self.ctx.world.nodes["omini"].ip]
            if v and v.gateway and snap.up(snap.master()):
                iface = next((p.name for p in self.box.ports.values() if p.kind == "vlan" and p.vlan == v.id), "bridge")
                table.insert(0, (v.gateway, acme.carp_mac(v.id), iface))
        out = []
        for k, (ip, m, iface) in enumerate(table):
            n = self.by_mac.get(m)
            fresh = n is None or snap.present(n.name)
            out.append(
                {
                    ".id": f"*{k + 1:X}",
                    "address": ip,
                    "mac-address": ros_mac(m),
                    "interface": iface,
                    "status": "reachable" if fresh else "stale",
                    "dynamic": "true",
                    "complete": "true",
                    "invalid": "false",
                    "disabled": "false",
                    "published": "false",
                    "DHCP": b(m in leased),
                }
            )
        return out

    def neighbors(self, snap: Snapshot) -> list[dict[str, str]]:
        out = []
        for k, nb in enumerate(snap.lldp(self.node)):
            local = self.box.ports[nb.local_port]
            chain = [nb.local_port] + ([local.lag] if local.lag else [])
            if self.bridge and (local.lag or nb.local_port) in self.bridge_ports:
                chain.append(self.bridge)
            other = nb.node
            if other.os == "IOS-XE":
                desc = (
                    f"Cisco IOS Software [Dublin], Catalyst L3 Switch Software (CAT9K_IOSXE), Version "
                    f"{other.version}, RELEASE SOFTWARE (fc3)"
                )
                caps, enabled, by = "bridge,router", "bridge,router", "cdp,lldp"
            elif other.kind == "hypervisor":
                kernel = other.extra.get("kernel", "6.14.11-2-pve")
                desc = f"Debian GNU/Linux 13 (trixie) Linux {kernel} #1 SMP PREEMPT_DYNAMIC PMX x86_64"
                caps, enabled, by = "bridge,router,station", "bridge,station", "lldp"
            elif other.vendor == "TP-Link":
                desc = f"JetStream 28-Port Gigabit L2+ Managed Switch with 24-Port PoE+ ({other.model})"
                caps, enabled, by = "bridge", "bridge", "lldp"
            else:
                desc = f"{other.vendor} {other.model}".strip()
                caps, enabled, by = "bridge", "bridge", "lldp"
            row = {
                ".id": f"*{k + 1:X}",
                "interface": ",".join(chain),
                "mac-address": ros_mac(other.mac),
                "identity": other.hostname or other.name,
                "platform": "",
                "version": "",
                "board": "",
                "interface-name": nb.port.name,
                "system-description": desc,
                "system-caps": caps,
                "system-caps-enabled": enabled,
                "age": f"{int(h01('age', self.node, k, snap.b) * 28) + 1}s",
                "ipv6": "false",
                "discovered-by": by,
            }
            if nb.mgmt_ip:
                row["address"] = nb.mgmt_ip
                row["address4"] = nb.mgmt_ip
            out.append(row)
        return out

    def dhcp_scopes(self) -> list[tuple[int, str, tuple[str, str]]]:
        if self.node != "br-gw01":
            return []
        out = []
        for (site, vid), v in sorted(self.ctx.world.d.vlans.items()):
            if site == self.box.site and v.dhcp:
                out.append((vid, f"vlan{vid}", v.dhcp))
        return out

    def dhcp_servers(self, snap: Snapshot) -> list[dict[str, str]]:
        return [
            {
                ".id": f"*{k + 1:X}",
                "name": f"dhcp-{iface}",
                "interface": iface,
                "address-pool": f"pool-{iface}",
                "lease-time": "1h" if vid == 80 else "8h" if vid == 32 else "1d",
                "authoritative": "yes",
                "use-radius": "no",
                "lease-script": "",
                "invalid": "false",
                "dynamic": "false",
                "disabled": "false",
            }
            for k, (vid, iface, _) in enumerate(self.dhcp_scopes())
        ]

    def pools(self, snap: Snapshot) -> list[dict[str, str]]:
        return [
            {".id": f"*{k + 1:X}", "name": f"pool-{iface}", "ranges": f"{lo}-{hi}"}
            for k, (_, iface, (lo, hi)) in enumerate(self.dhcp_scopes())
        ]

    def leases(self, snap: Snapshot) -> list[dict[str, str]]:
        if self.node != "br-gw01":
            return []
        out = []
        for k, lease in enumerate(snap.leases(self.box.site)):
            server = f"dhcp-vlan{lease.vlan}"
            present = snap.present(lease.node.name)
            seen = 0 if present else snap.t - (snap.last_seen(lease.node.name) or snap.t)
            row = {
                ".id": f"*{k + 1:X}",
                "address": lease.ip,
                "mac-address": ros_mac(lease.mac),
                "client-id": client_id(lease.mac),
                "address-lists": "",
                "server": server,
                "dhcp-option": "",
                "status": "bound",
                "expires-after": duration(lease.end - snap.t),
                "last-seen": duration(seen + int(h01("seen", lease.mac, snap.b) * 40) + 1),
                "active-address": lease.ip,
                "active-mac-address": ros_mac(lease.mac),
                "active-client-id": client_id(lease.mac),
                "active-server": server,
                "radius": "false",
                "dynamic": b(not lease.static),
                "blocked": "false",
                "disabled": "false",
            }
            if lease.hostname:
                row["host-name"] = lease.hostname
            if lease.static:
                row["comment"] = lease.node.name
            out.append(row)
        return out
