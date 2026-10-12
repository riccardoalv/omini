"""The configuration lab-fw01's REST API changes: assigned interfaces, VLANs,
DHCP servers with their static mappings, firewall rules and aliases, and the
DNS Resolver's host overrides (Wake-on-LAN packets and killed firewall states
are only recorded).

It starts from the design (WAN and LAN, Kea on the LAN with the lab's pool and
static mappings, pfSense's default LAN rules) the first time it is read and
then keeps what the API writes, so Omini's network.set, network.delete,
reservation.set, reservation.delete, dns.set, dns.delete, pause.set and
pause.delete can be applied and read back. Nothing
of it reaches the emulated network's traffic: it is the router's configuration
only. Like the package, VLANs, rules and static mappings are identified by
their position in their list (ids move down when an earlier one is removed),
interfaces and DHCP servers by pfSense's interface name.
"""

from __future__ import annotations

import copy
import re
from dataclasses import dataclass, field
from ipaddress import ip_address
from typing import Any


@dataclass
class Config:
    interfaces: dict[str, dict[str, Any]] = field(default_factory=dict)  # wan, lan, opt1 → item
    vlans: list[dict[str, Any]] = field(default_factory=list)
    dhcp: dict[str, dict[str, Any]] = field(default_factory=dict)  # interface → server
    rules: list[dict[str, Any]] = field(default_factory=list)
    aliases: list[dict[str, Any]] = field(default_factory=list)
    host_overrides: list[dict[str, Any]] = field(default_factory=list)
    woken: list[dict[str, Any]] = field(default_factory=list)  # Wake-on-LAN packets sent
    killed: list[dict[str, str]] = field(default_factory=list)  # firewall state kills (their filters)


class Refused(Exception):
    """A change the package refuses: (HTTP code, response id, message)."""

    def __init__(self, code: int, response_id: str, message: str):
        super().__init__(message)
        self.code, self.response_id, self.message = code, response_id, message


def default_rules() -> list[dict[str, Any]]:
    """pfSense's default LAN rules: allow to any, IPv4 and IPv6."""
    return [
        {
            "type": "pass",
            "interface": ["lan"],
            "ipprotocol": proto,
            "protocol": None,
            "source": "lan",
            "destination": "any",
            "destination_port": None,
            "descr": f"Default allow LAN {'IPv6 ' if proto == 'inet6' else ''}to any rule",
            "disabled": False,
            "floating": False,
        }
        for proto in ("inet", "inet6")
    ]


# --- reads ----------------------------------------------------------------------


def interfaces(cfg: Config) -> list[dict[str, Any]]:
    return [{"id": k, **copy.deepcopy(v)} for k, v in cfg.interfaces.items()]


def vlans(cfg: Config) -> list[dict[str, Any]]:
    return [{"id": i, **v} for i, v in enumerate(cfg.vlans)]


def server(cfg: Config, name: str) -> dict[str, Any]:
    s = copy.deepcopy(cfg.dhcp[name])
    s["staticmap"] = [{"parent_id": name, "id": i, **m} for i, m in enumerate(s.get("staticmap") or [])]
    s["pool"] = [{"parent_id": name, "id": i, **p} for i, p in enumerate(s.get("pool") or [])]
    return {"id": name, "interface": name, **s}


def servers(cfg: Config) -> list[dict[str, Any]]:
    return [server(cfg, k) for k in cfg.dhcp]


def rules(cfg: Config) -> list[dict[str, Any]]:
    return [{"id": i, **copy.deepcopy(r)} for i, r in enumerate(cfg.rules)]


def aliases(cfg: Config) -> list[dict[str, Any]]:
    return [{"id": i, **copy.deepcopy(a)} for i, a in enumerate(cfg.aliases)]


def host_overrides(cfg: Config) -> list[dict[str, Any]]:
    return [{"id": i, **copy.deepcopy(o)} for i, o in enumerate(cfg.host_overrides)]


# --- writes ---------------------------------------------------------------------


def _index(items: list[Any], value: Any, what: str) -> int:
    try:
        i = int(value)
    except (TypeError, ValueError):
        raise Refused(400, "FIELD_INVALID_TYPE", "Field `id` must be of type `integer`.") from None
    if not 0 <= i < len(items):
        raise Refused(404, "MODEL_OBJECT_NOT_FOUND", f"{what} with ID `{i}` does not exist.")
    return i


def _check_alias(alias: dict[str, Any]) -> None:
    """A host alias holds addresses, one description per address at most."""
    address, detail = alias.get("address") or [], alias.get("detail") or []
    if len(detail) > len(address):
        raise Refused(400, "FIELD_INVALID", "Field `detail` cannot have more items than `address`.")
    if alias.get("type") == "host":
        for a in address:
            try:
                ip_address(str(a))
            except ValueError:
                raise Refused(400, "FIELD_INVALID", f"Field `address` item `{a}` is not an IP address.") from None


def _in_use(cfg: Config, name: str) -> bool:
    return (cfg.dhcp.get(name) or {}).get("enable") is True or any(
        name in (r.get("interface") or []) for r in cfg.rules
    )


def write(cfg: Config, method: str, path: str, query: dict[str, str], body: dict[str, Any]) -> Any:
    """One write of the REST API; returns the envelope's data."""
    key = (method, path)
    if key == ("POST", "/api/v2/interface/vlan"):
        parent, tag = str(body.get("if") or ""), body.get("tag")
        if not parent or not isinstance(tag, int) or not 1 <= tag <= 4094:
            raise Refused(400, "INTERFACE_VLAN_TAG_INVALID", "Field `tag` must be between 1 and 4094.")
        if any(v["if"] == parent and v["tag"] == tag for v in cfg.vlans):
            raise Refused(400, "FIELD_MUST_BE_UNIQUE", "Field `tag` must be unique for this parent interface.")
        cfg.vlans.append(
            {"if": parent, "tag": tag, "vlanif": f"{parent}.{tag}", "pcp": None, "descr": body.get("descr") or ""}
        )
        return {"id": len(cfg.vlans) - 1, **cfg.vlans[-1]}
    if key == ("DELETE", "/api/v2/interface/vlan"):
        i = _index(cfg.vlans, query.get("id"), "Interface VLAN")
        if any(a["if"] == cfg.vlans[i]["vlanif"] for a in cfg.interfaces.values()):
            raise Refused(
                409, "INTERFACE_VLAN_CANNOT_BE_DELETED_WHILE_IN_USE", "VLAN cannot be deleted while it is in use."
            )
        return cfg.vlans.pop(i)
    if key == ("POST", "/api/v2/interface"):
        device = str(body.get("if") or "")
        if any(a["if"] == device for a in cfg.interfaces.values()):
            raise Refused(400, "FIELD_MUST_BE_UNIQUE", f"Field `if` must be unique; `{device}` is already assigned.")
        n = 1 + max([int(k[3:]) for k in cfg.interfaces if k.startswith("opt")] or [0])
        cfg.interfaces[f"opt{n}"] = {"gateway": "", **body}
        return {"id": f"opt{n}", **cfg.interfaces[f"opt{n}"]}
    if key == ("PATCH", "/api/v2/interface"):
        name = str(body.get("id") or "")
        if name not in cfg.interfaces:
            raise Refused(404, "MODEL_OBJECT_NOT_FOUND", f"Network Interface with ID `{name}` does not exist.")
        cfg.interfaces[name].update({k: v for k, v in body.items() if k != "id"})
        return {"id": name, **cfg.interfaces[name]}
    if key == ("DELETE", "/api/v2/interface"):
        name = str(query.get("id") or "")
        if name not in cfg.interfaces:
            raise Refused(404, "MODEL_OBJECT_NOT_FOUND", f"Network Interface with ID `{name}` does not exist.")
        if name in ("wan", "lan") or _in_use(cfg, name):
            raise Refused(409, "INTERFACE_CANNOT_BE_DELETED_WHILE_IN_USE", "Interface cannot be deleted while in use.")
        cfg.dhcp.pop(name, None)
        return {"id": name, **cfg.interfaces.pop(name)}
    if key == ("POST", "/api/v2/services/dhcp_server"):
        name = str(body.get("interface") or "")
        if name not in cfg.interfaces or name in cfg.dhcp:
            raise Refused(400, "FIELD_INVALID_CHOICE", f"Field `interface` cannot be `{name}`.")
        cfg.dhcp[name] = {
            "dnsserver": [],
            "staticmap": [],
            "pool": [],
            **{k: v for k, v in body.items() if k != "interface"},
        }
        return server(cfg, name)
    if key == ("PATCH", "/api/v2/services/dhcp_server"):
        name = str(body.get("id") or "")
        if name not in cfg.dhcp:
            raise Refused(404, "MODEL_OBJECT_NOT_FOUND", f"DHCP Server with ID `{name}` does not exist.")
        cfg.dhcp[name].update({k: v for k, v in body.items() if k != "id"})
        return server(cfg, name)
    if key == ("DELETE", "/api/v2/services/dhcp_server"):
        name = str(query.get("id") or "")
        if name not in cfg.dhcp:
            raise Refused(404, "MODEL_OBJECT_NOT_FOUND", f"DHCP Server with ID `{name}` does not exist.")
        out = server(cfg, name)
        del cfg.dhcp[name]
        return out
    if path == "/api/v2/services/dhcp_server/static_mapping":
        parent = str(body.get("parent_id") or query.get("parent_id") or "")
        if parent not in cfg.dhcp:
            raise Refused(404, "MODEL_PARENT_NOT_FOUND", f"DHCP Server with ID `{parent}` does not exist.")
        maps = cfg.dhcp[parent].setdefault("staticmap", [])
        lo, hi = cfg.dhcp[parent].get("range_from"), cfg.dhcp[parent].get("range_to")
        inside = bool(body.get("ipaddr") and lo and hi) and (
            ip_address(str(lo)) <= ip_address(str(body["ipaddr"])) <= ip_address(str(hi))
        )
        if method in ("POST", "PATCH") and inside:
            # As pfSense: a static mapping stays out of the DHCP range.
            raise Refused(400, "FIELD_INVALID", "The IP address must not be within the DHCP range for this interface.")
        if method == "POST":
            maps.append({k: v for k, v in body.items() if k != "parent_id"})
            return {"parent_id": parent, "id": len(maps) - 1, **maps[-1]}
        i = _index(maps, body.get("id") if method == "PATCH" else query.get("id"), "Static mapping")
        if method == "PATCH":
            maps[i].update({k: v for k, v in body.items() if k not in ("parent_id", "id")})
            return {"parent_id": parent, "id": i, **maps[i]}
        return maps.pop(i)
    if key == ("POST", "/api/v2/firewall/rule"):
        rule = {"protocol": None, "destination_port": None, "disabled": False, "floating": False, **body}
        cfg.rules.append(rule)
        return {"id": len(cfg.rules) - 1, **rule}
    if key == ("DELETE", "/api/v2/firewall/rule"):
        return cfg.rules.pop(_index(cfg.rules, query.get("id"), "Firewall Rule"))
    if key == ("POST", "/api/v2/firewall/alias"):
        if any(a["name"] == body.get("name") for a in cfg.aliases):
            raise Refused(400, "FIELD_MUST_BE_UNIQUE", "Field `name` must be unique.")
        _check_alias(body)
        cfg.aliases.append(dict(body))
        return {"id": len(cfg.aliases) - 1, **body}
    if key == ("PATCH", "/api/v2/firewall/alias"):
        i = _index(cfg.aliases, body.get("id"), "Firewall Alias")
        _check_alias({**cfg.aliases[i], **body})
        cfg.aliases[i].update({k: v for k, v in body.items() if k != "id"})
        return {"id": i, **cfg.aliases[i]}
    if key == ("DELETE", "/api/v2/firewall/alias"):
        i = _index(cfg.aliases, query.get("id"), "Firewall Alias")
        name = cfg.aliases[i]["name"]
        if any(name in (str(r.get("source")).lstrip("!"), str(r.get("destination")).lstrip("!")) for r in cfg.rules):
            raise Refused(409, "ALIAS_CANNOT_BE_DELETED_WHILE_IN_USE", "Alias cannot be deleted while in use.")
        return cfg.aliases.pop(i)
    if key == ("DELETE", "/api/v2/firewall/states"):
        cfg.killed.append(dict(query))
        return []
    if path == "/api/v2/services/dns_resolver/host_override":
        if method == "POST":
            for f in ("host", "domain", "ip"):
                if not body.get(f):
                    raise Refused(400, "FIELD_IS_REQUIRED", f"Field `{f}` is required.")
            cfg.host_overrides.append({"descr": "", "aliases": [], **body})
            return {"id": len(cfg.host_overrides) - 1, **cfg.host_overrides[-1]}
        i = _index(cfg.host_overrides, body.get("id") if method == "PATCH" else query.get("id"), "Host Override")
        if method == "PATCH":
            cfg.host_overrides[i].update({k: v for k, v in body.items() if k != "id"})
            return {"id": i, **cfg.host_overrides[i]}
        return cfg.host_overrides.pop(i)
    if key == ("POST", "/api/v2/services/wake_on_lan/send"):
        if str(body.get("interface") or "") not in cfg.interfaces:
            raise Refused(400, "FIELD_INVALID_CHOICE", f"Field `interface` cannot be `{body.get('interface')}`.")
        if not re.fullmatch(r"[0-9a-f]{2}(:[0-9a-f]{2}){5}", str(body.get("mac_addr") or ""), re.IGNORECASE):
            raise Refused(400, "WAKE_ON_LAN_SEND_INVALID_MAC", "Field `mac_addr` must be a valid MAC address.")
        cfg.woken.append(dict(body))
        return dict(body)
    if method == "POST" and path in (
        "/api/v2/interface/apply",
        "/api/v2/services/dhcp_server/apply",
        "/api/v2/services/dns_resolver/apply",
        "/api/v2/firewall/apply",
    ):
        return {"applied": True}
    raise Refused(405, "ENDPOINT_METHOD_NOT_ALLOWED", f"Resource at {path} does not support the `{method}` method.")
