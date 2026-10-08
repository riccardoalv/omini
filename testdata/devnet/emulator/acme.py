"""Acme's network, as DESIGN.md describes it: every node, port, link, VLAN and SSID.

The people and their devices are generated from a fixed seed, so the network
is the same on every run. Changing this file changes the devnet: keep
DESIGN.md in step.
"""

from __future__ import annotations

import hashlib
import random
from typing import Any

from .model import Design, Node, Port, Ssid, Vlan, WifiAttach

# Real IEEE prefixes (OUIs) per maker. Tests check each against Omini's registry.
OUI = {
    "apc": "00c0b7",
    "apple": "a4b197",
    "apple2": "f0b3ec",
    "brother": "3c2af4",
    "cisco": "00000c",
    "cisco2": "f4cfe2",
    "dell": "f8bc12",
    "dell2": "b07b25",
    "espressif": "a4cf12",
    "gertec": "7857b0",
    "gl": "9483c4",
    "google": "f4f5d8",
    "hikvision": "bcad28",
    "honeywell": "00226a",
    "hp": "3024a9",
    "hpe": "000b86",  # Aruba
    "huawei": "001882",
    "intel": "3c6aa7",
    "intelbras": "24fd0d",
    "ingenico": "547f54",
    "juniper": "0014f6",
    "lenovo": "a48cdb",
    "lg": "c875dd",
    "logitech": "38f0c8",
    "mellanox": "0c42a1",
    "mercusys": "30169d",
    "microsoft": "00155d",  # Hyper-V; used for nothing physical
    "mikrotik": "48a98a",
    "motorola": "4ccc34",
    "proxmox": "bc2411",
    "qnap": "245ebe",
    "raspberry": "d83add",
    "realtek": "00e04c",
    "samsung": "8425db",
    "shelly": "8400ec",
    "sonos": "347e5c",
    "supermicro": "3cecef",
    "synology": "9009d0",
    "teltonika": "209727",
    "tplink": "5c628b",
    "tuya": "105a17",
    "ubiquiti": "0418d6",
    "xiaomi": "04106b",
    "yealink": "249ad8",
    "daikin": "04ed62",
    "hid": "00308e",
    "hongrui": "1c2aa3",  # Shenzhen HongRui: Horaco switches
}

SEED = 20261008
LOCAL_UTC_OFFSET = -3 * 3600  # São Paulo (no DST)


def mac(oui: str, name: str, salt: str = "") -> str:
    """A stable MAC: the maker's prefix + 3 bytes from the name."""
    h = hashlib.sha256(f"{name}/{salt}".encode()).digest()
    p = OUI[oui]
    return ":".join([p[0:2], p[2:4], p[4:6], f"{h[0]:02x}", f"{h[1]:02x}", f"{h[2]:02x}"])


def random_mac(name: str) -> str:
    """A locally administered (randomized, "private Wi-Fi address") MAC."""
    h = hashlib.sha256(f"random/{name}".encode()).digest()
    first = (h[0] & 0xFC) | 0x02
    return ":".join(f"{b:02x}" for b in (first, *h[1:6]))


def offset_mac(base: str, n: int) -> str:
    v = int(base.replace(":", ""), 16) + n
    s = f"{v:012x}"
    return ":".join(s[i : i + 2] for i in range(0, 12, 2))


def carp_mac(vhid: int) -> str:
    return f"00:00:5e:00:01:{vhid:02x}"


# ---------------------------------------------------------------------------
# VLANs and SSIDs

HQ_VLANS = [
    # id, name, subnet, routed, dhcp pool, purpose
    (10, "MGMT", "10.10.10.0/24", True, ("10.10.10.100", "10.10.10.199"), "Network gear, hypervisors, BMCs, UPS"),
    (20, "SERVERS", "10.10.20.0/24", True, None, "Servers and VMs"),
    (30, "USERS", "10.10.30.0/24", True, ("10.10.30.100", "10.10.30.250"), "Wired desktops"),
    (32, "WIFI-CORP", "10.10.32.0/23", True, ("10.10.32.20", "10.10.33.250"), 'Wi-Fi "Acme" (802.1X)'),
    (34, "WH-WIFI", "10.10.34.0/24", True, ("10.10.34.100", "10.10.34.200"), "Warehouse Wi-Fi (Mercusys Halo)"),
    (40, "VOICE", "10.10.40.0/24", True, ("10.10.40.100", "10.10.40.250"), "IP phones, PBX"),
    (50, "IOT", "10.10.50.0/24", True, ("10.10.50.100", "10.10.50.250"), "Building automation, TVs, sensors"),
    (60, "CAMERAS", "10.10.60.0/24", True, None, "CCTV (no internet)"),
    (70, "PRINT", "10.10.70.0/24", True, ("10.10.70.100", "10.10.70.150"), "Printers, print server"),
    (80, "GUEST", "10.10.80.0/23", True, ("10.10.80.20", "10.10.81.250"), "Visitors (internet only)"),
    (90, "DMZ", "10.10.90.0/24", True, None, "Public servers (physically separate switch)"),
    (100, "STORAGE", "10.10.100.0/24", False, None, "NFS/SMB storage (not routed)"),
    (101, "CEPH-PUB", "10.10.101.0/24", False, None, "Ceph public network"),
    (102, "CEPH-CLU", "10.10.102.0/24", False, None, "Ceph cluster (replication) network"),
    (103, "COROSYNC", "10.10.103.0/24", False, None, "Proxmox cluster heartbeat"),
    (110, "BACKUP", "10.10.110.0/24", True, None, "Backup server and target"),
    (120, "LAB", "10.10.120.0/24", True, None, "Lab: the pfSense lab router's WAN"),
    (900, "WAN-FIBER", None, False, None, "ISP fiber ONT ↔ firewalls (L2 only)"),
    (901, "WAN-LTE", None, False, None, "5G router ↔ firewalls (L2 only)"),
]
BRANCH_VLANS = [
    (10, "MGMT", "10.20.10.0/24", True, None, "Router, switch, APs, controller"),
    (30, "USERS", "10.20.30.0/24", True, ("10.20.30.100", "10.20.30.200"), "Wired"),
    (32, "WIFI-CORP", "10.20.32.0/24", True, ("10.20.32.20", "10.20.32.250"), 'Wi-Fi "Acme"'),
    (40, "VOICE", "10.20.40.0/24", True, ("10.20.40.100", "10.20.40.200"), "IP phones"),
    (80, "GUEST", "10.20.80.0/24", True, ("10.20.80.20", "10.20.80.250"), "Visitors"),
]
WAN_VLAN = 4000  # pseudo-VLAN of untagged WAN segments (router port ↔ ISP equipment)
LAB_LAN = 1200  # pseudo-VLAN of the lab router's LAN (an internal Proxmox bridge)
STORE_LAN = 1  # the store's flat LAN
STORE_GUEST = 3  # the store's guest Wi-Fi

GATEWAYS = {"hq": "fw", "branch": "br-gw01", "store": "store-rt01", "lab": "lab-fw01", "wh": "fw"}


def vlan_iface(site: str, vid: int) -> str:
    return {"hq": f"vlan0.{vid}", "branch": f"vlan{vid}"}.get(site, f"vlan{vid}")


# ---------------------------------------------------------------------------
# People (generated)

FIRST = [
    "Ana", "Bruno", "Carla", "Daniel", "Eduarda", "Felipe", "Gabriela", "Henrique", "Isabela", "João", "Juliana",
    "Lucas", "Mariana", "Mateus", "Natália", "Otávio", "Paula", "Rafael", "Sofia", "Thiago", "Vanessa", "Vinícius",
    "Beatriz", "Caio", "Débora", "Enzo", "Fernanda", "Gustavo", "Helena", "Igor", "Larissa", "Leonardo", "Letícia",
    "Marcelo", "Mirela", "Nicolas", "Patrícia", "Pedro", "Renata", "Rodrigo", "Sabrina", "Samuel", "Tatiane",
    "Tiago", "Valentina", "Victor", "Yasmin", "André", "Bianca", "Camila", "Diego", "Elisa", "Fábio", "Giovana",
    "Heitor", "Ingrid", "Júlio", "Laura", "Luiz", "Manuela", "Murilo", "Nathalia", "Priscila", "Ricardo", "Roberta",
    "Sérgio", "Simone", "Teresa", "Ulisses", "Viviane", "Wagner", "Alice", "Arthur", "Cecília", "Davi", "Elaine",
    "Francisco", "Graziela", "Hugo", "Irene", "Jorge", "Kátia", "Lorena", "Márcio", "Nelson", "Olívia", "Paulo"
]  # fmt: skip
LAST = [
    "Silva", "Santos", "Oliveira", "Souza", "Rodrigues", "Ferreira", "Alves", "Pereira", "Lima", "Gomes", "Costa",
    "Ribeiro", "Martins", "Carvalho", "Almeida", "Lopes", "Soares", "Fernandes", "Vieira", "Barbosa", "Rocha",
    "Dias", "Nascimento", "Andrade", "Moreira", "Nunes", "Marques", "Machado", "Mendes", "Freitas", "Cardoso",
    "Ramos", "Gonçalves", "Santana", "Teixeira"
]  # fmt: skip

# (floor, department, people, desk: laptop | desktop, desk phone)
HQ_DEPARTMENTS = [
    (1, "Diretoria", 6, "laptop", True),
    (1, "Financeiro", 8, "desktop", True),
    (1, "RH", 4, "laptop", True),
    (1, "Recepção", 2, "desktop", True),
    (1, "Facilities", 4, "laptop", False),
    (2, "Vendas", 16, "laptop", True),
    (2, "Marketing", 8, "laptop", False),
    (3, "Engenharia", 10, "desktop", True),
    (3, "TI", 6, "laptop", True),
    (3, "Produto", 8, "laptop", False),
]
WAREHOUSE_STAFF = 6
BRANCH_STAFF = 14
STORE_STAFF = 4

LAPTOPS = [
    # weight, oui, model, os, hostname pattern
    (5, "dell", "Latitude 5450", "Windows 11 Pro 24H2", "ACME-NB-{n:04d}"),
    (3, "lenovo", "ThinkPad T14 Gen 5", "Windows 11 Pro 24H2", "ACME-NB-{n:04d}"),
    (2, "apple", "MacBook Pro (14-inch, M4, 2024)", "macOS 26.0", "MacBook-Pro-de-{first}"),
    (1, "apple2", "MacBook Air (13-inch, M3, 2024)", "macOS 26.0", "MacBook-Air-de-{first}"),
]
PHONES = [
    (4, "apple", "iPhone17,1", "iOS 26.0", "iPhone-de-{first}"),
    (2, "apple2", "iPhone16,2", "iOS 26.0", "iPhone-de-{first}"),
    (3, "samsung", "SM-S931B", "Android 16", "Galaxy-S25-de-{first}"),
    (2, "samsung", "SM-A556E", "Android 15", "Galaxy-A55-de-{first}"),
    (1, "motorola", "XT2451-1", "Android 15", "moto-g85-{first}"),
    (1, "xiaomi", "2407FRN8EG", "Android 15", "Redmi-Note-14-{first}"),
]


def _pick(rng: random.Random, table: list[tuple]) -> tuple:
    total = sum(t[0] for t in table)
    x = rng.uniform(0, total)
    for t in table:
        x -= t[0]
        if x <= 0:
            return t
    return table[-1]


def _ascii(s: str) -> str:
    table = str.maketrans("áàâãéêíóôõúçÁÂÃÉÍÓÔÚÇ", "aaaaeeiooouc" + "AAAEIOOUC")
    return s.translate(table)


# ---------------------------------------------------------------------------


class Builder:
    def __init__(self) -> None:
        self.d = Design()
        self.rng = random.Random(SEED)
        self.asset = 100
        self.ips: dict[str, set[str]] = {}

    # -- helpers --
    def node(self, name: str, kind: str, **kw: Any) -> Node:
        return self.d.add(Node(name=name, kind=kind, **kw))

    def endpoint(
        self,
        name: str,
        kind: str,
        oui: str | None,
        ip: str | None,
        vlan: int | None,
        site: str = "hq",
        port: str = "eth0",
        speed: int | None = 1000,
        m: str | None = None,
        **kw: Any,
    ) -> Node:
        if m is None:
            m = mac(oui, name) if oui else random_mac(name)
        n = self.node(name, kind, site=site, mac=m, ip=ip, vlan=vlan, **kw)
        n.add(Port(port, "wifi" if port.startswith("wlan") else "ethernet", speed, mac=m, untagged=vlan))
        if not n.hostname:
            n.hostname = name
        return n

    def wire(self, a: str, pa: str, b: str, pb: str, kind: str = "copper") -> None:
        self.d.link(a, pa, b, pb, kind)

    def wifi(self, client: str, ssid: str, ap: str) -> None:
        self.d.wifi[client] = WifiAttach(ssid, ap)

    # -- the whole network --
    def build(self) -> Design:
        self.vlans()
        self.hq_edge()
        self.hq_core()
        self.hq_servers()
        self.hq_floors()
        self.warehouse()
        self.branch()
        self.store()
        self.lab()
        return self.d

    def vlans(self) -> None:
        d = self.d
        for site, table in (("hq", HQ_VLANS), ("branch", BRANCH_VLANS)):
            for vid, name, subnet, routed, pool, purpose in table:
                gw = None
                if subnet and routed:
                    gw = subnet.rsplit(".", 1)[0] + ".1"
                d.vlans[(site, vid)] = Vlan(vid, name, subnet, gw, site, routed, pool, purpose=purpose)
        d.vlans[("store", STORE_LAN)] = Vlan(
            STORE_LAN,
            "LAN",
            "10.30.1.0/24",
            "10.30.1.1",
            "store",
            True,
            ("10.30.1.100", "10.30.1.200"),
            purpose="The store's LAN",
        )
        d.vlans[("store", STORE_GUEST)] = Vlan(
            STORE_GUEST,
            "GUEST",
            "10.30.80.0/24",
            "10.30.80.1",
            "store",
            True,
            ("10.30.80.20", "10.30.80.250"),
            purpose="Customers' Wi-Fi",
        )
        d.vlans[("lab", LAB_LAN)] = Vlan(
            LAB_LAN,
            "LAB-LAN",
            "192.168.200.0/24",
            "192.168.200.1",
            "lab",
            True,
            ("192.168.200.100", "192.168.200.199"),
            purpose="Behind the pfSense lab router",
        )
        d.ssids = {
            "Acme": Ssid("Acme", 32, ("2.4", "5", "6"), "wpa2-enterprise"),
            "Acme-IoT": Ssid("Acme-IoT", 50, ("2.4",), "wpa2"),
            "Acme-Visitantes": Ssid("Acme-Visitantes", 80, ("2.4", "5"), "wpa2", guest=True),
            "Acme-Galpao": Ssid("Acme-Galpao", 34, ("2.4", "5"), "wpa2", site="hq"),
            "Acme-Filial": Ssid("Acme", 32, ("2.4", "5"), "wpa2-enterprise", site="branch"),
            "Acme-Filial-Visitantes": Ssid("Acme-Visitantes", 80, ("2.4", "5"), "wpa2", guest=True, site="branch"),
            "Acme-Loja": Ssid("Acme-Loja", STORE_LAN, ("2.4", "5"), "wpa2", site="store"),
            "Loja-Clientes": Ssid("Loja-Clientes", STORE_GUEST, ("2.4", "5"), "wpa2", guest=True, site="store"),
        }
        d.sites = {
            "hq": {"name": "Matriz (São Paulo)", "domain": "acme.local"},
            "branch": {"name": "Filial Campinas", "domain": "cps.acme.local"},
            "store": {"name": "Loja Santos", "domain": "loja.acme.local"},
            "lab": {"name": "Lab (behind lab-fw01)", "domain": "lab.acme.local"},
        }

    # -- HQ edge: ISP equipment and the OPNsense HA pair --
    def hq_edge(self) -> None:
        ont = self.endpoint(
            "ont-fiber",
            "modem",
            "huawei",
            "192.168.100.1",
            900,
            vendor="Huawei",
            model="HG8010H",
            version="V3R017C10S120",
            presence="always",
            hostname="ont-fiber",
        )
        ont.ports["eth0"].untagged = 900
        ont.extra["isp"] = "Vivo Empresas"
        lte = self.endpoint(
            "lte-gw01",
            "modem",
            "teltonika",
            "192.168.8.1",
            901,
            vendor="Teltonika",
            model="RUTX50",
            version="RUTX_R_00.07.14",
            hostname="lte-gw01",
        )
        lte.ports["eth0"].untagged = 901
        lte.scan = {"ports": [22, 80, 443], "titles": {443: "Teltonika RUTX50"}, "ssh": "SSH-2.0-dropbear_2022.83"}
        lte.extra["isp"] = "Claro 5G"
        for i, (name, ip, prio) in enumerate(
            (("fw-hq-01", "10.10.10.2", "master"), ("fw-hq-02", "10.10.10.3", "backup"))
        ):
            base = mac("intel", name)
            fw = self.node(
                name,
                "firewall",
                vendor="Deciso",
                model="DEC2752",
                os="OPNsense",
                version="26.7.2",
                serial=f"DEC2752{2410 + i:04d}A{311 + i * 7}",
                mac=base,
                ip=ip,
                vlan=10,
                hostname=name,
                api=f"opnsense-{name}",
                extra={
                    "carp": prio,
                    "peer": "fw-hq-02" if i == 0 else "fw-hq-01",
                    "cpu": "AMD Ryzen Embedded V1500B",
                    "cores": 8,
                    "mem_mb": 16384,
                },
                boot=1788580800 + i * 3600,  # 2026-09-05, last update
            )
            fw.lldp = True
            fw.add(
                Port(
                    "igc0",
                    speed=1000,
                    mac=offset_mac(base, 0),
                    descr="WAN1 fiber",
                    untagged=900,
                    connector="rj45",
                    role="wan",
                ),
                Port(
                    "igc1",
                    speed=1000,
                    mac=offset_mac(base, 1),
                    descr="WAN2 LTE",
                    untagged=901,
                    connector="rj45",
                    role="wan",
                    ips=(f"192.168.8.{10 + i}/24",),
                ),
                Port(
                    "igc2",
                    speed=1000,
                    mac=offset_mac(base, 2),
                    descr="DMZ",
                    untagged=90,
                    connector="rj45",
                    ips=(f"10.10.90.{2 + i}/24",),
                    role="lan",
                ),
                Port(
                    "igc3",
                    speed=1000,
                    mac=offset_mac(base, 3),
                    descr="SYNC",
                    untagged=999,
                    connector="rj45",
                    ips=(f"10.255.255.{1 + i}/30",),
                    role="sync",
                ),
                Port("ax0", "sfp", 10000, mac=offset_mac(base, 4), lag="lagg0", connector="sfp"),
                Port("ax1", "sfp", 10000, mac=offset_mac(base, 4), lag="lagg0", connector="sfp"),
                Port(
                    "lagg0",
                    "lag",
                    20000,
                    mac=offset_mac(base, 4),
                    members=("ax0", "ax1"),
                    tagged=tuple(v for v, _, s, r, _, _ in HQ_VLANS if s and r and v != 90),
                    descr="LAN trunk",
                ),
                Port(
                    "pppoe0",
                    "ppp",
                    None,
                    descr="WAN1 fiber (PPPoE)",
                    parent="igc0",
                    ips=("203.0.113.45/32",) if i == 0 else (),
                    role="wan",
                ),
                Port("wg0", "wg", None, descr="WireGuard: Filial Campinas", ips=("10.255.0.1/30",)),
                Port("enc0", "ipsec", None, descr="IPsec: Loja Santos"),
                Port("lo0", "loopback", None, ips=("127.0.0.1/8",)),
            )
            for vid, vname, subnet, routed, _pool, _ in HQ_VLANS:
                if not subnet or not routed or vid == 90:
                    continue
                net = subnet.split("/")[0].rsplit(".", 1)[0]
                bits = subnet.split("/")[1]
                fw.add(
                    Port(
                        vlan_iface("hq", vid),
                        "vlan",
                        None,
                        mac=offset_mac(base, 4),
                        vlan=vid,
                        parent="lagg0",
                        untagged=vid,
                        ips=(f"{net}.{2 + i}/{bits}",),
                        descr=vname,
                    )
                )
        self.wire("fw-hq-01", "igc3", "fw-hq-02", "igc3")

    # -- HQ core, storage, DMZ switches --
    def hq_core(self) -> None:
        b = self.node(
            "core-sw01",
            "switch",
            vendor="Cisco",
            model="C9300-48UXM",
            os="IOS-XE",
            version="17.12.4",
            serial="FOC2731X0QZ",
            mac=mac("cisco2", "core-sw01"),
            ip="10.10.10.5",
            vlan=10,
            hostname="core-sw01",
            api="snmp-core-sw01",
            lldp=True,
            boot=1784000000,
            extra={"stack": ["C9300-48UXM FOC2731X0QZ", "C9300-48UXM FOC2731X1AB"], "snmp": "v3"},
        )
        for m in (1, 2):
            for p in range(1, 49):
                if p <= 36:
                    b.add(Port(f"Tw{m}/0/{p}", speed=2500, untagged=1, connector="rj45", poe=True))
                else:
                    b.add(Port(f"Te{m}/0/{p}", speed=10000, untagged=1, connector="rj45", poe=True))
            for p in range(1, 9):
                b.add(Port(f"Te{m}/1/{p}", "sfp", 10000, untagged=1, connector="sfp"))
        b.add(Port("Vl10", "vlan", None, vlan=10, untagged=10, ips=("10.10.10.5/24",), mac=offset_mac(b.mac, 0x40)))

        def po(
            name: str, members: tuple[str, ...], tagged: tuple[int, ...], descr: str, untagged: int | None = None
        ) -> None:
            for m in members:
                b.ports[m].lag = name
                b.ports[m].untagged, b.ports[m].tagged = untagged, tagged
            b.add(
                Port(name, "lag", 10000 * len(members), members=members, tagged=tagged, untagged=untagged, descr=descr)
            )

        fw_trunk = tuple(v for v, _, s, r, _, _ in HQ_VLANS if s and r and v != 90)
        po("Po11", ("Te1/1/1", "Te2/1/1"), fw_trunk, "fw-hq-01 lagg0")
        po("Po12", ("Te1/1/2", "Te2/1/2"), fw_trunk, "fw-hq-02 lagg0")
        po("Po21", ("Te1/1/3", "Te2/1/3"), (10, 30, 32, 40, 50, 60, 70, 80), "sw-f1-01")
        po("Po22", ("Te1/1/4", "Te2/1/4"), (10, 30, 32, 40, 50, 60, 70, 80), "sw-f2-01")
        po("Po23", ("Te1/1/5", "Te2/1/5"), (10, 30, 32, 40, 50, 60, 70, 80), "sw-f3-01")
        po("Po30", ("Te1/1/6", "Te2/1/6"), (10, 20, 30, 40, 50, 60, 70, 100, 110, 120), "sw-stor-01")

        def access(port: str, vlan: int, descr: str) -> None:
            b.ports[port].untagged, b.ports[port].descr = vlan, descr

        b.ports["Te1/1/7"].untagged, b.ports["Te1/1/7"].tagged = 10, (30, 34, 50, 60, 70)
        b.ports["Te1/1/7"].descr = "sw-wh-01 (warehouse, OS2 fiber)"
        access("Tw1/0/1", 900, "fw-hq-01 igc0 WAN1")
        access("Tw2/0/1", 900, "fw-hq-02 igc0 WAN1")
        access("Tw1/0/2", 900, "ONT Vivo")
        access("Tw1/0/3", 901, "fw-hq-01 igc1 WAN2")
        access("Tw2/0/3", 901, "fw-hq-02 igc1 WAN2")
        access("Tw2/0/2", 901, "RUTX50 5G")
        for i, node in enumerate(("pve01", "pve02", "pve03")):
            access(f"Tw1/0/{10 + i}", 10, f"idrac-{node}")
        po("Po41", ("Tw1/0/13", "Tw2/0/13"), (103,), "pve01 bond0", untagged=10)
        po("Po42", ("Tw1/0/14", "Tw2/0/14"), (103,), "pve02 bond0", untagged=10)
        po("Po43", ("Tw1/0/15", "Tw2/0/15"), (103,), "pve03 bond0", untagged=10)
        access("Tw1/0/20", 10, "ups-rack01 NMC")
        access("Tw2/0/20", 10, "sw-dmz-01 me0")
        access("Tw2/0/22", 60, "nvr01")
        for p in b.ports.values():
            if p.kind != "lag" and p.untagged == 1 and not p.lag and not p.tagged:
                p.admin_up = True  # unused ports: VLAN 1, no link
        self.wire("core-sw01", "Po11", "fw-hq-01", "lagg0", "fiber")
        self.wire("core-sw01", "Te1/1/1", "fw-hq-01", "ax0", "dac")
        self.wire("core-sw01", "Te2/1/1", "fw-hq-01", "ax1", "dac")
        self.wire("core-sw01", "Po12", "fw-hq-02", "lagg0", "fiber")
        self.wire("core-sw01", "Te1/1/2", "fw-hq-02", "ax0", "dac")
        self.wire("core-sw01", "Te2/1/2", "fw-hq-02", "ax1", "dac")
        self.wire("core-sw01", "Tw1/0/1", "fw-hq-01", "igc0")
        self.wire("core-sw01", "Tw2/0/1", "fw-hq-02", "igc0")
        self.wire("core-sw01", "Tw1/0/2", "ont-fiber", "eth0")
        self.wire("core-sw01", "Tw1/0/3", "fw-hq-01", "igc1")
        self.wire("core-sw01", "Tw2/0/3", "fw-hq-02", "igc1")
        self.wire("core-sw01", "Tw2/0/2", "lte-gw01", "eth0")

        # Storage switch: MikroTik CRS518 (REST API, and SNMP too).
        s = self.node(
            "sw-stor-01",
            "switch",
            vendor="MikroTik",
            model="CRS518-16XS-2XQ",
            os="RouterOS",
            version="7.16.2",
            serial="HFB09RQ1N7K",
            mac=mac("mikrotik", "sw-stor-01"),
            ip="10.10.10.6",
            vlan=10,
            hostname="sw-stor-01",
            api="mikrotik-sw-stor-01",
            lldp=True,
            boot=1786400000,
            extra={"snmp": "v2c"},
        )
        s.add(Port("ether1", speed=1000, untagged=10, connector="rj45", mac=s.mac, descr="mgmt"))
        for p in range(1, 17):
            s.add(Port(f"sfp28-{p}", "sfp", 25000, mac=offset_mac(s.mac, p), connector="sfp"))
        s.add(
            Port("qsfp28-1-1", "sfp", 100000, mac=offset_mac(s.mac, 17), connector="qsfp"),
            Port("qsfp28-2-1", "sfp", 100000, mac=offset_mac(s.mac, 21), connector="qsfp"),
        )
        stor_trunk = (20, 30, 40, 50, 60, 70, 90, 100, 101, 102, 110, 120)
        for p, (node, lag) in enumerate((("pve01", "bond1"), ("pve02", "bond2"), ("pve03", "bond3"))):
            for k in (1, 2):
                port = s.ports[f"sfp28-{p * 2 + k}"]
                port.lag, port.tagged, port.descr = lag, stor_trunk, f"{node} ens1f{k - 1}np{k - 1}"
            s.add(
                Port(
                    lag,
                    "lag",
                    50000,
                    members=(f"sfp28-{p * 2 + 1}", f"sfp28-{p * 2 + 2}"),
                    tagged=stor_trunk,
                    mac=offset_mac(s.mac, p * 2 + 1),
                    descr=node,
                )
            )
        for p in (15, 16):
            port = s.ports[f"sfp28-{p}"]
            port.lag, port.speed, port.untagged, port.tagged = (
                "bond-core",
                10000,
                10,
                (20, 30, 40, 50, 60, 70, 100, 110, 120),
            )
            port.descr = f"core-sw01 Te{p - 14}/1/6"
        s.add(
            Port(
                "bond-core",
                "lag",
                20000,
                members=("sfp28-15", "sfp28-16"),
                untagged=10,
                tagged=(20, 30, 40, 50, 60, 70, 100, 110, 120),
                mac=offset_mac(s.mac, 15),
                descr="uplink core-sw01",
            )
        )
        s.ports["sfp28-7"].speed, s.ports["sfp28-7"].untagged, s.ports["sfp28-7"].tagged = 10000, 10, (20, 100, 110)
        s.ports["sfp28-7"].descr = "nas01"
        s.ports["sfp28-8"].speed, s.ports["sfp28-8"].untagged, s.ports["sfp28-8"].tagged = 10000, 10, (110,)
        s.ports["sfp28-8"].descr = "bkp-nas01"
        s.ports["ether1"].untagged = 10
        s.add(
            Port(
                "bridge",
                "bridge",
                None,
                mac=s.mac,
                members=tuple(p for p in s.ports if p.startswith(("sfp28", "bond"))),
            )
        )
        s.add(Port("vlan10", "vlan", None, vlan=10, parent="bridge", untagged=10, ips=("10.10.10.6/24",), mac=s.mac))
        self.wire("core-sw01", "Po30", "sw-stor-01", "bond-core", "fiber")
        self.wire("core-sw01", "Te1/1/6", "sw-stor-01", "sfp28-15", "fiber")
        self.wire("core-sw01", "Te2/1/6", "sw-stor-01", "sfp28-16", "fiber")

        # DMZ switch: Juniper EX2300 (SNMP), physically separate; its OOB me0 on MGMT.
        j = self.node(
            "sw-dmz-01",
            "switch",
            vendor="Juniper",
            model="EX2300-24T",
            os="Junos",
            version="23.4R2-S3",
            serial="JW3624AN0517",
            mac=mac("juniper", "sw-dmz-01"),
            ip="10.10.10.7",
            vlan=10,
            hostname="sw-dmz-01",
            api="snmp-sw-dmz-01",
            lldp=True,
            boot=1781000000,
            extra={"snmp": "v2c"},
        )
        for p in range(24):
            j.add(Port(f"ge-0/0/{p}", speed=1000, untagged=90, connector="rj45", mac=offset_mac(j.mac, p + 1)))
        for p in range(4):
            j.add(Port(f"xe-0/1/{p}", "sfp", 10000, untagged=90, connector="sfp", mac=offset_mac(j.mac, p + 30)))
        j.add(Port("me0", speed=1000, untagged=10, ips=("10.10.10.7/24",), mac=offset_mac(j.mac, 40), descr="OOB mgmt"))
        j.ports["ge-0/0/0"].descr, j.ports["ge-0/0/1"].descr = "fw-hq-01 igc2", "fw-hq-02 igc2"
        self.wire("sw-dmz-01", "ge-0/0/0", "fw-hq-01", "igc2")
        self.wire("sw-dmz-01", "ge-0/0/1", "fw-hq-02", "igc2")
        self.wire("sw-dmz-01", "me0", "core-sw01", "Tw2/0/20")
        for k, (name, ip, model, role) in enumerate(
            (
                ("web01", "10.10.90.11", "PowerEdge R250", "nginx"),
                ("web02", "10.10.90.12", "PowerEdge R250", "nginx"),
                ("mx01", "10.10.90.20", "PowerEdge R250", "postfix"),
            )
        ):
            n = self.endpoint(
                name,
                "server",
                "dell",
                ip,
                90,
                vendor="Dell",
                model=model,
                os="Ubuntu 24.04.3 LTS",
                hostname=f"{name}.acme.com.br",
                traffic="dmz-web" if role == "nginx" else "dmz-mail",
                lldp=True,
                port="eno1",
            )
            n.scan = {
                "ports": [22, 80, 443] if role == "nginx" else [22, 25, 587],
                "titles": {443: "Acme | Soluções Industriais", 80: "301 Moved Permanently"} if role == "nginx" else {},
                "ssh": "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13",
            }
            j.ports[f"ge-0/0/{10 + k}"].descr = name
            self.wire("sw-dmz-01", f"ge-0/0/{10 + k}", name, "eno1")

        # UPS with a network card (SNMP v1/v2c "public" left at its default).
        u = self.endpoint(
            "ups-rack01",
            "ups",
            "apc",
            "10.10.10.40",
            10,
            vendor="APC",
            model="Smart-UPS SRT 5000",
            version="NMC3 AOS v3.2.1.0",
            serial="AS2316160742",
            hostname="ups-rack01",
            api="snmp-ups-rack01",
            speed=100,
        )
        u.extra = {"snmp": "public", "nmc": "AP9641", "battery_pct": 100, "runtime_min": 27, "load_pct": 38}
        u.scan = {"ports": [22, 80, 443], "titles": {443: "Network Management Card"}}
        self.wire("core-sw01", "Tw1/0/20", "ups-rack01", "eth0")
        nvr = self.endpoint(
            "nvr01",
            "nvr",
            "hikvision",
            "10.10.60.10",
            60,
            vendor="Hikvision",
            model="DS-7716NI-I4",
            version="V4.62.210",
            hostname="nvr01",
            traffic="nvr",
            serial="DS-7716NI-I41620230817CCRRK12345678",
        )
        nvr.scan = {"ports": [80, 443, 554, 8000], "titles": {80: "NVR", 443: "NVR"}}
        self.wire("core-sw01", "Tw2/0/22", "nvr01", "eth0")

    # -- HQ servers: Proxmox cluster, NAS, VMs --
    def hq_servers(self) -> None:
        stor_trunk = (20, 30, 40, 50, 60, 70, 90, 100, 101, 102, 110, 120)
        for i, name in enumerate(("pve01", "pve02", "pve03")):
            base = mac("dell", name)
            nic25 = mac("mellanox", name)
            pve = self.node(
                name,
                "hypervisor",
                vendor="Dell",
                model="PowerEdge R650xs",
                os="Proxmox VE",
                version="9.0.10",
                serial=f"7XQ{4 + i}K53",
                mac=base,
                ip=f"10.10.10.{11 + i}",
                vlan=10,
                hostname=f"{name}.acme.local",
                api="proxmox",
                lldp=True,
                cores=64,
                mem_mb=524288,
                disk_gb=480,
                boot=1786000000 + i * 1800,
                extra={"cpu": "Intel(R) Xeon(R) Silver 4314 CPU @ 2.40GHz", "sockets": 2, "kernel": "6.14.11-2-pve"},
            )
            pve.add(
                Port("eno1", speed=1000, mac=base, lag="bond0", connector="rj45"),
                Port("eno2", speed=1000, mac=offset_mac(base, 1), lag="bond0", connector="rj45"),
                Port("ens1f0np0", "sfp", 25000, mac=nic25, lag="bond1", connector="sfp"),
                Port("ens1f1np1", "sfp", 25000, mac=offset_mac(nic25, 1), lag="bond1", connector="sfp"),
                Port("bond0", "lag", 2000, mac=base, members=("eno1", "eno2"), untagged=10, tagged=(103,)),
                Port("bond1", "lag", 50000, mac=nic25, members=("ens1f0np0", "ens1f1np1"), tagged=stor_trunk),
                Port(
                    "vmbr0",
                    "bridge",
                    None,
                    mac=base,
                    members=("bond0",),
                    untagged=10,
                    ips=(f"10.10.10.{11 + i}/24",),
                    descr="Management",
                ),
                Port(
                    "vmbr0.103",
                    "vlan",
                    None,
                    vlan=103,
                    parent="vmbr0",
                    ips=(f"10.10.103.{11 + i}/24",),
                    descr="Corosync",
                ),
                Port(
                    "vmbr1",
                    "bridge",
                    None,
                    mac=nic25,
                    members=("bond1",),
                    tagged=stor_trunk,
                    descr="VM trunk (VLAN aware)",
                ),
                Port(
                    "vmbr1.100",
                    "vlan",
                    None,
                    vlan=100,
                    parent="vmbr1",
                    ips=(f"10.10.100.{11 + i}/24",),
                    descr="Storage",
                ),
                Port(
                    "vmbr1.101",
                    "vlan",
                    None,
                    vlan=101,
                    parent="vmbr1",
                    ips=(f"10.10.101.{11 + i}/24",),
                    descr="Ceph public",
                ),
                Port(
                    "vmbr1.102",
                    "vlan",
                    None,
                    vlan=102,
                    parent="vmbr1",
                    ips=(f"10.10.102.{11 + i}/24",),
                    descr="Ceph cluster",
                ),
                Port(
                    "vmbr1.110", "vlan", None, vlan=110, parent="vmbr1", ips=(f"10.10.110.{11 + i}/24",), descr="Backup"
                ),
            )
            pve.scan = {
                "ports": [22, 111, 3128, 8006],
                "titles": {8006: f"{name} - Proxmox Virtual Environment"},
                "ssh": "SSH-2.0-OpenSSH_10.0p2 Debian-7",
            }
            if name == "pve02":
                pve.add(
                    Port(
                        "vmbr2",
                        "bridge",
                        None,
                        mac=offset_mac(nic25, 0x10),
                        untagged=LAB_LAN,
                        descr="Lab LAN (no uplink)",
                    )
                )
            self.wire("core-sw01", f"Po4{i + 1}", name, "bond0")
            self.wire("core-sw01", f"Tw1/0/{13 + i}", name, "eno1")
            self.wire("core-sw01", f"Tw2/0/{13 + i}", name, "eno2")
            self.wire("sw-stor-01", f"bond{i + 1}", name, "bond1", "dac")
            self.wire("sw-stor-01", f"sfp28-{i * 2 + 1}", name, "ens1f0np0", "dac")
            self.wire("sw-stor-01", f"sfp28-{i * 2 + 2}", name, "ens1f1np1", "dac")
            idrac = self.endpoint(
                f"idrac-{name}",
                "bmc",
                "dell2",
                f"10.10.10.{14 + i}",
                10,
                vendor="Dell",
                model="iDRAC9",
                version="7.20.30.00",
                hostname=f"idrac-{name}",
                speed=1000,
            )
            idrac.scan = {"ports": [22, 80, 443], "titles": {443: "iDRAC9"}, "ssh": "SSH-2.0-OpenSSH_8.7"}
            self.wire("core-sw01", f"Tw1/0/{10 + i}", f"idrac-{name}", "eth0")

        nas = self.node(
            "nas01",
            "nas",
            vendor="Synology",
            model="RS1221+",
            os="DSM",
            version="7.2.2-72806 Update 4",
            serial="2290RQRV6D2X1",
            mac=mac("synology", "nas01"),
            ip="10.10.10.30",
            vlan=10,
            hostname="nas01",
            api="snmp-nas01",
            traffic="nas",
            lldp=False,
            boot=1785500000,
            extra={"snmp": "v2c", "disks": 8, "volume_tb": 64},
        )
        nas.add(
            Port(
                "eth4",
                "sfp",
                10000,
                mac=nas.mac,
                untagged=10,
                tagged=(20, 100, 110),
                connector="sfp",
                ips=("10.10.10.30/24", "10.10.20.25/24", "10.10.100.25/24", "10.10.110.25/24"),
            )
        )
        nas.scan = {
            "ports": [22, 80, 139, 443, 445, 548, 5000, 5001],
            "titles": {5001: "Synology DiskStation - nas01", 5000: "Synology DiskStation - nas01"},
            "mdns": ["_smb._tcp", "_afpovertcp._tcp", "_http._tcp"],
            "netbios": "NAS01",
            "ssdp": {"manufacturer": "Synology", "model": "RS1221+", "type": "Basic"},
        }
        self.wire("sw-stor-01", "sfp28-7", "nas01", "eth4", "fiber")
        bkp = self.node(
            "bkp-nas01",
            "nas",
            vendor="QNAP",
            model="TS-873AU-RP",
            os="QTS",
            version="5.2.6.3195",
            serial="Q23BI12345",
            mac=mac("qnap", "bkp-nas01"),
            ip="10.10.10.31",
            vlan=10,
            hostname="bkp-nas01",
            api="snmp-bkp-nas01",
            traffic="idle",
            boot=1785900000,
            extra={"snmp": "v2c", "disks": 8},
        )
        bkp.add(
            Port(
                "eth4",
                "sfp",
                10000,
                mac=bkp.mac,
                untagged=10,
                tagged=(110,),
                connector="sfp",
                ips=("10.10.10.31/24", "10.10.110.20/24"),
            )
        )
        bkp.scan = {
            "ports": [22, 80, 443, 445, 8080],
            "titles": {8080: "QTS - bkp-nas01", 443: "QTS - bkp-nas01"},
            "netbios": "BKP-NAS01",
            "mdns": ["_smb._tcp", "_qdiscover._tcp"],
        }
        self.wire("sw-stor-01", "sfp28-8", "bkp-nas01", "eth4", "fiber")

        # VMs and containers: (vmid, name, node, type, vlan, ip, os, cores, mem GB, disk GB, traffic, extras)
        guests = [
            (100, "dc01", "pve01", "qemu", 20, "10.10.20.10", "win2022", 4, 8, 120, "dc", {}),
            (102, "erp-app01", "pve01", "qemu", 20, "10.10.20.30", "l26", 8, 32, 200, "erp", {}),
            (104, "unifi01", "pve01", "qemu", 10, "10.10.10.20", "l26", 2, 4, 40, "light", {}),
            (106, "pbx01", "pve01", "qemu", 40, "10.10.40.10", "l26", 2, 4, 40, "pbx", {}),
            (108, "docker01", "pve01", "qemu", 20, "10.10.20.50", "l26", 8, 16, 300, "light", {}),
            (200, "netbox", "pve01", "lxc", 20, "10.10.20.60", "debian", 2, 4, 20, "light", {}),
            (201, "omini", "pve01", "lxc", 10, "10.10.10.250", "debian", 2, 2, 16, "light", {}),
            (101, "dc02", "pve02", "qemu", 20, "10.10.20.11", "win2022", 4, 8, 120, "dc", {}),
            (103, "erp-db01", "pve02", "qemu", 20, "10.10.20.31", "l26", 8, 64, 800, "erp", {}),
            (105, "file01", "pve02", "qemu", 20, "10.10.20.20", "win2022", 4, 16, 4000, "file", {}),
            (107, "mon01", "pve02", "qemu", 20, "10.10.20.40", "l26", 4, 8, 200, "light", {}),
            (109, "homeassistant", "pve02", "qemu", 50, "10.10.50.10", "l26", 2, 4, 64, "light", {}),
            (130, "lab-fw01", "pve02", "qemu", 120, "10.10.120.10", "other", 2, 4, 32, "light", {}),
            (202, "lab-k3s-01", "pve02", "lxc", LAB_LAN, "192.168.200.11", "ubuntu", 4, 8, 64, "lab", {}),
            (203, "lab-k3s-02", "pve02", "lxc", LAB_LAN, "192.168.200.12", "ubuntu", 4, 8, 64, "lab", {}),
            (204, "lab-k3s-03", "pve02", "lxc", LAB_LAN, "192.168.200.13", "ubuntu", 4, 8, 64, "lab", {}),
            (110, "pbs01", "pve03", "qemu", 110, "10.10.110.10", "l26", 8, 32, 64, "pbs", {}),
            (112, "print01", "pve03", "qemu", 70, "10.10.70.5", "win2022", 2, 4, 80, "light", {}),
            (114, "wsus01", "pve03", "qemu", 20, "10.10.20.45", "win2022", 2, 8, 500, "light", {}),
            (205, "rproxy-int", "pve03", "lxc", 20, "10.10.20.70", "debian", 1, 1, 8, "light", {}),
        ]
        scans = {
            "dc01": {"ports": [53, 88, 135, 139, 389, 445, 636, 3268, 3389], "netbios": "DC01"},
            "dc02": {"ports": [53, 88, 135, 139, 389, 445, 636, 3268, 3389], "netbios": "DC02"},
            "file01": {"ports": [135, 139, 445, 3389], "netbios": "FILE01"},
            "print01": {"ports": [135, 139, 445, 515, 631, 3389, 9100], "netbios": "PRINT01"},
            "wsus01": {"ports": [135, 139, 445, 3389, 8530, 8531], "netbios": "WSUS01"},
            "erp-app01": {
                "ports": [22, 80, 443, 8069],
                "titles": {443: "Odoo", 80: "Odoo"},
                "ssh": "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13",
            },
            "erp-db01": {"ports": [22, 5432], "ssh": "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13"},
            "unifi01": {
                "ports": [22, 8080, 8443],
                "titles": {8443: "UniFi Network"},
                "ssh": "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u7",
            },
            "pbx01": {
                "ports": [22, 443, 5001, 5060],
                "titles": {443: "3CX Phone System Management Console", 5001: "3CX Phone System Management Console"},
                "ssh": "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u7",
            },
            "docker01": {
                "ports": [22, 3000, 3001, 8000, 8082, 9443],
                "ssh": "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u7",
                "titles": {
                    3000: "Grafana",
                    3001: "Uptime Kuma",
                    8000: "Paperless-ngx",
                    8082: "Vaultwarden Web",
                    9443: "Portainer",
                },
            },
            "netbox": {"ports": [22, 80, 443], "titles": {443: "Home | NetBox"}},
            "omini": {"ports": [22, 8080], "titles": {8080: "Omini"}},
            "mon01": {
                "ports": [22, 80, 10051],
                "titles": {80: "Zabbix"},
                "ssh": "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13",
                "snmp": True,
            },
            "homeassistant": {
                "ports": [8123, 4357],
                "titles": {8123: "Home Assistant"},
                "mdns": ["_home-assistant._tcp", "_hap._tcp"],
            },
            "lab-fw01": {"ports": [22, 80, 443], "titles": {443: "pfSense - Login"}},
            "pbs01": {"ports": [22, 8007], "titles": {8007: "pbs01 - Proxmox Backup Server"}},
            "rproxy-int": {"ports": [22, 80, 81, 443], "titles": {81: "Nginx Proxy Manager"}},
            "lab-k3s-01": {"ports": [22, 6443]},
            "lab-k3s-02": {"ports": [22, 6443]},
            "lab-k3s-03": {"ports": [22, 6443]},
        }
        oses = {
            "win2022": "Windows Server 2022",
            "l26": "Linux",
            "debian": "Debian 12",
            "ubuntu": "Ubuntu 24.04",
            "other": "FreeBSD",
        }
        for vmid, name, host, kind, vlan, ip, ostype, cores, mem, disk, traffic, _ in guests:
            m = mac("proxmox", name)
            port = "net0"
            n = self.node(
                name,
                "vm" if kind == "qemu" else "lxc",
                site="lab" if vlan == LAB_LAN else "hq",
                vendor="Proxmox",
                os=oses[ostype],
                mac=m,
                ip=ip,
                vlan=vlan,
                hostname=name,
                host=host,
                vmid=vmid,
                cores=cores,
                mem_mb=mem * 1024,
                disk_gb=disk,
                traffic=traffic,
                extra={"ostype": ostype},
            )
            n.add(Port(port, "virtual", 10000, mac=m, untagged=vlan))
            n.scan = scans.get(name, {})
            hp = self.d.nodes[host]
            tap = f"tap{vmid}i0" if kind == "qemu" else f"veth{vmid}i0"
            bridge = "vmbr2" if vlan == LAB_LAN else ("vmbr0" if vlan == 10 else "vmbr1")
            hp.add(Port(tap, "tap" if kind == "qemu" else "veth", 10000, untagged=vlan, parent=bridge, descr=name))
            self.wire(host, tap, name, port, "virtual")
        # The lab router VM: a second NIC on the internal bridge, and its pfSense interfaces.
        lab = self.d.nodes["lab-fw01"]
        lab.kind, lab.vendor, lab.os, lab.version = "firewall", "Netgate", "pfSense CE", "2.8.0-RELEASE"
        lab.api = "pfsense"
        lab.model = "pfSense (VM)"
        lab.ports.clear()
        lm = lab.mac
        lab.add(
            Port("vtnet0", "virtual", 10000, mac=lm, untagged=120, ips=("10.10.120.10/24",), descr="WAN", role="wan"),
            Port(
                "vtnet1",
                "virtual",
                10000,
                mac=mac("proxmox", "lab-fw01", "net1"),
                untagged=LAB_LAN,
                ips=("192.168.200.1/24",),
                descr="LAN",
                role="lan",
            ),
        )
        self.d.links = [lk for lk in self.d.links if lk.b != "lab-fw01"]
        self.wire("pve02", "tap130i0", "lab-fw01", "vtnet0", "virtual")
        pve2 = self.d.nodes["pve02"]
        pve2.add(Port("tap130i1", "tap", 10000, untagged=LAB_LAN, parent="vmbr2", descr="lab-fw01"))
        self.wire("pve02", "tap130i1", "lab-fw01", "vtnet1", "virtual")

    # -- HQ floors: access switches, APs, people and their devices --
    def hq_floors(self) -> None:
        d = self.d
        # Floor switches: two Aruba 2930F (SNMP), one UniFi USW Pro 48 PoE (UniFi controller).
        for floor, name, ip in (
            (1, "sw-f1-01", "10.10.10.21"),
            (2, "sw-f2-01", "10.10.10.22"),
            (3, "sw-f3-01", "10.10.10.23"),
        ):
            if floor < 3:
                sw = self.node(
                    name,
                    "switch",
                    vendor="Aruba",
                    model="2930F-48G-PoE+-4SFP+ (JL256A)",
                    os="ArubaOS-Switch",
                    version="WC.16.11.0024",
                    serial=f"CN0{floor}KHM0{floor}F",
                    mac=mac("hpe", name),
                    ip=ip,
                    vlan=10,
                    hostname=name,
                    api=f"snmp-{name}",
                    lldp=True,
                    boot=1787000000 + floor * 5000,
                    extra={"snmp": "v2c"},
                )
                for p in range(1, 49):
                    sw.add(Port(str(p), speed=1000, untagged=30, poe=True, connector="rj45", mac=offset_mac(sw.mac, p)))
                for p in range(49, 53):
                    sw.add(Port(str(p), "sfp", 10000, untagged=1, connector="sfp", mac=offset_mac(sw.mac, p)))
                up = ("49", "50")
                lag = "Trk1"
            else:
                sw = self.node(
                    name,
                    "switch",
                    vendor="Ubiquiti",
                    model="USW-Pro-48-PoE",
                    os="UniFi",
                    version="7.1.26",
                    serial="",
                    mac=mac("ubiquiti", name),
                    ip=ip,
                    vlan=10,
                    hostname=name,
                    api="unifi",
                    lldp=True,
                    boot=1787100000,
                    extra={"unifi_model": "USL48PB"},
                )
                for p in range(1, 49):
                    sw.add(Port(f"Port {p}", speed=1000, untagged=30, poe=True, connector="rj45", index=p))
                for p in range(49, 53):
                    sw.add(Port(f"Port {p}", "sfp", 10000, untagged=1, connector="sfp", index=p))
                up = ("Port 49", "Port 50")
                lag = "Port 49-50"  # UniFi reports an aggregate under its first member; see the vendor emulator
            uplink_vlans = (30, 32, 40, 50, 60, 70, 80)
            for u in up:
                sw.ports[u].lag, sw.ports[u].untagged, sw.ports[u].tagged = lag, 10, uplink_vlans
                sw.ports[u].descr = "core-sw01"
            sw.add(Port(lag, "lag", 20000, members=up, untagged=10, tagged=uplink_vlans, descr="uplink core-sw01"))
            sw.add(Port("mgmt", "vlan", None, vlan=10, untagged=10, ips=(f"{ip}/24",), mac=sw.mac))
            po = f"Po2{floor}"
            self.wire("core-sw01", po, name, lag, "fiber")
            self.wire("core-sw01", f"Te1/1/{2 + floor}", name, up[0], "fiber")
            self.wire("core-sw01", f"Te2/1/{2 + floor}", name, up[1], "fiber")

        def swport(floor: int, p: int) -> tuple[str, str]:
            name = f"sw-f{floor}-01"
            return name, (str(p) if floor < 3 else f"Port {p}")

        # UniFi APs on the floor switches' PoE ports: (name, model, floor, port)
        aps = [
            ("ap-f1-01", "U6-Pro", 1, 45),
            ("ap-f1-02", "U6-Pro", 1, 46),
            ("ap-cafe-01", "U6-Enterprise", 1, 47),
            ("ap-f2-01", "U6-Pro", 2, 45),
            ("ap-f2-02", "U6-Pro", 2, 46),
            ("ap-f3-01", "U6-Pro", 3, 45),
            ("ap-f3-02", "U6-Pro", 3, 46),
            ("ap-mr-01", "U7-Pro", 3, 47),
        ]
        ap_vlans = (32, 50, 80)
        for k, (name, model, floor, p) in enumerate(aps):
            ap = self.node(
                name,
                "ap",
                vendor="Ubiquiti",
                model=model,
                os="UniFi",
                version="7.0.95" if model != "U7-Pro" else "8.1.21",
                mac=mac("ubiquiti", name),
                ip=f"10.10.10.{101 + k}",
                vlan=10,
                hostname=name,
                api="unifi",
                lldp=True,
                dhcp=True,
                static_lease=True,
                boot=1787200000 + k * 600,
            )
            bands = ("2.4", "5", "6") if model in ("U7-Pro", "U6-Enterprise") else ("2.4", "5")
            ap.add(
                Port(
                    "eth0",
                    speed=2500 if model in ("U7-Pro", "U6-Enterprise") else 1000,
                    mac=ap.mac,
                    untagged=10,
                    tagged=ap_vlans,
                    connector="rj45",
                )
            )
            for r, band in enumerate(bands):
                ap.add(
                    Port(
                        f"wifi{r}",
                        "wifi",
                        {"2.4": 574, "5": 2402, "6": 5765}[band],
                        band=band,
                        tagged=ap_vlans,
                        mac=offset_mac(ap.mac, r + 1),
                    )
                )
            sname, sp = swport(floor, p)
            port = d.nodes[sname].ports[sp]
            port.untagged, port.tagged, port.descr = 10, ap_vlans, name
            self.wire(sname, sp, name, "eth0")
        # ap-f2-02's cable is bad: it negotiates 100 Mbps.
        d.nodes["ap-f2-02"].ports["eth0"].speed = 100
        d.nodes["sw-f2-01"].ports["46"].speed = 100
        # Outdoor mesh AP on the patio, meshed to the cafeteria AP.
        patio = self.node(
            "ap-patio-01",
            "ap",
            vendor="Ubiquiti",
            model="U6-Mesh",
            os="UniFi",
            version="7.0.95",
            mac=mac("ubiquiti", "ap-patio-01"),
            ip="10.10.10.109",
            vlan=10,
            hostname="ap-patio-01",
            api="unifi",
            lldp=True,
            dhcp=True,
            static_lease=True,
            boot=1787300000,
            extra={"mesh_uplink": "ap-cafe-01"},
        )
        patio.add(
            Port("eth0", speed=1000, mac=patio.mac, untagged=10, tagged=ap_vlans, connector="rj45", admin_up=True)
        )
        patio.add(
            Port("wifi0", "wifi", 574, band="2.4", tagged=ap_vlans, mac=offset_mac(patio.mac, 1)),
            Port("wifi1", "wifi", 2402, band="5", tagged=ap_vlans, mac=offset_mac(patio.mac, 2)),
        )
        patio.add(
            Port("mesh0", "wifi", 1200, tagged=(10, *ap_vlans), mac=offset_mac(patio.mac, 3), descr="mesh uplink")
        )
        cafe = d.nodes["ap-cafe-01"]
        cafe.add(
            Port("mesh1", "wifi", 1200, tagged=(10, *ap_vlans), mac=offset_mac(cafe.mac, 9), descr="mesh downlink")
        )
        self.wire("ap-cafe-01", "mesh1", "ap-patio-01", "mesh0", "wifi")

        # People.
        self.people_hq(swport)
        self.floor_devices(swport)

    def person(self, site: str, floor: int, dept: str) -> dict[str, Any]:
        rng = self.rng
        while True:
            first, last = rng.choice(FIRST), rng.choice(LAST)
            key = _ascii(f"{first}.{last}").lower()
            if key not in self.d.people:
                break
        p = {
            "name": f"{first} {last}",
            "first": _ascii(first),
            "site": site,
            "floor": floor,
            "dept": dept,
            "key": key,
            "arrive": rng.randint(7 * 60 + 15, 9 * 60 + 30),
            "stay": rng.randint(8 * 60, 9 * 60 + 30),
            "off_days": [dd for dd in range(5) if rng.random() < 0.08],
            "lunch_cafe": rng.random() < 0.5,
        }
        self.d.people[key] = p
        return p

    def personal_devices(
        self, p: dict[str, Any], kind: str, ssid: str, ap: str, vlan: int, net: str, site: str = "hq"
    ) -> list[str]:
        """A person's laptop (when kind == "laptop") and phone, on Wi-Fi."""
        rng = self.rng
        out = []
        if kind == "laptop":
            self.asset += 1
            _, oui, model, os, pattern = _pick(rng, LAPTOPS)
            host = pattern.format(n=self.asset, first=p["first"])
            name = host
            n = self.endpoint(
                name,
                "laptop",
                oui,
                None,
                vlan,
                site=site,
                port="wlan0",
                speed=None,
                vendor=oui,
                model=model,
                os=os,
                hostname=host,
                dhcp=True,
                presence=f"office:{p['key']}",
                traffic="laptop",
                owner=p["key"],
            )
            n.vendor = {"dell": "Dell", "lenovo": "Lenovo", "apple": "Apple", "apple2": "Apple"}[oui]
            if oui.startswith("apple"):
                n.scan = {"mdns": ["_companion-link._tcp"], "ports": [22] if rng.random() < 0.3 else []}
            else:
                n.scan = {"netbios": host.upper(), "ports": [135, 139, 445]}
            self.wifi(name, ssid, ap)
            out.append(name)
        _, oui, model, os, pattern = _pick(rng, PHONES)
        host = pattern.format(first=p["first"])
        private = rng.random() < 0.3
        name = f"{host}" if host not in self.d.nodes else f"{host}-{p['key'].split('.')[1]}"
        n = self.endpoint(
            name,
            "mobile",
            oui if not private else None,
            None,
            vlan,
            site=site,
            port="wlan0",
            speed=None,
            model=model,
            os=os,
            hostname=name if not oui.startswith("apple") or rng.random() < 0.7 else "",
            dhcp=True,
            presence=f"office:{p['key']}",
            traffic="mobile",
            owner=p["key"],
        )
        n.vendor = {
            "apple": "Apple",
            "apple2": "Apple",
            "samsung": "Samsung",
            "motorola": "Motorola",
            "xiaomi": "Xiaomi",
        }[oui]
        if oui.startswith("samsung") and rng.random() < 0.5:
            n.hostname = model  # Android often sends its model code
        if oui.startswith("apple"):
            n.scan = {"ports": [62078]}
        n.extra["private_mac"] = private
        self.wifi(name, ssid, ap)
        out.append(name)
        return out

    def people_hq(self, swport: Any) -> None:
        d = self.d
        floor_aps = {1: ["ap-f1-01", "ap-f1-02"], 2: ["ap-f2-01", "ap-f2-02"], 3: ["ap-f3-01", "ap-f3-02"]}
        next_port = {1: 1, 2: 1, 3: 1}
        for floor, dept, count, desk, deskphone in HQ_DEPARTMENTS:
            for k in range(count):
                p = self.person("hq", floor, dept)
                ap = floor_aps[floor][k % 2]
                self.personal_devices(p, "laptop" if desk == "laptop" else "", "Acme", ap, 32, "")
                port_no = next_port[floor]
                next_port[floor] += 1
                sname, sp = swport(floor, port_no)
                swp = d.nodes[sname].ports[sp]
                if deskphone:
                    phone = self.node(
                        f"SEP{mac('yealink', p['key']).replace(':', '').upper()}",
                        "phone",
                        vendor="Yealink",
                        model="SIP-T54W" if dept != "Diretoria" else "SIP-T58W",
                        os="Yealink",
                        version="96.86.0.70",
                        mac=mac("yealink", p["key"]),
                        vlan=40,
                        dhcp=True,
                        presence="always",
                        traffic="voip",
                        lldp=True,
                        owner=p["key"],
                    )
                    phone.hostname = f"yealink-{phone.mac[-8:].replace(':', '')}"
                    phone.add(
                        Port("LAN", speed=1000, mac=phone.mac, untagged=30, tagged=(40,)),
                        Port("PC", speed=1000, untagged=30),
                    )
                    phone.scan = {"ports": [80, 443, 5060], "titles": {443: "Yealink SIP-T54W Phone"}}
                    swp.untagged, swp.tagged = 30, (40,)
                    self.wire(sname, sp, phone.name, "LAN")
                    attach = (phone.name, "PC")
                else:
                    attach = (sname, sp)
                if desk == "desktop":
                    self.asset += 1
                    host = f"ACME-PC-{self.asset:04d}"
                    if dept == "Engenharia" and k >= 7:
                        n = self.endpoint(
                            f"ws-eng-{k:02d}",
                            "desktop",
                            "intel",
                            None,
                            30,
                            vendor="Dell",
                            model="Precision 3680",
                            os="Ubuntu 24.04.3 LTS",
                            dhcp=True,
                            presence=f"desktop:{p['key']}",
                            traffic="desktop",
                            owner=p["key"],
                        )
                        n.scan = {"ports": [22], "ssh": "SSH-2.0-OpenSSH_9.6p1 Ubuntu-3ubuntu13.13"}
                    else:
                        n = self.endpoint(
                            host,
                            "desktop",
                            "dell2",
                            None,
                            30,
                            vendor="Dell",
                            model="OptiPlex 7020 Micro",
                            os="Windows 11 Pro 24H2",
                            hostname=host,
                            dhcp=True,
                            presence=f"desktop:{p['key']}",
                            traffic="desktop",
                            owner=p["key"],
                        )
                        n.scan = {
                            "netbios": host,
                            "ports": [135, 139, 445, 3389] if dept == "Engenharia" else [135, 139, 445],
                        }
                    self.wire(attach[0], attach[1], n.name, "eth0")
        # Reception: digital signage and the Sonos speaker; the IoT gear is generated in floor_devices.

    def floor_devices(self, swport: Any) -> None:
        d = self.d
        rng = self.rng
        # Printers (reservations in PRINT VLAN), cameras, badge readers, HVAC, meeting rooms.
        printers = [
            ("prn-f1-01", "hp", "HP", "LaserJet Enterprise M609dn", 1, 30, "10.10.70.11", "HP LaserJet M609"),
            ("prn-f1-rec", "brother", "Brother", "MFC-L8900CDW", 1, 31, "10.10.70.12", "Brother MFC-L8900CDW series"),
            (
                "prn-f2-01",
                "hp",
                "HP",
                "Color LaserJet Pro MFP M480f",
                2,
                30,
                "10.10.70.13",
                "HP Color LaserJet MFP M480f",
            ),
            ("prn-f3-01", "brother", "Brother", "HL-L6415DW", 3, 30, "10.10.30.150", "Brother HL-L6415DW series"),
        ]
        for name, oui, vendor, model, floor, p, ip, title in printers:
            vlan = 70 if ip.startswith("10.10.70.") else 30
            n = self.endpoint(
                name,
                "printer",
                oui,
                ip,
                vlan,
                vendor=vendor,
                model=model,
                dhcp=vlan == 70,
                static_lease=vlan == 70,
                traffic="printer",
            )
            n.scan = {
                "ports": [80, 443, 515, 631, 9100],
                "titles": {443: title, 80: title},
                "mdns": ["_ipp._tcp", "_pdl-datastream._tcp", "_printer._tcp"],
                "ssdp": {"manufacturer": vendor, "model": model, "type": "Printer"},
            }
            sname, sp = swport(floor, p)
            d.nodes[sname].ports[sp].untagged = vlan
            self.wire(sname, sp, name, "eth0")
        # prn-f3-01 was set up by hand with a static address inside the USERS DHCP pool: duplicate IP.
        d.nodes["prn-f3-01"].extra["static_in_pool"] = True

        cam = 0
        for floor in (1, 2, 3):
            for k in range(4):
                cam += 1
                name = f"cam-f{floor}-{k + 1:02d}"
                n = self.endpoint(
                    name,
                    "camera",
                    "hikvision",
                    f"10.10.60.{100 + cam}",
                    60,
                    vendor="Hikvision",
                    model="DS-2CD2143G2-IU" if k < 3 else "DS-2CD2387G2-LU",
                    version="V5.7.23 build 240925",
                    traffic="camera",
                    speed=100,
                )
                n.scan = {"ports": [80, 443, 554, 8000], "titles": {80: "Hikvision", 443: "Hikvision"}}
                sname, sp = swport(floor, 37 + k)
                d.nodes[sname].ports[sp].untagged = 60
                self.wire(sname, sp, name, "eth0")
        # Badge readers (wired, IOT) and HVAC controllers.
        for floor in (1, 2, 3):
            name = f"badge-f{floor}"
            n = self.endpoint(
                name,
                "iot",
                "hid",
                None,
                50,
                vendor="HID Global",
                model="iCLASS SE RK40",
                dhcp=True,
                static_lease=True,
                traffic="iot",
                speed=100,
            )
            n.scan = {"ports": [443], "titles": {443: "HID Reader"}}
            sname, sp = swport(floor, 41)
            d.nodes[sname].ports[sp].untagged = 50
            self.wire(sname, sp, name, "eth0")
            name = f"hvac-f{floor}"
            n = self.endpoint(
                name,
                "iot",
                "daikin",
                None,
                50,
                vendor="Daikin",
                model="DCM601A51 iTM",
                dhcp=True,
                static_lease=True,
                traffic="iot",
                speed=100,
            )
            n.scan = {"ports": [80], "titles": {80: "intelligent Touch Manager"}}
            sname, sp = swport(floor, 42)
            d.nodes[sname].ports[sp].untagged = 50
            self.wire(sname, sp, name, "eth0")
        # Wi-Fi IoT on "Acme-IoT": Shelly relays, ESP32 sensors, Tuya plugs, an air purifier.
        iot = [
            ("shelly-luz-f1", "shelly", "Shelly", "Shelly Pro 4PM", "ap-f1-01"),
            ("shelly-luz-f2", "shelly", "Shelly", "Shelly Pro 4PM", "ap-f2-01"),
            ("shelly-luz-f3", "shelly", "Shelly", "Shelly Pro 4PM", "ap-f3-01"),
            ("shelly-rack", "shelly", "Shelly", "Shelly Plus 1PM", "ap-f1-02"),
        ]
        iot += [
            (f"esp-sensor-{k:02d}", "espressif", "Espressif", "ESP32-C3 (ESPHome)", f"ap-f{1 + k % 3}-0{1 + k % 2}")
            for k in range(1, 7)
        ]
        iot += [(f"tuya-plug-{k:02d}", "tuya", "Tuya", "Smart Plug 16A", "ap-cafe-01") for k in range(1, 5)]
        iot += [("purifier-dir", "xiaomi", "Xiaomi", "Smart Air Purifier 4 Pro", "ap-f1-01")]
        for name, oui, vendor, model, ap in iot:
            n = self.endpoint(
                name,
                "iot",
                oui,
                None,
                50,
                port="wlan0",
                speed=None,
                vendor=vendor,
                model=model,
                dhcp=True,
                traffic="iot",
            )
            if oui == "shelly":
                n.scan = {"ports": [80], "titles": {80: "Shelly"}, "mdns": ["_shelly._tcp", "_http._tcp"]}
            elif oui == "espressif":
                n.scan = {"ports": [80, 6053], "mdns": ["_esphomelib._tcp"]}
            self.wifi(name, "Acme-IoT", ap)
        sonos = self.endpoint(
            "sonos-recepcao",
            "media",
            "sonos",
            None,
            50,
            port="wlan0",
            speed=None,
            vendor="Sonos",
            model="Era 100",
            dhcp=True,
            traffic="iot",
        )
        sonos.scan = {
            "ports": [1400, 1443],
            "mdns": ["_sonos._tcp", "_spotify-connect._tcp", "_airplay._tcp"],
            "ssdp": {"manufacturer": "Sonos, Inc.", "model": "Era 100", "type": "ZonePlayer"},
        }
        self.wifi("sonos-recepcao", "Acme-IoT", "ap-f1-01")
        signage = self.endpoint(
            "signage-lobby",
            "iot",
            "raspberry",
            None,
            50,
            vendor="Raspberry Pi",
            model="Raspberry Pi 5 Model B",
            os="Raspberry Pi OS (bookworm)",
            dhcp=True,
            traffic="iot",
        )
        signage.scan = {
            "ports": [22, 80],
            "ssh": "SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u7",
            "titles": {80: "Yodeck Player"},
        }
        sname, sp = swport(1, 43)
        d.nodes[sname].ports[sp].untagged = 50
        self.wire(sname, sp, "signage-lobby", "eth0")

        # Meeting rooms: TV + Chromecast (IOT), conference phone (VOICE).
        rooms = [
            ("santos", 2, 33, "samsung", "Samsung", "QM55B", "ap-f2-01"),
            ("campinas", 2, 34, "lg", "LG", "55UR8750PSA", "ap-f2-02"),
            ("paulista", 3, 33, "samsung", "Samsung", "QM75B", "ap-mr-01"),
            ("ibirapuera", 3, 34, "lg", "LG", "65UR8750PSA", "ap-mr-01"),
        ]
        for room, floor, p, oui, vendor, model, ap in rooms:
            tv = self.endpoint(
                f"tv-{room}",
                "tv",
                oui,
                None,
                50,
                vendor=vendor,
                model=model,
                dhcp=True,
                traffic="tv",
                os="Tizen 7.0" if oui == "samsung" else "webOS 23",
            )
            tv.scan = {
                "ports": [8001, 8002, 9197] if oui == "samsung" else [3000, 3001],
                "ssdp": {
                    "manufacturer": "Samsung Electronics" if oui == "samsung" else "LG Electronics",
                    "model": model,
                    "type": "MediaRenderer",
                },
                "mdns": ["_airplay._tcp"] if oui == "samsung" else ["_airplay._tcp", "_hap._tcp"],
            }
            sname, sp = swport(floor, p)
            d.nodes[sname].ports[sp].untagged = 50
            self.wire(sname, sp, tv.name, "eth0")
            cc = self.endpoint(
                f"chromecast-{room}",
                "media",
                "google",
                None,
                50,
                port="wlan0",
                speed=None,
                vendor="Google",
                model="Chromecast with Google TV (4K)",
                dhcp=True,
                traffic="tv",
                os="Google TV 14",
            )
            cc.scan = {
                "ports": [8008, 8009, 8443],
                "mdns": ["_googlecast._tcp"],
                "ssdp": {"manufacturer": "Google Inc.", "model": "Eureka Dongle", "type": "dial"},
            }
            self.wifi(cc.name, "Acme-IoT", ap)
            cp = self.node(
                f"cp-{room}",
                "phone",
                vendor="Yealink",
                model="CP965",
                os="Yealink",
                version="143.86.0.30",
                mac=mac("yealink", f"cp-{room}"),
                vlan=40,
                dhcp=True,
                traffic="voip",
                lldp=True,
            )
            cp.hostname = f"yealink-cp965-{room}"
            cp.add(Port("LAN", speed=1000, mac=cp.mac, untagged=40))
            sname, sp = swport(floor, p + 2)
            d.nodes[sname].ports[sp].untagged = 40
            self.wire(sname, sp, cp.name, "LAN")

        # A desk switch nobody manages under the sales team's table (sw-f2-01 port 30 is a printer;
        # use 28): a TP-Link SG105 with three devices behind it.
        sname, sp = swport(2, 28)
        desk = self.node(
            "desk-switch-vendas",
            "switch",
            vendor="TP-Link",
            model="TL-SG105",
            mac=mac("tplink", "sg105"),
            presence="always",
        )
        desk.extra["unmanaged"] = True
        for k in range(1, 6):
            desk.add(Port(f"{k}", speed=1000, untagged=30))
        self.wire(sname, sp, desk.name, "1")
        for k, (n, oui, model) in enumerate(
            (
                ("ACME-NB-SALA-1", "dell", "Latitude 3550"),
                ("ACME-NB-SALA-2", "lenovo", "ThinkPad E14"),
                ("impressora-vendas", "hp", "LaserJet Pro M404dn"),
            )
        ):
            e = self.endpoint(
                n,
                "printer" if "impressora" in n else "laptop",
                oui,
                None,
                30,
                vendor={"dell": "Dell", "lenovo": "Lenovo", "hp": "HP"}[oui],
                model=model,
                dhcp=True,
                presence="office:" + list(self.d.people)[20 + k] if "NB" in n else "always",
                traffic="laptop" if "NB" in n else "printer",
            )
            if "impressora" in n:
                e.scan = {"ports": [80, 443, 9100], "titles": {80: "HP LaserJet Pro M404dn"}}
            self.wire(desk.name, str(k + 2), n, "eth0")

        # Visitors on the guest Wi-Fi (random MACs), present on some weekday hours.
        for k in range(14):
            name = f"guest-{k:02d}"
            n = self.endpoint(
                name,
                "mobile",
                None,
                None,
                80,
                port="wlan0",
                speed=None,
                dhcp=True,
                presence=f"guest:{k}",
                traffic="guest",
                hostname="" if k % 3 else f"Galaxy-de-Visitante-{k}",
            )
            n.extra["private_mac"] = True
            self.wifi(name, "Acme-Visitantes", ["ap-f1-01", "ap-f2-01", "ap-mr-01", "ap-cafe-01"][k % 4])
        # Two weak clients on the patio AP.
        for k, oui in enumerate(("samsung", "apple")):
            name = f"patio-{['tablet', 'iphone'][k]}"
            self.endpoint(
                name,
                "tablet" if k == 0 else "mobile",
                oui,
                None,
                32,
                port="wlan0",
                speed=None,
                dhcp=True,
                vendor="Samsung" if k == 0 else "Apple",
                model="SM-X210" if k == 0 else "iPhone15,4",
                presence="always" if k == 0 else "office:" + list(self.d.people)[3],
                traffic="mobile",
            )
            self.wifi(name, "Acme", "ap-patio-01")
        # The security guard's phone, there at night and on weekends.
        g = self.endpoint(
            "Galaxy-A15-Portaria",
            "mobile",
            "samsung",
            None,
            32,
            port="wlan0",
            speed=None,
            dhcp=True,
            vendor="Samsung",
            model="SM-A155M",
            presence="shift",
            traffic="mobile",
        )
        g.hostname = "SM-A155M"
        self.wifi(g.name, "Acme", "ap-f1-01")
        _ = rng

    # -- Warehouse: Horaco switch + Mercusys Halo (AP mode) --
    def warehouse(self) -> None:
        d = self.d
        sw = self.node(
            "sw-wh-01",
            "switch",
            vendor="Horaco",
            model="HC-SWTGW218AS",
            os="",
            version="V200.1.7",
            mac=mac("hongrui", "sw-wh-01"),
            ip="10.10.10.8",
            vlan=10,
            hostname="sw-wh-01",
            api="horaco",
            boot=1787800000,
        )
        for p in range(1, 9):
            sw.add(Port(f"Port {p}", speed=2500, untagged=34, connector="rj45", index=p))
        for p in (9, 10):
            sw.add(Port(f"Port {p}", "sfp", 10000, untagged=1, connector="sfp", index=p))
        sw.ports["Port 9"].untagged, sw.ports["Port 9"].tagged = 10, (30, 34, 50, 60, 70)
        self.wire("core-sw01", "Te1/1/7", "sw-wh-01", "Port 9", "fiber")
        # Halo H80X units: main on Port 1, a satellite on Port 2 (wired backhaul), one meshed over Wi-Fi.
        for k, (name, port) in enumerate((("halo-wh-01", "Port 1"), ("halo-wh-02", "Port 2"), ("halo-wh-03", None))):
            h = self.node(
                name,
                "ap",
                vendor="Mercusys",
                model="Halo H80X",
                os="",
                version="1.2.0 Build 20240913 Rel. 47562",
                mac=mac("mercusys", name),
                ip=f"10.10.34.{10 + k}",
                vlan=34,
                hostname=name,
                dhcp=True,
                static_lease=True,
                api="mercusys",
                boot=1787900000 + k * 100,
                extra={"main": k == 0, "nickname": ["Galpão Doca", "Galpão Estoque", "Galpão Fundos"][k]},
            )
            h.add(
                Port("eth0", speed=1000, mac=h.mac, untagged=34, connector="rj45"),
                Port("eth1", speed=1000, mac=offset_mac(h.mac, 1), untagged=34, connector="rj45"),
                Port("wifi0", "wifi", 574, band="2.4", untagged=34, mac=offset_mac(h.mac, 2)),
                Port("wifi1", "wifi", 2402, band="5", untagged=34, mac=offset_mac(h.mac, 3)),
                Port(
                    "mesh",
                    "wifi",
                    866,
                    untagged=34,
                    mac=offset_mac(h.mac, 4),
                    descr="mesh downlink" if k == 1 else "mesh uplink",
                ),
            )
            if port:
                self.wire("sw-wh-01", port, name, "eth0")
        self.wire("halo-wh-02", "mesh", "halo-wh-03", "mesh", "wifi")
        for port, (name, oui, vendor, model, vlan, kind) in zip(  # noqa: B905
            ("Port 3", "Port 4", "Port 5", "Port 6"),
            (
                ("prn-wh-zebra", "honeywell", "Honeywell", "PM45 Label Printer", 70, "printer"),
                ("pc-galpao", "dell2", "Dell", "OptiPlex 3000", 30, "desktop"),
                ("cam-wh-01", "intelbras", "Intelbras", "VIP 3230 B", 60, "camera"),
                ("badge-wh", "hid", "HID Global", "iCLASS SE RK40", 50, "iot"),
            ),
        ):
            n = self.endpoint(
                name,
                kind,
                oui,
                "10.10.60.140" if kind == "camera" else None,
                vlan,
                vendor=vendor,
                model=model,
                dhcp=kind != "camera",
                static_lease=kind in ("printer", "iot"),
                traffic={"printer": "printer", "desktop": "desktop", "camera": "camera", "iot": "iot"}[kind],
                presence="desktop:wh" if kind == "desktop" else "always",
            )
            if kind == "desktop":
                n.hostname = "ACME-PC-GALPAO"
                n.os = "Windows 11 Pro 24H2"
                n.scan = {"netbios": "ACME-PC-GALPAO", "ports": [135, 139, 445]}
            sw.ports[port].untagged = vlan
            self.wire("sw-wh-01", port, name, "eth0")
        for k in range(WAREHOUSE_STAFF):
            p = self.person("hq", 0, "Logística")
            ap = ["halo-wh-01", "halo-wh-02", "halo-wh-03"][k % 3]
            name = f"tc52-{k + 1:02d}"
            n = self.endpoint(
                name,
                "scanner",
                "motorola",
                None,
                34,
                port="wlan0",
                speed=None,
                vendor="Zebra",
                model="TC52",
                os="Android 13",
                dhcp=True,
                presence=f"office:{p['key']}",
                traffic="iot",
                owner=p["key"],
            )
            n.hostname = f"TC52-{k + 1:02d}"
            self.wifi(name, "Acme-Galpao", ap)
            self.personal_devices(p, "", "Acme-Galpao", ap, 34, "")
        _ = d

    # -- Branch office (Campinas): MikroTik router + Omada switch/APs, WireGuard to HQ --
    def branch(self) -> None:
        gw = self.node(
            "br-gw01",
            "router",
            site="branch",
            vendor="MikroTik",
            model="RB5009UPr+S+",
            os="RouterOS",
            version="7.16.2",
            serial="HG109WS8R5N",
            mac=mac("mikrotik", "br-gw01"),
            ip="10.20.10.1",
            vlan=10,
            hostname="br-gw01",
            api="mikrotik-br-gw01",
            lldp=True,
            boot=1787650000,
        )
        gw.add(
            Port(
                "ether1",
                speed=1000,
                mac=gw.mac,
                untagged=WAN_VLAN,
                connector="rj45",
                ips=("198.51.100.20/29",),
                descr="WAN Vivo Fibra",
                role="wan",
            )
        )
        for p in range(2, 9):
            gw.add(
                Port(f"ether{p}", speed=1000, mac=offset_mac(gw.mac, p - 1), untagged=30, connector="rj45", poe=True)
            )
        gw.add(
            Port(
                "sfp-sfpplus1",
                "sfp",
                10000,
                mac=offset_mac(gw.mac, 8),
                untagged=10,
                tagged=(30, 32, 40, 80),
                connector="sfp",
                descr="br-sw01",
            )
        )
        gw.add(
            Port(
                "bridge",
                "bridge",
                None,
                mac=gw.mac,
                members=(*(f"ether{p}" for p in range(2, 9)), "sfp-sfpplus1"),
            )
        )
        for vid, vname, subnet, *_ in BRANCH_VLANS:
            bits = subnet.split("/")[1]
            gw.add(
                Port(
                    f"vlan{vid}",
                    "vlan",
                    None,
                    vlan=vid,
                    parent="bridge",
                    untagged=vid,
                    mac=gw.mac,
                    ips=(subnet.rsplit(".", 1)[0] + f".1/{bits}",),
                    descr=vname,
                )
            )
        gw.add(Port("wg-hq", "wg", None, ips=("10.255.0.2/30",), descr="WireGuard to HQ"))
        ont = self.endpoint(
            "br-ont",
            "modem",
            "huawei",
            "198.51.100.17",
            WAN_VLAN,
            site="branch",
            vendor="Huawei",
            model="EG8145X6",
            hostname="br-ont",
        )
        ont.extra["isp"] = "Vivo Fibra"
        self.wire("br-gw01", "ether1", "br-ont", "eth0")

        sw = self.node(
            "br-sw01",
            "switch",
            site="branch",
            vendor="TP-Link",
            model="SG3428MP",
            os="Omada",
            version="2.0.12",
            mac=mac("tplink", "br-sw01"),
            ip="10.20.10.10",
            vlan=10,
            hostname="br-sw01",
            api="omada",
            lldp=True,
            boot=1787660000,
            serial="2235210001234",
        )
        for p in range(1, 25):
            sw.add(Port(f"Port {p}", speed=1000, untagged=30, poe=True, connector="rj45", index=p))
        for p in range(25, 29):
            sw.add(Port(f"Port {p}", "sfp", 10000 if p >= 27 else 1000, untagged=1, connector="sfp", index=p))
        sw.ports["Port 28"].untagged, sw.ports["Port 28"].tagged = 10, (30, 32, 40, 80)
        self.wire("br-gw01", "sfp-sfpplus1", "br-sw01", "Port 28", "fiber")
        oc = self.endpoint(
            "br-oc200",
            "controller",
            "tplink",
            "10.20.10.5",
            10,
            site="branch",
            vendor="TP-Link",
            model="OC200",
            version="2.18.2",
            hostname="br-oc200",
            api="omada",
            speed=100,
        )
        oc.scan = {"ports": [80, 443, 29810, 29811], "titles": {443: "Omada Controller"}}
        sw.ports["Port 1"].untagged = 10
        self.wire("br-sw01", "Port 1", "br-oc200", "eth0")
        for k, name in enumerate(("br-ap01", "br-ap02")):
            ap = self.node(
                name,
                "ap",
                site="branch",
                vendor="TP-Link",
                model="EAP670",
                os="Omada",
                version="1.1.3",
                mac=mac("tplink", name),
                ip=f"10.20.10.{11 + k}",
                vlan=10,
                hostname=name,
                api="omada",
                lldp=True,
                boot=1787670000 + k * 300,
            )
            ap.add(
                Port("eth0", speed=2500, mac=ap.mac, untagged=10, tagged=(32, 80), connector="rj45"),
                Port("wifi0", "wifi", 574, band="2.4", tagged=(32, 80), mac=offset_mac(ap.mac, 1)),
                Port("wifi1", "wifi", 4804, band="5", tagged=(32, 80), mac=offset_mac(ap.mac, 2)),
            )
            p = f"Port {23 + k}"
            sw.ports[p].untagged, sw.ports[p].tagged, sw.ports[p].speed = 10, (32, 80), 1000
            self.wire("br-sw01", p, name, "eth0")
        next_port = 2
        for k in range(BRANCH_STAFF):
            p = self.person("branch", 1, "Filial")
            ap = ("br-ap01", "br-ap02")[k % 2]
            self.personal_devices(p, "laptop", "Acme-Filial", ap, 32, "", site="branch")
            if k < 8:
                phone = self.node(
                    f"SEP{mac('yealink', p['key']).replace(':', '').upper()}",
                    "phone",
                    site="branch",
                    vendor="Yealink",
                    model="SIP-T46U",
                    os="Yealink",
                    version="108.86.0.75",
                    mac=mac("yealink", p["key"]),
                    vlan=40,
                    dhcp=True,
                    traffic="voip",
                    lldp=True,
                    owner=p["key"],
                )
                phone.hostname = f"yealink-{phone.mac[-8:].replace(':', '')}"
                phone.add(Port("LAN", speed=1000, mac=phone.mac, untagged=40), Port("PC", speed=1000, untagged=30))
                port = f"Port {next_port}"
                sw.ports[port].tagged = ()
                sw.ports[port].untagged = 40
                self.wire("br-sw01", port, phone.name, "LAN")
                next_port += 1
        prn = self.endpoint(
            "br-prn01",
            "printer",
            "brother",
            None,
            30,
            site="branch",
            vendor="Brother",
            model="MFC-L5915DW",
            dhcp=True,
            static_lease=True,
            traffic="printer",
        )
        prn.scan = {"ports": [80, 443, 631, 9100], "titles": {443: "Brother MFC-L5915DW series"}}
        self.wire("br-sw01", "Port 20", "br-prn01", "eth0")
        tv = self.endpoint(
            "br-tv-reuniao",
            "tv",
            "samsung",
            None,
            30,
            site="branch",
            vendor="Samsung",
            model="QM50B",
            dhcp=True,
            traffic="tv",
        )
        self.wire("br-sw01", "Port 21", tv.name, "eth0")
        for k in range(3):
            name = f"br-guest-{k}"
            n = self.endpoint(
                name,
                "mobile",
                None,
                None,
                80,
                site="branch",
                port="wlan0",
                speed=None,
                dhcp=True,
                presence=f"guest:{20 + k}",
                traffic="guest",
            )
            n.extra["private_mac"] = True
            self.wifi(name, "Acme-Filial-Visitantes", "br-ap01")

    # -- Store (Santos): one OpenWrt router with Wi-Fi, IPsec to HQ --
    def store(self) -> None:
        rt = self.node(
            "store-rt01",
            "router",
            site="store",
            vendor="GL.iNet",
            model="GL-MT6000",
            os="OpenWrt",
            version="24.10.2",
            mac=mac("gl", "store-rt01"),
            ip="10.30.1.1",
            vlan=STORE_LAN,
            hostname="store-rt01",
            api="openwrt",
            boot=1788000000,
        )
        rt.add(
            Port(
                "eth1",
                speed=2500,
                mac=offset_mac(rt.mac, 1),
                untagged=WAN_VLAN,
                ips=("100.64.23.45/22",),
                role="wan",
                descr="wan",
            )
        )
        for k in range(1, 6):
            rt.add(Port(f"lan{k}", speed=1000, mac=rt.mac, untagged=STORE_LAN, connector="rj45"))
        rt.add(
            Port(
                "br-lan",
                "bridge",
                None,
                mac=rt.mac,
                members=(*(f"lan{k}" for k in range(1, 6)), "phy0-ap0", "phy1-ap0"),
                untagged=STORE_LAN,
                ips=("10.30.1.1/24",),
            ),
            Port(
                "br-guest",
                "bridge",
                None,
                mac=offset_mac(rt.mac, 9),
                members=("phy0-ap1", "phy1-ap1"),
                untagged=STORE_GUEST,
                ips=("10.30.80.1/24",),
            ),
            Port("phy0-ap0", "wifi", 574, band="2.4", untagged=STORE_LAN, mac=offset_mac(rt.mac, 2)),
            Port("phy1-ap0", "wifi", 4804, band="5", untagged=STORE_LAN, mac=offset_mac(rt.mac, 3)),
            Port("phy0-ap1", "wifi", 574, band="2.4", untagged=STORE_GUEST, mac=offset_mac(rt.mac, 4)),
            Port("phy1-ap1", "wifi", 4804, band="5", untagged=STORE_GUEST, mac=offset_mac(rt.mac, 5)),
        )
        modem = self.endpoint(
            "store-modem",
            "modem",
            "huawei",
            "100.64.20.1",
            WAN_VLAN,
            site="store",
            vendor="Huawei",
            model="EchoLife HG8145V5",
            hostname="store-modem",
        )
        modem.extra["isp"] = "Claro"
        self.wire("store-rt01", "eth1", "store-modem", "eth0")
        devs = [
            ("pos-01", "gertec", "Gertec", "GPOS720", "pos", "lan1"),
            ("pos-02", "ingenico", "Ingenico", "Move/5000", "pos", "lan2"),
            ("pc-loja", "lenovo", "Lenovo", "ThinkCentre M70q", "desktop", "lan3"),
            ("cam-loja-01", "intelbras", "Intelbras", "VIP 1230 B", "camera", "lan4"),
            ("prn-loja", "brother", "Brother", "QL-820NWB", "printer", "lan5"),
        ]
        for name, oui, vendor, model, kind, port in devs:
            n = self.endpoint(
                name,
                kind,
                oui,
                None,
                STORE_LAN,
                site="store",
                vendor=vendor,
                model=model,
                dhcp=True,
                static_lease=kind in ("pos", "printer", "camera"),
                traffic={"pos": "pos", "desktop": "desktop", "camera": "camera-cloud", "printer": "printer"}[kind],
                presence="store" if kind in ("pos", "desktop") else "always",
            )
            if kind == "desktop":
                n.hostname, n.os = "LOJA-SANTOS-PC", "Windows 11 Pro 24H2"
            self.wire("store-rt01", port, name, "eth0")
        for k in range(STORE_STAFF):
            p = self.person("store", 0, "Loja")
            p["arrive"], p["stay"] = 8 * 60 + 30 + k * 10, 11 * 60
            self.personal_devices(p, "", "Acme-Loja", "store-rt01", STORE_LAN, "", site="store")
        for k in range(5):
            name = f"cliente-loja-{k}"
            n = self.endpoint(
                name,
                "mobile",
                None,
                None,
                STORE_GUEST,
                site="store",
                port="wlan0",
                speed=None,
                dhcp=True,
                presence=f"guest:{30 + k}",
                traffic="guest",
            )
            n.extra["private_mac"] = True
            self.wifi(name, "Loja-Clientes", "store-rt01")

    # -- Lab behind the pfSense VM --
    def lab(self) -> None:
        kali = self.endpoint(
            "lab-kali",
            "vm",
            "proxmox",
            None,
            LAB_LAN,
            site="lab",
            vendor="Proxmox",
            os="Kali Linux 2025.3",
            dhcp=True,
            traffic="lab",
            port="net0",
            speed=10000,
        )
        kali.host, kali.vmid, kali.cores, kali.mem_mb, kali.disk_gb = "pve02", 131, 4, 8192, 80
        kali.kind = "vm"
        kali.extra["ostype"] = "l26"
        pve2 = self.d.nodes["pve02"]
        pve2.add(Port("tap131i0", "tap", 10000, untagged=LAB_LAN, parent="vmbr2", descr="lab-kali"))
        self.wire("pve02", "tap131i0", "lab-kali", "net0", "virtual")


def build() -> Design:
    return Builder().build()


# ---------------------------------------------------------------------------
# The emulated APIs: which address serves what (in the lab network namespace;
# tests serve them on 127.0.0.1 with random ports instead).

APIS: dict[str, dict[str, Any]] = {
    "opnsense-fw-hq-01": {"kind": "opnsense", "node": "fw-hq-01", "ip": "10.10.10.2", "port": 443, "tls": True},
    "opnsense-fw-hq-02": {"kind": "opnsense", "node": "fw-hq-02", "ip": "10.10.10.3", "port": 443, "tls": True},
    "proxmox": {
        "kind": "proxmox",
        "node": "pve01",
        "ip": "10.10.10.11",
        "port": 8006,
        "tls": True,
        "nodes": ["pve01", "pve02", "pve03"],
        "cluster": "acme-pve",
    },
    "unifi": {"kind": "unifi", "node": "unifi01", "ip": "10.10.10.20", "port": 8443, "tls": True, "site": "default"},
    "mikrotik-br-gw01": {"kind": "mikrotik", "node": "br-gw01", "ip": "10.20.10.1", "port": 443, "tls": True},
    "mikrotik-sw-stor-01": {"kind": "mikrotik", "node": "sw-stor-01", "ip": "10.10.10.6", "port": 443, "tls": True},
    "omada": {
        "kind": "omada",
        "node": "br-oc200",
        "ip": "10.20.10.5",
        "port": 443,
        "tls": True,
        "site": "Filial Campinas",
    },
    "openwrt": {"kind": "openwrt", "node": "store-rt01", "ip": "10.30.1.1", "port": 443, "tls": True},
    "pfsense": {"kind": "pfsense", "node": "lab-fw01", "ip": "10.10.120.10", "port": 443, "tls": True},
    "horaco": {"kind": "horaco", "node": "sw-wh-01", "ip": "10.10.10.8", "port": 80, "tls": False},
    "mercusys": {
        "kind": "mercusys",
        "node": "halo-wh-01",
        "ip": "10.10.34.10",
        "port": 443,
        "tls": True,
        "units": ["halo-wh-01", "halo-wh-02", "halo-wh-03"],
    },
    # SNMP agents (UDP 161).
    "snmp-core-sw01": {"kind": "snmp", "node": "core-sw01", "ip": "10.10.10.5", "port": 161, "v3": True},
    "snmp-sw-f1-01": {"kind": "snmp", "node": "sw-f1-01", "ip": "10.10.10.21", "port": 161},
    "snmp-sw-f2-01": {"kind": "snmp", "node": "sw-f2-01", "ip": "10.10.10.22", "port": 161},
    "snmp-sw-dmz-01": {"kind": "snmp", "node": "sw-dmz-01", "ip": "10.10.10.7", "port": 161},
    "snmp-sw-stor-01": {"kind": "snmp", "node": "sw-stor-01", "ip": "10.10.10.6", "port": 161},
    "snmp-nas01": {"kind": "snmp", "node": "nas01", "ip": "10.10.10.30", "port": 161},
    "snmp-bkp-nas01": {"kind": "snmp", "node": "bkp-nas01", "ip": "10.10.10.31", "port": 161},
    "snmp-ups-rack01": {"kind": "snmp", "node": "ups-rack01", "ip": "10.10.10.40", "port": 161, "community": "public"},
    "snmp-mon01": {"kind": "snmp", "node": "mon01", "ip": "10.10.20.40", "port": 161},
}
# Which emulated APIs live behind which tunnel (unreachable while it is down).
BEHIND = {"mikrotik-br-gw01": "wg-branch", "omada": "wg-branch", "openwrt": "ipsec-store"}
