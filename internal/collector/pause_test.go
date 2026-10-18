package collector_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

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
	// A device blocked by a rule set by hand on the router is never rewritten.
	s.mu.Lock()
	fw := s.devices[0]
	fw.Paused = append(slices.Clone(fw.Paused), model.PausedDevice{MAC: swMAC, IPs: []string{"192.168.1.200"}, Rule: model.Ptr("Block the switch")})
	s.devices[0] = fw
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	n = len(s.applied)
	coll.AfterRound(ctx)
	if len(s.applied) != n {
		t.Fatalf("a manual block was rewritten: %+v", s.applied[n:])
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

// A router that keeps refusing to follow a paused device's new address is
// asked again less and less often, and its refusal is kept in the history
// once, not every round.
func TestARefusedRetryBacksOff(t *testing.T) {
	var clock time.Time
	coll, s, st := switchNetworkWith(t, func(o *collector.Options) {
		now := o.Now
		o.Now = func() time.Time {
			if !clock.IsZero() {
				return clock
			}
			return now()
		}
	})
	ctx := context.Background()
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	if _, err := coll.Pause(ctx, phone); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.devices[0].Arp = []model.ArpEntry{{IP: "192.168.1.2", MAC: swMAC}, {IP: "192.168.1.77", MAC: phoneMAC}}
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.err = errors.New("the router refused")
	s.mu.Unlock()
	clock = time.Now()
	n := len(s.applied)
	coll.AfterRound(ctx)
	coll.AfterRound(ctx) // a round later: not asked again yet
	if got := len(s.applied) - n; got != 1 {
		t.Fatalf("asked %d times, want 1", got)
	}
	clock = clock.Add(time.Hour) // past the wait
	coll.AfterRound(ctx)
	if got := len(s.applied) - n; got != 2 {
		t.Fatalf("asked %d times after the wait, want 2", got)
	}
	failed := 0
	h, _ := st.ConfigChanges(ctx, phone, 50)
	for _, c := range h {
		if c.Error != "" {
			failed++
		}
	}
	if failed != 1 {
		t.Fatalf("%d failed entries in the history, want 1: %+v", failed, h)
	}
}
