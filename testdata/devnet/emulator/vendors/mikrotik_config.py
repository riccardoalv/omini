"""The configuration a RouterOS box keeps and the REST API can change (for the MikroTik emulator).

What Omini's changes write lives here, per emulated box, built from the design on first use:
the bridge ports' PVID and frame types, the bridge VLAN table, VLAN interfaces, IP addresses,
DHCP pools, servers, networks and static leases, firewall filter rules and address lists, and
/ip/dns. The vendor module renders these menus from it, adding what is live (running, counters).

Writes follow the REST API: PUT /rest/<menu> adds an item and answers it (with its new .id),
POST /rest/<menu>/add adds one too (with `place-before`) and answers {"ret": "<.id>"},
PATCH /rest/<menu>/<.id> sets properties and answers the item, DELETE removes it (204, no body),
POST /rest/ip/dns/set sets /ip/dns. Like RouterOS, an unknown property, a bad value or a
reference to something missing answers 400 with a "failure: ..." detail and changes nothing.
"""

from __future__ import annotations

import ipaddress
import itertools
import re
from typing import TYPE_CHECKING, Any

if TYPE_CHECKING:
    from .mikrotik import MikroTik

# What each menu takes on add/set (anything else: "unknown parameter").
PROPS: dict[str, set[str]] = {
    "/interface/bridge/port": {"pvid", "frame-types", "ingress-filtering", "comment", "disabled"},
    "/interface/bridge/vlan": {"bridge", "vlan-ids", "tagged", "untagged", "comment", "disabled"},
    "/interface/vlan": {"name", "vlan-id", "interface", "comment", "disabled", "mtu"},
    "/ip/address": {"address", "interface", "network", "comment", "disabled"},
    "/ip/pool": {"name", "ranges", "next-pool", "comment"},
    "/ip/dhcp-server": {"name", "interface", "address-pool", "lease-time", "authoritative", "comment", "disabled"},
    "/ip/dhcp-server/network": {"address", "gateway", "dns-server", "domain", "netmask", "comment"},
    "/ip/dhcp-server/lease": {"mac-address", "address", "server", "client-id", "comment", "disabled"},
    "/ip/firewall/filter": {
        "chain",
        "action",
        "in-interface",
        "out-interface",
        "in-interface-list",
        "out-interface-list",
        "protocol",
        "src-address",
        "dst-address",
        "src-address-list",
        "dst-address-list",
        "src-port",
        "dst-port",
        "connection-state",
        "connection-nat-state",
        "log",
        "log-prefix",
        "comment",
        "disabled",
    },
    "/ip/firewall/address-list": {"list", "address", "comment", "disabled"},
}
DNS_PROPS = {"servers", "allow-remote-requests", "cache-size", "mdns-repeat-ifaces"}
# Bridge ports are set, never added or removed, by these changes.
NO_ADD = {"/interface/bridge/port"}
FRAME_TYPES = ("admit-all", "admit-only-untagged-and-priority-tagged", "admit-only-vlan-tagged")
ACTIONS = ("accept", "drop", "reject", "jump", "return", "passthrough", "log", "fasttrack-connection")


class Failure(Exception):
    """A write RouterOS refuses (400, "failure: ...")."""


def names(value: Any) -> list[str]:
    return [v.strip() for v in str(value or "").split(",") if v.strip()]


def vlan_ids(value: Any) -> list[int]:
    out: list[int] = []
    for part in names(value):
        m = re.fullmatch(r"(\d+)(?:-(\d+))?", part)
        if not m:
            raise Failure(f"invalid value for argument vlan-ids: {part}")
        lo, hi = int(m.group(1)), int(m.group(2) or m.group(1))
        if not 1 <= lo <= hi <= 4094:
            raise Failure(f"invalid value for argument vlan-ids: {part}")
        out.extend(range(lo, hi + 1))
    return out


def defconf_filter() -> list[dict[str, str]]:
    """RouterOS's default firewall (defconf), as the branch router got it."""
    rows = [
        (
            "input",
            "accept",
            {"connection-state": "established,related,untracked"},
            "accept established,related,untracked",
        ),
        ("input", "drop", {"connection-state": "invalid"}, "drop invalid"),
        ("input", "accept", {"protocol": "icmp"}, "accept ICMP"),
        ("input", "accept", {"dst-address": "127.0.0.1"}, "accept to local loopback (for CAPsMAN)"),
        ("input", "drop", {"in-interface-list": "!LAN"}, "drop all not coming from LAN"),
        ("forward", "accept", {"ipsec-policy": "in,ipsec"}, "accept in ipsec policy"),
        ("forward", "accept", {"ipsec-policy": "out,ipsec"}, "accept out ipsec policy"),
        (
            "forward",
            "fasttrack-connection",
            {"connection-state": "established,related", "hw-offload": "true"},
            "fasttrack",
        ),
        (
            "forward",
            "accept",
            {"connection-state": "established,related,untracked"},
            "accept established,related, untracked",
        ),
        ("forward", "drop", {"connection-state": "invalid"}, "drop invalid"),
        (
            "forward",
            "drop",
            {"connection-state": "new", "connection-nat-state": "!dstnat", "in-interface-list": "WAN"},
            "drop all from WAN not DSTNATed",
        ),
    ]
    return [
        {
            ".id": f"*{k + 1:X}",
            "chain": chain,
            "action": action,
            **extra,
            "log": "false",
            "log-prefix": "",
            "comment": f"defconf: {comment}",
            "invalid": "false",
            "dynamic": "false",
            "disabled": "false",
        }
        for k, (chain, action, extra, comment) in enumerate(rows)
    ]


class RouterConfig:
    def __init__(self, svc: MikroTik) -> None:
        box = svc.box
        self.svc = svc
        self.ids = itertools.count(0x100)
        self.bridge = svc.bridge
        self.tables: dict[str, list[dict[str, str]]] = {m: [] for m in PROPS}
        # Bridge ports: their PVID and frame types, as the design has them.
        for k, name in enumerate(svc.bridge_ports):
            p = box.ports[name]
            frames = (
                "admit-only-vlan-tagged"
                if not p.untagged and p.tagged
                else "admit-only-untagged-and-priority-tagged"
                if not p.tagged
                else "admit-all"
            )
            self.tables["/interface/bridge/port"].append(
                {
                    ".id": f"*{k:X}",
                    "interface": name,
                    "bridge": self.bridge or "bridge",
                    "pvid": str(p.untagged or 1),
                    "frame-types": frames,
                    "ingress-filtering": "true",
                    "disabled": "false",
                }
            )
        # The bridge VLAN table: one static entry per VLAN but 1 (the bridge itself is tagged
        # where the router has a VLAN interface).
        vids: dict[int, dict[str, list[str]]] = {}
        for name in svc.bridge_ports:
            p = box.ports[name]
            vids.setdefault(p.untagged or 1, {"tagged": [], "untagged": []})["untagged"].append(name)
            for v in p.tagged:
                vids.setdefault(v, {"tagged": [], "untagged": []})["tagged"].append(name)
        cpu = {q.vlan for q in box.ports.values() if q.kind == "vlan" and q.parent == self.bridge}
        for k, vid in enumerate(sorted(vids)):
            if vid == 1:
                continue
            tagged = ([self.bridge] if vid in cpu and self.bridge else []) + vids[vid]["tagged"]
            self.tables["/interface/bridge/vlan"].append(
                {
                    ".id": f"*{k + 1:X}",
                    "bridge": self.bridge or "bridge",
                    "vlan-ids": str(vid),
                    "tagged": ",".join(tagged),
                    "untagged": ",".join(vids[vid]["untagged"]),
                    "dynamic": "false",
                    "disabled": "false",
                }
            )
        for p in box.ports.values():
            if p.kind != "vlan":
                continue
            row = {
                ".id": svc.ids[p.name],
                "name": p.name,
                "interface": p.parent or "bridge",
                "vlan-id": str(p.vlan),
                "mtu": str(p.mtu),
                "disabled": "false",
            }
            if p.descr:
                row["comment"] = p.descr
            self.tables["/interface/vlan"].append(row)
        k = 0
        for p in box.ports.values():
            for a in p.ips:
                k += 1
                row = {".id": f"*{k:X}", "address": a, "interface": p.name, "disabled": "false"}
                if p.descr:
                    row["comment"] = p.descr
                self.tables["/ip/address"].append(row)
        for k, (vid, iface, (lo, hi)) in enumerate(svc.dhcp_scopes()):
            gw = next(a for a in box.ports[iface].ips).split("/")[0]
            net = str(ipaddress.ip_interface(box.ports[iface].ips[0]).network)
            self.tables["/ip/pool"].append({".id": f"*{k + 1:X}", "name": f"pool-{iface}", "ranges": f"{lo}-{hi}"})
            self.tables["/ip/dhcp-server"].append(
                {
                    ".id": f"*{k + 1:X}",
                    "name": f"dhcp-{iface}",
                    "interface": iface,
                    "address-pool": f"pool-{iface}",
                    "lease-time": "1h" if vid == 80 else "8h" if vid == 32 else "1d",
                    "authoritative": "yes",
                    "disabled": "false",
                }
            )
            self.tables["/ip/dhcp-server/network"].append(
                {".id": f"*{k + 1:X}", "address": net, "gateway": gw, "dns-server": gw, "domain": "acme.local"}
            )
        if box.kind == "router":
            self.tables["/ip/firewall/filter"] = defconf_filter()
        self.dns: dict[str, str] = {
            "servers": "",
            "dynamic-servers": "198.51.100.17" if box.kind == "router" else "",
            "allow-remote-requests": "true" if box.kind == "router" else "false",
            "cache-size": "2048KiB",
            "mdns-repeat-ifaces": "",
        }
        # Leases come from the world (who is on the network); a change keeps here which static
        # leases it removed (by MAC) or set ({MAC: properties}); added ones are their own table.
        self.lease_gone: set[str] = set()
        self.lease_set: dict[str, dict[str, str]] = {}

    # -- reading ------------------------------------------------------------------
    def rows(self, menu: str) -> list[dict[str, str]]:
        return self.tables[menu]

    def pvid_ports(self, vid: int) -> list[str]:
        """Bridge ports RouterOS makes untagged members of vid (their PVID; not tagged-only)."""
        return [
            p["interface"]
            for p in self.rows("/interface/bridge/port")
            if p["pvid"] == str(vid) and p["frame-types"] != "admit-only-vlan-tagged" and p["disabled"] != "true"
        ]

    def bridge_vlans(self) -> list[dict[str, str]]:
        """The static entries with what RouterOS adds by itself, and the dynamic ones of PVIDs
        no entry has."""
        out = []
        covered: set[int] = set()
        for e in self.rows("/interface/bridge/vlan"):
            ids = vlan_ids(e["vlan-ids"])
            covered.update(ids)
            untagged = names(e.get("untagged"))
            for vid in ids:
                untagged += [p for p in self.pvid_ports(vid) if p not in untagged]
            out.append(
                {
                    **e,
                    "tagged": e.get("tagged", ""),
                    "untagged": e.get("untagged", ""),
                    "current-tagged": e.get("tagged", ""),
                    "current-untagged": ",".join(untagged),
                }
            )
        pvids = sorted({int(p["pvid"]) for p in self.rows("/interface/bridge/port")} - covered)
        for vid in pvids:
            out.append(
                {
                    ".id": f"*{0x1000 + vid:X}",
                    "bridge": self.bridge or "bridge",
                    "vlan-ids": str(vid),
                    "tagged": "",
                    "untagged": "",
                    "current-tagged": "",
                    "current-untagged": ",".join(
                        self.pvid_ports(vid) + ([self.bridge] if vid == 1 and self.bridge else [])
                    ),
                    "dynamic": "true",
                    "disabled": "false",
                }
            )
        return sorted(out, key=lambda e: vlan_ids(e["vlan-ids"])[0])

    # -- writing ------------------------------------------------------------------
    def add(self, menu: str, body: dict[str, Any], before: str | None = None) -> dict[str, str]:
        if menu in NO_ADD:
            raise Failure("cannot add a bridge port here")
        row = {".id": f"*{next(self.ids):X}", "disabled": "false", **self.clean(menu, body)}
        self.check(menu, row, None)
        rows = self.rows(menu)
        at = len(rows)
        if before:
            at = next((k for k, r in enumerate(rows) if r[".id"] == before), -1)
            if at < 0:
                raise Failure("no such item (place-before)")
        if menu in ("/ip/firewall/filter", "/interface/bridge/vlan"):
            row.setdefault("dynamic", "false")
        rows.insert(at, row)
        return row

    def set(self, menu: str, item: str, body: dict[str, Any]) -> dict[str, str] | None:
        row = self.find(menu, item)
        if row is None:
            return None
        new = {**row, **self.clean(menu, body)}
        self.check(menu, new, row)
        row.update(new)
        return row

    def remove(self, menu: str, item: str) -> bool:
        row = self.find(menu, item)
        if row is None:
            return False
        self.rows(menu).remove(row)
        return True

    def find(self, menu: str, item: str) -> dict[str, str] | None:
        return next((r for r in self.rows(menu) if item in (r[".id"], r.get("name"))), None)

    def set_dns(self, body: dict[str, Any]) -> None:
        unknown = set(body) - DNS_PROPS
        if unknown:
            raise Failure(f"unknown parameter {sorted(unknown)[0]}")
        for i in names(body.get("mdns-repeat-ifaces")):
            if i not in self.svc.interface_names():
                raise Failure(f"input does not match any value of interface: {i}")
        self.dns.update({k: str(v) for k, v in body.items()})

    @staticmethod
    def clean(menu: str, body: dict[str, Any]) -> dict[str, str]:
        unknown = set(body) - PROPS[menu]
        if unknown:
            raise Failure(f"unknown parameter {sorted(unknown)[0]}")
        for k, v in body.items():
            if not isinstance(v, str):
                raise Failure(f"invalid value for argument {k}")
        return {k: v for k, v in body.items()}

    def check(self, menu: str, row: dict[str, str], old: dict[str, str] | None) -> None:
        """Refuses what RouterOS would (a bad value, a reference to nothing, a duplicate)."""
        ifaces = self.svc.interface_names()
        others = [r for r in self.rows(menu) if r is not old and r[".id"] != row[".id"]]
        if menu == "/interface/bridge/port":
            if not 1 <= int(row.get("pvid") or 0) <= 4094:
                raise Failure("invalid value for argument pvid")
            if row.get("frame-types") not in FRAME_TYPES:
                raise Failure("invalid value for argument frame-types")
        elif menu == "/interface/bridge/vlan":
            if row.get("bridge") != self.bridge:
                raise Failure("input does not match any value of bridge")
            ids = vlan_ids(row.get("vlan-ids"))
            if not ids:
                raise Failure("vlan-ids must be set")
            members = {p["interface"] for p in self.rows("/interface/bridge/port")} | {self.bridge}
            tagged, untagged = names(row.get("tagged")), names(row.get("untagged"))
            for p in tagged + untagged:
                if p not in members:
                    raise Failure(f"input does not match any value of interface: {p}")
            if set(tagged) & set(untagged):
                raise Failure("interface cannot be both tagged and untagged")
            for o in others:
                if set(vlan_ids(o["vlan-ids"])) & set(ids):
                    raise Failure("vlan already exists in the bridge vlan table")
        elif menu == "/interface/vlan":
            if not row.get("name") or (row["name"] in ifaces and (old is None or old["name"] != row["name"])):
                raise Failure("already have interface with such name")
            if row.get("interface") not in ifaces:
                raise Failure("input does not match any value of interface")
            if not 1 <= int(row.get("vlan-id") or 0) <= 4094:
                raise Failure("invalid value for argument vlan-id")
            if any(o["interface"] == row["interface"] and o["vlan-id"] == row["vlan-id"] for o in others):
                raise Failure("vlan with such vlan-id already exists on this interface")
        elif menu == "/ip/address":
            try:
                ipaddress.ip_interface(row.get("address", ""))
            except ValueError as e:
                raise Failure("invalid value for argument address") from e
            if row.get("interface") not in ifaces | {r["name"] for r in self.rows("/interface/vlan")}:
                raise Failure("input does not match any value of interface")
        elif menu == "/ip/pool":
            if not row.get("name") or any(o["name"] == row["name"] for o in others):
                raise Failure("pool with such name already exists")
            for part in names(row.get("ranges")):
                for ip in part.split("-"):
                    try:
                        ipaddress.IPv4Address(ip)
                    except ValueError as e:
                        raise Failure("invalid value for argument ranges") from e
        elif menu == "/ip/dhcp-server":
            if row.get("interface") not in ifaces:
                raise Failure("input does not match any value of interface")
            pools = {p["name"] for p in self.rows("/ip/pool")} | {"static-only"}
            if row.get("address-pool") not in pools:
                raise Failure("input does not match any value of address-pool")
            if any(o["interface"] == row["interface"] for o in others):
                raise Failure("this interface already has a DHCP server")
            if not row.get("name") or any(o["name"] == row["name"] for o in others):
                raise Failure("server with such name already exists")
        elif menu == "/ip/dhcp-server/network":
            try:
                ipaddress.ip_network(row.get("address", ""))
            except ValueError as e:
                raise Failure("invalid value for argument address") from e
        elif menu == "/ip/dhcp-server/lease":
            if not re.fullmatch(r"([0-9A-Fa-f]{2}:){5}[0-9A-Fa-f]{2}", row.get("mac-address", "")):
                raise Failure("invalid value for argument mac-address")
            try:
                ipaddress.IPv4Address(row.get("address", ""))
            except ValueError as e:
                raise Failure("invalid value for argument address") from e
            if row.get("server", "all") not in {s["name"] for s in self.rows("/ip/dhcp-server")} | {"all"}:
                raise Failure("input does not match any value of server")
        elif menu == "/ip/firewall/filter":
            if row.get("chain") not in ("input", "forward", "output") or row.get("action") not in ACTIONS:
                raise Failure("chain and action must be set")
            for k in ("in-interface", "out-interface"):
                if row.get(k) and row[k].lstrip("!") not in ifaces:
                    raise Failure(f"input does not match any value of interface: {row[k]}")
            if row.get("protocol") not in (None, "tcp", "udp", "icmp"):
                raise Failure("invalid value for argument protocol")
            if row.get("dst-port") and row.get("protocol") not in ("tcp", "udp"):
                raise Failure("dst-port can be set only for tcp or udp")
        elif menu == "/ip/firewall/address-list":
            if not row.get("list"):
                raise Failure("list must be set")
            try:
                ipaddress.ip_network(row.get("address", ""), strict=False)
            except ValueError as e:
                raise Failure("invalid value for argument address") from e
