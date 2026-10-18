package collector_test

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/model"
)

func TestMagicPacket(t *testing.T) {
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	p := collector.MagicPacket(mac)
	if len(p) != 102 || p[0] != 0xff || p[5] != 0xff || p[6] != 0xaa || p[101] != 0xff || p[96] != 0xaa {
		t.Fatalf("packet: % x", p)
	}
}

// On Omini's own network the packet goes out from here; elsewhere the
// router sends it; with neither there is no way.
func TestWakingADevice(t *testing.T) {
	var sent []netip.Prefix
	local := []netip.Prefix{}
	coll, s, st := switchNetworkWith(t, func(o *collector.Options) {
		o.LocalNets = func() []netip.Prefix { return local }
		o.SendMagic = func(p netip.Prefix, _ net.HardwareAddr) error {
			sent = append(sent, p)
			return nil
		}
	})
	ctx := context.Background()
	phone := ""
	for _, n := range coll.State().Topology.Nodes {
		if n.MAC == string(phoneMAC) {
			phone = n.ID
		}
	}
	// Not on Omini's network: the router sends it.
	if via := coll.CanWake(ctx, phone); via.Via != "router" {
		t.Fatalf("via: %+v", via)
	}
	if _, err := coll.Wake(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.applied[len(s.applied)-1].Value.(collector.WolValue); !ok || v.MAC != phoneMAC || v.IP != "192.168.1.41" {
		t.Fatalf("router: %+v", s.applied)
	}
	// On Omini's network: from here, kept in the device's history.
	local = []netip.Prefix{netip.MustParsePrefix("192.168.1.3/24")}
	n := len(s.applied)
	if _, err := coll.Wake(ctx, phone); err != nil || len(s.applied) != n || len(sent) != 1 || sent[0].String() != "192.168.1.3/24" {
		t.Fatalf("local: %v %v", err, sent)
	}
	// Both are in the device's history; the one the router sent is in the
	// router's too.
	fw := "dev:" + string(fwMAC)
	if h, err := st.ConfigChanges(ctx, phone, 5); err != nil || len(h) != 2 || h[0].NodeID != phone ||
		h[1].NodeID != fw || h[1].TargetID != phone || h[1].Kind != string(model.ChangeKindWolSend) {
		t.Fatalf("history: %+v %v", h, err)
	}
	if h, err := st.ConfigChanges(ctx, fw, 5); err != nil || len(h) != 1 || h[0].TargetID != phone {
		t.Fatalf("router's history: %+v %v", h, err)
	}
	if _, err := coll.Wake(ctx, "no-such-node"); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("unknown: %v", err)
	}
}

// A PC off overnight has left the map (offline for over an hour) but not the
// inventory: it can still be woken, with its last MAC and address.
func TestWakingADeviceOffTheMap(t *testing.T) {
	var clock time.Time
	coll, s, _ := switchNetworkWith(t, func(o *collector.Options) {
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
	s.mu.Lock()
	s.devices[0].Arp = []model.ArpEntry{{IP: "192.168.1.2", MAC: swMAC}}
	s.devices[1].Fdb = slices.DeleteFunc(slices.Clone(s.devices[1].Fdb), func(f model.FdbEntry) bool { return f.MAC == phoneMAC })
	s.mu.Unlock()
	clock = time.Now().Add(collector.OfflineVisible + 2*time.Hour)
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	for _, n := range coll.State().Topology.Nodes {
		if n.ID == phone {
			t.Fatalf("still on the map: %+v", n)
		}
	}
	if via := coll.CanWake(ctx, phone); via.Via != "router" {
		t.Fatalf("via: %+v", via)
	}
	if _, err := coll.Wake(ctx, phone); err != nil {
		t.Fatal(err)
	}
	if v, ok := s.applied[len(s.applied)-1].Value.(collector.WolValue); !ok || v.MAC != phoneMAC || v.IP != "192.168.1.41" {
		t.Fatalf("router: %+v", s.applied)
	}
}
