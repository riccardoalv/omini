"""The configuration an OPNsense 26.7 API changes: interface assignments,
VLAN devices, Kea's DHCPv4 subnets, firewall rules and aliases.

It starts from the design (each firewall's interfaces and HQ's DHCP pools)
the first time it is read and then keeps what the API writes, so Omini's
network.set and network.delete can be applied and read back. Nothing of it
reaches the emulated network's traffic: it is the router's configuration only.
"""

from __future__ import annotations

import itertools
from dataclasses import dataclass, field
from typing import Any
from xml.sax.saxutils import escape

from ..model import Node
from ..world import Snapshot

# How the design's VLANs are reached, as the rules Omini reads them.
GUEST_LIKE = ("GUEST", "WIFI")
LOCAL_LIKE = ("CAMERAS", "IOT")


@dataclass
class Config:
    assigned: dict[str, dict[str, Any]] = field(default_factory=dict)  # opt3 → item
    vlans: dict[str, dict[str, Any]] = field(default_factory=dict)  # uuid → item
    kea: dict[str, str] = field(default_factory=dict)
    subnets: dict[str, dict[str, Any]] = field(default_factory=dict)
    rules: dict[str, dict[str, Any]] = field(default_factory=dict)
    aliases: dict[str, dict[str, Any]] = field(default_factory=dict)
    reservations: dict[str, dict[str, Any]] = field(default_factory=dict)
    mdns: dict[str, str] = field(default_factory=lambda: {"enabled": "0", "interfaces": "lan"})
    # Unbound is the DNS server; its host overrides start with one set by hand.
    overrides: dict[str, dict[str, Any]] = field(
        default_factory=lambda: {
            "override-00000000-0000-4000-8000-000000000001": {
                "enabled": "1",
                "hostname": "intranet",
                "domain": "acme.local",
                "rr": "A",
                "server": "10.10.20.10",
                "description": "Intranet (IT)",
            }
        }
    )
    woken: list[dict[str, str]] = field(default_factory=list)  # os-wol packets sent
    killed: list[str] = field(default_factory=list)  # state kills asked
    ids: Any = field(default_factory=lambda: itertools.count(1))

    def uuid(self, kind: str) -> str:
        return f"{kind}-{next(self.ids):08d}-0000-4000-8000-000000000000"


def rows(items: dict[str, dict[str, Any]], key: str = "uuid") -> dict[str, Any]:
    out = [{key: k, **v} for k, v in items.items()]
    return {"total": len(out), "rowCount": len(out), "current": 1, "rows": out}


def preset(cfg: Config, ifname: str, name: str, access: str) -> None:
    def rule(action: str, dst: str, **extra: str) -> None:
        cfg.rules[cfg.uuid("rule")] = {
            "enabled": "1",
            "action": action,
            "quick": "1",
            "interface": ifname,
            "direction": "in",
            "ipprotocol": "inet",
            "protocol": "any",
            "source_net": ifname,
            "destination_net": dst,
            "description": f"{name} {access}",
            **extra,
        }

    if access == "all":
        rule("pass", "any")
        return
    rule("pass", f"{ifname}ip", protocol="TCP/UDP", destination_port="53")
    if access == "internet":
        rule("block", "omini_private")
        rule("pass", "any")


def initial(assigned: list[tuple[str, str, str]], n: Node, snap: Snapshot) -> Config:
    """The configuration the design implies."""
    cfg = Config()
    by_device = {p.name: p for p in n.ports.values()}
    pools = {v.id: v for (site, _), v in snap.design.vlans.items() if site == "hq" and v.dhcp and v.subnet}
    kea_ifs = []
    for device, ident, descr in assigned:
        p = by_device.get(device)
        ip = next((a for a in (p.ips if p else ()) if "." in a and "/" in a), "")
        static = bool(ip) and p is not None and p.kind not in ("ppp", "wg")
        cfg.assigned[ident] = {
            "if": device,
            "descr": descr,
            "enable": "1",
            "type4": "staticv4" if static else ("pppoe" if p and p.kind == "ppp" else ""),
            "ipaddr": ip if static else "",
        }
        if p is not None and p.kind == "vlan" and p.vlan:
            cfg.vlans[cfg.uuid("vlan")] = {
                "if": p.parent or "",
                "tag": str(p.vlan),
                "pcp": "0",
                "proto": "",
                "descr": p.descr,
                "vlanif": device,
            }
            if p.vlan in pools:
                kea_ifs.append(ident)
        if static and p is not None and p.kind == "vlan":
            upper = descr.upper()
            access = (
                "internet"
                if any(w in upper for w in GUEST_LIKE)
                else "local"
                if any(w in upper for w in LOCAL_LIKE)
                else "all"
            )
            preset(cfg, ident, descr, access)
    cfg.kea = {"enabled": "1", "interfaces": ",".join(kea_ifs)}
    for v in sorted(pools.values(), key=lambda v: v.id):
        a, b = v.dhcp
        cfg.subnets[cfg.uuid("subnet")] = {
            "subnet": v.subnet,
            "pools": f"{a}-{b}",
            "option_data_autocollect": "1",
            "description": v.name,
        }
    return cfg


def post(cfg: Config, path: str, body: dict[str, Any]) -> tuple[int, dict[str, Any]]:
    """Applies one API write; (status, answer) like OPNsense's."""
    last = path.rsplit("/", 1)[-1]
    ok = {"result": "saved", "status": "ok"}
    if path == "/api/interfaces/vlan_settings/add_item":
        v = body.get("vlan") or {}
        if not v.get("if") or not str(v.get("tag", "")).isdigit():
            return 200, {"result": "failed", "validations": {"vlan.tag": "A tag and a parent are needed."}}
        if any(x["tag"] == str(v["tag"]) and x["if"] == v["if"] for x in cfg.vlans.values()):
            return 200, {"result": "failed", "validations": {"vlan.tag": "This tag is in use."}}
        uid = cfg.uuid("vlan")
        cfg.vlans[uid] = {**v, "tag": str(v["tag"]), "vlanif": f"vlan0.{v['tag']}"}
        return 200, {**ok, "uuid": uid}
    if path.startswith("/api/interfaces/vlan_settings/del_item/"):
        cfg.vlans.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/interfaces/assignment/add_item":
        item = body.get("interface") or {}
        if any(a["if"] == item.get("if") for a in cfg.assigned.values()):
            return 200, {
                "result": "failed",
                "validations": {"interface.if": "Can not assign a device to more interfaces."},
            }
        n = 1 + max([int(k[3:]) for k in cfg.assigned if k.startswith("opt")] or [0])
        cfg.assigned[f"opt{n}"] = {"enable": "1", **item}
        return 200, {**ok, "uuid": f"opt{n}"}
    if path.startswith("/api/interfaces/assignment/set_item/"):
        if last not in cfg.assigned:
            return 200, {"result": "failed"}
        cfg.assigned[last].update(body.get("interface") or {})
        return 200, ok
    if path.startswith("/api/interfaces/assignment/del_item/"):
        if last in ("wan", "lan"):
            return 200, {"result": "failed", "validations": {"interface": "locked"}}
        cfg.assigned.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/kea/dhcpv4/add_subnet":
        uid = cfg.uuid("subnet")
        cfg.subnets[uid] = dict(body.get("subnet4") or {})
        return 200, {**ok, "uuid": uid}
    if path.startswith("/api/kea/dhcpv4/set_subnet/"):
        cfg.subnets.setdefault(last, {}).update(body.get("subnet4") or {})
        return 200, ok
    if path.startswith("/api/kea/dhcpv4/del_subnet/"):
        cfg.subnets.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/kea/dhcpv4/set":
        cfg.kea.update(((body.get("dhcpv4") or {}).get("general")) or {})
        return 200, ok
    if path == "/api/firewall/filter/add_rule":
        uid = cfg.uuid("rule")
        cfg.rules[uid] = dict(body.get("rule") or {})
        return 200, {**ok, "uuid": uid}
    if path.startswith("/api/firewall/filter/del_rule/"):
        cfg.rules.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/mdnsrepeater/settings/set":
        cfg.mdns.update(body.get("mdnsrepeater") or {})
        return 200, ok
    if path == "/api/kea/dhcpv4/add_reservation":
        uid = cfg.uuid("reservation")
        cfg.reservations[uid] = dict(body.get("reservation") or {})
        return 200, {**ok, "uuid": uid}
    if path.startswith("/api/kea/dhcpv4/set_reservation/"):
        cfg.reservations.setdefault(last, {}).update(body.get("reservation") or {})
        return 200, ok
    if path.startswith("/api/kea/dhcpv4/del_reservation/"):
        cfg.reservations.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/firewall/alias/add_item":
        uid = cfg.uuid("alias")
        cfg.aliases[uid] = dict(body.get("alias") or {})
        return 200, {**ok, "uuid": uid}
    if path == "/api/unbound/settings/add_host_override":
        h = body.get("host") or {}
        if not h.get("hostname") or not h.get("domain"):
            return 200, {"result": "failed", "validations": {"host.hostname": "A host name and domain are required."}}
        uid = cfg.uuid("override")
        cfg.overrides[uid] = dict(h)
        return 200, {**ok, "uuid": uid}
    if path.startswith("/api/unbound/settings/set_host_override/"):
        if last not in cfg.overrides:
            return 200, {"result": "failed"}
        cfg.overrides[last].update(body.get("host") or {})
        return 200, ok
    if path.startswith("/api/unbound/settings/del_host_override/"):
        cfg.overrides.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/wol/wol/set":
        wake = body.get("wake") or {}
        if wake.get("interface") not in cfg.assigned or not wake.get("mac"):
            return 200, {"result": "failed", "validations": {"wake.interface": "Unknown interface."}}
        cfg.woken.append(dict(wake))
        return 200, {"status": "OK"}
    if path.startswith("/api/firewall/alias/set_item/"):
        if last not in cfg.aliases:
            return 200, {"result": "failed"}
        cfg.aliases[last].update(body.get("alias") or {})
        return 200, ok
    if path.startswith("/api/firewall/alias/del_item/"):
        name = (cfg.aliases.get(last) or {}).get("name")
        if any(name in (r.get("source_net"), r.get("destination_net")) for r in cfg.rules.values()):
            return 200, {"result": "failed", "validations": {"alias.name": "Alias in use."}}
        cfg.aliases.pop(last, None)
        return 200, {"result": "deleted"}
    if path == "/api/diagnostics/firewall/kill_states":
        cfg.killed.append(str(body.get("filter") or ""))
        return 200, {"result": "ok", "dropped_states": 0}
    if last in ("reconfigure", "apply"):
        return 200, {"status": "ok"}
    return 404, {"errorMessage": "Endpoint not found", "errorTitle": "Error"}


def get(cfg: Config, path: str) -> dict[str, Any] | None:
    if path == "/api/interfaces/assignment/search_item":
        return rows(cfg.assigned, "identifier")
    if path == "/api/interfaces/vlan_settings/search_item":
        return rows(cfg.vlans)
    if path == "/api/kea/dhcpv4/get":
        return {"dhcpv4": {"general": dict(cfg.kea)}}
    if path == "/api/kea/dhcpv4/search_subnet":
        return rows(cfg.subnets)
    if path == "/api/firewall/filter/search_rule":
        return rows(cfg.rules)
    if path == "/api/firewall/alias/search_item":
        return rows(cfg.aliases)
    if path == "/api/kea/dhcpv4/search_reservation":
        return rows(cfg.reservations)
    if path == "/api/mdnsrepeater/settings/get":
        return {"mdnsrepeater": dict(cfg.mdns)}
    if path == "/api/unbound/settings/get":
        return {"unbound": {"general": {"enabled": "1", "port": "53"}}}
    if path == "/api/unbound/settings/search_host_override":
        return rows(cfg.overrides)
    if path == "/api/dnsmasq/settings/get":  # installed, but DNS and DHCP are Unbound's and Kea's
        return {"dnsmasq": {"enable": "0", "port": "0", "domain": ""}}
    if path == "/api/dnsmasq/settings/search_host":
        return rows({})
    return None


def _xml(tag: str, value: Any, indent: str) -> str:
    if isinstance(value, dict):
        inner = "".join(_xml(k, v, indent + "  ") for k, v in value.items())
        return f"{indent}<{tag}>\n{inner}{indent}</{tag}>\n"
    return f"{indent}<{tag}>{escape(str(value))}</{tag}>\n"


def backup_xml(cfg: Config, hostname: str, now: float) -> str:
    """config.xml as /api/core/backup/download/this answers it: the configuration
    the API changed, and the revision stamp of the last save (which moves)."""
    host, _, domain = hostname.partition(".")
    doc = {
        "version": "26.7",
        "system": {"hostname": host, "domain": domain},
        "interfaces": cfg.assigned,
        "vlans": {f"vlan{i}": v for i, v in enumerate(cfg.vlans.values())},
        "Kea": {"general": cfg.kea, "subnets": {f"subnet{i}": s for i, s in enumerate(cfg.subnets.values())}},
        "reservations": {f"reservation{i}": r for i, r in enumerate(cfg.reservations.values())},
        "filter": {f"rule{i}": r for i, r in enumerate(cfg.rules.values())},
        "aliases": {f"alias{i}": a for i, a in enumerate(cfg.aliases.values())},
        "unbound": {f"host{i}": h for i, h in enumerate(cfg.overrides.values())},
        "mdnsrepeater": cfg.mdns,
    }
    body = "".join(_xml(k, v, "  ") for k, v in doc.items())
    revision = (
        "  <revision>\n"
        "    <username>root@10.10.10.50</username>\n"
        f"    <time>{now:.4f}</time>\n"
        "    <description>/api/core/backup/download/this</description>\n"
        "  </revision>\n"
    )
    return f'<?xml version="1.0"?>\n<opnsense>\n{body}{revision}</opnsense>\n'
