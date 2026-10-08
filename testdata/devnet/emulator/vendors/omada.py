"""TP-Link Omada Open API of the branch office's OC200 hardware controller.

The OC200 (firmware 2.18.x, Omada Controller 5.15.x) manages the Campinas
switch (br-sw01, SG3428MP) and its two EAP670 access points; the router is a
MikroTik, so the site has no Omada gateway. The controller is not one of its
own devices.

What is emulated, as the controller answers it:

- ``GET /`` and ``GET /{omadacId}/login``: the web interface ("Omada Controller").
- ``GET /api/info``: controller version and Omada ID, no login needed.
- ``POST /openapi/authorize/token?grant_type=client_credentials`` (body
  ``{omadacId, client_id, client_secret}``) and ``grant_type=refresh_token``:
  access tokens valid 7200 s of the controller's (simulated) clock.
- ``/openapi/v1/{omadacId}/...`` with ``Authorization: AccessToken=<token>``:
  sites, devices, upgradeable devices, switch statistics, SFP DDM, LLDP
  neighbors, AP info / ports / radios, clients. Every answer is
  ``{"errorCode", "msg", "result"}``; lists are paged (``page`` and
  ``pageSize`` ≤ 1000 required, ``totalRows``).

Error codes: -44106 wrong client id / secret, -44111 bad grant type, -44112
expired token, -44113 invalid token, -1001 invalid parameters, -1600 unknown
path (HTTP 404).
"""

from __future__ import annotations

import hashlib
import secrets
from typing import Any

from .. import acme
from ..server import HttpService, Request, Response
from ..world import Snapshot, h01

CONTROLLER_VER = "5.15.20.18"
TOKEN_TTL = 7200
MAX_PAGE = 1000
# Mbit/s → Open API linkSpeed code.
SPEED_CODE = {10: 1, 100: 2, 1000: 3, 2500: 4, 10000: 5, 5000: 6, 25000: 7, 100000: 8, 40000: 9}
# Controller-side dressing of the managed devices.
MODELS = {
    "switch": ("SG3428MP v2.0", "SG3428MP"),
    "ap": ("EAP670(EU) v1.0", "EAP670"),
}
# Firmware the controller offers (its cloud check found one for br-ap02 only:
# br-ap01 was upgraded by hand, br-ap02 was offline that day).
UPGRADES = {"br-ap02": "1.1.6 Build 20250731 Rel. 51219"}
AP_UPGRADED = {"br-ap01": "1.1.6 Build 20250731 Rel. 51219"}
SERIALS = {"br-ap01": "22427J8000731", "br-ap02": "22427J8000748"}
LLDP_CAPS = {"router": "Bridge, Router", "switch": "Bridge", "ap": "Bridge, WLAN AP", "phone": "Bridge, Telephone"}
AP_ETH = {"eth0": "ETH1"}  # world port → the AP's own port name


def api_mac(m: str | None) -> str:
    return (m or "").replace(":", "-").upper()


def norm_mac(m: str) -> str:
    return m.replace("-", ":").lower()


def ok(result: Any) -> Response:
    return Response.json({"errorCode": 0, "msg": "Success.", "result": result})


def err(code: int, msg: str, status: int = 200) -> Response:
    return Response.json({"errorCode": code, "msg": msg}, status)


def uptime_str(s: int) -> str:
    s = max(int(s), 0)
    return f"{s // 86400}day(s) {s % 86400 // 3600}h {s % 3600 // 60}m {s % 60}s"


class Omada(HttpService):
    def __init__(self, *a: Any, **kw: Any) -> None:
        super().__init__(*a, **kw)
        self.tokens: dict[str, float] = {}  # access token → expiry (controller clock)
        self.refresh: dict[str, float] = {}
        oid = self.omadac_id
        self.site_id = hashlib.sha256(f"site:{oid}:branch".encode()).hexdigest()[:24]
        self.site = self.node_obj.site

    # -- identity -----------------------------------------------------------

    @property
    def omadac_id(self) -> str:
        return self.secrets.get("omadac_id") or hashlib.md5(self.name.encode()).hexdigest()

    @property
    def node_obj(self) -> Any:
        return self.ctx.world.nodes[self.node]

    def managed(self) -> list[Any]:
        """Adopted devices: switches and APs of the site whose API is this controller."""
        nodes = self.ctx.world.nodes.values()
        return [n for n in nodes if n.api == self.name and n.kind in ("switch", "ap") and n.site == self.site]

    def find(self, mac_path: str, kind: str) -> Any | None:
        m = norm_mac(mac_path)
        return next((n for n in self.managed() if n.kind == kind and n.mac == m), None)

    # -- HTTP ---------------------------------------------------------------

    def handle(self, req: Request, snap: Snapshot) -> Response:
        path = req.path.rstrip("/") or "/"
        if req.method in ("GET", "HEAD") and path in ("/", f"/{self.omadac_id}/login"):
            return Response.html_title("Omada Controller")
        if path == "/api/info" and req.method == "GET":
            return ok(
                {
                    "controllerVer": CONTROLLER_VER,
                    "apiVer": "3",
                    "configured": True,
                    "type": 1,
                    "supportApp": True,
                    "omadacId": self.omadac_id,
                    "registeredRoot": True,
                    "omadacCategory": "advanced",
                    "mspMode": False,
                }
            )
        if path == "/openapi/authorize/token":
            if req.method != "POST":
                return err(-1, "Request method not supported.", 405)
            return self.authorize(req, snap)
        prefix = "/openapi/v1/"
        if path.startswith(prefix):
            rest = path[len(prefix) :].split("/")
            if rest[0] != self.omadac_id:
                return err(-1001, "Invalid request parameters.")
            auth = req.headers.get("authorization", "")
            if not auth.startswith("AccessToken="):
                return err(-44113, "The access token is Invalid.")
            token = auth[len("AccessToken=") :].strip()
            exp = self.tokens.get(token)
            if exp is None:
                return err(-44113, "The access token is Invalid.")
            if exp <= snap.t:
                return err(
                    -44112,
                    "The access token has expired. Please re-initiate the refreshToken process "
                    "to obtain the access token.",
                )
            if req.method != "GET":
                return err(-1, "Request method not supported.", 405)
            return self.api(rest[1:], req, snap)
        return err(-1600, "Unsupported request path.", 404)

    def authorize(self, req: Request, snap: Snapshot) -> Response:
        grant = req.arg("grant_type")
        if grant == "client_credentials":
            try:
                body = req.json() or {}
            except ValueError:
                return err(-1001, "Invalid request parameters.")
            if not isinstance(body, dict):
                return err(-1001, "Invalid request parameters.")
            if (
                body.get("omadacId") != self.omadac_id
                or body.get("client_id") != self.secrets.get("client_id")
                or body.get("client_secret") != self.secrets.get("client_secret")
            ):
                return err(-44106, "The client id or client secret is invalid.")
        elif grant == "refresh_token":
            rt = req.arg("refresh_token") or ""
            if req.arg("client_id") != self.secrets.get("client_id") or req.arg("client_secret") != self.secrets.get(
                "client_secret"
            ):
                return err(-44106, "The client id or client secret is invalid.")
            if self.refresh.get(rt, 0) <= snap.t:
                return err(
                    -44114,
                    "The refresh token has expired. Please re-initiate the authentication process "
                    "to obtain the refresh token.",
                )
            del self.refresh[rt]
        else:
            return err(-44111, "The grant type is invalid.")
        # Expired tokens are forgotten.
        self.tokens = {k: v for k, v in self.tokens.items() if v > snap.t}
        access = "AT-" + secrets.token_hex(16)
        refresh = "RT-" + secrets.token_hex(16)
        self.tokens[access] = snap.t + TOKEN_TTL
        self.refresh[refresh] = snap.t + 14 * 86400
        return ok({"accessToken": access, "tokenType": "bearer", "expiresIn": TOKEN_TTL, "refreshToken": refresh})

    # -- Open API -----------------------------------------------------------

    def page(self, req: Request, rows: list[dict[str, Any]]) -> Response:
        try:
            page, size = int(req.arg("page") or ""), int(req.arg("pageSize") or "")
        except ValueError:
            return err(-1001, "Invalid request parameters.")
        if page < 1 or not 1 <= size <= MAX_PAGE:
            return err(-1001, "Invalid request parameters.")
        chunk = rows[(page - 1) * size : page * size]
        return ok({"totalRows": len(rows), "currentPage": page, "currentSize": size, "data": chunk})

    def api(self, parts: list[str], req: Request, snap: Snapshot) -> Response:
        if parts == ["sites"]:
            return self.page(req, [self.site_row()])
        if len(parts) < 3 or parts[0] != "sites":
            if len(parts) == 2 and parts[0] == "sites":
                if parts[1] != self.site_id:
                    return err(-1001, "Invalid request parameters.")
                return ok(self.site_row())
            return err(-1600, "Unsupported request path.", 404)
        if parts[1] != self.site_id:
            return err(-1005, "Operation forbidden.")
        p = parts[2:]
        if p == ["devices"]:
            return self.page(req, [self.device_row(n, snap) for n in self.managed()])
        if p == ["grid", "devices", "upgradeable"]:
            return self.page(req, self.upgradeable(snap))
        if p == ["clients"]:
            return self.page(req, self.clients(snap))
        if len(p) == 3 and p[:2] == ["stat", "switches"]:
            sw = self.find(p[2], "switch")
            return self.online(sw, snap) or ok(self.switch_stat(sw, snap))
        if len(p) >= 2 and p[0] == "switches":
            sw = self.find(p[1], "switch")
            gone = self.online(sw, snap)
            if p[2:] == ["lldp-neighbors"]:
                return gone or self.page(req, self.lldp(sw, snap))
            if p[2:] == ["ddm", "info"]:
                return gone or ok(self.ddm(sw, snap))
            if p[2:] == []:
                return gone or ok(self.device_row(sw, snap))
        if len(p) >= 2 and p[0] == "aps":
            ap = self.find(p[1], "ap")
            gone = self.online(ap, snap)
            if p[2:] == []:
                return gone or ok(self.ap_info(ap, snap))
            if p[2:] == ["ports"]:
                return gone or ok(self.ap_ports(ap, snap))
            if p[2:] == ["radios"]:
                return gone or ok(self.ap_radios(ap, snap))
        if len(p) >= 2 and p[0] == "gateways":
            return err(-39701, "The gateway does not exist.")
        return err(-1600, "Unsupported request path.", 404)

    def online(self, n: Any, snap: Snapshot) -> Response | None:
        if n is None:
            return err(-39050, "The device does not exist.")
        if not snap.up(n.name):
            return err(-39043, "The device is disconnected.")
        return None

    def site_row(self) -> dict[str, Any]:
        return {
            "siteId": self.site_id,
            "name": self.spec.get("site") or "Default",
            "region": "Brazil",
            "timeZone": "America/Sao_Paulo",
            "scenario": "Office",
            "type": 0,
            "primary": True,
        }

    # -- devices ------------------------------------------------------------

    def firmware(self, n: Any) -> str:
        build = "Build 20240923 Rel.62034" if n.kind == "switch" else "Build 20240418 Rel. 57320"
        return AP_UPGRADED.get(n.name) or f"{n.version} {build}"

    def uplink_port(self, sw: Any) -> str | None:
        gw = acme.GATEWAYS.get(sw.site)
        for name in sw.ports:
            pr = self.ctx.world.peer.get((sw.name, name))
            if pr and pr[0] == gw:
                return name
        return None

    def device_row(self, n: Any, snap: Snapshot) -> dict[str, Any]:
        model, short = MODELS[n.kind]
        up = snap.up(n.name)
        row: dict[str, Any] = {
            "mac": api_mac(n.mac),
            "name": n.name,
            "type": n.kind,
            "model": model,
            "modelName": short,
            "showModel": model,
            "modelVersion": model.rsplit(" v", 1)[-1],
            "ip": n.ip,
            "ipv6": [],
            "status": 1 if up else 0,
            "detailStatus": 14 if up else 0,
            "statusCategory": 1 if up else 0,
            "firmwareVersion": self.firmware(n),
            "sn": n.serial or SERIALS.get(n.name, ""),
            "lastSeen": int((snap.t if up else snap.boot(n.name)) * 1000),
            "tagName": None,
        }
        if n.kind == "switch":
            row["subtype"] = "smart"
        if up:
            row.update(
                uptime=uptime_str(snap.uptime(n.name)), cpuUtil=round(snap.cpu(n.name)), memUtil=round(snap.mem(n.name))
            )
        if n.kind == "ap":
            pr = self.ctx.world.peer.get((n.name, "eth0"))
            if pr and self.ctx.world.nodes[pr[0]].api == self.name:
                sw = self.ctx.world.nodes[pr[0]]
                row.update(
                    uplinkDeviceMac=api_mac(sw.mac),
                    uplinkDeviceName=sw.name,
                    uplinkDevicePort=str(sw.ports[pr[1]].index),
                    uplinkDeviceType="switch",
                )
                st = snap.port(n.name, "eth0")
                row.update(linkSpeed=SPEED_CODE.get(st.speed or 0, 0), duplex=2 if st.up else 0)
        return row

    def upgradeable(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for n in self.managed():
            latest = UPGRADES.get(n.name)
            if not latest:
                continue
            model, _ = MODELS[n.kind]
            up = snap.up(n.name)
            out.append(
                {
                    "name": n.name,
                    "mac": api_mac(n.mac),
                    "type": n.kind,
                    "model": model,
                    "modelVersion": model.rsplit(" v", 1)[-1],
                    "status": 14 if up else 0,
                    "statusCategory": 1 if up else 0,
                    "latestVersion": latest,
                    "omadacId": self.omadac_id,
                    "siteId": self.site_id,
                    "version": self.firmware(n),
                    "wirelessLinked": False,
                    "fwDownload": False,
                    "needUpgrade": True,
                }
            )
        return out

    # -- switch -------------------------------------------------------------

    def switch_stat(self, sw: Any, snap: Snapshot) -> dict[str, Any]:
        w = self.ctx.world
        ports = []
        used_poe = 0.0
        uplink = self.uplink_port(sw)
        for name, p in sorted(sw.ports.items(), key=lambda kv: kv[1].index):
            if p.kind not in ("ethernet", "sfp"):
                continue
            st = snap.port(sw.name, name)
            pr = w.peer.get((sw.name, name))
            peer = w.nodes[pr[0]] if pr else None
            powered = bool(p.poe and st.up and peer is not None and peer.kind in ("ap", "phone", "camera"))
            watts = 0.0
            if powered and peer is not None:
                watts = round((13.5 if peer.kind == "ap" else 3.4) + h01("poe", sw.name, name, snap.b) * 1.8, 1)
                used_poe += watts
            row: dict[str, Any] = {
                "port": p.index,
                "name": p.descr or f"Port{p.index}",
                "type": 3 if p.kind == "sfp" else 1,
                "operation": "SWITCHING",
                "disable": not p.admin_up,
                "maxSpeed": SPEED_CODE.get(p.speed or 1000, 3),
                "pvid": p.untagged or 1,
                "profileName": self.profile(p),
                "portStatus": {
                    "linkStatus": 1 if st.up else 0,
                    "linkSpeed": SPEED_CODE.get(st.speed or 0, 0) if st.up else 0,
                    "duplex": 2 if st.up else 0,
                    "poe": powered,
                    "poePower": watts,
                    "tx": st.tx_bytes,
                    "rx": st.rx_bytes,
                    "total": st.tx_bytes + st.rx_bytes,
                    "txRate": int(st.tx_rate),
                    "rxRate": int(st.rx_rate),
                    "stpDiscarding": False,
                },
            }
            if peer is not None and peer.api == self.name and peer.kind in ("ap", "switch") and st.up:
                row["downlink"] = {
                    "mac": api_mac(peer.mac),
                    "type": peer.kind,
                    "name": peer.name,
                    "statusCategory": 1,
                    "model": MODELS[peer.kind][1],
                }
            ports.append(row)
        out: dict[str, Any] = {
            "name": sw.name,
            "mac": api_mac(sw.mac),
            "status": 14,
            "statusCategory": 1,
            "portNum": len(ports),
            "ports": ports,
            "supportPoe": True,
            "poeRemain": round(384.0 - used_poe, 1),
            "totalPower": 384.0,
            "speeds": [0, 1, 2, 3, 5],
            "supportStack": False,
            "supportSTP": True,
        }
        if uplink:
            pr = w.peer[(sw.name, uplink)]
            gw = w.nodes[pr[0]]
            out["uplink"] = {
                "port": sw.ports[uplink].index,
                "mac": api_mac(gw.mac),
                "name": gw.name,
                "type": "other",
                "statusCategory": 1,
                "model": gw.model,
            }
        return out

    def profile(self, p: Any) -> str:
        if p.tagged:
            return "All"
        vlan = self.ctx.world.d.vlans.get((self.site, p.untagged or 0))
        return vlan.name if vlan else "Default"

    def ddm(self, sw: Any, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for name, p in sorted(sw.ports.items(), key=lambda kv: kv[1].index):
            if p.kind != "sfp" or not snap.port_up(sw.name, name):
                continue
            j = h01("ddm", sw.name, name, snap.b)
            temp = round(38.0 + 6 * snap.cpu(sw.name) / 100 + j, 1)
            rx_dbm = round(-3.2 + (j - 0.5) * 0.3, 2)
            tx_dbm = round(-2.4 + (j - 0.5) * 0.2, 2)
            out.append(
                {
                    "port": p.index,
                    "standardPort": f"1/0/{p.index}",
                    "temperature": temp,
                    "temperatureFah": round(temp * 9 / 5 + 32, 1),
                    "voltage": round(3.29 + j * 0.02, 3),
                    "biasCurrent": round(6.4 + j * 0.6, 2),
                    "txPower": round(10 ** (tx_dbm / 10), 4),
                    "txPowerDbm": tx_dbm,
                    "rxPower": round(10 ** (rx_dbm / 10), 4),
                    "rxPowerDbm": rx_dbm,
                    "transmitFault": 0,
                    "lossOfSignal": 0,
                    "dataReady": 1,
                }
            )
        return out

    def lldp(self, sw: Any, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        for nb in snap.lldp(sw.name):
            idx = sw.ports[nb.local_port].index
            other = nb.node
            port = AP_ETH.get(nb.port.name, nb.port.name) if other.kind == "ap" else nb.port.name
            out.append(
                {
                    "portId": idx,
                    "standardPort": f"1/0/{idx}",
                    "deviceId": api_mac(other.mac),
                    "systemName": other.hostname or other.name,
                    "neighborPortId": port,
                    "portDesc": nb.port.descr or nb.port.name,
                    "managementAddress": nb.mgmt_ip,
                    "ttl": 120,
                    "capabilities": LLDP_CAPS.get(other.kind, "Station"),
                }
            )
        return out

    # -- access points ------------------------------------------------------

    def ap_info(self, ap: Any, snap: Snapshot) -> dict[str, Any]:
        model, _ = MODELS["ap"]
        return {
            "type": "ap",
            "mac": api_mac(ap.mac),
            "name": ap.name,
            "ip": ap.ip,
            "wirelessLinked": False,
            "model": model,
            "firmwareVersion": self.firmware(ap),
            "cpuUtil": round(snap.cpu(ap.name)),
            "memoryUtil": round(snap.mem(ap.name)),
            "uptimeLong": snap.uptime(ap.name),
            "status": 14,
            "statusCategory": 1,
            "clientNum": len(snap.wifi_clients(ap.name)),
        }

    def ap_ports(self, ap: Any, snap: Snapshot) -> list[dict[str, Any]]:
        st = snap.port(ap.name, "eth0")
        return [
            {
                "id": f"{api_mac(ap.mac)}-1",
                "port": 1,
                "portType": 0,
                "lanPort": AP_ETH["eth0"],
                "name": None,
                "linkStatus": 1 if st.up else 0,
                "linkSpeed": SPEED_CODE.get(st.speed or 0, 0) if st.up else 0,
                "duplex": 2 if st.up else 0,
                "uplinkPort": True,
                "logicUplinkPort": True,
                "supportPoe": False,
                "portStatus": {"tx": st.tx_bytes, "rx": st.rx_bytes},
            }
        ]

    def ap_radios(self, ap: Any, snap: Snapshot) -> dict[str, Any]:
        out: dict[str, Any] = {}
        clients = snap.wifi_clients(ap.name)
        for pname, p in ap.ports.items():
            if p.kind != "wifi":
                continue
            key = {"2.4": "2g", "5": "5g", "6": "6g"}.get(p.band or "", "5g")
            st = snap.port(ap.name, pname)
            out[f"radioTraffic{key}"] = {
                "rxPkts": st.rx_packets,
                "txPkts": st.tx_packets,
                "rx": st.rx_bytes,
                "tx": st.tx_bytes,
                "rxDropPkts": 0,
                "txDropPkts": 0,
                "rxErrPkts": 0,
                "txErrPkts": 0,
            }
            chans = [a.channel for a in clients if a.radio == pname]
            channel = chans[0] if chans else (6 if key == "2g" else 36)
            out[f"wp{key}"] = {
                "actualChannel": str(channel),
                "maxTxRate": p.speed,
                "txPower": 20 if key == "2g" else 23,
                "bandWidth": "20MHz" if key == "2g" else "80MHz",
                "rdMode": "802.11b/g/n/ax" if key == "2g" else "802.11a/n/ac/ax",
            }
        return out

    # -- clients ------------------------------------------------------------

    def clients(self, snap: Snapshot) -> list[dict[str, Any]]:
        w = self.ctx.world
        managed = self.managed()
        own = {n.mac for n in managed}  # the controller itself is listed, a wired client like any other
        out: list[dict[str, Any]] = []
        for ap in managed:
            if ap.kind != "ap" or not snap.up(ap.name):
                continue
            for a in snap.wifi_clients(ap.name):
                c = a.client
                ssid = w.d.ssids.get(a.ssid)
                guest = bool(ssid and ssid.guest)
                out.append(
                    {
                        **self.client_base(c, snap),
                        "connectType": 0 if guest else 1,
                        "connectDevType": "ap",
                        "wireless": True,
                        "guest": guest,
                        "ssid": ssid.name if ssid else a.ssid,
                        "networkName": self.vlan_name(ssid.vlan if ssid else 0),
                        "vid": ssid.vlan if ssid else 0,
                        "signalLevel": max(0, min(100, 2 * (a.signal + 100))),
                        "signalRank": 4 if a.signal > -60 else 3 if a.signal > -70 else 2,
                        "apName": ap.name,
                        "apMac": api_mac(ap.mac),
                        "radioId": {"2.4": 0, "5": 1, "6": 3}.get(a.band, 1),
                        "wifiMode": 5,
                        "channel": a.channel,
                        "rxRate": a.rx_rate * 1000,
                        "txRate": a.tx_rate * 1000,
                        "powerSave": False,
                        "rssi": a.signal,
                        "snr": a.signal - a.noise,
                        "activity": int(a.tx_rate_now),
                        "uploadActivity": int(a.rx_rate_now),
                        "trafficDown": a.tx_bytes,
                        "trafficUp": a.rx_bytes,
                        "uptime": int(snap.t - a.since),
                    }
                )
        for sw in managed:
            if sw.kind != "switch" or not snap.up(sw.name):
                continue
            for m, port, vlan in snap.fdb(sw.name):
                pr = w.peer.get((sw.name, port))
                if m in own or (pr and w.nodes[pr[0]].api == self.name and w.nodes[pr[0]].kind == "ap"):
                    continue
                c = next((n for n in w.nodes.values() if n.mac == m), None)
                if c is None:
                    continue
                p = sw.ports[port]
                st = snap.port(sw.name, port)
                share = 1.0 if pr and pr[0] == c.name else 0.0
                out.append(
                    {
                        **self.client_base(c, snap),
                        "connectType": 2,
                        "connectDevType": "switch",
                        "wireless": False,
                        "guest": False,
                        "switchMac": api_mac(sw.mac),
                        "switchName": sw.name,
                        "vid": vlan,
                        "networkName": self.vlan_name(vlan),
                        "port": p.index,
                        "portName": p.descr or f"Port{p.index}",
                        "activity": int(st.tx_rate * share),
                        "uploadActivity": int(st.rx_rate * share),
                        "trafficDown": int(st.tx_bytes * share),
                        "trafficUp": int(st.rx_bytes * share),
                        "uptime": int(snap.t - snap.arrived(c.name)),
                    }
                )
        return sorted(out, key=lambda r: r["mac"])

    def client_base(self, c: Any, snap: Snapshot) -> dict[str, Any]:
        mac = api_mac(c.mac)
        host = c.hostname or None
        return {
            "id": hashlib.md5(f"{self.omadac_id}:{c.mac}".encode()).hexdigest()[:24],
            "mac": mac,
            "name": host or mac,
            "hostName": host,
            "vendor": c.vendor or None,
            "deviceType": c.kind,
            "osName": c.os or None,
            "model": c.model or None,
            "ip": c.ip,
            "ipv6List": [],
            "active": True,
            "lastSeen": int(snap.t * 1000),
            "authStatus": 0,
            "blocked": False,
        }

    def vlan_name(self, vid: int) -> str:
        v = self.ctx.world.d.vlans.get((self.site, vid))
        return v.name if v else "Default"
