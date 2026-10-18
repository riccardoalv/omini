package collector_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/wifichan"
)

// chany is a controller of access points that moves their radios' channels.
type chany struct {
	mu      sync.Mutex
	aps     []model.Device
	applied []model.Change
	// refuse is the access point that refuses; stuck the one that says yes
	// but stays where it was.
	refuse, stuck string
}

func (*chany) Info() integration.Info {
	return integration.Info{Type: "chany", Name: "Chany", Kind: integration.KindCore, Changes: []model.ChangeKind{model.ChangeKindWifiChannel}}
}

func (a *chany) Collect(context.Context, integration.Config) ([]model.Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.aps), nil
}

func (*chany) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

func (a *chany) Apply(_ context.Context, _ integration.Config, c model.Change) ([]model.Device, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.applied = append(a.applied, c)
	if c.Device == a.refuse {
		return nil, errors.New("the access point did not answer")
	}
	v := c.Value.(model.RadioChange)
	for i, d := range a.aps {
		if d.Key != c.Device {
			continue
		}
		d.Radios = slices.Clone(d.Radios)
		for j := range d.Radios {
			if d.Radios[j].Name == v.Radio && c.Device != a.stuck {
				d.Radios[j].Channel = model.Ptr(v.Channel)
			}
		}
		a.aps[i] = d
		return []model.Device{d}, nil
	}
	return nil, errors.New("no such access point")
}

var b24 = model.WifiBandA24Ghz

func ap(mac, name string, ch uint8, clients ...model.MACAddress) model.Device {
	d := model.Device{
		Key: mac, Name: name, Role: model.Ptr(model.DeviceRoleAp), MACs: []model.MACAddress{model.MACAddress(mac)},
		Radios: []model.Radio{
			{Name: "wifi0", Band: b24, Channel: model.Ptr(ch), WidthMhz: model.Ptr(model.RadioWidthMhz(20))},
			{Name: "wifi1", Band: model.WifiBandA5Ghz, Channel: model.Ptr[uint8](36)},
		},
	}
	for _, m := range clients {
		d.WirelessClients = append(d.WirelessClients, model.WirelessClient{MAC: m, Band: &b24})
	}
	return d
}

const (
	hallMAC   = "f0:9f:c2:00:00:01"
	officeMAC = "f0:9f:c2:00:00:02"
	atticMAC  = "f0:9f:c2:00:00:03"
	laptopMAC = model.MACAddress("3c:22:fb:00:00:01")
)

// airNetwork is three access points on 6: the hall with three clients, the
// office with one, the attic with two (Omini's laptop among them when
// ominiOnAttic).
func airNetwork(t *testing.T, ominiOnAttic bool) (*collector.Collector, *chany, *store.Store) {
	t.Helper()
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	box, _ := secret.New(make([]byte, 32))
	reg := integration.NewRegistry()
	attic := []model.MACAddress{"3c:22:fb:00:00:09", laptopMAC}
	a := &chany{aps: []model.Device{
		ap(hallMAC, "hall", 6, "3c:22:fb:00:00:05", "3c:22:fb:00:00:06", "3c:22:fb:00:00:07"),
		ap(officeMAC, "office", 6, "3c:22:fb:00:00:08"),
		ap(atticMAC, "attic", 6, attic...),
	}}
	// The controller knows the laptop's address.
	a.aps[0].Arp = []model.ArpEntry{{IP: "192.168.1.50", MAC: laptopMAC}}
	reg.Register(a)
	self := collector.Self{MACs: []model.MACAddress{"bc:24:11:00:00:99"}}
	if ominiOnAttic {
		self.MACs = append(self.MACs, laptopMAC)
	}
	now := time.Unix(1_790_000_000, 0).UTC()
	var clock sync.Mutex
	coll := collector.New(st, reg, box, collector.Options{Self: self, Now: func() time.Time {
		clock.Lock()
		defer clock.Unlock()
		now = now.Add(time.Second)
		return now
	}})
	if _, err := st.CreateIntegration(ctx, store.Integration{Name: "air", Type: "chany", Enabled: true, Config: integration.Config{}}); err != nil {
		t.Fatal(err)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	return coll, a, st
}

func TestPlanAndApplyWifiChannels(t *testing.T) {
	coll, a, st := airNetwork(t, false)
	ctx := context.Background()
	plan := coll.PlanChannels("")
	if len(plan.Steps) != 2 || plan.Steps[0].NodeID != "dev:"+officeMAC || plan.Steps[0].To != 1 || plan.Steps[0].Clients != 1 ||
		plan.Steps[1].NodeID != "dev:"+atticMAC || plan.Steps[1].To != 11 || plan.Steps[0].Protected != nil {
		t.Fatalf("the hall stays, the quietest moves first: %+v", plan.Steps)
	}
	if alerts := coll.State().Alerts; !slices.ContainsFunc(alerts, func(x store.Alert) bool { return x.Rule == "wifi_channel_conflict" }) {
		t.Fatalf("the conflict is an alert: %+v", alerts)
	}
	res, err := coll.ApplyChannels(ctx, false)
	if err != nil || len(res) != 2 || res[0].Status != collector.StepDone || res[1].Status != collector.StepDone || res[0].Change == nil {
		t.Fatalf("applied: %+v %v", res, err)
	}
	if len(a.applied) != 2 || a.applied[0].Kind != model.ChangeKindWifiChannel ||
		a.applied[0].Value.(model.RadioChange) != (model.RadioChange{Radio: "wifi0", Channel: 1}) {
		t.Fatalf("one access point at a time: %+v", a.applied)
	}
	if left := coll.PlanChannels(""); len(left.Steps) != 0 {
		t.Fatalf("nothing left to move: %+v", left)
	}
	if hist, _ := st.ConfigChanges(ctx, "dev:"+officeMAC, 10); len(hist) != 1 || hist[0].Kind != "wifi.channel" {
		t.Fatalf("in the history: %+v", hist)
	}
}

func TestWifiChannelsStopAtTheFirstFailure(t *testing.T) {
	coll, a, _ := airNetwork(t, false)
	a.refuse = officeMAC
	res, err := coll.ApplyChannels(context.Background(), false)
	if err == nil || len(res) != 2 || res[0].Status != collector.StepFailed || res[1].Status != collector.StepNotRun {
		t.Fatalf("stopped: %+v %v", res, err)
	}
	if len(a.applied) != 1 {
		t.Fatalf("the attic was not tried: %+v", a.applied)
	}

	// A device that says yes but stays on its channel fails the check.
	coll, a, _ = airNetwork(t, false)
	a.stuck = atticMAC
	res, err = coll.ApplyChannels(context.Background(), false)
	if err == nil || res[0].Status != collector.StepDone || res[1].Status != collector.StepFailed {
		t.Fatalf("the first stays done, the second failed: %+v %v", res, err)
	}
}

// Omini's laptop is on the attic's Wi-Fi: moving it is destructive.
func TestWifiChannelOnOminisRadioIsDestructive(t *testing.T) {
	coll, a, _ := airNetwork(t, true)
	ctx := context.Background()
	plan := coll.PlanChannels("")
	if plan.Steps[1].Protected == nil || plan.Steps[1].Protected.Reason != "omini" || plan.Steps[0].Protected != nil {
		t.Fatalf("the attic is protected: %+v", plan.Steps)
	}
	var de *collector.DestructiveError
	if _, err := coll.ApplyChannels(ctx, true); !errors.As(err, &de) || de.Allowed {
		t.Fatalf("blocked by default: %v", err)
	}
	if len(a.applied) != 0 {
		t.Fatal("nothing applied before the check")
	}
	if err := coll.SetAllowDestructive(ctx, true); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.ApplyChannels(ctx, false); !errors.As(err, &de) || !de.Allowed {
		t.Fatalf("asked a second time: %v", err)
	}
	if res, err := coll.ApplyChannels(ctx, true); err != nil || len(res) != 2 {
		t.Fatalf("confirmed: %+v %v", res, err)
	}
}

// The browser asking is a client of the attic: the plan says so.
func TestWifiChannelPlanTellsTheBrowser(t *testing.T) {
	coll, _, _ := airNetwork(t, false)
	var ip string
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(laptopMAC) {
			ip = n.IP
		}
	}
	if ip != "192.168.1.50" {
		t.Fatalf("the laptop's address: %q", ip)
	}
	plan := coll.PlanChannels(ip)
	if !plan.Steps[1].Browser || plan.Steps[0].Browser {
		t.Fatalf("the attic has the browser: %+v", plan.Steps)
	}
}

// A mesh unit is never moved, not even through the change itself.
func TestAMeshUnitIsNeverMoved(t *testing.T) {
	coll, a, _ := airNetwork(t, false)
	ctx := context.Background()
	if err := coll.SetAllowDestructive(ctx, true); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.aps[2].WirelessUplink = model.Ptr(true)
	a.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	plan := coll.PlanChannels("")
	if len(plan.Skipped) != 1 || plan.Skipped[0].Skip != wifichan.SkipMesh || len(plan.Steps) != 2 {
		t.Fatalf("the attic is skipped, the hall moves around it: %+v", plan)
	}
	_, err := coll.Apply(ctx, collector.ChangeRequest{
		NodeID: "dev:" + atticMAC, Kind: model.ChangeKindWifiChannel, Value: map[string]any{"radio": "wifi0", "channel": 11}, Confirmed: true,
	})
	if !errors.Is(err, collector.ErrInvalidChange) {
		t.Fatalf("refused: %v", err)
	}
	for _, bad := range []map[string]any{
		{"radio": "wifi1", "channel": 40},
		{"radio": "wifi0", "channel": 14},
		{"radio": "nope", "channel": 1},
		{"channel": 1},
	} {
		if _, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: "dev:" + hallMAC, Kind: model.ChangeKindWifiChannel, Value: bad}); !errors.Is(err, collector.ErrInvalidChange) {
			t.Errorf("%v: %v", bad, err)
		}
	}
}
