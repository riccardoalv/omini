package collector_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/riccardoalv/omini/internal/collector"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

// Omini sets A records only: an AAAA record someone made for the device is
// shown with its names, and never deleted nor taken for Omini's own.
func TestDNSNamesKeepAAAARecords(t *testing.T) {
	coll, s, st := switchNetwork(t)
	ctx := context.Background()
	s.mu.Lock()
	s.devices[0].Ndp = []model.ArpEntry{{IP: "2001:db8:1::41", MAC: phoneMAC}}
	s.devices[0].DnsRecords = []model.DnsRecord{{Name: "tv.lan", IP: "2001:DB8:1::41"}}
	s.mu.Unlock()
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	phone, fw := "", ""
	for _, n := range coll.State().Topology.Nodes {
		switch n.MAC {
		case string(phoneMAC):
			phone = n.ID
			if !slices.Contains(n.IPs6, "2001:db8:1::41") {
				t.Fatalf("phone's IPv6 = %v", n.IPs6)
			}
		case string(fwMAC):
			fw = n.ID
		}
	}
	got, err := coll.DNSNameOf(ctx, phone)
	if err != nil || len(got.Others) != 1 || got.Others[0].Name != "tv.lan" {
		t.Fatalf("the AAAA record is not shown: %+v %v", got, err)
	}
	// Omini set tv.lan before; its A record is gone, the AAAA stays: turning
	// the name off has nothing of Omini's to delete.
	if err := st.SetSetting(ctx, "dns_applied:"+string(phoneMAC), fw+"|tv.lan"); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.SetDNSName(ctx, phone, "-"); err != nil {
		t.Fatal(err)
	}
	for _, c := range s.applied {
		if c.Kind == model.ChangeKindDnsDelete {
			t.Fatalf("asked to delete an AAAA record: %+v", c)
		}
	}
	if _, err := coll.Apply(ctx, collector.ChangeRequest{NodeID: fw, Kind: model.ChangeKindDnsDelete, Value: collector.DnsDelete{Name: "tv.lan"}}); !errors.Is(err, collector.ErrNotChangeable) {
		t.Fatalf("dns.delete of an AAAA record: %v", err)
	}
	// With the address fixed and a name, Omini sets its A record.
	if _, err := coll.Reserve(ctx, phone, "192.168.1.41"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateInventory(ctx, phone, store.InventoryUpdate{Alias: model.Ptr("tv")}); err != nil {
		t.Fatal(err)
	}
	if err := coll.CollectNow(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := coll.SetDNSName(ctx, phone, ""); err != nil {
		t.Fatal(err)
	}
	last := s.applied[len(s.applied)-1]
	if v, ok := last.Value.(model.DnsRecord); !ok || v.Name != "tv.lan" || v.IP != "192.168.1.41" {
		t.Fatalf("A record: %+v", last)
	}
}
