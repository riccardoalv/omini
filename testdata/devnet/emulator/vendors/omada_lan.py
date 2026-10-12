"""The Omada site's LAN networks and port profiles (the controller's VLANs).

Kept by the Omada service, built from the design on first use: the site's
VLANs become VLAN-only networks (purpose 0: the branch router is a MikroTik,
not an Omada gateway) next to the Default LAN (VLAN 1); each network has its
own profile (native = it), plus the built-in "All" and "Disable" and one
"Trunk <native> <tagged>" profile per trunk combination the switch's ports use.

What is answered, as the Open API documents it:

- ``GET /openapi/v2|v1/{omadacId}/sites/{siteId}/lan-networks`` (paged),
  ``POST`` (create), ``PATCH .../lan-networks/{id}`` (modify: the body
  replaces the network), ``DELETE .../lan-networks/{id}`` (-33505 Default LAN,
  -33566 native network of a profile a port uses, -33503 unknown).
- ``GET /openapi/v1/.../lan-profiles`` (paged), ``POST`` (-33506 name taken),
  ``DELETE .../lan-profiles/{id}`` (built-in ones and ones in use refused).
- The switch's ``portList`` (Get switch info) and
  ``PUT .../switches/{mac}/ports/{port}/profile`` with ``{"profileId"}``
  (-33507 unknown profile, -39701 unknown port).
"""

from __future__ import annotations

from typing import Any

BASE: dict[str, Any] = {
    "poe": 2,
    "dot1x": 1,
    "portIsolationEnable": False,
    "lldpMedEnable": True,
    "topoNotifyEnable": False,
    "bandWidthCtrlType": 0,
    "spanningTreeEnable": True,
    "loopbackDetectEnable": False,
    "eeeEnable": False,
    "flowControlEnable": False,
    "loopbackDetectVlanBasedEnable": False,
    "dhcpL2RelaySettings": {"enable": False, "format": 0},
    "supportESEnable": False,
}


def ok(result: Any = None) -> tuple[int, str, Any]:
    return 0, "Success.", result


def err(code: int, msg: str) -> tuple[int, str, Any]:
    return code, msg, None


class Lan:
    def __init__(self, vlans: list[Any], ports: list[Any]) -> None:
        """``vlans``: the site's design VLANs; ``ports``: the switch's
        ethernet/SFP ports (with index, untagged, tagged)."""
        self.ids = 0
        self.nets: list[dict[str, Any]] = [
            {
                "id": self.new_id("net"),
                "name": "Default",
                "purpose": 1,
                "vlanType": 0,
                "vlan": 1,
                "application": 0,
                "gatewaySubnet": "192.168.0.1/24",
                "igmpSnoopEnable": False,
                "mldSnoopEnable": False,
                "primary": True,
                "portal": False,
                "accessControlRule": False,
                "rateLimit": False,
            }
        ]
        for v in sorted(vlans, key=lambda v: v.id):
            self.nets.append(
                {
                    "id": self.new_id("net"),
                    "name": v.name,
                    "purpose": 0,
                    "vlan": v.id,
                    "igmpSnoopEnable": False,
                    "mldSnoopEnable": False,
                    "primary": False,
                    "portal": False,
                    "accessControlRule": False,
                    "rateLimit": False,
                }
            )
        default = self.nets[0]["id"]
        self.profiles: list[dict[str, Any]] = [
            {
                "id": self.new_id("prof"),
                "name": "All",
                "flag": 0,
                "type": 0,
                "nativeNetworkId": default,
                "tagNetworkIds": [],
                "untagNetworkIds": [],
                **BASE,
            },
            {
                "id": self.new_id("prof"),
                "name": "Disable",
                "flag": 0,
                "type": 1,
                "tagNetworkIds": [],
                "untagNetworkIds": [],
                **BASE,
            },
        ]
        for n in self.nets:
            self.native_profile(n, flag=0 if n["primary"] else 1)
        self.port_profile: dict[int, str] = {}
        for p in ports:
            native = self.net_by_vlan(p.untagged or 1)
            tags = [n["id"] for t in p.tagged if (n := self.net_by_vlan(t))]
            if not tags:
                prof = next(x for x in self.profiles if x.get("nativeNetworkId") == native["id"] and x["type"] == 2)
            else:
                name = f"Trunk {native['vlan']} {','.join(str(t) for t in sorted(p.tagged))}"
                prof = next((x for x in self.profiles if x["name"] == name), None)
                if prof is None:
                    prof = {
                        "id": self.new_id("prof"),
                        "name": name,
                        "flag": 2,
                        "type": 2,
                        "nativeNetworkId": native["id"],
                        "tagNetworkIds": tags,
                        "untagNetworkIds": [],
                        **BASE,
                    }
                    self.profiles.append(prof)
            self.port_profile[p.index] = prof["id"]

    def new_id(self, prefix: str) -> str:
        self.ids += 1
        return f"{prefix}{self.ids:04d}"

    def native_profile(self, n: dict[str, Any], flag: int = 1) -> None:
        self.profiles.append(
            {
                "id": self.new_id("prof"),
                "name": n["name"],
                "flag": flag,
                "type": 2,
                "nativeNetworkId": n["id"],
                "tagNetworkIds": [],
                "untagNetworkIds": [],
                **BASE,
            }
        )

    def net_by_vlan(self, vid: int) -> dict[str, Any] | None:
        return next((n for n in self.nets if n.get("vlan") == vid), None)

    def profile(self, pid: str) -> dict[str, Any] | None:
        return next((p for p in self.profiles if p["id"] == pid), None)

    # -- what the switch's statistics show ------------------------------------

    def port_info(self, index: int) -> tuple[str, int]:
        """(profile name, PVID) of a switch port."""
        prof = self.profile(self.port_profile.get(index, "")) or {}
        native = next((n for n in self.nets if n["id"] == prof.get("nativeNetworkId")), None)
        return prof.get("name", "All"), (native or {}).get("vlan") or 1

    def port_list(self, ports: list[Any]) -> list[dict[str, Any]]:
        out = []
        for p in ports:
            prof = self.profile(self.port_profile.get(p.index, "")) or {}
            out.append(
                {
                    "port": p.index,
                    "name": p.descr or f"Port{p.index}",
                    "profileId": prof.get("id"),
                    "profileName": prof.get("name"),
                    "profileOverrideEnable": False,
                    "poeMode": 1 if p.poe else 0,
                    "lagPort": False,
                    "status": 1 if p.admin_up else 0,
                }
            )
        return out

    # -- writes ---------------------------------------------------------------

    def set_port_profile(self, index: int, body: Any) -> tuple[int, str, Any]:
        if index not in self.port_profile:
            return err(-39701, "This port does not exist")
        pid = body.get("profileId") if isinstance(body, dict) else None
        if not isinstance(pid, str) or self.profile(pid) is None:
            return err(-33507, "This profile does not exist.")
        self.port_profile[index] = pid
        return ok()

    def networks(self, method: str, rest: list[str], body: Any) -> tuple[int, str, Any] | None:
        """``rest``: the path after lan-networks. None: answer the list (paged)."""
        if not rest:
            if method == "GET":
                return None
            if method != "POST":
                return err(-1, "Request method not supported.")
            if not isinstance(body, dict) or not body.get("name") or body.get("purpose") not in (0, 1):
                return err(-1001, "Invalid request parameters.")
            vid = body.get("vlan")
            if body["purpose"] != 0 or not isinstance(vid, int) or not 1 <= vid <= 4090:
                return err(-1001, "Invalid request parameters.")  # only VLAN-only networks emulated
            if any(n["name"] == body["name"] for n in self.nets):
                return err(-33500, "Network name already existed.")
            if self.net_by_vlan(vid):
                return err(-33502, "This VLAN ID already exists.")
            n = {
                "id": self.new_id("net"),
                **body,
                "primary": False,
                "portal": False,
                "accessControlRule": False,
                "rateLimit": False,
            }
            self.nets.append(n)
            self.native_profile(n)
            return ok({"id": n["id"]})
        net = next((n for n in self.nets if n["id"] == rest[0]), None)
        if net is None or len(rest) != 1:
            return err(-33503, "This network does not exist.")
        if method == "GET":
            return ok(dict(net))
        if method == "PATCH":
            if not isinstance(body, dict) or not body.get("name"):
                return err(-1001, "Invalid request parameters.")
            if any(n["name"] == body["name"] and n is not net for n in self.nets):
                return err(-33500, "Network name already existed.")
            if net.get("primary") and body.get("vlan", 1) != 1:
                return err(-33527, "The interface of Default LAN cannot be modified.")
            keep = {k: net[k] for k in ("id", "primary", "portal", "accessControlRule", "rateLimit") if k in net}
            old = net["name"]
            net.clear()
            net.update({**body, **keep})
            for p in self.profiles:  # the network's own profile follows its name
                if p["flag"] == 1 and p["nativeNetworkId"] == net["id"] and p["name"] == old:
                    p["name"] = net["name"]
            return ok()
        if method == "DELETE":
            if net.get("primary"):
                return err(-33505, "Default LAN cannot be deleted.")
            used = set(self.port_profile.values())
            if any(p["id"] in used and p.get("nativeNetworkId") == net["id"] for p in self.profiles):
                return err(
                    -33566, "The network has been configured as the switch's native network. Cannot delete the network."
                )
            self.nets.remove(net)
            self.profiles = [p for p in self.profiles if not (p["flag"] == 1 and p.get("nativeNetworkId") == net["id"])]
            for p in self.profiles:
                p["tagNetworkIds"] = [x for x in p.get("tagNetworkIds") or [] if x != net["id"]]
                p["untagNetworkIds"] = [x for x in p.get("untagNetworkIds") or [] if x != net["id"]]
            return ok()
        return err(-1, "Request method not supported.")

    def lan_profiles(self, method: str, rest: list[str], body: Any) -> tuple[int, str, Any] | None:
        if not rest:
            if method == "GET":
                return None
            if method != "POST":
                return err(-1, "Request method not supported.")
            ids = {n["id"] for n in self.nets}
            if (
                not isinstance(body, dict)
                or not body.get("name")
                or body.get("nativeNetworkId") not in ids
                or any(x not in ids for x in body.get("tagNetworkIds") or [])
                or body.get("nativeNetworkId") in (body.get("tagNetworkIds") or [])
            ):
                return err(-1001, "Invalid request parameters.")
            if any(p["name"] == body["name"] for p in self.profiles):
                return err(-33506, "This Profile name already exists.")
            p = {**BASE, **body, "id": self.new_id("prof"), "flag": 2, "type": 2}
            p.setdefault("untagNetworkIds", [])
            self.profiles.append(p)
            return ok({"id": p["id"]})
        prof = self.profile(rest[0])
        if prof is None or len(rest) != 1:
            return err(-33507, "This profile does not exist.")
        if method == "DELETE":
            if prof["flag"] != 2:
                return err(-1001, "Invalid request parameters.")
            if prof["id"] in self.port_profile.values():
                return err(-33559, "The profile is in use.")
            self.profiles.remove(prof)
            return ok()
        return err(-1, "Request method not supported.")
