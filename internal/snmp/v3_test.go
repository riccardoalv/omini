package snmp_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/demo"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

func TestV3Check(t *testing.T) {
	for _, tc := range []struct {
		v3   snmp.V3
		want string
	}{
		{snmp.V3{User: "omini", Auth: "sha256", AuthPass: "a", Priv: "aes", PrivPass: "b"}, ""},
		{snmp.V3{User: "omini", Auth: "none", Priv: "none"}, ""},
		{snmp.V3{User: "omini", Auth: "sha"}, ""},
		{snmp.V3{User: "", Auth: "sha"}, "user is required"},
		{snmp.V3{User: "omini", Auth: "rot13"}, "unknown SNMP v3 authentication"},
		{snmp.V3{User: "omini", Auth: "none", Priv: "aes"}, "privacy needs authentication"},
	} {
		err := tc.v3.Check()
		if (tc.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%+v: err = %v, want %q", tc.v3, err, tc.want)
		}
	}
}

// A host that only speaks v2c: the v3 user is tried first, then the communities.
func TestProbeTriesV3ThenCommunities(t *testing.T) {
	agent := snmptest.Start(t, "homelab", snmptest.FromDevice(demo.Network(time.Unix(1_790_000_000, 0))[1], snmptest.Options{}))
	tg := snmp.Target{
		Host: "127.0.0.1", Port: agent.Port, Timeout: 200 * time.Millisecond,
		V3: &snmp.V3{User: "omini", Auth: "sha", AuthPass: "authpass12", Priv: "aes", PrivPass: "privpass12"},
	}
	got, ok := snmp.Probe(context.Background(), tg, []string{"public", "homelab"})
	if !ok || got != "homelab" {
		t.Fatalf("probe = %q %v", got, ok)
	}
}
