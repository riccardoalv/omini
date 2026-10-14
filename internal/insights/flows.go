package insights

import (
	"slices"
	"sort"
	"strings"

	"github.com/riccardoalv/omini/internal/topology"
)

// Rules on the traffic flows show: a device that asks DNS servers on the
// internet instead of the network's own, one that sends much more to the
// internet than it usually does, and one that scans the network.

func init() {
	Rules["dns_bypass"] = dnsBypass
	Rules["upload_unusual"] = uploadUnusual
	Rules["lan_scan"] = lanScan
}

// Flows are the facts the flow collector found (nil without flows).
type Flows struct {
	// DNSOutside: devices that sent DNS queries to servers on the internet in
	// the last hour, and those servers (a name when known, else the address).
	DNSOutside map[string][]string
	// Upload: each device's upload to the internet.
	Upload map[string]Upload
	// Scans: devices that reached many local services within a minute in the
	// last hour, and the most they reached.
	Scans map[string]int
}

// Upload is a device's upload in the last full hour, and its average per
// hour over the week before (Hours: how many hours that average covers).
type Upload struct {
	LastHour uint64
	Average  uint64
	Hours    int
}

const (
	// UnusualUpload: a device sent at least this much in an hour...
	UnusualUpload = 200 << 20
	// ...and this many times its average hour.
	UnusualFactor = 5
	// UploadHistory: hours of history needed to know a device's usual upload.
	UploadHistory = 24
)

// noFlowAlerts are the types whose job is talking to many places: their
// traffic says nothing unusual about them.
var noFlowAlerts = []string{"router", "firewall", "server", "nas", "hypervisor", "virtual_machine"}

// dnsServer reports whether a device answers DNS on the network (it reports
// local names or the lookups it answered).
func dnsServer(in Input, id string) bool {
	for _, n := range in.Topology.Nodes {
		if n.ID == id && answersDNS(n) {
			return true
		}
	}
	return false
}

func answersDNS(n topology.Node) bool {
	for _, d := range n.Devices() {
		if len(d.DnsRecords) > 0 || len(d.DnsLookups) > 0 {
			return true
		}
	}
	return false
}

// localDNS reports whether the network has a DNS server Omini reads.
func localDNS(in Input) bool {
	for _, n := range in.Topology.Nodes {
		if answersDNS(n) {
			return true
		}
	}
	return false
}

// dnsBypass: a device goes around the network's DNS server (its ad blocking
// and local names do not apply to it).
func dnsBypass(in Input, idx index) []Insight {
	if in.Flows == nil || !localDNS(in) {
		return nil
	}
	var out []Insight
	for id, servers := range in.Flows.DNSOutside {
		n := idx.byID[id]
		if n == nil || slices.Contains(noFlowAlerts, n.Type) || dnsServer(in, id) || len(servers) == 0 {
			continue
		}
		list := slices.Clone(servers)
		sort.Strings(list)
		if len(list) > 3 {
			list = list[:3]
		}
		out = append(out, node("dns_bypass", Warning, n, "", map[string]any{"servers": strings.Join(list, ", ")}))
	}
	return out
}

// uploadUnusual: a device sent much more to the internet in the last hour
// than it usually does (a camera or TV uploading, a backup at night, malware).
func uploadUnusual(in Input, idx index) []Insight {
	if in.Flows == nil {
		return nil
	}
	var out []Insight
	for id, u := range in.Flows.Upload {
		n := idx.byID[id]
		if n == nil || slices.Contains(noFlowAlerts, n.Type) || u.Hours < UploadHistory ||
			u.LastHour < UnusualUpload || u.LastHour < UnusualFactor*u.Average {
			continue
		}
		out = append(out, node("upload_unusual", Warning, n, "", map[string]any{
			"mb": u.LastHour >> 20, "usual_mb": u.Average >> 20,
		}))
	}
	return out
}

// lanScan: a device reached many services on the network within a minute.
func lanScan(in Input, idx index) []Insight {
	if in.Flows == nil {
		return nil
	}
	var out []Insight
	for id, targets := range in.Flows.Scans {
		n := idx.byID[id]
		if n == nil || n.Type == "router" || n.Type == "firewall" {
			continue
		}
		out = append(out, node("lan_scan", Warning, n, "", map[string]any{"targets": targets}))
	}
	return out
}
