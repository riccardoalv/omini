# SNMP profiles

Standard MIBs give Omini ports, traffic, LLDP, MAC tables and ARP from any SNMP
device. What they lack — the vendor, CPU, memory, temperatures, firmware — comes
from **profiles**: small YAML files that say which vendor OIDs to read.

Omini ships profiles in `internal/snmp/profiles/`. Add your own in
`<data>/profiles/*.yaml` (`/data/profiles` in Docker); a file with the same
`id` as a shipped one replaces it. Restart Omini after changing them.

```yaml
id: synology                # unique
name: Synology DSM
priority: 20                # every matching profile applies, lowest first: later ones win
match:                      # every key given must match; within a list, any entry may
  sys_object_id: [1.3.6.1.4.1.8072.3.2.10]  # sysObjectID prefixes
  sys_descr: "DSM"                          # regular expression (case-insensitive)
  oid_exists: [1.3.6.1.4.1.6574.1.5.1.0]    # the device answers one of these
vendor: Synology
role: server                # router | switch | ap | firewall | server
model: { oid: 1.3.6.1.4.1.6574.1.5.1.0 }
os_version: { oid: 1.3.6.1.4.1.6574.1.5.3.0 }
serial: { oid: 1.3.6.1.4.1.6574.1.5.2.0 }
cpu_pct: { oid: 1.3.6.1.4.1.2021.11.11.0, invert: true }    # 100 - idle
mem_pct:                    # a percentage from used / free / total
  total: { oid: 1.3.6.1.4.1.2021.4.5.0 }
  free: [{ oid: 1.3.6.1.4.1.2021.4.6.0 }, { oid: 1.3.6.1.4.1.2021.4.15.0 }]
load_avg: [{ oid: 1.3.6.1.4.1.2021.10.1.3.1 }, { oid: 1.3.6.1.4.1.2021.10.1.3.2 }, { oid: 1.3.6.1.4.1.2021.10.1.3.3 }]
temperatures:
  - { sensor: Disks (hottest), kind: disk, oid: 1.3.6.1.4.1.6574.2.1.1.6, walk: max }
firmware:
  current: { oid: 1.3.6.1.4.1.6574.1.5.3.0 }
  update_available: { oid: 1.3.6.1.4.1.6574.1.5.4.0, map: { "1": "true", "2": "false" } }
```

A value is `{ oid }` for a scalar (`….0`), or `{ oid, walk: avg | max | min | sum | first }`
to read a whole column. `scale` multiplies it (`0.1` for tenths of a degree),
`invert` gives `100 - value`. Texts with units ("45 C", "12 %") are read by their
first number. Percentages can also be `used` + `total`, `free` + `total` (several
`free` OIDs are added up) or `used` + `free`.

Profiles only read: Omini never writes to a device.
