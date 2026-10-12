package collector_test

import (
	"context"
	"errors"
	"testing"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/model"
)

// A client's internet is paused on its router and given back; a paused
// device that gets another address is followed; network gear and Omini's
// host are never paused.
func TestPausingADevice(t *testing.T) {
	coll, s, _ := switchNetwork(t)
	ctx := context.Background()
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	if r, ok := coll.CanPause(phone); !ok || r != "dev:"+string(fwMAC) {
		t.Fatalf("can pause: %q %v", r, ok)
	}
	if _, err := coll.Pause(ctx, phone); err != nil {
		t.Fatal(err)
	}
	v, ok := s.applied[len(s.applied)-1].Value.(model.PausedDevice)
	if !ok || v.MAC != phoneMAC || len(v.IPs) != 1 || v.IPs[0] != "192.168.1.41" {
		t.Fatalf("paused: %+v", s.applied)
	}
	// Nothing changed: the round sends nothing.
	n := len(s.applied)
	coll.AfterRound(ctx)
	if len(s.applied) != n {
		t.Fatalf("nothing to follow: %+v", s.applied[n:])
	}
	// The phone got another address: the router blocks the new one.
	s.mu.Lock()
	s.devices[0].Arp = []model.ArpEntry{{IP: "192.168.1.2", MAC: swMAC}, {IP: "192.168.1.77", MAC: phoneMAC}}
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	coll.AfterRound(ctx)
	if v, ok := s.applied[len(s.applied)-1].Value.(model.PausedDevice); !ok || v.IPs[0] != "192.168.1.77" {
		t.Fatalf("followed: %+v", s.applied[n:])
	}
	if _, err := coll.Resume(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.applied[len(s.applied)-1].Value.(collector.MACValue); !ok {
		t.Fatalf("resumed: %+v", s.applied[len(s.applied)-1])
	}
	if _, err := coll.Resume(ctx, phone); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("again: %v", err)
	}
	for _, id := range []string{"dev:" + string(swMAC), "dev:" + string(fwMAC)} {
		if _, err := coll.Pause(ctx, id); !errors.Is(err, collector.ErrNotChangeable) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	omini := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(ominiMAC) {
			omini = n.ID
		}
	}
	if _, err := coll.Pause(ctx, omini); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("omini: %v", err)
	}
}
