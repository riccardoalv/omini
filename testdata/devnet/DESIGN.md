# Acme's network (devnet design)

This is the network the devnet emulates. It is the source of truth: `emulator/acme.py` builds exactly this
(nodes, ports, cables, VLANs, SSIDs, people), `emulator/world.py` makes it live, and the tests check the map Omini
draws against the "Expected map" section at the end. Change this file and `acme.py` together
(`tests/test_design.py` compares the tables below with the code).

Acme Indústria e Comércio Ltda. is a mid-size company: headquarters in São Paulo (3 office floors, a server
room and a warehouse annex, about 80 people), a branch office in Campinas (14 people) and a small store in Santos.
Local time is UTC-3 (no daylight saving). All names, addresses and serial numbers are fictitious; public
addresses come from the documentation ranges (RFC 5737) and CGNAT space.

## Sites

| Site | Where | Uplinks | Router | Reached from HQ through |
|---|---|---|---|---|
| HQ | São Paulo | Fiber 1 Gbps/500 Mbps (Vivo Empresas, PPPoE) + 5G backup (Claro, Teltonika RUTX50) | OPNsense HA pair (CARP) | — |
| Branch | Campinas | Fiber (Vivo, static /29) | MikroTik RB5009 | WireGuard (`wg0` ↔ `wg-hq`, 10.255.0.0/30) |
| Store | Santos | Cable/fiber with CGNAT (Claro) | OpenWrt (GL.iNet GL-MT6000) | IPsec (policy based, 10.30.0.0/16 ↔ 10.10.0.0/16) |
| Lab | HQ server room | HQ's LAB VLAN | pfSense CE VM on pve02 | Routed (double NAT) |

## Physical topology

```
                       Internet
              ┌───────────┴──────────────┐
        ont-fiber (Huawei HG8010H)   lte-gw01 (Teltonika RUTX50 5G)
              │ VLAN 900 WAN-FIBER        │ VLAN 901 WAN-LTE
              └────────────┬──────────────┘
   ┌───────────────────────┴──────────────────── core-sw01 ─────────────────────────────────────┐
   │ Cisco C9300-48UXM, stack of 2 (Tw1/0/x, Tw2/0/x 2.5G mGig · Te1/1/x, Te2/1/x 10G SFP+)       │
   │  Po11 ── fw-hq-01 lagg0 (ax0+ax1)   Po12 ── fw-hq-02 lagg0         (LACP, one leg per member)│
   │  Po21/22/23 ── sw-f1-01 / sw-f2-01 / sw-f3-01 (2×10G each)                                   │
   │  Po30 ── sw-stor-01 bond-core (2×10G)    Te1/1/7 ── sw-wh-01 Port 9 (OS2 fiber to warehouse)│
   │  Po41/42/43 ── pve01/02/03 bond0 (2×1G)  Tw1/0/10-12 iDRACs · Tw1/0/20 UPS · Tw2/0/22 NVR    │
   │  Tw1/0/1, Tw2/0/1 fw igc0 (WAN1) · Tw1/0/2 ONT · Tw1/0/3, Tw2/0/3 fw igc1 (WAN2) · Tw2/0/2 5G │
   │  Tw2/0/20 ── sw-dmz-01 me0 (out-of-band management)                                          │
   └──────────────────────────────────────────────────────────────────────────────────────────────┘
   fw-hq-01 igc2 / fw-hq-02 igc2 ── sw-dmz-01 (Juniper EX2300-24T) ── web01, web02, mx01
   fw-hq-01 igc3 ══ fw-hq-02 igc3   (pfsync / HA sync, direct cable, 10.255.255.0/30)

   sw-stor-01 (MikroTik CRS518, 25G) ── bond1/2/3 ── pve01/02/03 ens1f0np0+ens1f1np1 (2×25G DAC)
                                     ── sfp28-7 nas01 (10G) · sfp28-8 bkp-nas01 (10G)

   sw-f1-01, sw-f2-01 (Aruba 2930F-48G-PoE+), sw-f3-01 (UniFi USW-Pro-48-PoE): desks (Yealink phones with
   the PC behind them), printers, cameras, badge readers, HVAC, meeting-room TVs and conference phones,
   UniFi APs on PoE (ap-f2-02's cable is bad: 100 Mbps), ap-patio-01 meshed to ap-cafe-01 over Wi-Fi,
   a TP-Link desk switch nobody manages under the sales team's table (sw-f2-01 port 28).

   sw-wh-01 (Horaco HC-SWTGW218AS, warehouse) ── Port 1 halo-wh-01 · Port 2 halo-wh-02 ~~ halo-wh-03 (mesh)
                                                 Port 3 label printer · Port 4 PC · Port 5 camera · Port 6 badge

   Branch: br-ont ── br-gw01 ether1 · br-gw01 sfp-sfpplus1 ── br-sw01 Port 28 (Omada SG3428MP)
           br-sw01 Port 1 br-oc200 (Omada controller) · Port 23/24 br-ap01/02 (EAP670) · phones, printer, TV
   Store:  store-modem ── store-rt01 eth1 · lan1-5: POS ×2, PC, camera, receipt printer · Wi-Fi on the router
```

## VLANs and addressing

HQ: one /24 per VLAN in 10.10.0.0/16, third octet = VLAN id (WIFI-CORP and GUEST are /23). Gateways are CARP
VIPs `.1` (vhid = VLAN id, virtual MAC 00:00:5e:00:01:<vhid>); fw-hq-01 is `.2`, fw-hq-02 `.3`. DHCP is Kea on
both firewalls (HA hot-standby). DNS: the domain controllers (dc01/dc02) for `acme.local`, Unbound on the
firewalls for the rest.

| VLAN | Name | Subnet | Routed | DHCP pool | Use |
|---|---|---|---|---|---|
| 10 | MGMT | 10.10.10.0/24 | yes | 10.10.10.100–10.10.10.199 | Network gear, hypervisors, BMCs, UPS |
| 20 | SERVERS | 10.10.20.0/24 | yes | — | Servers and VMs |
| 30 | USERS | 10.10.30.0/24 | yes | 10.10.30.100–10.10.30.250 | Wired desktops |
| 32 | WIFI-CORP | 10.10.32.0/23 | yes | 10.10.32.20–10.10.33.250 | Wi-Fi "Acme" (802.1X) |
| 34 | WH-WIFI | 10.10.34.0/24 | yes | 10.10.34.100–10.10.34.200 | Warehouse Wi-Fi (Mercusys Halo) |
| 40 | VOICE | 10.10.40.0/24 | yes | 10.10.40.100–10.10.40.250 | IP phones, PBX |
| 50 | IOT | 10.10.50.0/24 | yes | 10.10.50.100–10.10.50.250 | Building automation, TVs, sensors |
| 60 | CAMERAS | 10.10.60.0/24 | yes | — | CCTV (no internet) |
| 70 | PRINT | 10.10.70.0/24 | yes | 10.10.70.100–10.10.70.150 | Printers, print server |
| 80 | GUEST | 10.10.80.0/23 | yes | 10.10.80.20–10.10.81.250 | Visitors (internet only) |
| 90 | DMZ | 10.10.90.0/24 | yes | — | Public servers (physically separate switch) |
| 100 | STORAGE | 10.10.100.0/24 | no | — | NFS/SMB storage (not routed) |
| 101 | CEPH-PUB | 10.10.101.0/24 | no | — | Ceph public network |
| 102 | CEPH-CLU | 10.10.102.0/24 | no | — | Ceph cluster (replication) network |
| 103 | COROSYNC | 10.10.103.0/24 | no | — | Proxmox cluster heartbeat |
| 110 | BACKUP | 10.10.110.0/24 | yes | — | Backup server and target |
| 120 | LAB | 10.10.120.0/24 | yes | — | Lab: the pfSense lab router's WAN |
| 900 | WAN-FIBER | — | no | — | ISP fiber ONT ↔ firewalls (L2 only) |
| 901 | WAN-LTE | — | no | — | 5G router ↔ firewalls (L2 only) |

Other sites:

| Site | VLAN | Subnet | Gateway | DHCP |
|---|---|---|---|---|
| Branch | 10 MGMT | 10.20.10.0/24 | 10.20.10.1 (br-gw01) | static |
| Branch | 30 USERS | 10.20.30.0/24 | 10.20.30.1 | 10.20.30.100–200 (RouterOS) |
| Branch | 32 WIFI-CORP | 10.20.32.0/24 | 10.20.32.1 | 10.20.32.20–250 |
| Branch | 40 VOICE | 10.20.40.0/24 | 10.20.40.1 | 10.20.40.100–200 |
| Branch | 80 GUEST | 10.20.80.0/24 | 10.20.80.1 | 10.20.80.20–250 |
| Store | LAN (br-lan) | 10.30.1.0/24 | 10.30.1.1 (store-rt01) | 10.30.1.100–200 (dnsmasq) |
| Store | GUEST (br-guest) | 10.30.80.0/24 | 10.30.80.1 | 10.30.80.20–250 |
| Lab | LAB-LAN (vmbr2 on pve02) | 192.168.200.0/24 | 192.168.200.1 (lab-fw01) | 192.168.200.100–199 |

Links between sites and the internet: HQ PPPoE 203.0.113.45 (only on the CARP master: the backup disconnects
dial-up interfaces), LTE 192.168.8.10/.11 behind the 5G router 192.168.8.1, branch WAN 198.51.100.20/29 (gateway
.17, the ONT), store WAN 100.64.23.45/22 (CGNAT, gateway 100.64.20.1), WireGuard 10.255.0.1–2/30, HA sync
10.255.255.1–2/30.

### Fixed addresses (HQ)

| Address | Host | | Address | Host |
|---|---|---|---|---|
| 10.10.10.1 | CARP VIP (gateway, DNS) | | 10.10.10.21–23 | sw-f1-01, sw-f2-01, sw-f3-01 |
| 10.10.10.2–3 | fw-hq-01, fw-hq-02 | | 10.10.10.30–31 | nas01, bkp-nas01 |
| 10.10.10.5 | core-sw01 (Vl10) | | 10.10.10.40 | ups-rack01 |
| 10.10.10.6 | sw-stor-01 | | 10.10.10.101–109 | UniFi APs (DHCP reservations) |
| 10.10.10.7 | sw-dmz-01 (me0) | | 10.10.10.250 | omini (LXC on pve01) |
| 10.10.10.8 | sw-wh-01 | | 10.10.20.10–11 | dc01, dc02 |
| 10.10.10.11–13 | pve01–03 (vmbr0) | | 10.10.20.20 / .25 | file01 / nas01 (SMB) |
| 10.10.10.14–16 | iDRACs | | 10.10.20.30–31 | erp-app01, erp-db01 |
| 10.10.10.20 | unifi01 (UniFi Network) | | 10.10.20.40 / .45 / .50 | mon01 / wsus01 / docker01 |
| 10.10.34.10–12 | halo-wh-01–03 | | 10.10.20.60 / .70 | netbox / rproxy-int |
| 10.10.40.10 | pbx01 (3CX) | | 10.10.50.10 | homeassistant |
| 10.10.60.10 | nvr01 | | 10.10.60.101–112, .140 | cameras |
| 10.10.70.5 | print01 | | 10.10.70.11–13 | printers |
| 10.10.90.11–12, .20 | web01, web02, mx01 | | 10.10.110.10 / .20 | pbs01 / bkp-nas01 |
| 10.10.120.10 | lab-fw01 (WAN) | | 10.10.30.150 | prn-f3-01 (static, inside the DHCP pool!) |

## Equipment

| Device | Model | Firmware | Address | Role | Emulated by |
|---|---|---|---|---|---|
| fw-hq-01, fw-hq-02 | Deciso DEC2752 | OPNsense 26.7.x | 10.10.10.2, .3 | HA firewalls (CARP), Kea DHCP, WireGuard, IPsec | OPNsense REST API (`opnsense-fw-hq-01/02`) |
| core-sw01 | Cisco C9300-48UXM ×2 (stack) | IOS-XE 17.12.4 | 10.10.10.5 | Core, L2 only (routing on the firewalls) | SNMPv3 agent |
| sw-f1-01, sw-f2-01 | Aruba 2930F-48G-PoE+-4SFP+ | WC.16.11.0024 | 10.10.10.21, .22 | Floors 1 and 2 | SNMPv2c agents |
| sw-f3-01 | UniFi USW-Pro-48-PoE | 7.1.26 | 10.10.10.23 | Floor 3 | UniFi Network controller |
| sw-stor-01 | MikroTik CRS518-16XS-2XQ | RouterOS 7.16.2 | 10.10.10.6 | Storage / Ceph switch (25G) | RouterOS REST API + SNMPv2c |
| sw-dmz-01 | Juniper EX2300-24T | Junos 23.4R2-S3 | 10.10.10.7 | DMZ (physically separate) | SNMPv2c agent |
| sw-wh-01 | Horaco HC-SWTGW218AS | V200.1.7 | 10.10.10.8 | Warehouse (2.5G, 10G fiber uplink) | Its web pages |
| ap-f1-01 … ap-f3-02, ap-cafe-01, ap-mr-01, ap-patio-01 | UniFi U6-Pro, U6-Enterprise, U7-Pro, U6-Mesh | 7.0.95 / 8.1.21 | 10.10.10.101–109 | Office Wi-Fi | UniFi Network controller (unifi01) |
| halo-wh-01–03 | Mercusys Halo H80X (AP mode) | 1.2.0 | 10.10.34.10–12 | Warehouse Wi-Fi (consumer mesh) | Halo local API |
| pve01–03 | Dell PowerEdge R650xs (2× Xeon Silver 4314, 512 GB) | Proxmox VE 9.0 | 10.10.10.11–13 | Cluster `acme-pve`, Ceph | Proxmox VE API |
| nas01 | Synology RS1221+ | DSM 7.2.2 | 10.10.10.30 | Files, ISO store, Hyper Backup source | SNMPv2c agent |
| bkp-nas01 | QNAP TS-873AU-RP | QTS 5.2.6 | 10.10.10.31 | Backup target (PBS datastore, Hyper Backup) | SNMPv2c agent |
| ups-rack01 | APC Smart-UPS SRT 5000 + NMC3 | AOS 3.2.1 | 10.10.10.40 | Rack UPS (community left at "public") | SNMPv1/v2c agent |
| mon01 | Ubuntu VM with net-snmp | — | 10.10.20.40 | Zabbix | SNMPv2c agent |
| br-gw01 | MikroTik RB5009UPr+S+ | RouterOS 7.16.2 | 10.20.10.1 | Branch router | RouterOS REST API |
| br-oc200, br-sw01, br-ap01/02 | TP-Link OC200, SG3428MP, EAP670 | — | 10.20.10.5, .10, .11–12 | Branch LAN | Omada Open API (OC200) |
| store-rt01 | GL.iNet GL-MT6000 | OpenWrt 24.10.2 | 10.30.1.1 | Store router + Wi-Fi | ubus over HTTP |
| lab-fw01 | pfSense CE VM | 2.8.0 | 10.10.120.10 | Lab router (double NAT) | pfSense REST API v2 |
| ont-fiber, lte-gw01, br-ont, store-modem | Huawei HG8010H, Teltonika RUTX50, Huawei EG8145X6, Huawei HG8145V5 | — | — | ISP equipment | Seen only by others |

## Servers and VMs (Proxmox cluster `acme-pve`)

Every guest sits on `vmbr1` (VLAN aware, over bond1 to sw-stor-01) with its VLAN tag, except the MGMT ones on
`vmbr0` and the lab on `vmbr2` (pve02, no uplink). While a node reboots (Sunday 02:00 / 02:20 / 02:40) its
guests live-migrate to the next node (pve01 → pve02 → pve03 → pve01) and come back; the lab and Home Assistant
stay (and stop with pve02).

| VMID | Guest | Node | Type | VLAN | Address | Runs |
|---|---|---|---|---|---|---|
| 100 | dc01 | pve01 | VM | 20 | 10.10.20.10 | Windows Server 2022, AD DS, DNS |
| 102 | erp-app01 | pve01 | VM | 20 | 10.10.20.30 | Odoo (ERP) |
| 104 | unifi01 | pve01 | VM | 10 | 10.10.10.20 | UniFi Network Application |
| 106 | pbx01 | pve01 | VM | 40 | 10.10.40.10 | 3CX |
| 108 | docker01 | pve01 | VM | 20 | 10.10.20.50 | Docker: Grafana, Uptime Kuma, Paperless-ngx, Vaultwarden, Portainer |
| 200 | netbox | pve01 | CT | 20 | 10.10.20.60 | NetBox |
| 201 | omini | pve01 | CT | 10 | 10.10.10.250 | Omini itself |
| 101 | dc02 | pve02 | VM | 20 | 10.10.20.11 | Windows Server 2022, AD DS, DNS |
| 103 | erp-db01 | pve02 | VM | 20 | 10.10.20.31 | PostgreSQL |
| 105 | file01 | pve02 | VM | 20 | 10.10.20.20 | Windows file server |
| 107 | mon01 | pve02 | VM | 20 | 10.10.20.40 | Zabbix (net-snmp) |
| 109 | homeassistant | pve02 | VM | 50 | 10.10.50.10 | Home Assistant OS |
| 130 | lab-fw01 | pve02 | VM | 120 + vmbr2 | 10.10.120.10, 192.168.200.1 | pfSense CE |
| 131 | lab-kali | pve02 | VM | vmbr2 | 192.168.200.50 | Kali Linux |
| 202–204 | lab-k3s-01–03 | pve02 | CT | vmbr2 | 192.168.200.11–13 | k3s |
| 110 | pbs01 | pve03 | VM | 110 | 10.10.110.10 | Proxmox Backup Server |
| 112 | print01 | pve03 | VM | 70 | 10.10.70.5 | Windows print server |
| 114 | wsus01 | pve03 | VM | 20 | 10.10.20.45 | WSUS |
| 205 | rproxy-int | pve03 | CT | 20 | 10.10.20.70 | Nginx Proxy Manager |

DMZ (physical, Dell PowerEdge R250, Ubuntu 24.04): web01 10.10.90.11, web02 10.10.90.12 (nginx), mx01 10.10.90.20
(Postfix relay).

## Wi-Fi

| SSID | Where | VLAN | Bands | Security | APs |
|---|---|---|---|---|---|
| Acme | HQ | 32 | 2.4/5/6 GHz | WPA2/WPA3-Enterprise | UniFi |
| Acme-IoT | HQ | 50 | 2.4 GHz | WPA2-PSK | UniFi |
| Acme-Visitantes | HQ | 80 | 2.4/5 GHz | WPA2-PSK, client isolation | UniFi |
| Acme-Galpao | Warehouse | 34 | 2.4/5 GHz | WPA2-PSK | Mercusys Halo |
| Acme (branch) | Campinas | 32 | 2.4/5 GHz | WPA2-Enterprise | Omada |
| Acme-Visitantes (branch) | Campinas | 80 | 2.4/5 GHz | WPA2-PSK | Omada |
| Acme-Loja | Store | LAN | 2.4/5 GHz | WPA2-PSK | store-rt01 |
| Loja-Clientes | Store | GUEST | 2.4/5 GHz | WPA2-PSK | store-rt01 |

## People and their devices

Generated from a fixed seed (`acme.SEED`): 96 people with Brazilian names. HQ departments (floor, people, desk):
Diretoria (1, 6, laptop), Financeiro (1, 8, desktop), RH (1, 4, laptop), Recepção (1, 2, desktop), Facilities
(1, 4, laptop), Vendas (2, 16, laptop), Marketing (2, 8, laptop), Engenharia (3, 10, desktop), TI (3, 6, laptop),
Produto (3, 8, laptop); 6 in the warehouse (Zebra TC52 handhelds), 14 at the branch, 4 at the store. Everyone has
a phone on Wi-Fi (iPhone, Galaxy, Moto, Redmi; 30% with a private MAC); laptops are Dell Latitude, ThinkPad or
MacBook on Wi-Fi; desktops (Dell OptiPlex, Precision with Ubuntu) are wired behind the desk's Yealink phone (LLDP-MED,
voice VLAN 40 tagged, PC untagged in 30). Meeting rooms (Santos, Campinas on floor 2; Paulista, Ibirapuera on
floor 3) have a Samsung or LG TV (IOT), a Chromecast (Acme-IoT) and a Yealink CP965. Plus printers (HP, Brother,
Honeywell label printer), 13 Hikvision/Intelbras cameras and the NVR, badge readers (HID), HVAC controllers
(Daikin), Shelly relays, ESPHome sensors, Tuya plugs, a Sonos speaker, a Raspberry Pi signage player, visitors on
the guest Wi-Fi and the security guard's phone. About 404 nodes in all; ~360 on the network on a weekday morning,
~190 on a Sunday night.

## Emulated APIs

Each runs on the device's real address in the lab; tests serve them on 127.0.0.1 with random ports.

| API | Kind | Address | Omini integration |
|---|---|---|---|
| opnsense-fw-hq-01 | opnsense | https://10.10.10.2 | OPNsense plugin |
| opnsense-fw-hq-02 | opnsense | https://10.10.10.3 | OPNsense plugin |
| proxmox | proxmox | https://10.10.10.11:8006 | Proxmox VE plugin |
| unifi | unifi | https://10.10.10.20:8443 | UniFi Network plugin |
| mikrotik-br-gw01 | mikrotik | https://10.20.10.1 | MikroTik RouterOS plugin |
| mikrotik-sw-stor-01 | mikrotik | https://10.10.10.6 | MikroTik RouterOS plugin |
| omada | omada | https://10.20.10.5 | TP-Link Omada plugin |
| openwrt | openwrt | https://10.30.1.1 | OpenWrt plugin |
| pfsense | pfsense | https://10.10.120.10 | pfSense plugin |
| horaco | horaco | http://10.10.10.8 | Horaco switch plugin |
| mercusys | mercusys | https://10.10.34.10 | Mercusys Halo plugin |
| snmp-core-sw01 | snmp | udp://10.10.10.5:161 | Network scan (SNMP v3) |
| snmp-sw-f1-01 | snmp | udp://10.10.10.21:161 | Network scan (SNMP v2c) |
| snmp-sw-f2-01 | snmp | udp://10.10.10.22:161 | Network scan (SNMP v2c) |
| snmp-sw-dmz-01 | snmp | udp://10.10.10.7:161 | Network scan (SNMP v2c) |
| snmp-sw-stor-01 | snmp | udp://10.10.10.6:161 | Network scan (SNMP v2c) |
| snmp-nas01 | snmp | udp://10.10.10.30:161 | Network scan (SNMP v2c) |
| snmp-bkp-nas01 | snmp | udp://10.10.10.31:161 | Network scan (SNMP v2c) |
| snmp-ups-rack01 | snmp | udp://10.10.10.40:161 | Network scan (SNMP v1/v2c "public") |
| snmp-mon01 | snmp | udp://10.10.20.40:161 | Network scan (SNMP v2c) |

The network scan itself runs for real in the lab (it scans HQ's MGMT VLAN directly, with ARP, and the networks
the routers report, routed): every host answers on its real open ports (web page titles, SSH banners), NetBIOS,
mDNS and SSDP as the `scan` entries of `acme.py` describe, and the firewalls' address 10.10.10.1 answers reverse
DNS. The branch's APIs are reachable only while the WireGuard tunnel is up, the store's while IPsec is.

## Scenario (one simulated week, repeating)

Every time is local (UTC-3). The week repeats, so tests ask for an instant ("Tuesday 14:10") and get the same
network every time.

| When | What |
|---|---|
| Mon–Fri 07:15–09:30 | People arrive (each at their own time, ±15 min a day); laptops and phones join Wi-Fi, desktops start (60% are switched off at night, their switch ports go down) |
| Mon–Fri 09:00–18:00 | Meetings (≈73 a week): participants' devices roam to the room's AP; the room's TV and Chromecast stream |
| Mon–Fri 12:00–13:00 | Half the people take their phones to the cafeteria (ap-cafe-01) |
| Mon–Fri ~16:30–19:00 | People leave; ARP entries age out after 20 min, MAC tables after 5 min, Wi-Fi leases stay 8 h |
| Mon–Fri 09:00–17:00 | Visitors on the guest Wi-Fi (random MACs, 1 h leases) |
| every night 19:00–07:00, weekends | The security guard's phone |
| daily 01:00–03:00 | vzdump: each Proxmox node sends ~2.3 Gbps to pbs01; pbs01 writes 6.8 Gbps to bkp-nas01 |
| daily 02:00–04:00 | Synology Hyper Backup nas01 → bkp-nas01 (2.9 Gbps): bkp-nas01's 10G port is saturated 02:00–03:00 |
| daily 04:30–06:30 | Offsite copy bkp-nas01 → cloud, 300 Mbps over the fiber |
| daily 03:30–03:40 | IPsec to the store drops (rekey failure): the store's router is unreachable |
| daily 04:00 | halo-wh-02 reboots (consumer firmware's scheduled reboot); halo-wh-03 (meshed through it) drops too |
| daily 11:00–11:30 | The 5G backup degrades (~200 ms, 8–15% loss) |
| daily 16:00–16:12 | The WireGuard tunnel to Campinas drops: the branch's MikroTik and Omada are unreachable |
| Tue and Thu 14:05–14:20 | The fiber goes down: internet fails over to 5G (capped at 280/55 Mbps) |
| Wed 03:00 | UniFi APs reboot one by one for a firmware update (3 min each) |
| Wed–Fri 10:00–18:00 | nas01's disk 3 runs hot (61–64 °C) |
| Fri 17:00–19:00 | ERP month-end batch: erp-app01 at 96% CPU, 180 Mbps to erp-db01 |
| Mon 09:00 → Sat 22:00/22:30 | OPNsense update pending on both firewalls (needs a reboot) |
| Sat 22:00 | fw-hq-02 updates and reboots (6 min) |
| Sat 22:28–22:36 | fw-hq-01 updates: CARP fails over to fw-hq-02 (it holds the VIPs and PPPoE); fw-hq-01 reboots at 22:30 |
| Sat 23:00 | sw-f2-01 reboots (floor 2 power work, 4 min) |
| Sun 02:00, 02:20, 02:40 | pve01, pve02, pve03 reboot for a kernel update (8 min each, guests migrate) |
| Sun 02:30–03:30 | Ceph recovery traffic between the nodes |
| Mon 05:00 | store-rt01 reboots |
| all week | sw-stor-01 sfp28-16 (fiber to core-sw01): Rx power drifts from −4 dBm (Monday) to −14.5 dBm (Sunday night), CRC errors once below −11 dBm (the optic is replaced every Monday) |
| all week | nas01's volume fills from 82% to 91.5% (a cleanup job runs on Mondays) |
| always | prn-f3-01 has a static 10.10.30.150 inside the USERS DHCP pool, also leased to ACME-PC-0107 (duplicate IP); sw-f1-01 port 12 has a bad patch cable (CRC errors during business hours); ap-f2-02 links at 100 Mbps; docker01 runs at 93% memory; ups-rack01 answers SNMP with "public" |

Traffic: each device has a profile (laptop, phone, desktop, IP phone calls, camera 2–6 Mbps to the NVR, printer
jobs from print01, TV during meetings, IoT, servers, DMZ web, backups, Ceph) shaped by business hours and a
per-device noise. Flows follow the real paths (switch ports, LAGs with LACP hashing, VLAN interfaces of the
firewalls, PPPoE or the 5G uplink, the tunnels), so every port's counters are consistent with its neighbors'.

The clock: `DEVNET_SPEED` simulated seconds per real second (default 60: one hour per minute); the lab starts on
Monday 07:00 the first time and then continues where it stopped (`data-devnet/devnet-clock.json`).
`python -m emulator clock --at "tue 14:00"` moves it.

## Expected map

What Omini should draw from all of this, and what the tests assert (`internal/collector/devnet_test.go`, run
with the real plugins against the emulator). Placements Omini gets wrong today are listed in the test as known
bugs (they are logged, not failed) and reported to the maintainer.

- The WAN nodes "Fiber" (PPPoE) and "LTE" are parents of the CARP master; the ONT hangs under the fiber WAN.
- fw-hq-01 and fw-hq-02 both hang from core-sw01 (Po11/Po12); core-sw01 is the root of HQ's LAN.
- sw-f1-01, sw-f2-01, sw-f3-01, sw-stor-01 and sw-wh-01 hang from core-sw01; sw-dmz-01 from the firewalls.
- pve01–03 hang from sw-stor-01 (bond1) or core-sw01 (bond0); every guest hangs from the node running it.
- nas01 and bkp-nas01 hang from sw-stor-01; ups-rack01, nvr01 and the iDRACs from core-sw01.
- UniFi APs hang from their floor switch's port; ap-patio-01 from ap-cafe-01 (mesh).
- halo-wh-01 and halo-wh-02 hang from sw-wh-01 (Port 1, Port 2); halo-wh-03 from halo-wh-02.
- Desk phones hang from their floor switch's port, the PC behind each phone from the phone (or the same port).
- Wi-Fi clients hang from the AP they are on now (through the SSID's mini node).
- lab-fw01 hangs from pve02; the lab hosts behind it.
- The branch: br-gw01 → br-sw01 → br-ap01/02, br-oc200, phones, printer; the store: store-rt01 with its clients.
- Alerts at Tuesday 10:30: duplicate IP 10.10.30.150, slow uplink (ap-f2-02, 100 Mbps), weak Wi-Fi on
  ap-patio-01, interface errors (sw-f1-01 port 12; errors grow between two rounds, so a single collection shows
  none), update pending (fw-hq-01/02), likely unmanaged switch (desk switch on sw-f2-01 port 28). docker01's
  memory is high, but Omini does not alert on a virtual machine's memory (the guest's cache makes it unreliable). At Tuesday 14:10: the fiber WAN is down. At 16:05: the
  branch's integrations fail.
