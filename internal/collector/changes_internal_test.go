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

// A port's traffic sample carries its optical module's power and temperature.
func TestTrafficSamplesCarryTheOpticalModule(t *testing.T) {
	topo := topology.Topology{Nodes: []topology.Node{{
		ID: "dev:sw", Kind: topology.KindDevice, Online: true,
		Traffic: map[string]topology.Rate{"SFP 1": {RxBps: 10}, "Port 1": {RxBps: 5}},
		Device: &model.Device{Interfaces: []model.Interface{
			{Name: "SFP 1", Transceiver: &model.Transceiver{RxPowerDBM: model.Ptr(-9.5), TemperatureC: model.Ptr(41.0)}},
			{Name: "Port 1"},
		}},
	}}}
	for _, s := range trafficSamples(topo) {
		switch s.Iface {
		case "SFP 1":
			if s.RxPowerDBM == nil || *s.RxPowerDBM != -9.5 || s.TempC == nil || *s.TempC != 41 {
				t.Errorf("SFP 1: %+v", s)
			}
		case "Port 1":
			if s.RxPowerDBM != nil || s.TempC != nil {
				t.Errorf("Port 1: %+v", s)
			}
		}
	}
}
