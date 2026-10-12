"""Pi-hole v6 (FTL 6.2) in Docker on docker01 (host network, web interface on 10.10.20.50:8089).

The servers VLAN's local names live here as Local DNS records (``dns.hosts``).
Its DHCP server is off (HQ's firewalls hand out the addresses). The REST API,
as Pi-hole's /api/docs describes it:

- ``POST /api/auth {"password"}``: a session (``session.sid``) for the
  ``X-FTL-SID`` header (or the ``sid`` query parameter); ``DELETE /api/auth``
  ends it. A wrong password: 401 with ``session.valid = false``. At most
  MAX_SESSIONS at a time (then 429).
- Reads: ``/api/config`` (the whole configuration), ``/api/config/dns/hosts``,
  ``/api/info/version``, ``/api/info/host``, ``/api/network/interfaces``,
  ``/api/dhcp/leases``, ``/api/lists``, ``/api/groups``, ``/api/domains``,
  ``/api/clients``.
- Local DNS records: ``PUT`` / ``DELETE /api/config/dns/hosts/<"ip name">``
  (URL-encoded); a duplicate answers 400 "Item already present", a missing
  one 404.

State (the configuration, lists) is built from the design on first use and
kept for the life of the service.
"""

from __future__ import annotations

import copy
import secrets
from typing import Any

from ..server import HttpService, Request, Response
from ..world import Snapshot

MAX_SESSIONS = 16
SESSION_S = 1800
HOSTS = "/api/config/dns/hosts"
# Local names of the servers VLAN: (names, node whose address they answer).
NAMES = (
    (("netbox.acme.local",), "netbox"),
    (("rproxy-int.acme.local",), "rproxy-int"),
    (("mon01.acme.local", "zabbix.acme.local"), "mon01"),
    (("docker01.acme.local", "grafana.acme.local"), "docker01"),
)


def error(status: int, key: str, message: str, hint: str | None = None) -> Response:
    return Response.json({"error": {"key": key, "message": message, "hint": hint}, "took": 0.0001}, status)


class Pihole(HttpService):
    def __init__(self, *args: Any, **kwargs: Any) -> None:
        super().__init__(*args, **kwargs)
        self.config: dict[str, Any] | None = None
        self.sessions: dict[str, float] = {}  # sid → expiry (simulated time)

    # -- state --
    def build(self, snap: Snapshot) -> dict[str, Any]:
        hosts = [" ".join([snap.node(node).ip or "", *names]) for names, node in NAMES]
        return {
            "dns": {
                "upstreams": ["10.10.20.10", "10.10.20.11"],
                "hosts": hosts,
                "domain": {"name": "acme.local", "local": True},
                "cnameRecords": ["metrics.acme.local,grafana.acme.local"],
                "revServers": ["true,10.10.0.0/16,10.10.10.1,acme.local"],
                "blocking": {"active": True, "mode": "NULL"},
            },
            "dhcp": {
                "active": False,
                "start": "",
                "end": "",
                "router": "",
                "netmask": "",
                "leaseTime": "",
                "hosts": [],
            },
            "webserver": {
                "port": "8089o",
                "api": {
                    "pwhash": "$BALLOON-SHA256$v=1$s=1024,t=32$devnet$notarealhash",
                    "app_pwhash": "$BALLOON-SHA256$v=1$s=1024,t=32$devnet$notarealhashapp",
                    "totp_secret": "",
                    "app_sudo": True,
                    "max_sessions": MAX_SESSIONS,
                },
            },
            "misc": {"privacylevel": 0},
        }

    # -- HTTP --
    def handle(self, req: Request, snap: Snapshot) -> Response:
        if req.method in ("GET", "HEAD") and req.path in ("/", "/admin", "/admin/", "/admin/login"):
            return Response.html_title("Pi-hole - pihole", '<div class="login-box"></div>')
        if not req.path.startswith("/api/"):
            return Response.text("Not found", 404, "text/plain")
        if self.config is None:
            self.config = self.build(snap)
        if req.path == "/api/auth" and req.method == "POST":
            return self.login(req, snap)
        sid = req.headers.get("x-ftl-sid") or req.arg("sid")
        if not sid or self.sessions.get(sid, 0) < snap.t:
            self.sessions.pop(sid or "", None)
            return error(401, "unauthorized", "Unauthorized")
        self.sessions[sid] = snap.t + SESSION_S
        if req.path == "/api/auth" and req.method == "DELETE":
            del self.sessions[sid]
            return Response(204)
        if req.method == "GET":
            return self.read(req.path, snap)
        if req.path.startswith(HOSTS + "/") and req.method in ("PUT", "DELETE"):
            return self.hosts_item(req.method, req.path[len(HOSTS) + 1 :])
        return error(404, "not_found", "Not found")

    def login(self, req: Request, snap: Snapshot) -> Response:
        try:
            body = req.json() or {}
        except ValueError:
            body = {}
        if not isinstance(body, dict) or body.get("password") != self.secrets.get("password"):
            return Response.json(
                {
                    "session": {
                        "valid": False,
                        "totp": False,
                        "sid": None,
                        "validity": -1,
                        "message": "password incorrect",
                    },
                    "took": 0.003,
                },
                401,
            )
        self.sessions = {s: e for s, e in self.sessions.items() if e >= snap.t}
        if len(self.sessions) >= MAX_SESSIONS:
            return error(429, "api_seats_exceeded", "API seats exceeded", "increase webserver.api.max_sessions")
        sid = secrets.token_urlsafe(18)
        self.sessions[sid] = snap.t + SESSION_S
        return Response.json(
            {
                "session": {
                    "valid": True,
                    "totp": False,
                    "sid": sid,
                    "csrf": secrets.token_urlsafe(16),
                    "validity": SESSION_S,
                    "message": "app-password correct",
                },
                "took": 0.04,
            }
        )

    def read(self, path: str, snap: Snapshot) -> Response:
        conf = self.config or {}
        node = snap.node(self.node)
        reads = {
            "/api/config": lambda: {"config": conf},
            HOSTS: lambda: {"config": {"dns": {"hosts": conf["dns"]["hosts"]}}},
            "/api/info/version": lambda: {
                "version": {
                    "core": {"local": {"branch": "master", "version": "v6.1.2", "hash": "b4c2e1d"}},
                    "web": {"local": {"branch": "master", "version": "v6.2.1", "hash": "a91f0c3"}},
                    "ftl": {"local": {"branch": "master", "version": "v6.2.3", "hash": "7d3e5f2"}},
                    "docker": {"local": "2025.08.0"},
                }
            },
            "/api/info/host": lambda: {
                "host": {
                    "uname": {"sysname": "Linux", "nodename": "docker01", "machine": "x86_64"},
                    "model": None,
                }
            },
            # Host networking: the container sees docker01's interfaces.
            "/api/network/interfaces": lambda: {
                "interfaces": [
                    {
                        "name": "lo",
                        "address": "00:00:00:00:00:00",
                        "addresses": [{"family": "inet", "address": "127.0.0.1", "prefixlen": 8}],
                    },
                    {
                        "name": "eth0",
                        "address": node.mac,
                        "addresses": [{"family": "inet", "address": node.ip, "prefixlen": 24}],
                    },
                ]
            },
            "/api/dhcp/leases": lambda: {"leases": []},
            "/api/lists": lambda: {
                "lists": [
                    {
                        "address": "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
                        "type": "block",
                        "comment": "Migrated from /etc/pihole/adlists.list",
                        "groups": [0],
                        "enabled": True,
                        "id": 1,
                        "date_added": 1767225600,
                        "date_modified": 1767225600,
                        "date_updated": int(snap.t // 86400 * 86400),
                        "number": 81000 + int(snap.t // 86400) % 89,
                        "invalid_domains": 0,
                        "abp_entries": 0,
                        "status": 2,
                    }
                ]
            },
            "/api/groups": lambda: {
                "groups": [
                    {
                        "name": "Default",
                        "comment": "The default group",
                        "enabled": True,
                        "id": 0,
                        "date_added": 1767225600,
                        "date_modified": 1767225600,
                    }
                ]
            },
            "/api/domains": lambda: {"domains": []},
            "/api/clients": lambda: {"clients": []},
        }
        if path not in reads:
            return error(404, "not_found", "Not found")
        return Response.json({**copy.deepcopy(reads[path]()), "took": 0.0005})

    def hosts_item(self, method: str, line: str) -> Response:
        hosts: list[str] = self.config["dns"]["hosts"]  # type: ignore[index]
        parts = line.split()
        if len(parts) < 2:
            return error(400, "bad_request", "Invalid value", 'expected "ip name"')
        if method == "PUT":
            if line in hosts:
                return error(400, "bad_request", "Item already present", "Uniqueness of items is enforced")
            hosts.append(line)
            return Response.json({"took": 0.02}, 201)
        if line not in hosts:
            return error(404, "not_found", "Item not found")
        hosts.remove(line)
        return Response(204)
