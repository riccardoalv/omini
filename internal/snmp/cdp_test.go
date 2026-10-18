package snmp_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
	"github.com/riccardoalv/omini/internal/snmp/snmptest"
)

// TestCDPNeighbors: CISCO-CDP-MIB's cache gives the neighbors of the ports
// LLDP is silent on (IPv4 or IPv6 address, port, platform, capabilities);
// a port LLDP already names keeps only its LLDP neighbor.
func TestCDPNeighbors(t *testing.T) {
	lldp, cdp := model.Ptr(model.NeighborProtocolLldp), model.Ptr(model.NeighborProtocolCdp)
	sw := model.Device{
		Key: "aa:bb:cc:00:00:01", Name: "sw",
		Interfaces: []model.Interface{{Name: "Gi1/0/1"}, {Name: "Gi1/0/2"}, {Name: "Gi1/0/3"}},
		Neighbors: []model.Neighbor{
			{LocalPort: "Gi1/0/1", Protocol: lldp, RemoteMAC: model.Ptr(model.MACAddress("aa:bb:cc:00:00:02")), RemoteName: model.Ptr("ap1"), RemotePort: model.Ptr("eth0")},
			{LocalPort: "Gi1/0/1", Protocol: cdp, RemoteName: model.Ptr("ap1.cdp"), RemotePort: model.Ptr("GigabitEthernet0")},
			{
				LocalPort: "Gi1/0/2", Protocol: cdp, RemoteName: model.Ptr("SEP001122334455"), RemotePort: model.Ptr("Port 1"),
				RemotePlatform: model.Ptr("Cisco IP Phone 7841"), RemoteIP: model.Ptr("10.0.0.20"),
				Capabilities: []model.NeighborCapability{model.NeighborCapabilityStation, model.NeighborCapabilityTelephone},
			},
			{
				LocalPort: "Gi1/0/3", Protocol: cdp, RemoteName: model.Ptr("rtr1.example"), RemotePort: model.Ptr("Gi0/0/1"),
				RemotePlatform: model.Ptr("cisco ISR4331/K9"), RemoteIP: model.Ptr("2001:db8::1"),
				Capabilities: []model.NeighborCapability{model.NeighborCapabilityRouter, model.NeighborCapabilityBridge},
			},
		},
	}
	agent := snmptest.Start(t, "public", snmptest.FromDevice(sw, snmptest.Options{}))
	d, err := snmp.Collect(context.Background(), target(agent, "public"))
	if err != nil {
		t.Fatal(err)
	}
	want := []model.Neighbor{sw.Neighbors[0], sw.Neighbors[2], sw.Neighbors[3]}
	want[2].Capabilities = []model.NeighborCapability{model.NeighborCapabilityRouter, model.NeighborCapabilityBridge}
	if !reflect.DeepEqual(d.Neighbors, want) {
		g, _ := json.MarshalIndent(d.Neighbors, "", "  ")
		w, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("neighbors\n--- got\n%s\n--- want\n%s", g, w)
	}
}
