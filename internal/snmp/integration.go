package snmp

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// Integration is the generic SNMP v2c integration.
type Integration struct{}

func New() *Integration { return &Integration{} }

func (*Integration) Info() integration.Info {
	return integration.Info{
		Type:        "snmp",
		Name:        "SNMP (generic)",
		Description: "Any managed switch, router or firewall with SNMP v2c: ports, traffic, LLDP neighbors, MAC table and ARP.",
		Kind:        integration.KindCore,
		Fields: []model.FormField{
			{Key: "host", Type: model.FormFieldTypeHost, Label: model.Ptr("Host"), Required: true},
			{
				Key: "community", Type: model.FormFieldTypeSecret, Label: model.Ptr("Community"), Default: "public", Required: true,
				Help: model.Ptr("Read-only community configured on the device."),
			},
			{Key: "port", Type: model.FormFieldTypeInt, Label: model.Ptr("Port"), Default: 161},
		},
	}
}

func (s *Integration) Test(ctx context.Context, cfg integration.Config) (string, error) {
	c, err := s.connect(ctx, cfg)
	if err != nil {
		return "", err
	}
	defer c.close()
	sys, err := c.get(oidSysName, oidSysDescr)
	if err != nil {
		return "", fmt.Errorf("no SNMP response (check host, community and that SNMP v2c is enabled): %w", err)
	}
	name := pduString(sys[oidSysName])
	if name == "" {
		name = cfg.String("host")
	}
	return fmt.Sprintf("Connected to %s", name), nil
}

func (s *Integration) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	c, err := s.connect(ctx, cfg)
	if err != nil {
		return nil, err
	}
	defer c.close()
	host := cfg.String("host")

	d, err := collectSystem(c, host)
	if err != nil {
		return nil, err
	}
	log := slog.With("integration", "snmp", "host", host)

	// Everything below is optional: agents implement different subsets of MIBs.
	ifNames, err := collectInterfaces(c, &d)
	if err != nil {
		log.Debug("interfaces unavailable", "err", err)
	}
	if err := collectIPs(c, &d); err != nil {
		log.Debug("ip addresses unavailable", "err", err)
	}
	if err := collectARP(c, &d, ifNames); err != nil {
		log.Debug("arp unavailable", "err", err)
	}
	if err := collectFDB(c, &d, ifNames); err != nil {
		log.Debug("fdb unavailable", "err", err)
	}
	if err := collectLLDP(c, &d, ifNames); err != nil {
		log.Debug("lldp unavailable", "err", err)
	}
	if err := collectResources(c, &d); err != nil {
		log.Debug("host resources unavailable", "err", err)
	}
	d.Role = model.Ptr(guessRole(c, &d))
	return []model.Device{d}, nil
}

func (*Integration) connect(ctx context.Context, cfg integration.Config) (*client, error) {
	host := cfg.String("host")
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	community := cfg.String("community")
	if community == "" {
		community = "public"
	}
	return dial(ctx, host, cfg.Int("port", 161), community, 3*time.Second)
}

func collectSystem(c *client, host string) (model.Device, error) {
	sys, err := c.get(oidSysDescr, oidSysObjectID, oidSysUpTime, oidSysName,
		oidLldpLocChassisIDSubtype, oidLldpLocChassisID)
	if err != nil {
		return model.Device{}, fmt.Errorf("no SNMP response from %s (check host, community and that SNMP v2c is enabled): %w", host, err)
	}
	d := model.Device{Host: model.Ptr(host), Name: pduString(sys[oidSysName])}
	if d.Name == "" {
		d.Name = host
	}
	if descr := pduString(sys[oidSysDescr]); descr != "" {
		first, _, _ := strings.Cut(descr, "\n")
		if len(first) > 120 {
			first = first[:120]
		}
		d.Model = model.Ptr(first)
	}
	if v := vendorFromSysObjectID(pduString(sys[oidSysObjectID])); v != "" {
		d.Vendor = model.Ptr(v)
	}
	if ticks, ok := pduUint(sys[oidSysUpTime]); ok {
		d.UptimeS = model.Ptr(ticks / 100)
	}
	if st, ok := pduInt(sys[oidLldpLocChassisIDSubtype]); ok && st == chassisSubtypeMAC {
		if mac := model.MACFromBytes(pduBytes(sys[oidLldpLocChassisID])); mac != "" {
			d.MACs = append(d.MACs, mac)
		}
	}
	return d, nil
}

func vendorFromSysObjectID(oid string) string {
	parts := splitOID(strings.TrimPrefix(oid, "."))
	// 1.3.6.1.4.1.<enterprise>...
	if len(parts) < 7 || parts[0] != 1 || parts[1] != 3 || parts[2] != 6 || parts[3] != 1 || parts[4] != 4 || parts[5] != 1 {
		return ""
	}
	return enterprises[parts[6]]
}

// collectInterfaces fills interfaces and device MACs; it returns ifIndex -> port name.
func collectInterfaces(c *client, d *model.Device) (map[string]string, error) {
	ifRows, err := c.walk(oidIfTable)
	if err != nil {
		return nil, err
	}
	xRows, err := c.walk(oidIfXTable)
	if err != nil {
		xRows = nil // ifXTable is optional (old agents)
	}
	it, xt := table(ifRows), table(xRows)

	indexes := make([]string, 0, len(it[2]))
	for idx := range it[2] {
		indexes = append(indexes, idx)
	}
	sort.Slice(indexes, func(a, b int) bool {
		x, _ := strconv.Atoi(indexes[a])
		y, _ := strconv.Atoi(indexes[b])
		return x < y
	})

	names := map[string]string{}
	seenMAC := map[model.MACAddress]bool{}
	for _, m := range d.MACs {
		seenMAC[m] = true
	}
	for _, idx := range indexes {
		descr := pduString(it[2][idx])
		name := pduString(xt[1][idx])
		if name == "" {
			name = descr
		}
		if name == "" {
			name = "if" + idx
		}
		names[idx] = name

		iface := model.Interface{Name: name}
		if alias := pduString(xt[18][idx]); alias != "" {
			iface.Description = model.Ptr(alias)
		} else if descr != "" && descr != name {
			iface.Description = model.Ptr(descr)
		}
		if t, ok := pduInt(it[3][idx]); ok {
			typ := model.InterfaceTypeOther
			if s, known := ifTypes[t]; known {
				typ = model.InterfaceType(s)
			}
			iface.Type = &typ
		}
		if mac := model.MACFromBytes(pduBytes(it[6][idx])); mac != "" {
			iface.MAC = &mac
			if !seenMAC[mac] {
				seenMAC[mac] = true
				d.MACs = append(d.MACs, mac)
			}
		}
		if st, ok := pduInt(it[8][idx]); ok {
			iface.Up = model.Ptr(st == 1)
		}
		if hs, ok := pduUint(xt[15][idx]); ok && hs > 0 {
			iface.SpeedMbps = model.Ptr(hs)
		} else if sp, ok := pduUint(it[5][idx]); ok && sp > 0 {
			iface.SpeedMbps = model.Ptr(sp / 1_000_000)
		}
		// Prefer 64-bit counters; 32-bit ones wrap in seconds on fast links.
		if v, ok := pduUint(xt[6][idx]); ok {
			iface.RxBytes = model.Ptr(v)
		} else if v, ok := pduUint(it[10][idx]); ok {
			iface.RxBytes = model.Ptr(v)
		}
		if v, ok := pduUint(xt[10][idx]); ok {
			iface.TxBytes = model.Ptr(v)
		} else if v, ok := pduUint(it[16][idx]); ok {
			iface.TxBytes = model.Ptr(v)
		}
		if v, ok := pduUint(it[14][idx]); ok {
			iface.RxErrors = model.Ptr(v)
		}
		if v, ok := pduUint(it[20][idx]); ok {
			iface.TxErrors = model.Ptr(v)
		}
		d.Interfaces = append(d.Interfaces, iface)
	}
	return names, nil
}

func collectIPs(c *client, d *model.Device) error {
	rows, err := c.walk(oidIPAddrIfIndex)
	if err != nil {
		return err
	}
	ips := make([]string, 0, len(rows))
	for ip := range rows {
		if parsed := net.ParseIP(ip); parsed != nil && !parsed.IsLoopback() {
			ips = append(ips, ip)
		}
	}
	sort.Strings(ips)
	d.IPs = ips
	return nil
}

func collectARP(c *client, d *model.Device, ifNames map[string]string) error {
	rows, err := c.walk(oidIPNetToMedia)
	if err != nil {
		return err
	}
	t := table(rows)
	for idx, p := range t[2] {
		if typ, ok := pduInt(t[4][idx]); ok && typ == 2 { // invalid
			continue
		}
		parts := strings.SplitN(idx, ".", 2)
		if len(parts) != 2 {
			continue
		}
		mac := model.MACFromBytes(pduBytes(p))
		if mac == "" || net.ParseIP(parts[1]) == nil {
			continue
		}
		e := model.ArpEntry{IP: parts[1], MAC: mac}
		if name := ifNames[parts[0]]; name != "" {
			e.Interface = model.Ptr(name)
		}
		d.Arp = append(d.Arp, e)
	}
	if len(d.Arp) > 0 {
		return nil
	}

	// Fallback: ipNetToPhysicalTable (index ifIndex.addrType.len.addr), IPv4 only.
	rows, err = c.walk(oidIPNetToPhysical)
	if err != nil {
		return err
	}
	for idx, p := range rows {
		n := splitOID(idx)
		if len(n) != 7 || n[1] != 1 || n[2] != 4 {
			continue
		}
		mac := model.MACFromBytes(pduBytes(p))
		if mac == "" {
			continue
		}
		e := model.ArpEntry{IP: fmt.Sprintf("%d.%d.%d.%d", n[3], n[4], n[5], n[6]), MAC: mac}
		if name := ifNames[strconv.Itoa(n[0])]; name != "" {
			e.Interface = model.Ptr(name)
		}
		d.Arp = append(d.Arp, e)
	}
	return nil
}

func collectFDB(c *client, d *model.Device, ifNames map[string]string) error {
	portIf := map[string]string{} // bridge port -> ifIndex
	if rows, err := c.walk(oidBasePortIfIndex); err == nil {
		for port, p := range rows {
			if v, ok := pduInt(p); ok {
				portIf[port] = strconv.FormatInt(v, 10)
			}
		}
	}
	portName := func(bridgePort int64) string {
		bp := strconv.FormatInt(bridgePort, 10)
		ifIdx, ok := portIf[bp]
		if !ok {
			ifIdx = bp // common fallback: bridge port == ifIndex
		}
		if name := ifNames[ifIdx]; name != "" {
			return name
		}
		return "port" + bp
	}
	add := func(macOID []int, portPDU, statusPDU gosnmp.SnmpPDU, vlan *uint16) {
		port, ok := pduInt(portPDU)
		if !ok || port == 0 {
			return
		}
		if st, ok := pduInt(statusPDU); ok && st == 4 { // self
			return
		}
		b := make([]byte, 6)
		for i, x := range macOID {
			b[i] = byte(x)
		}
		mac := model.MACFromBytes(b)
		if mac == "" {
			return
		}
		d.Fdb = append(d.Fdb, model.FdbEntry{MAC: mac, Port: portName(port), Vlan: vlan})
	}

	// Q-BRIDGE-MIB (VLAN aware) first, then plain BRIDGE-MIB.
	rows, err := c.walk(oidDot1qTpFdb)
	if err != nil {
		return err
	}
	t := table(rows)
	for idx, p := range t[2] {
		n := splitOID(idx)
		if len(n) != 7 {
			continue
		}
		var vlan *uint16
		if n[0] > 0 && n[0] <= 4095 {
			vlan = model.Ptr(uint16(n[0]))
		}
		add(n[1:], p, t[3][idx], vlan)
	}
	if len(d.Fdb) > 0 {
		return nil
	}
	rows, err = c.walk(oidDot1dTpFdb)
	if err != nil {
		return err
	}
	t = table(rows)
	for idx, p := range t[2] {
		n := splitOID(idx)
		if len(n) != 6 {
			continue
		}
		add(n, p, t[3][idx], nil)
	}
	return nil
}

func collectLLDP(c *client, d *model.Device, ifNames map[string]string) error {
	rem, err := c.walk(oidLldpRem)
	if err != nil {
		return err
	}
	if len(rem) == 0 {
		return nil
	}
	locRows, _ := c.walk(oidLldpLocPort)
	loc := table(locRows)

	byName := map[string]bool{}
	for _, n := range ifNames {
		byName[n] = true
	}
	localPort := func(num string) string {
		if st, ok := pduInt(loc[2][num]); ok && (st == portSubtypeIfName || st == portSubtypeLocal) {
			if id := pduString(loc[3][num]); byName[id] {
				return id
			}
		}
		if desc := pduString(loc[4][num]); byName[desc] {
			return desc
		}
		if name := ifNames[num]; name != "" { // localPortNum is often the ifIndex
			return name
		}
		if desc := pduString(loc[4][num]); desc != "" {
			return desc
		}
		return "port" + num
	}

	mgmt := map[string]string{} // timeMark.localPort.remIndex -> IPv4
	if rows, err := c.walk(oidLldpRemManAddr); err == nil {
		for idx := range rows {
			n := splitOID(idx)
			// timeMark.localPort.remIndex.subtype(1=ipv4).len(4).a.b.c.d
			if len(n) == 9 && n[3] == 1 && n[4] == 4 {
				key := fmt.Sprintf("%d.%d.%d", n[0], n[1], n[2])
				mgmt[key] = fmt.Sprintf("%d.%d.%d.%d", n[5], n[6], n[7], n[8])
			}
		}
	}

	t := table(rem)
	for idx := range t[5] { // lldpRemChassisId
		n := splitOID(idx)
		if len(n) != 3 {
			continue
		}
		nb := model.Neighbor{
			LocalPort: localPort(strconv.Itoa(n[1])),
			Protocol:  model.Ptr(model.NeighborProtocolLldp),
		}
		switch st, _ := pduInt(t[4][idx]); st {
		case chassisSubtypeMAC:
			if mac := model.MACFromBytes(pduBytes(t[5][idx])); mac != "" {
				nb.RemoteMAC = &mac
			}
		case chassisSubtypeNetAddr:
			if b := pduBytes(t[5][idx]); len(b) == 5 && b[0] == 1 {
				nb.RemoteIP = model.Ptr(net.IP(b[1:]).String())
			}
		}
		portID, portDesc := pduString(t[7][idx]), pduString(t[8][idx])
		switch st, _ := pduInt(t[6][idx]); {
		case st == portSubtypeIfName || st == portSubtypeLocal:
			nb.RemotePort = nonEmpty(portID)
		case portDesc != "":
			nb.RemotePort = model.Ptr(portDesc)
		case st == portSubtypeMAC:
			nb.RemotePort = nonEmpty(string(model.MACFromBytes(pduBytes(t[7][idx]))))
		default:
			nb.RemotePort = nonEmpty(portID)
		}
		nb.RemoteName = nonEmpty(pduString(t[9][idx]))
		if descr := pduString(t[10][idx]); descr != "" {
			first, _, _ := strings.Cut(descr, "\n")
			nb.RemotePlatform = model.Ptr(first)
		}
		if ip, ok := mgmt[idx]; ok && nb.RemoteIP == nil {
			nb.RemoteIP = model.Ptr(ip)
		}
		d.Neighbors = append(d.Neighbors, nb)
	}
	sort.Slice(d.Neighbors, func(a, b int) bool { return d.Neighbors[a].LocalPort < d.Neighbors[b].LocalPort })
	return nil
}

func collectResources(c *client, d *model.Device) error {
	loads, err := c.walk(oidHrProcessorLoad)
	if err != nil {
		return err
	}
	var sum, n uint64
	for _, p := range loads {
		if v, ok := pduUint(p); ok {
			sum += v
			n++
		}
	}
	if n > 0 {
		d.CPUPct = model.Ptr(float64(sum) / float64(n))
	}

	rows, err := c.walk(oidHrStorage)
	if err != nil {
		return err
	}
	t := table(rows)
	for idx, p := range t[2] {
		if pduString(p) != oidHrStorageRAM {
			continue
		}
		size, ok1 := pduUint(t[5][idx])
		used, ok2 := pduUint(t[6][idx])
		if ok1 && ok2 && size > 0 {
			d.MemPct = model.Ptr(float64(used) * 100 / float64(size))
		}
		break
	}
	return nil
}

func guessRole(c *client, d *model.Device) model.DeviceRole {
	descr := strings.ToLower(model.Deref(d.Model))
	for _, fw := range []string{"opnsense", "pfsense", "fortigate", "sophos"} {
		if strings.Contains(descr, fw) {
			return model.DeviceRoleFirewall
		}
	}
	forwarding := false
	if r, err := c.get(oidIPForwarding, oidSysServices); err == nil {
		if v, ok := pduInt(r[oidIPForwarding]); ok && v == 1 {
			forwarding = true
		}
		// sysServices bit 2 (value 2) = datalink/subnetwork layer, i.e. bridging.
		if v, ok := pduInt(r[oidSysServices]); ok && !forwarding && v&2 != 0 && v&4 == 0 {
			return model.DeviceRoleSwitch
		}
	}
	switch {
	case forwarding:
		return model.DeviceRoleRouter
	case len(d.Fdb) > 0:
		return model.DeviceRoleSwitch
	case model.Deref(d.Vendor) == "Net-SNMP":
		return model.DeviceRoleServer
	}
	return model.DeviceRoleUnknown
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
