package collector_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
)

// airy is a Wi-Fi controller: two access points of one site.
type airy struct {
	mu      sync.Mutex
	wifi    []model.WifiNetwork
	applied []model.Change
}

func (*airy) Info() integration.Info {
	return integration.Info{
		Type: "airy", Name: "Airy", Kind: integration.KindCore,
		Changes: []model.ChangeKind{model.ChangeKindWifiSet, model.ChangeKindWifiDelete},
	}
}

func (a *airy) devices() []model.Device {
	out := []model.Device{}
	for i, m := range []model.MACAddress{"0c:ea:14:00:00:01", "0c:ea:14:00:00:02"} {
		out = append(out, model.Device{
			Key: string(m), Name: []string{"ap1", "ap2"}[i], Role: model.Ptr(model.DeviceRoleAp),
			MACs: []model.MACAddress{m}, WifiNetworks: slices.Clone(a.wifi),
		})
	}
	return out
}

func (a *airy) Collect(context.Context, integration.Config) ([]model.Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.devices(), nil
}

func (*airy) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (a *airy) Apply(_ context.Context, _ integration.Config, c model.Change) ([]model.Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applied = append(a.applied, c)
	switch v := c.Value.(type) {
	case model.WifiChange:
		name := model.Deref(v.Previous)
		if name == "" {
			name = v.SSID
		}
		a.wifi = slices.DeleteFunc(a.wifi, func(w model.WifiNetwork) bool { return w.SSID == name })
		a.wifi = append(a.wifi, model.WifiNetwork{SSID: v.SSID, Vlan: v.Vlan, Enabled: v.Enabled})
	case collector.WifiDelete:
		a.wifi = slices.DeleteFunc(a.wifi, func(w model.WifiNetwork) bool { return w.SSID == v.SSID })
	}
	return a.devices(), nil
}

func wifiNetwork(t *testing.T) (*collector.Collector, *airy, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	a := &airy{wifi: []model.WifiNetwork{{SSID: "Home", Enabled: model.Ptr(true)}}}
	reg.Register(a)
	coll := collector.New(st, reg, box, collector.Options{})
	if _, err := st.CreateIntegration(ctx, store.Integration{Name: "airy", Type: "airy", Enabled: true, Config: integration.Config{}}); err != nil {
		t.Fatal(err)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	return coll, a, st
}

// A controller changes its whole site: one change, not one per access point;
// the password reaches it, and never the history.
func TestAVLANsWifi(t *testing.T) {
	coll, a, st := wifiNetwork(t)
	ctx := context.Background()
	w := model.WifiChange{SSID: "Guests", Vlan: model.Ptr[uint16](80), Password: model.Ptr("correct horse"), Enabled: model.Ptr(true)}
	done, err := coll.SetWifi(ctx, w)
	if err != nil || len(done) != 1 || len(a.applied) != 1 {
		t.Fatalf("set: %+v %v", done, err)
	}
	if got := a.applied[0].Value.(model.WifiChange); model.Deref(got.Password) != "correct horse" {
		t.Fatalf("the device got no password: %+v", got)
	}
	if strings.Contains(string(done[0].After), "horse") {
		t.Fatalf("the history kept the password: %s", done[0].After)
	}
	list, _ := st.ConfigChanges(ctx, done[0].NodeID, 10)
	for _, c := range list {
		if strings.Contains(string(c.After), "horse") {
			t.Fatalf("stored: %s", c.After)
		}
	}
	// A new network needs a password; renaming one keeps it.
	if _, err := coll.SetWifi(ctx, model.WifiChange{SSID: "Lab"}); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("no password: %v", err)
	}
	if _, err := coll.SetWifi(ctx, model.WifiChange{SSID: "Visitors", Previous: model.Ptr("Guests"), Vlan: model.Ptr[uint16](80)}); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.SetWifi(ctx, model.WifiChange{SSID: "x", Password: model.Ptr("short")}); !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("short password: %v", err)
	}
	if done, err := coll.DeleteWifi(ctx, "Visitors"); err != nil || len(done) != 1 {
		t.Fatalf("delete: %+v %v", done, err)
	}
	if slices.ContainsFunc(a.wifi, func(w model.WifiNetwork) bool { return w.SSID == "Visitors" }) {
		t.Fatalf("still there: %+v", a.wifi)
	}
}

// stubborn is another site's access point that refuses every Wi-Fi change
// (first on the map: its MAC sorts first).
type stubborn struct{ airy }

func (*stubborn) Info() integration.Info {
	i := (&airy{}).Info()
	i.Type, i.Name = "stubborn", "Stubborn"
	return i
}

func (s *stubborn) devices() []model.Device {
	return []model.Device{{
		Key: "0c:ea:14:00:00:00", Name: "ap9", Role: model.Ptr(model.DeviceRoleAp),
		MACs: []model.MACAddress{"0c:ea:14:00:00:00"}, WifiNetworks: []model.WifiNetwork{{SSID: "Shop"}},
	}}
}

func (s *stubborn) Collect(context.Context, integration.Config) ([]model.Device, error) {
	return s.devices(), nil
}

func (*stubborn) Apply(context.Context, integration.Config, model.Change) ([]model.Device, error) {
	return nil, errors.New("no interface on VLAN 80")
}

// A Wi-Fi change is a plan like any other: the first access point that
// refuses stops it, and the error says which.
func TestTheFirstRefusalStopsTheWifiChange(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	a := &airy{}
	reg.Register(&stubborn{})
	reg.Register(a)
	a.wifi = []model.WifiNetwork{{SSID: "Home"}}
	coll := collector.New(st, reg, box, collector.Options{})
	for _, typ := range []string{"stubborn", "airy"} {
		if _, err := st.CreateIntegration(ctx, store.Integration{Name: typ, Type: typ, Enabled: true, Config: integration.Config{}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	done, err := coll.SetWifi(ctx, model.WifiChange{SSID: "Guests", Vlan: model.Ptr[uint16](80), Password: model.Ptr("correct horse")})
	if err == nil || !strings.Contains(err.Error(), "ap9") || len(a.applied) != 0 {
		t.Fatalf("err %v, applied %d, done %+v", err, len(a.applied), done)
	}
	a.wifi = append(a.wifi, model.WifiNetwork{SSID: "Shop"}) // both have it now
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.DeleteWifi(ctx, "Shop"); err == nil || !strings.Contains(err.Error(), "ap9") || len(a.applied) != 0 {
		t.Fatalf("delete: err %v, applied %d", err, len(a.applied))
	}
}
