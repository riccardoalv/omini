"""UniFi Network Application (self-hosted, 9.x) on the VM unifi01 at HQ.

The controller manages the third floor's switch (sw-f3-01, USW-Pro-48-PoE)
and every UniFi access point (eight wired U6-Pro / U6-Enterprise / U7-Pro and
the U6-Mesh on the patio, meshed to the cafeteria's AP). The gateway is not
UniFi (the OPNsense pair), so the site has no WAN subsystem; the floor 1–2
switches are Aruba (the APs there see them through LLDP only).

What is emulated, as the self-hosted application answers it:

- ``GET /`` → 302 ``/manage`` → 302 ``/manage/account/login`` (page title
  "UniFi Network"); ``GET /status`` without login (server version).
- ``POST /api/login`` (JSON username / password): ``unifises`` session cookie
  plus a ``csrf_token`` cookie; wrong credentials → 400 ``api.err.Invalid``.
  ``POST /api/logout``. ``/api/auth/login`` (UniFi OS only) → 404.
- ``GET /api/self``, ``/api/self/sites``.
- ``GET /api/s/<site>/...`` (session required, else 401 ``api.err.LoginRequired``;
  an unknown site → 400 ``api.err.NoSiteContext``): ``stat/device``,
  ``stat/device-basic``, ``stat/sta``, ``stat/health``, ``stat/sysinfo``,
  ``rest/networkconf``, ``rest/wlanconf``; anything else → 404 ``api.err.NotFound``.

Every answer is ``{"meta": {"rc": "ok"}, "data": [...]}``.

Scenario: the APs reboot on Wednesday from 03:00 (two minutes apart) to
install the firmware the controller offered since Monday; while one is down
(or its uplink is: a switch reboot, the mesh parent rebooting) the controller
lists it disconnected (state 0) with its clients gone. The switch always has a
firmware update pending (nobody installs it).
"""

from __future__ import annotations

import hashlib
import secrets
from typing import Any

from ..model import Node, Port
from ..server import HttpService, Request, Response
from ..world import Assoc, PortState, Snapshot, h01, wsec

APP_VERSION = "9.0.114"
APP_BUILD = "atag_9.0.114_27321"
SESSION_TTL = 7 * 86400  # "remember": true
SITE = {"name": "default", "desc": "Default", "_id": "66b0e1a2c4f1d23a5e8f0001", "role": "readonly"}

# Design model → UniFi model code.
MODEL_CODES = {
    "USW-Pro-48-PoE": "USL48PB",
    "U6-Pro": "UAP6MP",
    "U6-Enterprise": "U6ENT",
    "U6-Mesh": "U6M",
    "U7-Pro": "U7PRO",
}
BOARD_REV = {"USL48PB": 13, "UAP6MP": 18, "U6ENT": 7, "U6M": 9, "U7PRO": 4}
RADIO = {"2.4": "ng", "5": "na", "6": "6e"}
RADIO_PROTO = {"2.4": "ax", "5": "ax", "6": "ax"}
CHANNELS = {"2.4": [1, 6, 11], "5": [36, 52, 100, 116, 132, 149], "6": [37, 69, 101]}  # as world.assoc picks them
HT = {"2.4": 20, "5": 80, "6": 160}
SSIDS = ("Acme", "Acme-IoT", "Acme-Visitantes")  # what the controller broadcasts (Acme-Galpao is the Halos')
SSID_SHARE = {"Acme": 0.8, "Acme-Visitantes": 0.15, "Acme-IoT": 0.05}  # of a radio's bytes, per VAP
# Firmware: the APs' new build (installed by Wednesday's reboot) and the one before it.
AP_FIRMWARE = {"7.0.95": ("7.0.95.16123", "7.0.91.15985"), "8.1.21": ("8.1.21.17890", "8.0.36.17512")}
SWITCH_FIRMWARE = ("7.1.26.15869", "7.2.123.16565")  # (installed, offered)
POE_WATTS = {"ap": 13.5, "phone": 3.9, "camera": 4.8}
CISCO_LONG = {"Te": "TenGigabitEthernet", "Tw": "TwentyFiveGigE", "Gi": "GigabitEthernet", "Po": "Port-channel"}


def oid(*parts: Any) -> str:
    """A stable 24-hex Mongo ObjectId."""
    return hashlib.sha256("/".join(map(str, parts)).encode()).hexdigest()[:24]


def ok(data: list[Any]) -> Response:
    return Response.json({"meta": {"rc": "ok"}, "data": data})


def err(msg: str, status: int) -> Response:
    return Response.json({"meta": {"rc": "error", "msg": msg}, "data": []}, status)


def random_mac(m: str) -> bool:
    return bool(int(m[:2], 16) & 0x02)


def bssid(radio_mac: str, k: int) -> str:
    """A VAP's BSSID: the radio's MAC, locally administered, plus the VAP index."""
    b = [int(x, 16) for x in radio_mac.split(":")]
    b[0] |= 0x02
    b[5] = (b[5] + k * 16) % 256
    return ":".join(f"{x:02x}" for x in b)


def remote_port_descr(n: Node, p: Port) -> str:
    """The LLDP port description a neighbor sends (Cisco: the long interface name)."""
    if n.vendor == "Cisco":
        for short, long in CISCO_LONG.items():
            if p.name.startswith(short):
                return long + p.name[len(short) :]
    if n.vendor in ("Aruba", "Ubiquiti"):
        return p.name
    return p.descr or p.name


class UniFi(HttpService):
    def __init__(self, *a: Any, **kw: Any) -> None:
        super().__init__(*a, **kw)
        self.sessions: dict[str, float] = {}  # unifises → expiry (controller clock)

    # ------------------------------------------------------------------ HTTP

    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path.rstrip("/") or "/"
        if path == "/":
            return Response(302, b"", [("Location", "/manage")])
        if path == "/manage":
            return Response(302, b"", [("Location", "/manage/account/login?redirect=%2Fmanage")])
        if path.startswith("/manage"):
            return Response.html_title("UniFi Network", '<div id="root"></div>')
        if path == "/status":
            return Response.json(
                {
                    "meta": {
                        "rc": "ok",
                        "up": True,
                        "server_version": APP_VERSION,
                        "uuid": "3f1c2a7e-9b54-4c1e-8d0a-6a2f5b7c9e10",
                    },
                    "data": [],
                }
            )
        if path == "/api/login":
            if req.method != "POST":
                return err("api.err.Invalid", 400)
            return self.login(req, snap)
        if path == "/api/logout":
            self.sessions.pop(req.cookies.get("unifises", ""), None)
            return ok([])
        if not path.startswith("/api/"):
            return Response.text("<html><body><h1>HTTP Status 404 – Not Found</h1></body></html>", 404)
        if path == "/api/auth/login" or not path.startswith(("/api/s/", "/api/self", "/api/stat/sites")):
            return err("api.err.NotFound", 404)
        token = req.cookies.get("unifises", "")
        if self.sessions.get(token, 0) < snap.t:
            self.sessions.pop(token, None)
            return err("api.err.LoginRequired", 401)
        if req.method != "GET":
            return err("api.err.NoPermission", 403)  # a read-only account
        if path == "/api/self":
            return ok(
                [
                    {
                        "_id": oid("admin", self.secrets.get("username")),
                        "name": self.secrets.get("username"),
                        "email_alert_enabled": False,
                        "is_super": False,
                        "site_role": "readonly",
                        "last_site_name": "default",
                    }
                ]
            )
        if path in ("/api/self/sites", "/api/stat/sites"):
            return ok([SITE])
        site, _, endpoint = path[len("/api/s/") :].partition("/")
        if site != SITE["name"]:
            return err("api.err.NoSiteContext", 400)
        fn = {
            "stat/device": self.stat_device,
            "stat/device-basic": self.device_basic,
            "stat/sta": self.stat_sta,
            "stat/health": self.stat_health,
            "stat/sysinfo": self.sysinfo,
            "rest/networkconf": self.networkconf,
            "rest/wlanconf": self.wlanconf,
        }.get(endpoint)
        if fn is None:
            return err("api.err.NotFound", 404)
        return ok(fn(snap))

    def login(self, req: Request, snap: Snapshot) -> Response:
        try:
            body = req.json() or {}
        except ValueError:
            return err("api.err.Invalid", 400)
        if not isinstance(body, dict) or (body.get("username"), body.get("password")) != (
            self.secrets.get("username"),
            self.secrets.get("password"),
        ):
            return err("api.err.Invalid", 400)
        token = secrets.token_hex(16)
        self.sessions = {k: v for k, v in self.sessions.items() if v > snap.t}
        self.sessions[token] = snap.t + (SESSION_TTL if body.get("remember") else 3600)
        csrf = secrets.token_hex(16)
        return Response.json(
            {"meta": {"rc": "ok"}, "data": []},
            headers=[
                ("Set-Cookie", f"unifises={token}; Path=/; Secure; HttpOnly; SameSite=None"),
                ("Set-Cookie", f"csrf_token={csrf}; Path=/; Secure"),
                ("X-Csrf-Token", csrf),
            ],
        )

    # ------------------------------------------------------------ the world

    def managed(self, snap: Snapshot) -> list[Node]:
        """The switch first, then the APs (adoption order)."""
        nodes = [n for n in snap.nodes.values() if n.api == self.name and n.kind in ("switch", "ap")]
        return sorted(nodes, key=lambda n: (n.kind != "switch", n.ip or ""))

    def connected(self, snap: Snapshot, n: Node) -> bool:
        """Up and able to reach the controller (its uplink up)."""
        if not snap.up(n.name):
            return False
        if n.kind == "switch":
            return any(snap.port_up(n.name, p) for p, port in n.ports.items() if port.kind == "lag")
        if snap.port_up(n.name, "eth0"):
            return True
        mesh = next((p for p, port in n.ports.items() if port.descr == "mesh uplink"), None)
        if mesh and snap.port_up(n.name, mesh):
            parent = snap.peer(n.name, mesh)
            return bool(parent and self.connected(snap, parent[0]))
        return False

    def firmware(self, snap: Snapshot, n: Node) -> tuple[str, str | None]:
        """(installed, offered or None)."""
        if n.kind == "switch":
            return SWITCH_FIRMWARE
        new, old = AP_FIRMWARE.get(n.version, (n.version, n.version))
        # Offered from Monday; installed by the AP's Wednesday reboot.
        upgraded = snap.boot(n.name) >= snap.t - wsec(snap.t)
        return (new, None) if upgraded else (old, new)

    def clients(self, snap: Snapshot, ap: Node) -> list[tuple[Assoc, str, str, int]]:
        """(association, radio port, band, channel) of each client, on a band its SSID has."""
        out = []
        for a in snap.wifi_clients(ap.name):
            ssid = snap.design.ssids.get(a.ssid)
            radio, band, channel = a.radio, a.band, a.channel
            if ssid and band not in ssid.bands:
                # world.radio_for ignores the SSID's bands (Acme-IoT is 2.4 GHz only).
                port = next((p for p in ap.ports.values() if p.kind == "wifi" and p.band in ssid.bands), None)
                if port:
                    radio, band, channel = port.name, port.band or band, self.channel(ap, port.band or band)
            out.append((a, radio, band, channel))
        return out

    @staticmethod
    def channel(ap: Node, band: str) -> int:
        chan = CHANNELS.get(band, [36])
        return chan[int(h01("chan", ap.name, band) * len(chan))]

    @staticmethod
    def radios(ap: Node) -> list[Port]:
        return [p for p in ap.ports.values() if p.kind == "wifi" and p.band]

    def ssids_on(self, snap: Snapshot, radio: Port) -> list[str]:
        return [s for s in SSIDS if s in snap.design.ssids and radio.band in snap.design.ssids[s].bands]

    # -------------------------------------------------------------- devices

    def stat_device(self, snap: Snapshot) -> list[dict[str, Any]]:
        return [self.device(snap, n) for n in self.managed(snap)]

    def device_basic(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for n in self.managed(snap):
            code = MODEL_CODES.get(n.model, n.model)
            out.append(
                {
                    "mac": n.mac,
                    "state": 1 if self.connected(snap, n) else 0,
                    "adopted": True,
                    "disabled": False,
                    "type": "usw" if n.kind == "switch" else "uap",
                    "model": code,
                    "name": n.name,
                }
            )
        return out

    def device(self, snap: Snapshot, n: Node) -> dict[str, Any]:
        code = MODEL_CODES.get(n.model, n.model)
        connected = self.connected(snap, n)
        current, offered = self.firmware(snap, n)
        uptime = snap.uptime(n.name) if connected else 0
        d: dict[str, Any] = {
            "_id": oid("device", n.name),
            "mac": n.mac,
            "ip": n.ip,
            "name": n.name,
            "model": code,
            "model_in_lts": False,
            "model_in_eol": False,
            "type": "usw" if n.kind == "switch" else "uap",
            "serial": n.mac.replace(":", "").upper(),
            "version": current,
            "displayable_version": current.rsplit(".", 1)[0],
            "upgradable": offered is not None,
            "adopted": True,
            "disabled": False,
            "site_id": SITE["_id"],
            "board_rev": BOARD_REV.get(code, 1),
            "architecture": "aarch64" if n.kind == "ap" else "mips",
            "inform_url": "http://10.10.10.20:8080/inform",
            "inform_ip": "10.10.10.20",
            "cfgversion": f"{int(h01('cfg', n.name) * 2**48):012x}",
            "config_network": {"type": "dhcp" if n.kind == "ap" else "static", "ip": n.ip},
            "license_state": "registered",
            "state": 1 if connected else 0,
            "provisioned_at": int(snap.t - (snap.t - snap.boot(n.name)) * 0.5) if connected else None,
            "last_seen": int(snap.t if connected else snap.t - 60),
            "uptime": uptime,
            "_uptime": uptime,
            "ethernet_table": [{"mac": n.mac, "num_port": 52 if n.kind == "switch" else 1, "name": "eth0"}],
        }
        if offered:
            d["upgrade_to_firmware"] = offered
        if not connected:
            return d
        cpu, mem = snap.cpu(n.name), snap.mem(n.name)
        mem_total = (2048 if n.kind == "switch" else 1024 if n.model != "U6-Mesh" else 512) * 1048576
        d["system-stats"] = {"cpu": f"{cpu:.1f}", "mem": f"{mem:.1f}", "uptime": str(uptime)}
        load = round(cpu / 100 * (4 if n.kind == "ap" else 2), 2)
        d["sys_stats"] = {
            "loadavg_1": f"{load:.2f}",
            "loadavg_5": f"{load * 0.9:.2f}",
            "loadavg_15": f"{load * 0.85:.2f}",
            "mem_total": mem_total,
            "mem_used": int(mem_total * mem / 100),
            "mem_buffer": int(mem_total * 0.04),
        }
        if n.kind == "switch":
            self.switch(snap, n, d)
        else:
            self.ap(snap, n, d)
        d["lldp_table"] = self.lldp_table(snap, n)
        return d

    def port_entry(self, snap: Snapshot, n: Node, p: Port, idx: int, name: str) -> dict[str, Any]:
        st = snap.port(n.name, p.name)
        if not st.up and snap.peer(n.name, p.name) is None:
            # Never cabled (the world counts the mesh AP's own traffic on its unused eth0).
            st = PortState(False, None, 0, 0, 0, 0, 0, 0, 0.0, 0.0)
        media = "SFP+" if p.kind == "sfp" else {2500: "2.5GE", 10000: "10GE"}.get(p.speed or 0, "GE")
        e: dict[str, Any] = {
            "port_idx": idx,
            "name": name,
            "media": media,
            "up": st.up,
            "enable": p.admin_up,
            "speed": st.speed or 0,
            "full_duplex": st.up,
            "autoneg": True,
            "rx_bytes": st.rx_bytes,
            "tx_bytes": st.tx_bytes,
            "rx_packets": st.rx_packets,
            "tx_packets": st.tx_packets,
            "rx_errors": st.rx_errors,
            "tx_errors": st.tx_errors,
            "rx_dropped": 0,
            "tx_dropped": 0,
            "rx_bytes-r": int(st.rx_rate),
            "tx_bytes-r": int(st.tx_rate),
            "bytes-r": int(st.rx_rate + st.tx_rate),
            "speed_caps": 1048623 if p.kind != "sfp" else 1048832,
            "op_mode": "switch",
            "stp_state": "forwarding" if st.up else "disabled",
            "is_uplink": False,
            "sfp_found": False,
            "flowctrl_rx": False,
            "flowctrl_tx": False,
        }
        return e

    def switch(self, snap: Snapshot, n: Node, d: dict[str, Any]) -> None:
        nets = self.network_ids(snap)
        lag = next((p for p in n.ports.values() if p.kind == "lag"), None)
        members = list(lag.members) if lag else []
        table = []
        poe_total = 0.0
        for p in sorted((p for p in n.ports.values() if p.kind in ("ethernet", "sfp")), key=lambda x: x.index):
            idx = p.index
            e = self.port_entry(snap, n, p, idx, p.name)
            peer = snap.peer(n.name, p.name)
            e["native_networkconf_id"] = nets.get(p.untagged or 10, nets[10])
            e["tagged_vlan_mgmt"] = "custom" if p.tagged else "block_all"
            if p.tagged:
                e["excluded_networkconf_ids"] = [
                    nid for v, nid in nets.items() if v not in p.tagged and v != p.untagged
                ]
            if p.poe:
                e["port_poe"] = True
                e["poe_caps"] = 7
                e["poe_mode"] = "auto"
                watts = POE_WATTS.get(peer[0].kind, 0.0) if peer and e["up"] else 0.0
                if peer and peer[0].model == "U7-Pro":
                    watts = 18.2
                watts = round(watts * (0.92 + 0.16 * h01("poe", n.name, p.name, snap.b)), 2) if watts else 0.0
                poe_total += watts
                e["poe_enable"] = watts > 0
                e["poe_good"] = watts > 0
                e["poe_class"] = "Class 4" if watts > 7 else "Class 2" if watts else "Unknown"
                e["poe_power"] = f"{watts:.2f}"
                e["poe_voltage"] = f"{53.1 + h01('v', p.name) * 0.6:.2f}" if watts else "0.00"
                e["poe_current"] = f"{watts / 53.4 * 1000:.2f}" if watts else "0.00"
            if p.kind == "sfp" and peer:
                rx = -2.1 - 1.4 * h01("sfp-rx", p.name)
                e.update(
                    {
                        "sfp_found": True,
                        "sfp_vendor": "Ubiquiti Inc.",
                        "sfp_part": "UACC-OM-MM-10G-D",
                        "sfp_serial": f"UI{int(h01('sfp', n.name, p.name) * 1e8):08d}",
                        "sfp_compliance": "10G Base-SR",
                        "sfp_rev": "A",
                        "sfp_temperature": f"{36 + h01('sfpt', p.name) * 6:.1f}",
                        "sfp_voltage": "3.29",
                        "sfp_current": f"{6.2 + h01('sfpc', p.name):.2f}",
                        "sfp_txpower": f"{-2.4 + h01('sfptx', p.name) * 0.8:.2f}",
                        "sfp_rxpower": f"{rx:.2f}",
                    }
                )
            if p.name in members:
                e["op_mode"] = "aggregate"
                e["is_uplink"] = True
                e["lacp_state"] = [{"member_port": idx, "active": e["up"]}]
                if p.name == members[0]:
                    e["aggregate_num_ports"] = len(members)
                    e["aggregated_by"] = False
                else:
                    e["aggregated_by"] = n.ports[members[0]].index
            else:
                e["aggregated_by"] = False
            e["mac_table_count"] = 0
            table.append(e)
        by_name = {p.name: p.index for p in n.ports.values()}
        for _m, port, _v in snap.fdb(n.name):
            idx = by_name.get(members[0] if port == (lag.name if lag else None) else port)
            for e in table:
                if e["port_idx"] == idx:
                    e["mac_table_count"] += 1
        d["port_table"] = table
        d["total_max_power"] = 600
        d["poe_power_used"] = f"{poe_total:.2f}"
        d["has_fan"] = True
        d["fan_level"] = 30 + int(h01("fan", snap.b) * 20)
        d["has_temperature"] = True
        temp = snap.temperature(n.name)
        d["general_temperature"] = temp
        d["temperatures"] = [
            {"name": "CPU", "type": "cpu", "value": temp},
            {"name": "Local", "type": "board", "value": round(temp - 6.5, 1)},
            {"name": "PHY", "type": "other", "value": round(temp + 4.0, 1)},
        ]
        if lag:
            st = snap.port(n.name, lag.name)
            core = snap.peer(n.name, members[0]) if members else None
            d["uplink"] = {
                "type": "wire",
                "up": st.up,
                "speed": st.speed or 0,
                "full_duplex": True,
                "name": "eth48",
                "port_idx": n.ports[members[0]].index,
                "media": "SFP+",
                "max_speed": 10000,
                "uplink_mac": core[0].mac if core else None,
                "uplink_device_name": core[0].name if core else None,
                "uplink_remote_port": None,
                "uplink_source": "lldp",
                "rx_bytes": st.rx_bytes,
                "tx_bytes": st.tx_bytes,
                "rx_bytes-r": int(st.rx_rate),
                "tx_bytes-r": int(st.tx_rate),
            }
        d["num_sta"] = sum(1 for e in self.wired(snap) if e["sw_mac"] == n.mac)
        d["stp_version"] = "rstp"
        d["stp_priority"] = "32768"
        d["jumboframe_enabled"] = False
        d["dot1x_portctrl_enabled"] = False

    def ap(self, snap: Snapshot, n: Node, d: dict[str, Any]) -> None:
        eth = n.ports["eth0"]
        e = self.port_entry(snap, n, eth, 1, "Port 1")
        e["is_uplink"] = True
        e.pop("speed_caps", None)
        d["port_table"] = [e]
        clients = self.clients(snap, n)
        radio_table, radio_stats, vaps = [], [], []
        for k, r in enumerate(self.radios(n)):
            band = r.band or "5"
            on = [c for c in clients if c[1] == r.name]
            chan = self.channel(n, band)
            radio_table.append(
                {
                    "name": r.name,
                    "radio": RADIO[band],
                    "channel": chan,
                    "ht": HT[band],
                    "tx_power_mode": "auto",
                    "min_rssi_enabled": False,
                    "nss": 2 if band == "2.4" else 4,
                    "max_txpower": 23 if band == "2.4" else 26,
                    "min_txpower": 6,
                    "is_11ax": True,
                    "is_11be": n.model == "U7-Pro",
                    "has_dfs": band == "5",
                }
            )
            st = snap.port(n.name, r.name)
            radio_stats.append(
                {
                    "name": r.name,
                    "radio": RADIO[band],
                    "channel": chan,
                    "state": "RUN",
                    "num_sta": len(on),
                    "user-num_sta": sum(1 for c in on if not self.guest(snap, c[0])),
                    "guest-num_sta": sum(1 for c in on if self.guest(snap, c[0])),
                    "tx_power": 20 if band == "2.4" else 23,
                    "cu_total": 18 + int(h01("cu", n.name, band, snap.b) * 40),
                    "cu_self_rx": 5,
                    "cu_self_tx": 7,
                    "satisfaction": 92 if n.name != "ap-patio-01" else 61,
                    "tx_packets": st.tx_packets,
                    "tx_retries": st.tx_packets // 40,
                }
            )
            names = self.ssids_on(snap, r)
            share = sum(SSID_SHARE[s] for s in names) or 1.0
            for j, s in enumerate(names):
                ssid = snap.design.ssids[s]
                frac = SSID_SHARE[s] / share
                on_ssid = [c for c in on if c[0].ssid == s]
                vaps.append(
                    {
                        "name": f"wifi{k}ap{j}",
                        "essid": s,
                        "radio": RADIO[band],
                        "radio_name": r.name,
                        "bssid": bssid(r.mac or n.mac, j),
                        "channel": chan,
                        "up": True,
                        "state": "RUN",
                        "id": oid("wlan", s),
                        "usage": "guest" if ssid.guest else "user",
                        "is_guest": ssid.guest,
                        "num_sta": len(on_ssid),
                        "rx_bytes": int(st.rx_bytes * frac),
                        "tx_bytes": int(st.tx_bytes * frac),
                        "rx_packets": int(st.rx_packets * frac),
                        "tx_packets": int(st.tx_packets * frac),
                        "satisfaction": 95 if n.name != "ap-patio-01" else 58,
                        "avg_client_signal": int(sum(c[0].signal for c in on_ssid) / len(on_ssid)) if on_ssid else 0,
                        "vwire_enabled": False,
                    }
                )
        d["radio_table"] = radio_table
        d["radio_table_stats"] = radio_stats
        d["vap_table"] = vaps
        d["num_sta"] = len(clients)
        d["user-num_sta"] = sum(1 for c in clients if not self.guest(snap, c[0]))
        d["guest-num_sta"] = sum(1 for c in clients if self.guest(snap, c[0]))
        if n.model == "U6-Enterprise" or n.model == "U7-Pro":
            d["has_temperature"] = True
            d["general_temperature"] = snap.temperature(n.name)
        mesh = next((p for p in n.ports.values() if p.descr == "mesh uplink"), None)
        if snap.port_up(n.name, "eth0"):
            peer = snap.peer(n.name, "eth0")
            st = snap.port(n.name, "eth0")
            up: dict[str, Any] = {
                "type": "wire",
                "up": True,
                "speed": st.speed or 0,
                "full_duplex": True,
                "name": "eth0",
                "port_idx": 1,
                "media": e["media"],
                "max_speed": eth.speed,
                "uplink_source": "lldp",
                "rx_bytes": st.rx_bytes,
                "tx_bytes": st.tx_bytes,
            }
            if peer:
                pn, pp = peer
                up["uplink_mac"] = pn.mac
                up["uplink_device_name"] = pn.name
                up["uplink_remote_port"] = pp.index
            d["uplink"] = up
        elif mesh:
            peer = snap.peer(n.name, mesh.name)
            st = snap.port(n.name, mesh.name)
            sig = -58 - int(h01("mesh", snap.b) * 6)
            d["uplink"] = {
                "type": "wireless",
                "up": True,
                "name": "ath3",
                "radio": "na",
                "channel": self.channel(peer[0], "5") if peer else 36,
                "uplink_mac": peer[0].mac if peer else None,
                "ap_mac": peer[0].mac if peer else None,
                "uplink_device_name": peer[0].name if peer else None,
                "uplink_remote_port": None,
                "signal": sig,
                "rssi": sig + 95,
                "noise": -95,
                "tx_rate": 866700,
                "rx_rate": 780000,
                "rx_bytes": st.rx_bytes,
                "tx_bytes": st.tx_bytes,
                "rx_bytes-r": int(st.rx_rate),
                "tx_bytes-r": int(st.tx_rate),
                "uplink_source": "mesh",
            }
            d["mesh_sta_vap_enabled"] = True
        d["isolated"] = False
        d["vwireEnabled"] = mesh is not None

    def lldp_table(self, snap: Snapshot, n: Node) -> list[dict[str, Any]]:
        out = []
        for nb in snap.lldp(n.name):
            p = n.ports[nb.local_port]
            if p.kind not in ("ethernet", "sfp"):
                continue  # the mesh link is not LLDP
            idx = p.index if n.kind == "switch" else 1
            out.append(
                {
                    "chassis_id": nb.node.mac,
                    "chassis_id_subtype": "mac",
                    "is_wired": True,
                    "local_port_idx": idx,
                    "local_port_name": p.name if n.kind == "switch" else "Port 1",
                    "port_id": nb.port.name,
                    "port_descr": remote_port_descr(nb.node, nb.port),
                    "system_name": nb.node.hostname or nb.node.name,
                    "chassis_descr": f"{nb.node.vendor} {nb.node.model}".strip(),
                    "management_ips": [nb.mgmt_ip] if nb.mgmt_ip else [],
                }
            )
        return out

    # -------------------------------------------------------------- clients

    def guest(self, snap: Snapshot, a: Assoc) -> bool:
        s = snap.design.ssids.get(a.ssid)
        return bool(s and s.guest)

    def network_ids(self, snap: Snapshot) -> dict[int, str]:
        return {v: oid("network", v) for v in self.vlans(snap)}

    def vlans(self, snap: Snapshot) -> list[int]:
        """VLANs the UniFi devices carry (the controller's networks)."""
        vids: set[int] = set()
        for n in self.managed(snap):
            for p in n.ports.values():
                vids.update(v for v in (p.untagged, *p.tagged) if v and v != 1)
        return sorted(v for v in vids if ("hq", v) in snap.design.vlans)

    def base_client(self, snap: Snapshot, c: Node, vlan: int | None) -> dict[str, Any]:
        rand = random_mac(c.mac)
        e: dict[str, Any] = {
            "_id": oid("user", c.mac),
            "mac": c.mac,
            "site_id": SITE["_id"],
            "user_id": oid("user", c.mac),
            "oui": "" if rand else c.vendor,
            "is_guest": False,
            "first_seen": int(snap.arrived(c.name) if rand else 1786000000 + h01("first", c.name) * 4_000_000),
            "last_seen": int(snap.t),
            "uptime": max(int(snap.t - snap.arrived(c.name)), 0),
            "ip": c.ip,
            "authorized": True,
            "qos_policy_applied": True,
            "satisfaction": 98,
        }
        if c.dhcp and c.hostname:
            e["hostname"] = c.hostname
        if c.kind in ("tv", "media", "printer", "camera", "iot") and not rand:
            e["name"] = c.name  # named in the controller
        if vlan and ("hq", vlan) in snap.design.vlans:
            e["network"] = snap.design.vlans[("hq", vlan)].name
            e["network_id"] = oid("network", vlan)
        return e

    def wifi(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for ap in self.managed(snap):
            if ap.kind != "ap" or not self.connected(snap, ap):
                continue
            radios = {p.name: p for p in self.radios(ap)}
            for a, radio, band, channel in self.clients(snap, ap):
                c = a.client
                ssid = snap.design.ssids.get(a.ssid)
                e = self.base_client(snap, c, ssid.vlan if ssid else c.vlan)
                rp = radios.get(radio)
                names = self.ssids_on(snap, rp) if rp else []
                j = names.index(a.ssid) if a.ssid in names else 0
                tx, rx = a.tx_rate, a.rx_rate
                if band != a.band:
                    tx, rx = min(tx, 144), min(rx, 130)
                e.update(
                    {
                        "is_wired": False,
                        "is_guest": bool(ssid and ssid.guest),
                        "ap_mac": ap.mac,
                        "bssid": bssid(rp.mac or ap.mac, j) if rp else ap.mac,
                        "essid": a.ssid,
                        "radio": RADIO.get(band, "na"),
                        "radio_name": radio,
                        "radio_proto": "be" if ap.model == "U7-Pro" and band != "2.4" else RADIO_PROTO.get(band, "ac"),
                        "channel": channel,
                        "signal": a.signal,
                        "rssi": a.signal - a.noise,
                        "noise": a.noise,
                        "tx_rate": tx * 1000,
                        "rx_rate": rx * 1000,
                        "tx_bytes": a.tx_bytes,
                        "rx_bytes": a.rx_bytes,
                        "tx_packets": a.tx_bytes // 1200,
                        "rx_packets": a.rx_bytes // 600,
                        "tx_bytes-r": round(a.tx_rate_now, 1),
                        "rx_bytes-r": round(a.rx_rate_now, 1),
                        "assoc_time": int(a.since),
                        "latest_assoc_time": int(a.since),
                        "_uptime_by_uap": int(snap.t - a.since),
                        "_last_seen_by_uap": int(snap.t),
                        "idletime": int(h01("idle", c.name, snap.b) * 30),
                        "powersave_enabled": c.kind in ("mobile", "iot"),
                        "ccq": 333 if a.signal < -75 else 914,
                        "satisfaction": 45 if a.signal < -75 else 97,
                        "vlan": ssid.vlan if ssid else 0,
                    }
                )
                if ssid and ssid.security == "wpa2-enterprise" and c.owner:
                    e["1x_identity"] = f"{c.owner}@acme.com.br"
                out.append(e)
        return out

    def wired(self, snap: Snapshot) -> list[dict[str, Any]]:
        """Clients on the UniFi switch's access ports (not the uplink, not the APs' ports)."""
        out = []
        unifi = {n.mac for n in self.managed(snap)}
        wifi = set(snap.design.wifi)
        for sw in self.managed(snap):
            if sw.kind != "switch" or not self.connected(snap, sw):
                continue
            for m, port, vlan in snap.fdb(sw.name):
                p = sw.ports.get(port)
                if p is None or p.kind != "ethernet" or m in unifi:
                    continue
                peer = snap.peer(sw.name, port)
                if peer and peer[0].kind == "ap":
                    continue  # Wi-Fi clients: the APs report them
                c = snap.w.by_mac.get(m)
                if c is None or c.name in wifi:
                    continue
                e = self.base_client(snap, c, vlan)
                st = snap.port(c.name, snap.w.own_port(c))
                e.update(
                    {
                        "is_wired": True,
                        "sw_mac": sw.mac,
                        "sw_port": p.index,
                        "sw_depth": 1,
                        "vlan": vlan,
                        "wired_rate_mbps": snap.port_speed(sw.name, port) or 1000,
                        "wired-tx_bytes": st.rx_bytes,
                        "wired-rx_bytes": st.tx_bytes,
                        "wired-tx_packets": st.rx_packets,
                        "wired-rx_packets": st.tx_packets,
                        "wired-tx_bytes-r": round(st.rx_rate, 1),
                        "wired-rx_bytes-r": round(st.tx_rate, 1),
                    }
                )
                out.append(e)
        return out

    def stat_sta(self, snap: Snapshot) -> list[dict[str, Any]]:
        return self.wifi(snap) + self.wired(snap)

    # ---------------------------------------------------------------- site

    def stat_health(self, snap: Snapshot) -> list[dict[str, Any]]:
        devs = self.managed(snap)
        aps = [n for n in devs if n.kind == "ap"]
        sws = [n for n in devs if n.kind == "switch"]
        aps_down = sum(1 for n in aps if not self.connected(snap, n))
        sws_down = sum(1 for n in sws if not self.connected(snap, n))
        wifi, wired = self.wifi(snap), self.wired(snap)
        guests = sum(1 for c in wifi if c["is_guest"])
        rx = sum(c["rx_bytes-r"] for c in wifi)
        tx = sum(c["tx_bytes-r"] for c in wifi)
        return [
            {
                "subsystem": "wlan",
                "status": "warning" if aps_down else "ok",
                "num_ap": len(aps),
                "num_adopted": len(aps),
                "num_disabled": 0,
                "num_disconnected": aps_down,
                "num_pending": 0,
                "num_user": len(wifi) - guests,
                "num_guest": guests,
                "num_iot": 0,
                "tx_bytes-r": int(tx),
                "rx_bytes-r": int(rx),
            },
            {"subsystem": "wan", "status": "unknown"},
            {"subsystem": "www", "status": "unknown"},
            {
                "subsystem": "lan",
                "status": "warning" if sws_down else "ok",
                "num_sw": len(sws),
                "num_adopted": len(sws),
                "num_disconnected": sws_down,
                "num_pending": 0,
                "num_user": len(wired),
                "num_guest": 0,
                "num_iot": 0,
                "lan_ip": None,
            },
            {"subsystem": "vpn", "status": "unknown"},
        ]

    def sysinfo(self, snap: Snapshot) -> list[dict[str, Any]]:
        boot = snap.boot(self.node)
        return [
            {
                "timezone": "America/Sao_Paulo",
                "autobackup": True,
                "build": APP_BUILD,
                "version": APP_VERSION,
                "previous_version": "8.6.9",
                "data_retention_days": 90,
                "hostname": "unifi01",
                "name": "unifi01",
                "ip_addrs": [self.spec["ip"]],
                "inform_port": 8080,
                "https_port": 8443,
                "unsupported_device_count": 0,
                "update_available": False,
                "uptime": int(snap.t - boot),
                "is_cloud_console": False,
                "console_display_version": APP_VERSION,
            }
        ]

    def networkconf(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for vid in self.vlans(snap):
            v = snap.design.vlans[("hq", vid)]
            if vid == 10:
                gw = v.gateway or "10.10.10.1"
                plen = (v.subnet or "/24").split("/")[1]
                out.append(
                    {
                        "_id": oid("network", vid),
                        "name": "Default",
                        "purpose": "corporate",
                        "networkgroup": "LAN",
                        "ip_subnet": f"{gw}/{plen}",
                        "vlan_enabled": False,
                        "dhcpd_enabled": False,
                        "domain_name": v.domain,
                        "is_nat": True,
                        "attr_hidden_id": "LAN",
                        "attr_no_delete": True,
                        "site_id": SITE["_id"],
                    }
                )
                continue
            out.append(
                {
                    "_id": oid("network", vid),
                    "name": v.name,
                    "purpose": "vlan-only",
                    "vlan_enabled": True,
                    "vlan": vid,
                    "networkgroup": "LAN",
                    "igmp_snooping": vid == 50,
                    "dhcpguard_enabled": False,
                    "site_id": SITE["_id"],
                }
            )
        return out

    def wlanconf(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for s in SSIDS:
            ssid = snap.design.ssids.get(s)
            if not ssid:
                continue
            bands = {("2.4",): "2g", ("2.4", "5"): "both"}.get(ssid.bands, "both")
            out.append(
                {
                    "_id": oid("wlan", s),
                    "name": s,
                    "enabled": True,
                    "security": "wpaeap" if ssid.security == "wpa2-enterprise" else "wpapsk",
                    "wpa_mode": "wpa2",
                    "is_guest": ssid.guest,
                    "networkconf_id": oid("network", ssid.vlan),
                    "vlan": ssid.vlan,
                    "wlan_band": bands,
                    "wlan_bands": [{"2.4": "2g", "5": "5g", "6": "6g"}[b] for b in ssid.bands],
                    "hide_ssid": False,
                    "site_id": SITE["_id"],
                }
            )
        return out
