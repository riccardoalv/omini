package collector_test

import (
	"context"
	"errors"
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

func TestTheCatalogIsImportedOnce(t *testing.T) {
	coll, _, st := switchNetwork(t)
	ctx := context.Background()
	list, err := coll.Vlans(ctx)
	if err != nil || len(list) != 3 || list[1].Name != "Office" {
		t.Fatalf("imported: %+v %v", list, err)
	}
	for _, v := range list {
		_ = st.DeleteVlan(ctx, v.ID)
	}
	if list, _ := coll.Vlans(ctx); len(list) != 0 {
		t.Fatalf("emptied by hand stays empty: %+v", list)
	}
}

// A port goes to VLAN 10: the port changes, and the uplink to the firewall
// carries VLAN 10 tagged (adding a tagged VLAN to the uplink is not
// destructive); the firewall itself is a note (the router comes later).
func TestPlanAndApplyAPortsVLAN(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	want := model.PortVlans{Untagged: model.Ptr[uint16](10)}
	plan, err := coll.PlanVlans(ctx, swID, "Port 2", want)
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0].Port != "Port 2" || plan.Steps[1].Port != "Port 9" || plan.Steps[1].Protected != nil {
		t.Fatalf("steps: %+v", plan.Steps)
	}
	if len(plan.Notes) != 1 || plan.Notes[0].Reason != "router" {
		t.Fatalf("notes: %+v", plan.Notes)
	}
	done, err := coll.ApplyVlans(ctx, swID, "Port 2", want, false)
	if err != nil || len(done) != 2 || len(s.applied) != 2 {
		t.Fatalf("applied: %+v %v", done, err)
	}
	if p9 := s.applied[1].Value.(model.PortVlans); model.Deref(p9.Untagged) != 1 || !slices.Equal(p9.Tagged, []uint16{10}) {
		t.Fatalf("uplink: %+v", p9)
	}
}

func TestChangingTheUplinksNativeVLANIsDestructive(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	want := model.PortVlans{Untagged: model.Ptr[uint16](10)}
	var de *collector.DestructiveError
	if _, err := coll.ApplyVlans(ctx, swID, "Port 9", want, true); !errors.As(err, &de) || de.Allowed {
		t.Fatalf("blocked: %v", err)
	}
	if len(s.applied) != 0 {
		t.Fatal("nothing applied before the check")
	}
}

func TestRenameAndDeleteVLANs(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	if _, err := coll.Vlans(ctx); err != nil {
		t.Fatal(err)
	}
	if _, done, err := coll.SaveVlan(ctx, store.Vlan{ID: 20, Name: "Visitors"}, false); err != nil || len(done) != 1 {
		t.Fatalf("rename: %+v %v", done, err)
	}
	if v := s.applied[0].Value.(collector.VlanValue); v.Name != "Visitors" {
		t.Fatalf("renamed on the switch: %+v", v)
	}
	// In use on a port: kept everywhere.
	if _, err := coll.ApplyVlans(ctx, swID, "Port 2", model.PortVlans{Untagged: model.Ptr[uint16](20)}, false); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.DeleteVlan(ctx, 20, false); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("in use: %v", err)
	}
	if _, err := coll.ApplyVlans(ctx, swID, "Port 2", model.PortVlans{Untagged: model.Ptr[uint16](1)}, false); err != nil {
		t.Fatal(err)
	}
	// Port 9 still carries 20 tagged (the path is left as it is): free it first.
	if _, err := coll.Apply(ctx, collector.ChangeRequest{
		NodeID: swID, Kind: model.ChangeKindPortVlans, Port: "Port 9", Value: model.PortVlans{Untagged: model.Ptr[uint16](1)}, Confirmed: true,
	}); err != nil {
		if err := coll.SetAllowDestructive(ctx, true); err != nil {
			t.Fatal(err)
		}
		if _, err := coll.Apply(ctx, collector.ChangeRequest{
			NodeID: swID, Kind: model.ChangeKindPortVlans, Port: "Port 9", Value: model.PortVlans{Untagged: model.Ptr[uint16](1)}, Confirmed: true,
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := coll.DeleteVlan(ctx, 20, false); err != nil {
		t.Fatal(err)
	}
	if list, _ := st.Vlans(ctx); slices.ContainsFunc(list, func(v store.Vlan) bool { return v.ID == 20 }) {
		t.Fatalf("still in the catalog: %+v", list)
	}
	if _, err := coll.DeleteVlan(ctx, 1, false); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("VLAN 1: %v", err)
	}
}

// sitey is a controller whose VLANs belong to the whole site: a change made
// through one switch changes them all, and it returns every switch.
type sitey struct {
	mu      sync.Mutex
	vlans   []model.Vlan
	applied []model.Change
}

func (*sitey) Info() integration.Info {
	return integration.Info{
		Type: "sitey", Name: "Sitey", Kind: integration.KindCore,
		Changes: []model.ChangeKind{model.ChangeKindVlanSet, model.ChangeKindVlanDelete},
	}
}

func (s *sitey) devices() []model.Device {
	out := []model.Device{}
	for i, m := range []model.MACAddress{"1c:2a:a3:00:00:21", "1c:2a:a3:00:00:22"} {
		out = append(out, model.Device{
			Key: string(m), Name: []string{"sw1", "sw2"}[i], Role: model.Ptr(model.DeviceRoleSwitch),
			MACs: []model.MACAddress{m}, Vlans: slices.Clone(s.vlans),
		})
	}
	return out
}

func (s *sitey) Collect(context.Context, integration.Config) ([]model.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.devices(), nil
}

func (*sitey) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (s *sitey) Apply(_ context.Context, _ integration.Config, c model.Change) ([]model.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.applied = append(s.applied, c)
	v := c.Value.(collector.VlanValue)
	s.vlans = slices.DeleteFunc(s.vlans, func(x model.Vlan) bool { return x.ID == v.ID })
	if c.Kind == model.ChangeKindVlanSet {
		s.vlans = append(s.vlans, model.Vlan{ID: v.ID, Name: model.Ptr(v.Name)})
	}
	return s.devices(), nil
}

// A site's VLAN is renamed and deleted once, through one of its switches,
// not again through every other switch of the site.
func TestASitesVLANChangesOnce(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	s := &sitey{vlans: []model.Vlan{{ID: 1}, {ID: 30, Name: model.Ptr("Lab")}}}
	reg.Register(s)
	coll := collector.New(st, reg, box, collector.Options{})
	if _, err := st.CreateIntegration(ctx, store.Integration{Name: "site", Type: "sitey", Enabled: true, Config: integration.Config{}}); err != nil {
		t.Fatal(err)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if _, done, err := coll.SaveVlan(ctx, store.Vlan{ID: 30, Name: "Lab 2"}, false); err != nil || len(done) != 1 || len(s.applied) != 1 {
		t.Fatalf("rename: %+v %v %+v", done, err, s.applied)
	}
	if done, err := coll.DeleteVlan(ctx, 30, false); err != nil || len(done) != 1 || len(s.applied) != 2 {
		t.Fatalf("delete: %+v %v %+v", done, err, s.applied)
	}
}
