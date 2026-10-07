package snmp

import (
	"context"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func TestExpand(t *testing.T) {
	cases := map[string]int{"192.168.1.0/24": 254, "192.168.1.77/24": 254, "10.0.0.0/30": 2, "10.0.0.5/32": 1, "10.0.0.0/22": 1022}
	for cidr, want := range cases {
		hosts, err := expand(cidr)
		if err != nil || len(hosts) != want {
			t.Errorf("expand(%s) = %d hosts, %v; want %d", cidr, len(hosts), err, want)
		}
	}
	for _, bad := range []string{"10.0.0.0/16", "nonsense", "fd00::/120"} {
		if _, err := expand(bad); err == nil {
			t.Errorf("expand(%s) should fail", bad)
		}
	}
}

func TestDiscover(t *testing.T) {
	d := model.Device{Key: "x", Name: "sw-core", Model: model.Ptr("Horaco 8x2.5G\nfirmware 1.2")}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(d, snmptest.Options{Enterprise: 11863}))

	found, err := Discover(context.Background(), "127.0.0.1/32", ScanOptions{Port: agent.Port, Timeout: 300 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	want := Found{IP: "127.0.0.1", Name: "sw-core", Description: "Horaco 8x2.5G", Vendor: "TP-Link"}
	if len(found) != 1 || found[0] != want {
		t.Fatalf("found = %+v, want [%+v]", found, want)
	}

	found, _ = Discover(context.Background(), "127.0.0.1/32", ScanOptions{Port: agent.Port, Community: "wrong", Timeout: 200 * time.Millisecond})
	if len(found) != 0 {
		t.Fatalf("wrong community should find nothing, got %+v", found)
	}
}
