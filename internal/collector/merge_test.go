package collector_test

import (
	"context"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
)

// reporter is an integration that reports one device and records changes.
type reporter struct {
	typ     string
	kind    integration.Kind
	changes []model.ChangeKind
	device  model.Device
	mu      sync.Mutex
	applied []model.Change
}

func (r *reporter) Info() integration.Info {
	return integration.Info{Type: r.typ, Name: r.typ, Kind: r.kind, Changes: r.changes}
}

func (r *reporter) Collect(context.Context, integration.Config) ([]model.Device, error) {
	return []model.Device{r.device}, nil
}

func (*reporter) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (r *reporter) Apply(_ context.Context, _ integration.Config, c model.Change) ([]model.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.applied = append(r.applied, c)
	d := r.device
	if v, ok := c.Value.(model.DhcpReservation); ok {
		d.DhcpReservations = append(slices.Clone(d.DhcpReservations), v)
		r.device = d
	}
	return []model.Device{d}, nil
}

// A router read both by the network scan's SNMP (more ports, its VLANs) and
// by its plugin (its networks): the node offers both integrations' changes,
// and each goes to the integration that makes it, with that one's key.
func TestADeviceReadTwiceChangesThroughBoth(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	mac := model.MACAddress("58:9c:fc:10:8f:2c")
	var ports []model.Interface
	for _, p := range []string{"igc0", "igc1", "igc2", "igc3", "igc4", "igc5", "igc6", "igc7"} {
		ports = append(ports, model.Interface{Name: p, Up: model.Ptr(true)})
	}
	snmp := &reporter{
		typ: "scan", kind: integration.KindCore,
		changes: []model.ChangeKind{model.ChangeKindVlanSet, model.ChangeKindVlanDelete, model.ChangeKindReservationSet},
		device: model.Device{
			Key: string(mac), Name: "OPNsense", Role: model.Ptr(model.DeviceRoleRouter), MACs: []model.MACAddress{mac},
			Interfaces: ports, Vlans: []model.Vlan{{ID: 1}},
		},
	}
	plugin := &reporter{
		typ: "opn", kind: integration.KindPlugin,
		changes: []model.ChangeKind{model.ChangeKindReservationSet, model.ChangeKindReservationDelete},
		device: model.Device{
			Key: "opnsense-1", Name: "OPNsense", Role: model.Ptr(model.DeviceRoleFirewall), MACs: []model.MACAddress{mac},
			Interfaces: ports[:2],
			Networks:   []model.Network{{Vlan: 1, Gateway: model.Ptr("192.168.1.1/24")}},
			Arp:        []model.ArpEntry{{IP: "192.168.1.20", MAC: "aa:00:00:00:00:20"}},
			DhcpLeases: []model.DhcpLease{{IP: "192.168.1.20", MAC: "aa:00:00:00:00:20"}},
		},
	}
	reg := integration.NewRegistry()
	reg.Register(snmp)
	reg.Register(plugin)
	coll := collector.New(st, reg, box, collector.Options{})
	for _, typ := range []string{"scan", "opn"} {
		if _, err := st.CreateIntegration(ctx, store.Integration{Name: typ, Type: typ, Enabled: true, Config: integration.Config{}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	var fw, tv string
	for _, n := range coll.State().Topology.Nodes {
		switch n.MAC {
		case string(mac):
			fw = n.ID
			if !slices.Contains(n.Changes, model.ChangeKindReservationSet) || !slices.Contains(n.Changes, model.ChangeKindVlanSet) {
				t.Fatalf("changes of both: %v", n.Changes)
			}
			if model.Deref(n.Device.Networks[0].Gateway) != "192.168.1.1/24" {
				t.Fatalf("the plugin's report is the node's: %+v", n.Device)
			}
		case "aa:00:00:00:00:20":
			tv = n.ID
		}
	}
	if fw == "" || tv == "" {
		t.Fatal("nodes missing")
	}
	// A fixed address goes to the plugin (it declared it first), with its key.
	if _, err := coll.Reserve(ctx, tv, "192.168.1.20"); err != nil {
		t.Fatal(err)
	}
	if len(plugin.applied) != 1 || plugin.applied[0].Device != "opnsense-1" || len(snmp.applied) != 0 {
		t.Fatalf("plugin %+v, snmp %+v", plugin.applied, snmp.applied)
	}
	// A VLAN goes to the network scan, with its key.
	if _, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: fw, Kind: model.ChangeKindVlanSet, Value: collector.VlanValue{ID: 30, Name: "IOT"}}); err != nil {
		t.Fatal(err)
	}
	if len(snmp.applied) != 1 || snmp.applied[0].Device != string(mac) {
		t.Fatalf("snmp %+v", snmp.applied)
	}
}
