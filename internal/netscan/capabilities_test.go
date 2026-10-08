package netscan

import (
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBridgeOnly(t *testing.T) {
	p := netip.MustParsePrefix
	for name, c := range map[string]struct {
		nets map[string][]netip.Prefix
		want bool
	}{
		"docker bridge":        {map[string][]netip.Prefix{"eth0": {p("172.17.0.0/16")}}, true},
		"user bridge network":  {map[string][]netip.Prefix{"eth0": {p("172.20.0.0/16")}}, true},
		"host network":         {map[string][]netip.Prefix{"eno1": {p("192.168.1.0/24")}, "docker0": {p("172.17.0.0/16")}}, false},
		"macvlan on the LAN":   {map[string][]netip.Prefix{"eth0": {p("192.168.1.0/24")}}, false},
		"a LAN that is 172.16": {map[string][]netip.Prefix{"eno1": {p("172.16.5.0/24")}, "wlan0": {p("10.0.0.0/24")}}, false},
	} {
		if got := bridgeOnly(c.nets); got != c.want {
			t.Errorf("%s: %v, want %v", name, got, c.want)
		}
	}
}

func TestInContainer(t *testing.T) {
	root := t.TempDir() + "/"
	if inContainer(root) {
		t.Fatal("empty root is not a container")
	}
	_ = os.MkdirAll(filepath.Join(root, "proc/1"), 0o755)
	_ = os.WriteFile(filepath.Join(root, "proc/1/cgroup"), []byte("0::/system.slice/docker-abc.scope\n"), 0o644)
	if !inContainer(root) {
		t.Fatal("docker cgroup not seen")
	}
	_ = os.WriteFile(filepath.Join(root, ".dockerenv"), nil, 0o644)
	if !inContainer(root) {
		t.Fatal(".dockerenv not seen")
	}
}

func TestLimited(t *testing.T) {
	if got := (Capabilities{HostNetwork: true, Multicast: true, UnprivilegedPing: true}).Limited(); got != nil {
		t.Fatalf("all fine: %v", got)
	}
	got := (Capabilities{Container: true, RawSockets: true}).Limited()
	if !reflect.DeepEqual(got, []string{"host_network", "multicast"}) {
		t.Fatalf("bridge: %v", got)
	}
	// Raw sockets (root) ping too.
	if got := (Capabilities{HostNetwork: true, Multicast: true}).Limited(); !reflect.DeepEqual(got, []string{"ping"}) {
		t.Fatalf("no ping: %v", got)
	}
}

func TestDetectCapabilitiesRuns(t *testing.T) {
	c := DetectCapabilities()
	t.Logf("here: %+v", c)
}
