package collector

import (
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// The collector remembers when each VPN peer was last connected, for a day.
func TestObserveChangesRemembersVPNPeers(t *testing.T) {
	c := &Collector{}
	now := time.Unix(1_790_000_000, 0).UTC()
	topo := func(connected bool) topology.Topology {
		return topology.Topology{Nodes: []topology.Node{{
			ID: "dev:fw", Kind: topology.KindDevice, Online: true,
			Device: &model.Device{VpnPeers: []model.VpnPeer{{Name: "office", Connected: model.Ptr(connected)}}},
		}}}
	}
	c.observeChanges(topo(true), now)
	h := c.observeChanges(topo(false), now.Add(time.Hour))
	if at := h.VPNConnected["dev:fw|office"]; !at.Equal(now) {
		t.Fatalf("last connected: %v", at)
	}
	if h := c.observeChanges(topo(false), now.Add(25*time.Hour)); len(h.VPNConnected) != 0 {
		t.Fatalf("kept after a day: %v", h.VPNConnected)
	}
}
