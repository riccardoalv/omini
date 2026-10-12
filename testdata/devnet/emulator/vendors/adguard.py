"""AdGuard Home (v0.107) as the Home Assistant add-on on homeassistant (10.10.50.10:3000).

The IoT VLAN's local names live here as DNS rewrites. Its DHCP server is off
(HQ's firewalls hand out the addresses). The API, as AdGuard Home's
openapi.yaml describes it:

- Every ``/control/...`` request carries HTTP basic authentication (the
  web interface's user); without it, 401 with ``WWW-Authenticate``.
- Reads (``GET``): ``status``, ``rewrite/list``, ``dhcp/status``,
  ``dns_info``, ``filtering/status``, ``clients``, ``access/list``.
- Rewrites: ``POST rewrite/add`` and ``POST rewrite/delete`` with
  ``{domain, answer}``, ``PUT rewrite/update`` with ``{target, update}``.
  They answer ``OK`` (text); a refused one 400 with the reason as text.

State (rewrites, clients, filters) is built from the design on first use and
kept for the life of the service.
"""

from __future__ import annotations

import base64
import copy
import ipaddress
from typing import Any

from ..server import HttpService, Request, Response
from ..world import Snapshot

VERSION = "v0.107.57"
TITLE = "AdGuard Home"
# Local names of the IoT VLAN: (name, node whose address it answers).
NAMES = (
    ("homeassistant.acme.local", "homeassistant"),
    ("nvr01.acme.local", "nvr01"),
)


class AdGuard(HttpService):
    def __init__(self, *args: Any, **kwargs: Any) -> None:
        super().__init__(*args, **kwargs)
        self.state: dict[str, Any] | None = None

    # -- state --
    def build(self, snap: Snapshot) -> dict[str, Any]:
        ha = snap.node(self.node)
        rewrites = [{"domain": name, "answer": snap.node(node).ip} for name, node in NAMES]
        rewrites += [
            {"domain": "ha.acme.local", "answer": "homeassistant.acme.local"},  # a CNAME
            {"domain": "*.esphome.acme.local", "answer": ha.ip},  # a wildcard
        ]
        return {
            "rewrites": rewrites,
            "dns_info": {
                "upstream_dns": ["10.10.20.10", "10.10.20.11"],
                "upstream_mode": "load_balance",
                "bootstrap_dns": ["10.10.20.10"],
                "fallback_dns": [],
                "local_ptr_upstreams": ["10.10.10.1"],
                "use_private_ptr_resolvers": True,
                "ratelimit": 20,
                "blocking_mode": "default",
                "cache_size": 4194304,
                "cache_optimistic": False,
                "dnssec_enabled": False,
            },
            "filtering": {
                "enabled": True,
                "interval": 24,
                "filters": [
                    {
                        "id": 1,
                        "enabled": True,
                        "url": "https://adguardteam.github.io/HostlistsRegistry/assets/filter_1.txt",
                        "name": "AdGuard DNS filter",
                    }
                ],
                "whitelist_filters": [],
                "user_rules": ["@@||home-assistant.io^"],
            },
            "clients": [
                {
                    "name": "Home Assistant",
                    "ids": [ha.ip],
                    "use_global_settings": True,
                    "filtering_enabled": False,
                    "tags": ["device_other"],
                }
            ],
            "access": {"allowed_clients": [], "disallowed_clients": [], "blocked_hosts": ["version.bind"]},
        }

    # -- HTTP --
    def authorized(self, req: Request) -> bool:
        want = f"{self.secrets.get('username')}:{self.secrets.get('password')}"
        return req.headers.get("authorization") == "Basic " + base64.b64encode(want.encode()).decode()

    def handle(self, req: Request, snap: Snapshot) -> Response:
        if req.method in ("GET", "HEAD") and req.path in ("/", "/login.html", "/index.html"):
            return Response.html_title(TITLE, '<div id="root"></div>')
        if not req.path.startswith("/control/"):
            return Response.text("404 page not found", 404, "text/plain")
        if not self.authorized(req):
            return Response.text("Unauthorized", 401, "text/plain", [("WWW-Authenticate", 'Basic realm="Restricted"')])
        if self.state is None:
            self.state = self.build(snap)
        st = self.state
        what = req.path.removeprefix("/control/")
        if req.method == "GET":
            reads = {
                "status": lambda: self.status(snap),
                "rewrite/list": lambda: st["rewrites"],
                "dhcp/status": lambda: {
                    "enabled": False,
                    "interface_name": "",
                    "v4": {},
                    "v6": {},
                    "leases": [],
                    "static_leases": [],
                },
                "dns_info": lambda: st["dns_info"],
                "filtering/status": lambda: self.filtering(snap),
                "clients": lambda: {
                    "clients": st["clients"],
                    "auto_clients": self.auto_clients(snap),
                    "supported_tags": ["device_other", "device_pc", "device_phone"],
                },
                "access/list": lambda: st["access"],
            }
            if what not in reads:
                return Response.text("404 page not found", 404, "text/plain")
            return Response.json(copy.deepcopy(reads[what]()))
        try:
            body = req.json()
        except ValueError:
            return Response.text("json: invalid body", 400, "text/plain")
        if req.method == "POST" and what == "rewrite/add":
            return self.add(body)
        if req.method == "POST" and what == "rewrite/delete":
            return self.delete(body)
        if req.method == "PUT" and what == "rewrite/update":
            return self.update(body)
        return Response.text("Method Not Allowed", 405, "text/plain")

    def status(self, snap: Snapshot) -> dict[str, Any]:
        return {
            "dns_addresses": ["127.0.0.1", snap.node(self.node).ip, "::1"],
            "dns_port": 53,
            "http_port": self.spec["port"],
            "protection_enabled": True,
            "protection_disabled_duration": 0,
            "dhcp_available": True,
            "running": True,
            "version": VERSION,
            "language": "en",
            "start_time": int(snap.boot(self.node) * 1000),
        }

    def filtering(self, snap: Snapshot) -> dict[str, Any]:
        out = copy.deepcopy(self.state["filtering"])  # type: ignore[index]
        day = int(snap.t // 86400)
        for f in out["filters"]:
            # The lists update every day: new counts and dates (live state).
            f["rules_count"] = 61000 + day % 97
            f["last_updated"] = f"{day}"
        return out

    def auto_clients(self, snap: Snapshot) -> list[dict[str, Any]]:
        """What it saw lately: the IoT VLAN's devices online now."""
        out = []
        for n in snap.nodes.values():
            if n.vlan == 50 and n.ip and n.name != self.node and snap.up(n.name):
                out.append({"name": n.hostname or n.name, "ip": n.ip, "source": "rdns"})
        return sorted(out, key=lambda c: ipaddress.ip_address(c["ip"]))

    # -- rewrites --
    @staticmethod
    def row(body: Any) -> dict[str, str] | None:
        if not isinstance(body, dict) or not body.get("domain") or not body.get("answer"):
            return None
        return {"domain": str(body["domain"]), "answer": str(body["answer"])}

    def add(self, body: Any) -> Response:
        r = self.row(body)
        if r is None:
            return Response.text("rewrite: domain and answer are required", 400, "text/plain")
        rows = self.state["rewrites"]  # type: ignore[index]
        if r in rows:
            return Response.text("rewrite already exists", 400, "text/plain")
        rows.append(r)
        return Response.text("OK", 200, "text/plain")

    def delete(self, body: Any) -> Response:
        r = self.row(body)
        rows = self.state["rewrites"]  # type: ignore[index]
        if r is None or r not in rows:
            return Response.text("rewrite not found", 400, "text/plain")
        rows.remove(r)
        return Response.text("OK", 200, "text/plain")

    def update(self, body: Any) -> Response:
        target = self.row(body.get("target") if isinstance(body, dict) else None)
        new = self.row(body.get("update") if isinstance(body, dict) else None)
        rows = self.state["rewrites"]  # type: ignore[index]
        if target is None or new is None:
            return Response.text("rewrite: target and update are required", 400, "text/plain")
        if target not in rows:
            return Response.text("rewrite not found", 400, "text/plain")
        rows[rows.index(target)] = new
        return Response.text("OK", 200, "text/plain")
