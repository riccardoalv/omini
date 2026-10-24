package collector_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// router is an integration that answers with the ARP table the test sets.
type router struct {
	mu  sync.Mutex
	arp []model.ArpEntry
}

func (*router) Info() integration.Info {
	return integration.Info{Type: "router", Name: "Router", Kind: integration.KindPlugin, Fields: []model.FormField{
		{Key: "url", Type: model.FormFieldTypeURL},
	}}
}

func (r *router) Collect(context.Context, integration.Config) ([]model.Device, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return []model.Device{{Key: "aa:00:00:00:00:fe", Name: "gw", Role: model.Ptr(model.DeviceRoleRouter), Arp: r.arp}}, nil
}

func (*router) Test(context.Context, integration.Config) (string, error) { return "ok", nil }

// TestOfflineAlertTellsWhereDHCPMovedTheDevice: the router's leases were
// cleared and the switch got another address; its integration still has the
// old one. The alert says where the switch answers now.
func TestOfflineAlertTellsWhereDHCPMovedTheDevice(t *testing.T) {
	e := setup(t)
	gw := &router{}
	e.reg.Register(gw)
	e.addIntegration(t, "fake", integration.Config{"host": "192.168.1.96", "password": "x"})
	e.addIntegration(t, "router", integration.Config{"url": "https://192.168.1.1"})
	const mac = "1c:2a:a3:2f:57:f7"
	e.fake.set([]model.Device{{Key: mac, Name: "Horaco switch", Host: model.Ptr("192.168.1.96")}}, nil)
	gw.arp = []model.ArpEntry{{IP: "192.168.1.96", MAC: mac}}
	e.collect(t)

	e.fake.set(nil, errors.New("cannot connect to http://192.168.1.96: No route to host"))
	gw.mu.Lock()
	gw.arp = []model.ArpEntry{{IP: "192.168.1.110", MAC: mac}}
	gw.mu.Unlock()
	for range 2 {
		e.clock = e.clock.Add(time.Minute)
		e.collect(t)
	}
	alerts, err := e.st.ListAlerts(context.Background(), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range alerts {
		if a.Rule != "device_offline" {
			continue
		}
		if a.Params["moved_from"] != "192.168.1.96" || a.Params["moved_to"] != "192.168.1.110" {
			t.Fatalf("offline alert: %+v", a.Params)
		}
		return
	}
	t.Fatalf("no offline alert: %+v", alerts)
}
