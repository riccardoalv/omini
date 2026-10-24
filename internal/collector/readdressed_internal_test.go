package collector

import (
	"testing"

	"github.com/riccardoalv/omini/internal/insights"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

func TestReaddressedFindsADeviceDHCPMoved(t *testing.T) {
	fields := func(typ string) []model.FormField {
		switch typ {
		case "horaco":
			return []model.FormField{{Key: "host", Type: model.FormFieldTypeHost}, {Key: "password", Type: model.FormFieldTypeSecret}}
		case "opnsense":
			return []model.FormField{{Key: "url", Type: model.FormFieldTypeURL}}
		}
		return nil
	}
	device := func(id string, it int64, was, now string) topology.Node {
		d := &model.Device{Name: id, Host: model.Ptr(was)}
		return topology.Node{
			ID: id, Kind: topology.KindDevice, IntegrationID: it, IP: now, Device: d,
			Reports: []topology.Report{{IntegrationID: it, Device: d}},
		}
	}
	topo := topology.Topology{Nodes: []topology.Node{
		device("dev:sw", 1, "192.168.1.96", "192.168.1.110"),    // DHCP gave it another address
		device("dev:fw", 2, "192.168.1.1", "192.168.1.1"),       // same address: down, not moved
		device("dev:nas", 3, "192.168.1.20", "192.168.1.21"),    // configured by name
		device("dev:ap", 4, "192.168.1.30", "192.168.1.31"),     // its integration answers
		device("dev:ctl-ap", 5, "192.168.1.40", "192.168.1.41"), // a controller's device, not the controller
	}}
	all := []store.Integration{
		{ID: 1, Type: "horaco", Config: integration.Config{"host": "192.168.1.96", "password": "sealed"}},
		{ID: 2, Type: "opnsense", Config: integration.Config{"url": "https://192.168.1.1"}},
		{ID: 3, Type: "opnsense", Config: integration.Config{"url": "https://nas.lan:8443"}},
		{ID: 4, Type: "horaco", Config: integration.Config{"host": "192.168.1.30"}},
		{ID: 5, Type: "opnsense", Config: integration.Config{"url": "https://192.168.1.5"}},
	}
	statuses := []Status{
		{IntegrationID: 1, Error: "no route to host", Failures: 2},
		{IntegrationID: 2, Error: "timeout"},
		{IntegrationID: 3, Error: "timeout"},
		{IntegrationID: 4, OK: true},
		{IntegrationID: 5, Error: "timeout"},
	}
	got := readdressed(topo, all, statuses, fields)
	want := insights.Moved{NodeID: "dev:sw", From: "192.168.1.96", To: "192.168.1.110"}
	if len(got) != 1 || got[1] != want {
		t.Fatalf("got %+v, want only integration 1: %+v", got, want)
	}
}

func TestReaddressedReadsURLs(t *testing.T) {
	fields := func(string) []model.FormField {
		return []model.FormField{{Key: "url", Type: model.FormFieldTypeURL}}
	}
	d := &model.Device{Name: "fw", IPs: []string{"10.0.0.1/24"}}
	topo := topology.Topology{Nodes: []topology.Node{{
		ID: "dev:fw", Kind: topology.KindDevice, IntegrationID: 7, IP: "10.0.0.9", Device: d,
		Reports: []topology.Report{{IntegrationID: 7, Device: d}},
	}}}
	all := []store.Integration{{ID: 7, Type: "x", Config: integration.Config{"url": "https://10.0.0.1:8443/api"}}}
	got := readdressed(topo, all, []Status{{IntegrationID: 7, Error: "refused"}}, fields)
	if got[7].To != "10.0.0.9" || got[7].From != "10.0.0.1" {
		t.Fatalf("got %+v", got)
	}
}
