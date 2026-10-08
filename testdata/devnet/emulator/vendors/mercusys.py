"""Mercusys Halo H80X mesh (local web interface, TP-Link Deco protocol): the warehouse units.

halo-wh-01 is the main unit (the one Omini talks to, 10.10.34.10), halo-wh-02 a
satellite with a wired backhaul (Horaco sw-wh-01 Port 2), halo-wh-03 a
satellite meshed over 5 GHz Wi-Fi to halo-wh-02. The mesh runs in AP mode: no
routing, the clients get their addresses from HQ's DHCP in VLAN 34.

The protocol, as the firmware speaks it (LuCI JSON under /cgi-bin/luci):

- ``POST /cgi-bin/luci/;stok=/login?form=keys``: the RSA public key for the
  password (``result.password = [n, e]`` in hex).
- ``POST /cgi-bin/luci/;stok=/login?form=auth``: the RSA key for the request
  signatures and the sequence number (``result.key``, ``result.seq``).
- ``POST /cgi-bin/luci/;stok=/login?form=login``: ``sign=<hex>&data=<b64>``.
  The signature (RSA PKCS#1 v1.5 blocks) carries ``k=<aes key>&i=<aes iv>&
  h=md5(admin+password)&s=seq+len(data)``; data is AES-128-CBC with that key
  and IV. The answer ``{"data": <b64 AES>}`` holds the ``stok`` (and the
  ``sysauth`` cookie is set). A wrong password: ``error_code -5002`` with the
  attempts left; past MAX_ATTEMPTS the login locks for LOCK_S.
- Reads: ``POST /cgi-bin/luci/;stok=<stok>/admin/<module>?form=<form>``, the
  same signature without ``k``/``i``, answers encrypted with the session key.
  A stale or unknown stok answers 403. Signing in drops the previous session
  (one admin session at a time).
- Every request must carry ``Content-Type: application/json`` (even with the
  urlencoded body), else the unit answers ``no such callback``.

halo-wh-02 reboots every day at 04:00 (2 minutes): the main unit lists it
offline, and halo-wh-03 too (its only backhaul is halo-wh-02), with no clients.
"""

from __future__ import annotations

import base64
import hashlib
import json
import re
import secrets
import time
from typing import Any

from cryptography.hazmat.primitives import padding
from cryptography.hazmat.primitives.asymmetric import padding as rsa_padding
from cryptography.hazmat.primitives.asymmetric import rsa
from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes

from ..model import Node
from ..server import HttpService, Request, Response
from ..world import Snapshot, h01

TITLE = "MERCUSYS"  # <title> of the web interface (/webpages/index.html)
MODEL = "H80X"
HARDWARE = "1.0"
USERNAME = "admin"  # the units sign in with a password only; the signature hashes "admin" + password
MAX_ATTEMPTS = 10
LOCK_S = 30 * 60  # the login stays locked this long after MAX_ATTEMPTS wrong passwords
SESSION_IDLE_S = 60 * 60
# The Wi-Fi networks of the mesh (the wlan answer carries their passwords: fake ones, never to be kept by Omini).
SSID = "Acme-Galpao"
GUEST_SSID = "Acme-Galpao-Visitante"
WIFI_PASSWORD = "Galpao#Docas2026"
GUEST_PASSWORD = "visita-galpao-77"
CLIENT_TYPES = {
    "mobile": "phone",
    "laptop": "pc",
    "desktop": "pc",
    "tablet": "tablet",
    "tv": "tv",
    "camera": "camera",
    "printer": "printer",
    "scanner": "iot_device",
    "iot": "iot_device",
}
BAND_KEYS = {"2.4": "band2_4", "5": "band5", "6": "band6"}
PATH = re.compile(r"^/cgi-bin/luci/;stok=([^/]*)(/.*)?$")


# ------------------------------------------------------------------ crypto


def aes_encrypt(key: str, iv: str, plaintext: bytes) -> bytes:
    padder = padding.PKCS7(128).padder()
    data = padder.update(plaintext) + padder.finalize()
    enc = Cipher(algorithms.AES(key.encode()), modes.CBC(iv.encode())).encryptor()
    return enc.update(data) + enc.finalize()


def aes_decrypt(key: str, iv: str, ciphertext: bytes) -> bytes:
    dec = Cipher(algorithms.AES(key.encode()), modes.CBC(iv.encode())).decryptor()
    data = dec.update(ciphertext) + dec.finalize()
    unpadder = padding.PKCS7(128).unpadder()
    return unpadder.update(data) + unpadder.finalize()


def rsa_decrypt(key: rsa.RSAPrivateKey, hex_blocks: str) -> bytes:
    """The client splits the plaintext in PKCS#1 v1.5 blocks and hex-joins them."""
    size = key.key_size // 8
    raw = bytes.fromhex(hex_blocks)
    if not raw or len(raw) % size:
        raise ValueError("bad block size")
    return b"".join(key.decrypt(raw[i : i + size], rsa_padding.PKCS1v15()) for i in range(0, len(raw), size))


def public(key: rsa.RSAPrivateKey) -> list[str]:
    n = key.public_key().public_numbers()
    return [format(n.n, "X"), format(n.e, "06X")]


def b64(text: str) -> str:
    return base64.b64encode(text.encode()).decode()


def dashed(mac: str) -> str:
    return mac.upper().replace(":", "-")


# ----------------------------------------------------------------- service


class Mercusys(HttpService):
    def __init__(self, *args: Any, **kwargs: Any) -> None:
        super().__init__(*args, **kwargs)
        # The unit makes its keys at boot (1024 bits: the smallest `cryptography` makes; the firmware's signing
        # key is 512 bits, which only changes the block size).
        self.pw_key = rsa.generate_private_key(public_exponent=65537, key_size=1024)
        self.sign_key = rsa.generate_private_key(public_exponent=65537, key_size=1024)
        self.pending_seq = 0
        self.session: dict[str, Any] | None = None
        self.failures = 0
        self.locked_until = 0.0
        self.logins = 0  # successful and refused, for tests
        self.units: list[str] = list(self.spec.get("units") or [self.node])
        self.ids = {u: hashlib.sha1(f"device-id/{u}".encode()).hexdigest().upper() for u in self.units}

    # -- HTTP --
    def handle(self, req: Request, snap: Snapshot) -> Response:
        if req.method in ("GET", "HEAD"):
            if req.path in ("/", "/webpages/index.html", "/webpages/login.html"):
                return Response.html_title(TITLE, '<div id="app"></div>')
            return Response.text("<html><body><h1>404 Not Found</h1></body></html>", 404)
        m = PATH.match(req.path)
        if req.method != "POST" or not m:
            return Response.text("<html><body><h1>404 Not Found</h1></body></html>", 404)
        if not req.headers.get("content-type", "").lower().startswith("application/json"):
            return Response.json({"error_code": 1, "msg": "no such callback"})
        stok, sub = m.group(1), m.group(2) or ""
        form = req.arg("form") or ""
        if stok == "" and sub == "/login":
            return self.login(req, form, snap)
        return self.read(req, stok, sub, form, snap)

    # -- sign in --
    def login(self, req: Request, form: str, snap: Snapshot) -> Response:
        if form == "keys":
            return Response.json({"error_code": 0, "result": {"username": "", "password": public(self.pw_key)}})
        if form == "auth":
            self.pending_seq = secrets.randbelow(900_000_000) + 100_000_000
            return Response.json({"error_code": 0, "result": {"seq": self.pending_seq, "key": public(self.sign_key)}})
        if form != "login":
            return Response.json({"error_code": -1, "msg": "no such callback"})
        signed = self._signed(req)
        if signed is None:
            return Response.json({"error_code": -40401, "msg": "Invalid signature"})
        fields, data = signed
        key, iv = fields.get("k", ""), fields.get("i", "")
        if len(key) != 16 or len(iv) != 16 or fields.get("s") != str(self.pending_seq + len(data)):
            return Response.json({"error_code": -40401, "msg": "Invalid signature"})
        try:
            payload = json.loads(aes_decrypt(key, iv, base64.b64decode(data)))
        except ValueError:
            return Response.json({"error_code": -40401, "msg": "Invalid data"})

        def answer(obj: dict[str, Any], headers: list[tuple[str, str]] | None = None) -> Response:
            return Response.json({"data": self._encrypt(key, iv, obj)}, headers=headers)

        self.logins += 1
        now = time.monotonic()
        if now < self.locked_until:
            return answer(
                {"error_code": -5003, "result": {"attemptsAllowed": 0, "lockTime": int(self.locked_until - now)}}
            )
        params = payload.get("params") if isinstance(payload.get("params"), dict) else payload
        try:
            password = rsa_decrypt(self.pw_key, str(params.get("password") or "")).decode()
        except ValueError:
            password = None
        good = self.secrets.get("password", "")
        if payload.get("operation") != "login" or password != good or fields.get("h") != self._hash(good):
            self.failures += 1
            left = MAX_ATTEMPTS - self.failures
            if left <= 0:
                self.failures, self.locked_until = 0, now + LOCK_S
                return answer({"error_code": -5003, "result": {"attemptsAllowed": 0, "lockTime": LOCK_S}})
            return answer({"error_code": -5002, "result": {"attemptsAllowed": left, "failureCount": self.failures}})
        self.failures = 0
        # One admin session at a time: the new one replaces any other.
        self.session = {
            "stok": secrets.token_hex(16),
            "cookie": secrets.token_hex(16),
            "key": key,
            "iv": iv,
            "seq": self.pending_seq,
            "h": fields["h"],
            "last": now,
            "boot": snap.boot(self.node),
        }
        cookie = f"sysauth={self.session['cookie']}; path=/cgi-bin/luci; HttpOnly"
        return answer({"error_code": 0, "result": {"stok": self.session["stok"]}}, [("Set-Cookie", cookie)])

    # -- reads --
    def read(self, req: Request, stok: str, sub: str, form: str, snap: Snapshot) -> Response:
        s = self.session
        now = time.monotonic()
        if (
            not s
            or not stok
            or stok != s["stok"]
            or now - s["last"] > SESSION_IDLE_S
            or snap.boot(self.node) != s["boot"]
            or req.cookies.get("sysauth") != s["cookie"]
        ):
            return Response.text("<html><body><h1>403 Forbidden</h1></body></html>", 403)
        signed = self._signed(req)
        if signed is None:
            return Response.text("<html><body><h1>403 Forbidden</h1></body></html>", 403)
        fields, data = signed
        key, iv = s["key"], s["iv"]

        def answer(obj: dict[str, Any]) -> Response:
            return Response.json({"data": self._encrypt(key, iv, obj)})

        if fields.get("h") != s["h"] or fields.get("s") != str(s["seq"] + len(data)):
            return answer({"error_code": -40401, "msg": "Invalid signature"})
        try:
            payload = json.loads(aes_decrypt(key, iv, base64.b64decode(data)))
        except ValueError:
            return answer({"error_code": -40401, "msg": "Invalid data"})
        s["last"] = now
        handlers = {
            ("/admin/device", "device_list"): lambda p: {"device_list": self.device_list(snap)},
            ("/admin/client", "client_list"): lambda p: {"client_list": self.client_list(snap, p)},
            ("/admin/wireless", "wlan"): lambda p: self.wlan(snap),
            ("/admin/network", "performance"): lambda p: self.performance(snap),
        }
        if sub not in {k[0] for k in handlers}:
            return Response.text("<html><body><h1>404 Not Found</h1></body></html>", 404)
        fn = handlers.get((sub, form))
        if fn is None:
            return answer({"error_code": -1, "msg": "no such callback"})
        if payload.get("operation") != "read":
            return answer({"error_code": -40209, "msg": "operation not supported"})  # the devnet is read-only
        params = payload.get("params") if isinstance(payload.get("params"), dict) else {}
        return answer({"error_code": 0, "result": fn(params)})

    def _signed(self, req: Request) -> tuple[dict[str, str], str] | None:
        """The fields of the request's signature and its (base64) data, or None when it does not decrypt."""
        body = req.form()
        try:
            text = rsa_decrypt(self.sign_key, body.get("sign", "")).decode()
            fields = dict(x.split("=", 1) for x in text.split("&") if "=" in x)
        except (ValueError, UnicodeDecodeError):
            return None
        data = body.get("data", "")
        return (fields, data) if data else None

    @staticmethod
    def _hash(password: str) -> str:
        return hashlib.md5(f"{USERNAME}{password}".encode()).hexdigest()

    @staticmethod
    def _encrypt(key: str, iv: str, obj: dict[str, Any]) -> str:
        return base64.b64encode(aes_encrypt(key, iv, json.dumps(obj, ensure_ascii=False).encode())).decode()

    # -- the mesh --
    def main_unit(self) -> str:
        return next((u for u in self.units if self.ctx.world.nodes[u].extra.get("main")), self.units[0])

    def parent(self, snap: Snapshot, unit: str) -> tuple[str, str | None]:
        """(backhaul, parent unit): wired units hang from the main unit, meshed ones from their Wi-Fi peer."""
        main = self.main_unit()
        if unit == main:
            return "wired", None
        mesh = snap.peer(unit, "mesh") if "mesh" in snap.node(unit).ports else None
        if snap.peer(unit, "eth0") is None and mesh and mesh[0].name in self.units:
            return "wireless", mesh[0].name
        return "wired", main

    def online(self, snap: Snapshot, unit: str) -> bool:
        if not snap.up(unit):
            return False
        kind, parent = self.parent(snap, unit)
        return parent is None or kind == "wired" or self.online(snap, parent)

    def mesh_signal(self, snap: Snapshot, unit: str) -> int:
        """dBm of a meshed unit's 5 GHz backhaul (halo-wh-03 sits at the far end of the warehouse)."""
        return -63 + int((h01("mesh-sig", unit, snap.b) - 0.5) * 6)

    def device_list(self, snap: Snapshot) -> list[dict[str, Any]]:
        out = []
        main = self.main_unit()
        for unit in self.units:
            n = snap.node(unit)
            on = self.online(snap, unit)
            kind, parent = self.parent(snap, unit)
            fw = snap.firmware(unit)
            ports = n.ports
            e: dict[str, Any] = {
                "nand_flash": True,
                "hardware_ver": HARDWARE,
                "software_ver": fw["current"],
                "role": "master" if unit == main else "slave",
                "previous": dashed(snap.node(parent).mac if parent else n.mac),
                "inet_status": "online" if on else "offline",
                "inet_error_msg": "well" if on else "offline",
                "group_status": "connected" if on else "disconnected",
                "nickname": "custom",
                "custom_nickname": b64(n.extra.get("nickname") or unit),
                "mac": dashed(n.mac),
                "device_ip": n.ip,
                "device_model": MODEL,
                "device_type": "MER.MESH",
                "device_id": self.ids[unit],
                "hw_id": hashlib.md5(f"hw/{MODEL}".encode()).hexdigest().upper(),
                "oem_id": hashlib.md5(f"oem/{MODEL}/BR".encode()).hexdigest().upper(),
                "product_level": 200,
                "oversized_firmware": False,
                "support_plc": False,
                "set_gateway_support": unit == main,
                "bssid_2g": ports["wifi0"].mac.upper() if on else "",
                "bssid_5g": ports["wifi1"].mac.upper() if on else "",
                "signal_level": {"band2_4": "0", "band5": "0"},
            }
            if unit != main:
                e["parent_device_id"] = self.ids[parent] if parent else ""
                e["owner_transfer"] = True
                e["connection_type"] = ["wired"] if kind == "wired" else ["band5"]
                e["eth_bkhl_ports"] = {}
                e["port_count"] = 2
                e["speed_get_support"] = True
                e["bssid_sta_2g"] = ""
                e["bssid_sta_5g"] = ports["mesh"].mac.upper() if kind == "wireless" and on else ""
                if on:
                    if kind == "wireless":
                        dbm = self.mesh_signal(snap, unit)
                        level = 3 if dbm >= -60 else 2 if dbm >= -70 else 1
                        e["signal_level"] = {"band2_4": str(max(level - 1, 1)), "band5": str(level)}
                    else:
                        e["signal_level"] = {"band2_4": "3", "band5": "3"}
            out.append(e)
        return out

    def clients_of(self, snap: Snapshot, unit: str) -> list[dict[str, Any]]:
        if not self.online(snap, unit):
            return []
        out = []
        for a in snap.wifi_clients(unit):
            out.append(
                self._client(
                    a.client, "wireless", BAND_KEYS.get(a.band, "band5"), int(a.rx_rate_now), int(a.tx_rate_now)
                )
            )
        # Anything plugged into a unit's own LAN ports (the units' backhaul does not count).
        for port in ("eth0", "eth1"):
            pr = snap.peer(unit, port) if port in snap.node(unit).ports else None
            if not pr or pr[0].name in self.units or pr[0].kind in ("switch", "router", "firewall", "ap"):
                continue
            if snap.present(pr[0].name) and snap.up(pr[0].name):
                st = snap.port(unit, port)
                out.append(self._client(pr[0], "wired", "wired", int(st.rx_rate), int(st.tx_rate)))
        return out

    @staticmethod
    def _client(c: Node, wire: str, conn: str, up: int, down: int) -> dict[str, Any]:
        return {
            "mac": dashed(c.mac),
            "up_speed": max(up, 0),  # bytes/s from the client
            "down_speed": max(down, 0),  # bytes/s to the client
            "wire_type": wire,
            "access_host": "1",
            "connection_type": conn,
            "space_id": "1",
            "ip": c.ip or "",
            "client_mesh": True,
            "online": True,
            "name": b64(c.hostname) if c.hostname else "",
            "enable_priority": False,
            "remain_time": 0,
            "owner_id": "",
            "client_type": CLIENT_TYPES.get(c.kind, "other"),
            "interface": "main",
        }

    def client_list(self, snap: Snapshot, params: dict[str, Any]) -> list[dict[str, Any]]:
        want = str(params.get("device_mac") or "default")
        if want == "default":
            return [c for u in self.units for c in self.clients_of(snap, u)]
        norm = re.sub(r"[^0-9a-f]", "", want.lower())
        for u in self.units:
            if snap.node(u).mac.replace(":", "") == norm:
                return self.clients_of(snap, u)
        return []

    def wlan(self, snap: Snapshot) -> dict[str, Any]:
        main = self.main_unit()

        def band(b: str, channels: list[int]) -> dict[str, Any]:
            ch = channels[int(h01("chan", main, b) * len(channels))]
            return {
                "host": {
                    "ssid": b64(SSID),
                    "password": b64(WIFI_PASSWORD),
                    "enable": True,
                    "channel": ch,
                    "encryption": "psk2",
                    "hidden": False,
                },
                "guest": {
                    "ssid": b64(GUEST_SSID),
                    "password": b64(GUEST_PASSWORD),
                    "enable": False,
                    "encryption": "psk2",
                    "vlan_enable": False,
                    "vlan_id": 0,
                },
            }

        return {"band2_4": band("2.4", [1, 6, 11]), "band5_1": band("5", [36, 52, 100, 116, 132, 149])}

    def performance(self, snap: Snapshot) -> dict[str, Any]:
        main = self.main_unit()
        return {"cpu_usage": round(snap.cpu(main) / 100, 2), "mem_usage": round(snap.mem(main) / 100, 2)}
