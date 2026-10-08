"""OpenWrt ubus over HTTP (uhttpd-mod-ubus + rpcd), the JSON-RPC API LuCI itself uses.

Emulates store-rt01, the Santos store's GL.iNet GL-MT6000 (Flint 2) on vanilla
OpenWrt 24.10.2 (mediatek/filogic): `POST /ubus` with JSON-RPC 2.0 `call`
(and `list`), rpcd sessions (`session login` with the null session, 300 s
idle timeout renewed on use, lost when the router reboots) and rpcd's ACLs:
the login is restricted to the read-only ACL the plugin's README recommends
(`ACL` below). Like uhttpd, a call outside the session's ubus ACL answers the
JSON-RPC error -32002 "Access denied" (also for an expired or unknown
session), and rpcd's file plugin answers status 6 (permission denied) for a
path outside the ACL's "file" section; a wrong password answers status 6 to
`session login`.

Objects served: system (board, info), network.device (status),
network.interface (dump), iwinfo (devices, info, assoclist), luci-rpc
(getHostHints, getDHCPLeases), file (read, list: /proc/net/arp and the
thermal zones), hostapd.<ifname> (get_clients) and session (login, access).
Everything that changes comes from the world snapshot.
"""

from __future__ import annotations

import fnmatch
import ipaddress
import secrets
from typing import Any

from ..server import HttpService, Request, Response
from ..world import Snapshot, h01

NULL_SESSION = "0" * 32
SESSION_TIMEOUT = 300  # rpcd's default (seconds without use)

KERNEL = "6.6.93"
RELEASE = {
    "distribution": "OpenWrt",
    "version": "24.10.2",
    "revision": "r28739-d9340319c6",
    "target": "mediatek/filogic",
    "description": "OpenWrt 24.10.2 r28739-d9340319c6",
    "builddate": "1750711236",
}
MEM_TOTAL = 1_031_901_184  # what the 1 GiB of the GL-MT6000 leaves to Linux
ROOT_KIB = 7_331_840  # the overlay (rootfs_data on the 8 GB eMMC), KiB
TZ_OFFSET = -3 * 3600  # America/Sao_Paulo (system info "localtime" is local)

# The ACL of the "omini" login: exactly the one in the plugin's README
# (/usr/share/rpcd/acl.d/omini.json, granted as "read"). Tests may replace it.
ACL: dict[str, dict[str, list[str]]] = {
    "ubus": {
        "system": ["board", "info"],
        "network.device": ["status"],
        "network.interface": ["dump"],
        "luci-rpc": ["getHostHints", "getDHCPLeases"],
        "iwinfo": ["devices", "info", "assoclist"],
        "file": ["read", "list"],
    },
    "file": {
        "/proc/net/arp": ["read"],
        "/sys/class/thermal": ["list"],
        "/sys/class/thermal/thermal_zone*/type": ["read"],
        "/sys/class/thermal/thermal_zone*/temp": ["read"],
    },
}
# What every session (also the null one) may call: rpcd's "unauthenticated" group.
UNAUTHENTICATED = {"session": ["access", "login"]}

# ubus objects of a LuCI install (anything else is "Object not found").
OBJECTS = {
    "session": ["create", "list", "grant", "revoke", "access", "set", "get", "unset", "destroy", "login"],
    "system": ["board", "info", "reboot", "watchdog", "signal", "validate_firmware_image", "sysupgrade"],
    "network.device": ["status", "set_alias", "set_state", "stp_init"],
    "network.interface": [
        "up",
        "down",
        "renew",
        "status",
        "prepare",
        "dump",
        "add_device",
        "remove_device",
        "notify_proto",
        "remove",
        "set_data",
    ],
    "network": ["restart", "reload", "add_host_route", "get_proto_handlers", "add_dynamic", "netns_updown"],
    "network.wireless": ["up", "down", "reconf", "status", "notify", "get_validate"],
    "luci-rpc": [
        "getBoardJSON",
        "getDHCPLeases",
        "getDSLStatus",
        "getHostHints",
        "getNetworkDevices",
        "getWirelessDevices",
    ],
    "luci": [
        "getConntrackHelpers",
        "getFeatures",
        "getInitList",
        "getLocaltime",
        "getMountPoints",
        "getRealtimeStats",
        "getTimezones",
        "getUSBDevices",
        "setInitAction",
        "setLocaltime",
        "setPassword",
    ],
    "iwinfo": ["devices", "info", "scan", "assoclist", "freqlist", "txpowerlist", "countrylist", "survey", "phyname"],
    "file": ["read", "write", "list", "stat", "md5", "remove", "exec"],
    "uci": [
        "configs",
        "get",
        "state",
        "add",
        "set",
        "delete",
        "rename",
        "order",
        "changes",
        "revert",
        "commit",
        "apply",
        "confirm",
        "rollback",
    ],
    "rc": ["list", "init"],
    "service": [
        "set",
        "add",
        "list",
        "delete",
        "signal",
        "update_start",
        "update_complete",
        "event",
        "validate",
        "get_data",
        "state",
    ],
    "dhcp": ["ipv4leases", "ipv6leases", "ipv6ra", "add_lease"],
    "hostapd": ["config_add", "config_remove", "config_set"],
}
WIFI_IFACES = ("phy0-ap0", "phy0-ap1", "phy1-ap0", "phy1-ap1")
HOSTAPD_METHODS = [
    "get_clients",
    "get_status",
    "del_client",
    "list_bans",
    "update_beacon",
    "switch_chan",
    "set_vendor_elements",
    "notify_response",
    "bss_mgmt_enable",
    "rrm_nr_get_own",
    "wps_status",
]
for _i in WIFI_IFACES:
    OBJECTS[f"hostapd.{_i}"] = HOSTAPD_METHODS

# ubus status codes (first element of a call's "result").
OK, INVALID_COMMAND, INVALID_ARGUMENT, METHOD_NOT_FOUND, NOT_FOUND, NO_DATA, PERMISSION_DENIED = 0, 1, 2, 3, 4, 5, 6

STAT_KEYS = (
    "collisions",
    "rx_frame_errors",
    "tx_compressed",
    "multicast",
    "rx_length_errors",
    "tx_dropped",
    "rx_bytes",
    "rx_missed_errors",
    "tx_errors",
    "rx_compressed",
    "rx_over_errors",
    "tx_fifo_errors",
    "rx_crc_errors",
    "rx_packets",
    "tx_heartbeat_errors",
    "rx_dropped",
    "tx_aborted_errors",
    "tx_packets",
    "rx_errors",
    "tx_bytes",
    "tx_window_errors",
    "rx_fifo_errors",
    "tx_carrier_errors",
)
COPPER_1G = ["10baseT-H", "10baseT-F", "100baseT-H", "100baseT-F", "1000baseT-F"]
COPPER_2G5 = [*COPPER_1G, "2500baseT-F"]
BAND_CHANNELS = {"2.4": [1, 6, 11], "5": [36, 52, 100, 116, 132, 149], "6": [37, 69, 101]}
# DHCPv6/SLAAC from Claro (the WAN's IPv6, a /64 on eth1 and a /56 delegated to the LAN).
WAN6 = "2804:14d:5c80:8a31::1c4"
WAN6_GW = "fe80::1a52:82ff:fef8:df5d"
LAN6_PREFIX = "2804:14d:5c80:9e00::"


class OpenWrt(HttpService):
    """store-rt01's LuCI: uhttpd serving /ubus (JSON-RPC) and the LuCI login page."""

    # Per-instance rpcd sessions: sid → {"user", "expires", "boot"}.
    def __init__(self, *args: Any, **kw: Any) -> None:
        super().__init__(*args, **kw)
        self.sessions: dict[str, dict[str, Any]] = {}

    # ------------------------------------------------------------------ HTTP

    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path.rstrip("/") or "/"
        if path == "/ubus":
            if req.method == "OPTIONS":
                return Response(
                    204, b"", [("Access-Control-Allow-Origin", "*"), ("Access-Control-Allow-Methods", "POST, OPTIONS")]
                )
            if req.method != "POST":
                return Response.json(self._error(None, -32600, "Invalid request"), 400)
            return self._ubus(req, snap)
        if req.method not in ("GET", "HEAD", "POST"):
            return Response.text("<h1>Method Not Allowed</h1>", 405)
        if path in ("/", "/index.html", "/cgi-bin/luci"):
            return self._luci_page(snap)
        return Response.text(f"<h1>404 Not Found</h1>The requested URL {req.path} was not found on this server.", 404)

    def _luci_page(self, snap: Snapshot) -> Response:
        host = snap.node(self.node).hostname or self.node
        form = (
            '<form method="post" action="/cgi-bin/luci/"><div class="cbi-map"><h2 name="content">Authorization '
            'Required</h2><div class="cbi-map-descr">Please enter your username and password.</div>'
            '<input name="luci_username" type="text" value="root" /><input name="luci_password" type="password" />'
            '<button class="btn cbi-button-positive">Log in</button></div></form>'
            '<footer><a href="https://github.com/openwrt/luci">Powered by LuCI openwrt-24.10 branch '
            f"(25.172.70543~f4ae7d7)</a> / {RELEASE['description']}</footer>"
        )
        return Response.html_title(f"{host} - LuCI", form, 403)

    # -------------------------------------------------------------- JSON-RPC

    @staticmethod
    def _error(rid: Any, code: int, message: str) -> dict[str, Any]:
        return {"jsonrpc": "2.0", "id": rid, "error": {"code": code, "message": message}}

    def _ubus(self, req: Request, snap: Snapshot) -> Response:
        try:
            body = req.json()
        except ValueError:
            return Response.json(self._error(None, -32700, "Parse error"))
        if isinstance(body, list):
            return Response.json([self._one(x, snap) for x in body])
        return Response.json(self._one(body, snap))

    def _one(self, msg: Any, snap: Snapshot) -> dict[str, Any]:
        if not isinstance(msg, dict) or msg.get("jsonrpc") != "2.0" or not isinstance(msg.get("method"), str):
            return self._error(msg.get("id") if isinstance(msg, dict) else None, -32600, "Invalid request")
        rid, method, params = msg.get("id"), msg["method"], msg.get("params")
        if method == "list":
            return self._list(rid, params)
        if method != "call":
            return self._error(rid, -32601, "Method not found")
        if not isinstance(params, list) or len(params) < 3 or not all(isinstance(p, str) for p in params[:3]):
            return self._error(rid, -32602, "Invalid parameters")
        sid, obj, func = params[:3]
        args = params[3] if len(params) > 3 else {}
        if not isinstance(args, dict):
            return self._error(rid, -32602, "Invalid parameters")
        user = self._session_user(sid, snap)
        if not self._allowed(user, obj, func):
            return self._error(rid, -32002, "Access denied")
        if obj not in OBJECTS:
            return self._error(rid, -32000, "Object not found")
        if func not in OBJECTS[obj]:
            return {"jsonrpc": "2.0", "id": rid, "result": [METHOD_NOT_FOUND]}
        status, data = self._call(user, sid, obj, func, args, snap)
        return {"jsonrpc": "2.0", "id": rid, "result": [status, data] if data is not None else [status]}

    def _list(self, rid: Any, params: Any) -> dict[str, Any]:
        """`ubus list` over HTTP: object names, or their method signatures."""
        if not params:
            return {"jsonrpc": "2.0", "id": rid, "result": sorted(OBJECTS)}
        out = {}
        for pat in params if isinstance(params, list) else []:
            for obj in sorted(OBJECTS):
                if isinstance(pat, str) and fnmatch.fnmatchcase(obj, pat):
                    out[obj] = {m: {} for m in OBJECTS[obj]}
        return {"jsonrpc": "2.0", "id": rid, "result": out}

    # -------------------------------------------------------------- sessions

    def _session_user(self, sid: str, snap: Snapshot) -> str | None:
        """The login behind a session id, or None (null, unknown, expired, or lost in a reboot)."""
        if sid == NULL_SESSION:
            return None
        s = self.sessions.get(sid)
        if s is None:
            return None
        if snap.t > s["expires"] or s["boot"] != int(snap.boot(self.node)):
            self.sessions.pop(sid, None)
            return None
        s["expires"] = snap.t + SESSION_TIMEOUT  # every use renews it
        return s["user"]

    def _allowed(self, user: str | None, obj: str, func: str) -> bool:
        if func in UNAUTHENTICATED.get(obj, []):
            return True
        if user is None:
            return False
        return any(fnmatch.fnmatchcase(obj, o) and ("*" in funcs or func in funcs) for o, funcs in ACL["ubus"].items())

    def _file_allowed(self, path: str, perm: str) -> bool:
        return any(fnmatch.fnmatchcase(path, p) and perm in perms for p, perms in ACL.get("file", {}).items())

    def _login(self, args: dict[str, Any], snap: Snapshot) -> tuple[int, Any]:
        user, password = args.get("username"), args.get("password")
        if not isinstance(user, str) or not isinstance(password, str):
            return INVALID_ARGUMENT, None
        want_user, want_pw = self.secrets.get("username"), self.secrets.get("password")
        if not want_user or user != want_user or not secrets.compare_digest(password, want_pw or ""):
            return PERMISSION_DENIED, None
        sid = secrets.token_hex(16)
        self.sessions[sid] = {"user": user, "expires": snap.t + SESSION_TIMEOUT, "boot": int(snap.boot(self.node))}
        return OK, {
            "ubus_rpc_session": sid,
            "timeout": SESSION_TIMEOUT,
            "expires": SESSION_TIMEOUT,
            "acls": {
                "access-group": {"omini": ["read"], "unauthenticated": ["read"]},
                "ubus": {**{k: list(v) for k, v in ACL["ubus"].items()}, **UNAUTHENTICATED},
                "file": {k: list(v) for k, v in ACL.get("file", {}).items()},
            },
            "data": {"username": user},
        }

    # ------------------------------------------------------------------ calls

    def _call(
        self, user: str | None, sid: str, obj: str, func: str, args: dict[str, Any], snap: Snapshot
    ) -> tuple[int, Any]:
        if obj == "session":
            if func == "login":
                return self._login(args, snap)
            if func == "access":
                s, o, f = args.get("scope", "ubus"), args.get("object"), args.get("function")
                if s == "ubus" and isinstance(o, str) and isinstance(f, str):
                    return OK, {"access": self._allowed(user, o, f)}
                if s == "file" and isinstance(o, str) and isinstance(f, str):
                    return OK, {"access": user is not None and self._file_allowed(o, f)}
                return OK, {"access": False}
            return PERMISSION_DENIED, None
        handler = {
            ("system", "board"): self._board,
            ("system", "info"): self._info,
            ("network.device", "status"): self._devices,
            ("network.interface", "dump"): self._interfaces,
            ("iwinfo", "devices"): lambda s, a: (OK, {"devices": list(WIFI_IFACES)}),
            ("iwinfo", "info"): self._iw_info,
            ("iwinfo", "assoclist"): self._iw_assoclist,
            ("luci-rpc", "getHostHints"): self._host_hints,
            ("luci-rpc", "getDHCPLeases"): self._dhcp_leases,
            ("file", "read"): self._file_read,
            ("file", "list"): self._file_list,
        }.get((obj, func))
        if handler is None and obj.startswith("hostapd.") and func == "get_clients":
            return self._hostapd_clients(obj.split(".", 1)[1], snap)
        if handler is None:
            return METHOD_NOT_FOUND, None
        return handler(snap, args)

    # system

    def _board(self, snap: Snapshot, _args: dict[str, Any]) -> tuple[int, Any]:
        n = snap.node(self.node)
        return OK, {
            "kernel": KERNEL,
            "hostname": n.hostname or n.name,
            "system": "ARMv8 Processor rev 4",
            "model": f"{n.vendor} {n.model}",
            "board_name": "glinet,gl-mt6000",
            "rootfs_type": "squashfs",
            "release": dict(RELEASE),
        }

    def _info(self, snap: Snapshot, _args: dict[str, Any]) -> tuple[int, Any]:
        cpu = snap.cpu(self.node)
        load1 = cpu / 100 * 4 * 0.55  # four Cortex-A53 cores: mostly idle
        loads = [load1, load1 * 0.85 + 0.02, load1 * 0.7 + 0.03]
        mem_used = snap.mem(self.node) / 100
        available = int(MEM_TOTAL * (1 - mem_used))
        free = int(available * 0.78)
        tmp_total = MEM_TOTAL // 2 // 1024
        tmp_used = 2_400 + int(h01("tmp", self.node, snap.b) * 900)
        root_used = 61_204
        return OK, {
            "localtime": int(snap.t) + TZ_OFFSET,
            "uptime": snap.uptime(self.node),
            "load": [int(v * 65536) for v in loads],
            "memory": {
                "total": MEM_TOTAL,
                "free": free,
                "shared": 1_634_304,
                "buffered": 0,
                "available": available,
                "cached": available - free,
            },
            "root": {"total": ROOT_KIB, "free": ROOT_KIB - root_used, "used": root_used, "avail": ROOT_KIB - root_used},
            "tmp": {"total": tmp_total, "free": tmp_total - tmp_used, "used": tmp_used, "avail": tmp_total - tmp_used},
            "swap": {"total": 0, "free": 0},
        }

    # network.device

    def _stats(self, snap: Snapshot, port: str) -> dict[str, int]:
        st = snap.port(self.node, port)
        out = dict.fromkeys(STAT_KEYS, 0)
        out.update(
            rx_bytes=st.rx_bytes,
            tx_bytes=st.tx_bytes,
            rx_packets=st.rx_packets,
            tx_packets=st.tx_packets,
            rx_errors=st.rx_errors,
            tx_errors=st.tx_errors,
            rx_crc_errors=st.rx_errors,
            multicast=st.rx_packets // 400,
        )
        return out

    @staticmethod
    def _base(kind: str, mac: str | None, up: bool, carrier: bool, mtu: int = 1500) -> dict[str, Any]:
        return {
            "external": False,
            "present": True,
            "type": kind,
            "up": up,
            "carrier": carrier,
            "auth_status": False,
            "mtu": mtu,
            "mtu6": mtu,
            "macaddr": mac,
            "txqueuelen": 1000,
            "ipv6": True,
            "ip6segmentrouting": False,
            "promisc": False,
            "rpfilter": 0,
            "acceptlocal": False,
            "igmpversion": 0,
            "mldversion": 0,
            "neigh4reachabletime": 30000,
            "neigh6reachabletime": 30000,
            "neigh4gcstaletime": 60,
            "neigh6gcstaletime": 60,
            "neigh4locktime": 100,
            "dadtransmits": 1,
            "multicast": True,
            "sendredirects": True,
            "drop_v4_unicast_in_l2_multicast": False,
            "drop_v6_unicast_in_l2_multicast": False,
            "drop_gratuitous_arp": False,
            "drop_unsolicited_na": False,
            "arp_accept": False,
            "gro": True,
        }

    def _ethernet(self, snap: Snapshot, port: str, devtype: str, supported: list[str]) -> dict[str, Any]:
        n = snap.node(self.node)
        p = n.ports[port]
        st = snap.port(self.node, port)
        d = self._base("Network device", p.mac, True, st.up)
        d["devtype"] = devtype
        d["link-advertising"] = list(supported)
        d["link-supported"] = list(supported)
        if st.up and st.speed:
            peer = snap.peer(self.node, port)
            partner = COPPER_1G if peer is None or (peer[1].speed or 1000) <= 1000 else COPPER_2G5
            d["link-partner-advertising"] = [m for m in partner if m.endswith("-F") or (peer and m.startswith("10"))]
            d["speed"] = f"{st.speed}F"
            d["autoneg"] = True
        else:
            d["link-partner-advertising"] = []
            d["speed"] = "-1"
            d["autoneg"] = True
        d["statistics"] = self._stats(snap, port)
        return d

    def _devices(self, snap: Snapshot, args: dict[str, Any]) -> tuple[int, Any]:
        n = snap.node(self.node)
        out: dict[str, Any] = {}
        lo = self._base("Network device", "00:00:00:00:00:00", True, True, 65536)
        lo["statistics"] = dict.fromkeys(STAT_KEYS, 0)
        lo["statistics"].update(
            rx_bytes=snap.uptime(self.node) * 412,
            tx_bytes=snap.uptime(self.node) * 412,
            rx_packets=snap.uptime(self.node) * 3,
            tx_packets=snap.uptime(self.node) * 3,
        )
        out["lo"] = lo
        # eth0: the DSA conduit towards the MT7531 switch (2.5 Gb/s HSGMII); it carries every LAN port's frames.
        eth0 = self._base("Network device", n.mac, True, True, 1504)
        eth0.update(
            devtype="ethernet",
            speed="2500F",
            autoneg=False,
            **{"link-advertising": [], "link-partner-advertising": [], "link-supported": ["2500baseX-F"]},
        )
        agg = dict.fromkeys(STAT_KEYS, 0)
        for k in range(1, 6):
            for key, v in self._stats(snap, f"lan{k}").items():
                agg[key] += v
        eth0["statistics"] = agg
        out["eth0"] = eth0
        out["eth1"] = self._ethernet(snap, "eth1", "ethernet", COPPER_2G5)
        for k in range(1, 6):
            out[f"lan{k}"] = self._ethernet(snap, f"lan{k}", "dsa", COPPER_1G)
        for br in ("br-lan", "br-guest"):
            p = n.ports[br]
            d = self._base("bridge", p.mac, True, True)
            d["devtype"] = "bridge"
            d["bridge-members"] = list(p.members)
            d["bridge-attributes"] = {
                "stp": False,
                "forward_delay": 8,
                "priority": 32767,
                "ageing_time": 300,
                "hello_time": 2,
                "max_age": 20,
                "vlan_filtering": False,
                "multicast_querier": False,
            }
            d["statistics"] = self._stats(snap, br)
            out[br] = d
        for w in WIFI_IFACES:
            p = n.ports[w]
            d = self._base("Network device", p.mac, True, True)
            d["devtype"] = "wlan"
            d["wireless"] = True
            d["wireless-ap"] = True
            d["statistics"] = self._stats(snap, w)
            out[w] = d
        name = args.get("name")
        if isinstance(name, str):
            return (OK, out[name]) if name in out else (NOT_FOUND, None)
        return OK, out

    # network.interface

    def _interfaces(self, snap: Snapshot, _args: dict[str, Any]) -> tuple[int, Any]:
        n = snap.node(self.node)
        up_s = max(snap.uptime(self.node) - 18, 0)
        wan_up = snap.port_up(self.node, "eth1")
        wan_ip = ipaddress.ip_interface(n.ports["eth1"].ips[0])
        modem = snap.node("store-modem").ip

        def iface(name: str, proto: str, dev: str, up: bool, **extra: Any) -> dict[str, Any]:
            base: dict[str, Any] = {
                "interface": name,
                "up": up,
                "pending": False,
                "available": True,
                "autostart": True,
                "dynamic": False,
                "proto": proto,
                "device": dev,
                "updated": ["addresses"],
                "metric": 0,
                "dns_metric": 0,
                "delegation": True,
                "ipv4-address": [],
                "ipv6-address": [],
                "ipv6-prefix": [],
                "ipv6-prefix-assignment": [],
                "route": [],
                "dns-server": [],
                "dns-search": [],
                "neighbors": [],
                "inactive": {
                    "ipv4-address": [],
                    "ipv6-address": [],
                    "route": [],
                    "dns-server": [],
                    "dns-search": [],
                    "neighbors": [],
                },
                "data": {},
            }
            if up:
                base["uptime"] = up_s
                base["l3_device"] = dev
            base.update(extra)
            return base

        lan_ip = ipaddress.ip_interface(n.ports["br-lan"].ips[0])
        guest_ip = ipaddress.ip_interface(n.ports["br-guest"].ips[0])
        out = [
            iface(
                "guest",
                "static",
                "br-guest",
                True,
                **{"ipv4-address": [{"address": str(guest_ip.ip), "mask": guest_ip.network.prefixlen}]},
            ),
            iface(
                "lan",
                "static",
                "br-lan",
                True,
                **{
                    "ipv4-address": [{"address": str(lan_ip.ip), "mask": lan_ip.network.prefixlen}],
                    "ipv6-prefix-assignment": [
                        {
                            "address": LAN6_PREFIX,
                            "mask": 64,
                            "local-address": {"address": LAN6_PREFIX + "1", "mask": 64},
                        }
                    ],
                },
            ),
            iface("loopback", "static", "lo", True, **{"ipv4-address": [{"address": "127.0.0.1", "mask": 8}]}),
        ]
        if wan_up:
            out.append(
                iface(
                    "wan",
                    "dhcp",
                    "eth1",
                    True,
                    **{
                        "updated": ["addresses", "routes", "data"],
                        "ipv4-address": [{"address": str(wan_ip.ip), "mask": wan_ip.network.prefixlen}],
                        "route": [{"target": "0.0.0.0", "mask": 0, "nexthop": modem, "source": f"{wan_ip.ip}/32"}],
                        "dns-server": [modem],
                        "data": {"dhcpserver": modem, "hostname": n.hostname, "leasetime": 14400},
                    },
                )
            )
            out.append(
                iface(
                    "wan6",
                    "dhcpv6",
                    "eth1",
                    True,
                    **{
                        "updated": ["addresses", "routes", "prefixes", "data"],
                        "ipv6-address": [{"address": WAN6, "mask": 128, "preferred": 3500, "valid": 7100}],
                        "ipv6-prefix": [
                            {
                                "address": LAN6_PREFIX,
                                "mask": 56,
                                "preferred": 3500,
                                "valid": 7100,
                                "class": "wan6",
                                "assigned": {"lan": {"address": LAN6_PREFIX, "mask": 64}},
                            }
                        ],
                        "route": [
                            {
                                "target": "::",
                                "mask": 0,
                                "nexthop": WAN6_GW,
                                "metric": 512,
                                "valid": 1700,
                                "source": "::/0",
                            }
                        ],
                        "dns-server": ["2804:14d:1:0:181:213:132:2"],
                        "data": {"passthru": "00170010280400140001000000000181021301320002"},
                    },
                )
            )
        else:  # no carrier: netifd keeps the interfaces, down and waiting for the link
            out.append(
                iface("wan", "dhcp", "eth1", False, **{"errors": [{"subsystem": "interface", "code": "NO_DEVICE"}]})
            )
            out.append(iface("wan6", "dhcpv6", "eth1", False))
        return OK, {"interface": out}

    # iwinfo / hostapd

    def _radio(self, snap: Snapshot, ifname: str) -> tuple[str, int, int]:
        """(band, channel, frequency MHz) of a Wi-Fi interface's radio (the same channel the world uses)."""
        band = snap.node(self.node).ports[ifname].band or "5"
        chans = BAND_CHANNELS.get(band, [36])
        ch = chans[int(h01("chan", self.node, band) * len(chans))]
        freq = 2407 + 5 * ch if band == "2.4" else 5000 + 5 * ch if band == "5" else 5950 + 5 * ch
        return band, ch, freq

    def _ssid(self, snap: Snapshot, ifname: str) -> str:
        vlan = snap.node(self.node).ports[ifname].untagged
        for s in snap.design.ssids.values():
            if s.site == "store" and s.vlan == vlan:
                return s.name
        return "OpenWrt"

    def _iw_info(self, snap: Snapshot, args: dict[str, Any]) -> tuple[int, Any]:
        dev = args.get("device")
        if dev not in WIFI_IFACES:
            return NOT_FOUND, None
        p = snap.node(self.node).ports[dev]
        band, ch, freq = self._radio(snap, dev)
        two = band == "2.4"
        return OK, {
            "phy": dev.split("-")[0],
            "ssid": self._ssid(snap, dev),
            "bssid": (p.mac or "").upper(),
            "country": "BR",
            "mode": "Master",
            "channel": ch,
            "center_chan1": ch if two else {36: 42, 52: 58, 100: 106, 116: 122, 132: 138, 149: 155}.get(ch, ch),
            "frequency": freq,
            "frequency_offset": 0,
            "txpower": 20 if two else 23,
            "txpower_offset": 0,
            "quality": 0,
            "quality_max": 70,
            "noise": -92 if two else -95,
            "bitrate": 0,
            "encryption": {"enabled": True, "wpa": [2], "authentication": ["psk"], "ciphers": ["ccmp"]},
            "htmodes": ["HT20", "HT40", "HE20", "HE40"]
            if two
            else ["HT20", "HT40", "VHT20", "VHT40", "VHT80", "VHT160", "HE20", "HE40", "HE80", "HE160"],
            "hwmodes": ["b", "g", "n", "ax"] if two else ["a", "n", "ac", "ax"],
            "hwmodes_text": "802.11bgn/ax" if two else "802.11an/ac/ax",
            "hwmode": "ax",
            "htmode": "HE20" if two else "HE80",
            "hardware": {"id": [5315, 30598, 5315, 30598], "name": "MediaTek MT7986"},
        }

    def _clients(self, snap: Snapshot, ifname: str) -> list[Any]:
        return [a for a in snap.wifi_clients(self.node) if a.radio == ifname]

    @staticmethod
    def _rate(mbps: int, band: str, sent: int) -> dict[str, Any]:
        two = band == "2.4"
        mhz = 20 if two else 80
        top = 287 if two else 1201
        mcs = max(0, min(11, round(mbps / top * 11)))
        return {
            "drop_misc": 0,
            "packets": sent // 1100,
            "bytes": sent,
            "ht": False,
            "vht": False,
            "he": True,
            "eht": False,
            "mhz": mhz,
            "rate": mbps * 1000,
            "mcs": mcs,
            "nss": 2,
            "he_gi": 0,
            "he_dcm": 0,
        }

    def _iw_assoclist(self, snap: Snapshot, args: dict[str, Any]) -> tuple[int, Any]:
        dev = args.get("device")
        if dev not in WIFI_IFACES:
            return NOT_FOUND, None
        results = []
        for a in self._clients(snap, dev):
            busy = a.rx_rate_now + a.tx_rate_now > 200
            inactive = int(h01("inact", a.client.name, snap.b) * (300 if busy else 9000))
            results.append(
                {
                    "mac": a.client.mac.upper(),
                    "signal": a.signal,
                    "signal_avg": a.signal + (1 if h01("avg", a.client.name, snap.b) < 0.5 else 0),
                    "noise": a.noise,
                    "inactive": inactive,
                    "connected_time": max(int(snap.t - a.since), 0),
                    "thr": int(a.tx_rate * 0.62 * 1000),
                    "authorized": True,
                    "authenticated": True,
                    "preamble": "short",
                    "wme": True,
                    "mfp": False,
                    "tdls": False,
                    "mesh llid": 0,
                    "mesh plid": 0,
                    "mesh plink": "",
                    "mesh local PS": "",
                    "mesh peer PS": "",
                    "mesh non-peer PS": "",
                    "rx": self._rate(a.rx_rate, a.band, a.rx_bytes),
                    "tx": self._rate(a.tx_rate, a.band, a.tx_bytes),
                }
            )
        return OK, {"results": results}

    def _hostapd_clients(self, ifname: str, snap: Snapshot) -> tuple[int, Any]:
        clients = {}
        for a in self._clients(snap, ifname):
            clients[a.client.mac] = {
                "auth": True,
                "assoc": True,
                "authorized": True,
                "preauth": False,
                "wds": False,
                "wmm": True,
                "ht": True,
                "vht": a.band != "2.4",
                "he": True,
                "wps": False,
                "mfp": False,
                "rrm": [0, 0, 0, 0, 0],
                "extended_capabilities": [4, 0, 0, 2, 0, 0, 0, 64],
                "aid": 1 + len(clients),
                "signature": "",
                "bytes": {"rx": a.rx_bytes, "tx": a.tx_bytes},
                "airtime": {"rx": 0, "tx": 0},
                "packets": {"rx": a.rx_bytes // 1100, "tx": a.tx_bytes // 1100},
                "rate": {"rx": a.rx_rate * 1000, "tx": a.tx_rate * 1000},
                "signal": a.signal,
                "capabilities": {},
            }
        _, _, freq = self._radio(snap, ifname)
        return OK, {"freq": freq, "clients": clients}

    # luci-rpc

    def _host_hints(self, snap: Snapshot, _args: dict[str, Any]) -> tuple[int, Any]:
        """LuCI's host hints: every MAC from the neighbour table and DHCP leases, upper-case keys."""
        hints: dict[str, dict[str, Any]] = {}
        names = {lease.mac: lease.hostname for lease in snap.leases("store") if lease.hostname}
        for ip, mac, _dev in snap.arp(self.node):
            h = hints.setdefault(mac.upper(), {"ipaddrs": [], "ip6addrs": []})
            if ip not in h["ipaddrs"]:
                h["ipaddrs"].append(ip)
        for lease in snap.leases("store"):
            h = hints.setdefault(lease.mac.upper(), {"ipaddrs": [], "ip6addrs": []})
            if lease.ip not in h["ipaddrs"]:
                h["ipaddrs"].append(lease.ip)
        for mac, h in hints.items():
            name = names.get(mac.lower())
            if name:
                h["name"] = name
        return OK, hints

    def _dhcp_leases(self, snap: Snapshot, _args: dict[str, Any]) -> tuple[int, Any]:
        rows = []
        for lease in snap.leases("store"):
            row: dict[str, Any] = {"expires": max(int(lease.end - snap.t), 0), "macaddr": lease.mac, "ipaddr": lease.ip}
            if lease.hostname:
                row["hostname"] = lease.hostname
            rows.append(row)
        return OK, {"dhcp_leases": rows, "dhcp6_leases": []}

    # file (rpcd-mod-file), restricted to the ACL's paths

    def _thermal(self, snap: Snapshot) -> dict[str, tuple[str, int]]:
        """Thermal zones of the MT7986: zone → (type, millidegrees)."""
        return {"thermal_zone0": ("cpu-thermal", int(snap.temperature(self.node, "cpu") * 1000))}

    def _arp_text(self, snap: Snapshot) -> str:
        lines = ["IP address       HW type     Flags       HW address            Mask     Device"]
        for ip, mac, dev in snap.arp(self.node):
            lines.append(f"{ip:<16} 0x1         0x2         {mac:<17}     *        {dev}")
        return "\n".join(lines) + "\n"

    def _file_read(self, snap: Snapshot, args: dict[str, Any]) -> tuple[int, Any]:
        path = args.get("path")
        if not isinstance(path, str):
            return INVALID_ARGUMENT, None
        if not self._file_allowed(path, "read"):
            return PERMISSION_DENIED, None
        if path == "/proc/net/arp":
            return OK, {"data": self._arp_text(snap)}
        zones = self._thermal(snap)
        parts = path.split("/")
        if len(parts) == 6 and parts[:4] == ["", "sys", "class", "thermal"] and parts[4] in zones:
            kind, milli = zones[parts[4]]
            if parts[5] == "type":
                return OK, {"data": kind + "\n"}
            if parts[5] == "temp":
                return OK, {"data": f"{milli}\n"}
        return NOT_FOUND, None

    def _file_list(self, snap: Snapshot, args: dict[str, Any]) -> tuple[int, Any]:
        path = args.get("path")
        if not isinstance(path, str):
            return INVALID_ARGUMENT, None
        if not self._file_allowed(path.rstrip("/") or "/", "list"):
            return PERMISSION_DENIED, None
        if path.rstrip("/") != "/sys/class/thermal":
            return NOT_FOUND, None
        boot = int(snap.boot(self.node))
        entries = []
        for name in ("cooling_device0", *self._thermal(snap)):
            entries.append(
                {
                    "name": name,
                    "type": "symlink",
                    "size": 0,
                    "mode": 41471,
                    "atime": boot,
                    "mtime": boot,
                    "ctime": boot,
                    "inode": 3000 + len(entries),
                    "uid": 0,
                    "gid": 0,
                }
            )
        return OK, {"entries": entries}
