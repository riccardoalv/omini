package insights

import (
	"sort"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// keysOf runs one rule and returns the keys it found, sorted, with their
// severities.
func keysOf(t *testing.T, rule string, in Input) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, i := range Evaluate(in) {
		if i.Rule == rule {
			out[i.Key] = i.Severity
		}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestServiceStopped(t *testing.T) {
	srv := topology.Node{
		ID: "dev:srv", Kind: topology.KindDevice, Label: "srv", Online: true,
		Device: &model.Device{Services: []model.Service{
			{Name: "unbound", Enabled: model.Ptr(true), Running: model.Ptr(false), Description: model.Ptr("DNS resolver")},
			{Name: "sshd", Enabled: model.Ptr(true), Running: model.Ptr(true)},
			{Name: "ntpd", Enabled: model.Ptr(false), Running: model.Ptr(false)}, // off on purpose
			{Name: "cron", Running: model.Ptr(false)},                            // not said to be enabled
			{Name: "dhcpd", Enabled: model.Ptr(true)},                            // running unknown
		}},
	}
	off := topology.Node{
		ID: "dev:off", Kind: topology.KindDevice, Label: "off",
		Device: &model.Device{Services: []model.Service{{Name: "x", Enabled: model.Ptr(true), Running: model.Ptr(false)}}},
	}
	in := Input{Now: time.Unix(0, 0), Topology: topology.Topology{Nodes: []topology.Node{srv, off}}}
	got := keysOf(t, "service_stopped", in)
	if len(got) != 1 || got["service_stopped|dev:srv|unbound"] != Warning {
		t.Fatalf("got %v", got)
	}
	for _, i := range Evaluate(in) {
		if i.Rule == "service_stopped" && (i.Params["service"] != "unbound" || i.Params["description"] != "DNS resolver") {
			t.Fatalf("params %v", i.Params)
		}
	}
}
