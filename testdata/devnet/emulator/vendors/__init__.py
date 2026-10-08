"""Emulators of each vendor's API, one module per kind (acme.APIS[...]["kind"])."""

from __future__ import annotations

import importlib
from typing import Any

# kind → "module:Class"
KINDS = {
    "opnsense": "opnsense:OPNsense",
    "proxmox": "proxmox:Proxmox",
    "unifi": "unifi:UniFi",
    "mikrotik": "mikrotik:MikroTik",
    "omada": "omada:Omada",
    "openwrt": "openwrt:OpenWrt",
    "pfsense": "pfsense:PfSense",
    "horaco": "horaco:Horaco",
    "mercusys": "mercusys:Mercusys",
    "snmp": "snmp:SnmpAgent",
}


def service_class(kind: str) -> Any:
    module, cls = KINDS[kind].split(":")
    return getattr(importlib.import_module(f"{__name__}.{module}"), cls)
