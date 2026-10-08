"""Generated test credentials of the emulated devices, and the Omini integrations that use them.

The credentials are random per devnet instance and stored in a gitignored file
(`<data>/devnet-secrets.json`, mode 0600); they are never printed. Both the
emulator (which checks them) and bootstrap.py / the tests (which configure
Omini with them) read the same file.
"""

from __future__ import annotations

import json
import os
import secrets
import uuid
from pathlib import Path
from typing import Any

from . import acme


def generate() -> dict[str, Any]:
    tok = secrets.token_urlsafe
    out: dict[str, Any] = {}
    for name, spec in acme.APIS.items():
        kind = spec["kind"]
        if kind == "opnsense":
            out[name] = {"api_key": tok(60)[:80], "api_secret": tok(60)[:80]}
        elif kind == "proxmox":
            out[name] = {"token_id": "omini@pve!omini", "token_secret": str(uuid.uuid4())}
        elif kind in ("unifi", "mikrotik", "openwrt"):
            out[name] = {"username": "omini", "password": tok(18)}
        elif kind == "omada":
            out[name] = {
                "client_id": secrets.token_hex(16),
                "client_secret": secrets.token_hex(16),
                "omadac_id": secrets.token_hex(16),
            }
        elif kind == "pfsense":
            out[name] = {"api_key": secrets.token_hex(24)}
        elif kind == "horaco" or kind == "mercusys":
            out[name] = {"username": "admin", "password": tok(12)}
    out["snmp"] = {
        "community": "acme-ro-" + secrets.token_hex(4),
        "v3": {"user": "omini", "auth": "sha256", "auth_pass": tok(16), "priv": "aes", "priv_pass": tok(16)},
    }
    return out


def load(path: Path) -> dict[str, Any]:
    """Read the credentials file, creating it (0600) on first use."""
    if path.exists():
        return json.loads(path.read_text())
    data = generate()
    path.parent.mkdir(parents=True, exist_ok=True)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, "w") as f:
        json.dump(data, f, indent=1)
    return data


def snmp_credential(name: str, sec: dict[str, Any]) -> dict[str, Any]:
    """What an SNMP agent accepts: its v2c community and, when enabled, the v3 user."""
    spec = acme.APIS[name]
    return {
        "community": spec.get("community") or sec["snmp"]["community"],
        "v3": sec["snmp"]["v3"] if spec.get("v3") else None,
    }


# Omini plugin id → the emulated APIs it reads (one integration each).
PLUGIN_OF = {
    "opnsense": "opnsense",
    "proxmox": "proxmox",
    "unifi": "unifi",
    "mikrotik": "mikrotik",
    "omada": "omada",
    "openwrt": "openwrt",
    "pfsense": "pfsense",
    "horaco": "horaco",
    "mercusys": "mercusys",
}


def integrations(urls: dict[str, str], sec: dict[str, Any]) -> list[tuple[str, str, dict[str, Any]]]:
    """(api name, plugin id, config) for every emulated API with a plugin; urls maps API name → base URL."""
    out = []
    for name, spec in acme.APIS.items():
        kind = spec["kind"]
        if kind == "snmp" or name not in urls:
            continue
        s = sec[name]
        url = urls[name]
        if kind == "opnsense":
            cfg = {"url": url, "api_key": s["api_key"], "api_secret": s["api_secret"], "verify_tls": False}
        elif kind == "proxmox":
            cfg = {"url": url, "token_id": s["token_id"], "token_secret": s["token_secret"], "verify_tls": False}
        elif kind == "unifi":
            cfg = {
                "url": url,
                "username": s["username"],
                "password": s["password"],
                "site": spec.get("site", "default"),
                "verify_tls": False,
            }
        elif kind in ("mikrotik", "openwrt"):
            cfg = {"url": url, "username": s["username"], "password": s["password"], "verify_tls": False}
        elif kind == "omada":
            cfg = {
                "url": url,
                "client_id": s["client_id"],
                "client_secret": s["client_secret"],
                "site": spec.get("site", ""),
                "verify_tls": False,
            }
        elif kind == "pfsense":
            cfg = {"url": url, "api_key": s["api_key"], "verify_tls": False}
        elif kind == "horaco":
            cfg = {"host": url, "username": s["username"], "password": s["password"]}
        elif kind == "mercusys":
            cfg = {"host": url, "password": s["password"], "username": s["username"], "verify_tls": False}
        else:
            continue
        out.append((name, PLUGIN_OF[kind], cfg))
    return out


def scan_config(sec: dict[str, Any]) -> dict[str, Any]:
    """The network scan's settings for the lab: Acme's communities and v3 user."""
    v3 = sec["snmp"]["v3"]
    return {
        "subnets": "auto",
        "learned_subnets": True,
        "snmp": True,
        "snmp_communities": f"{sec['snmp']['community']}, public",
        "snmp_v3_user": v3["user"],
        "snmp_v3_auth": v3["auth"],
        "snmp_v3_auth_pass": v3["auth_pass"],
        "snmp_v3_priv": v3["priv"],
        "snmp_v3_priv_pass": v3["priv_pass"],
    }
