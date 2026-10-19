package collector_test

import (
	"testing"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/topology"
)

// A whole round: the scan finds Jellyfin on a Docker host, Docker reports the
// Swarm service that publishes it (and one whose app the scan did not find).
func TestRoundShowsAContainerAsTheAppTheScanFound(t *testing.T) {
	e := setup(t)
	e.addIntegration(t, "fake", integration.Config{})
	mac := model.MACAddress("bc:24:11:8d:d7:e5")
	scan := model.Device{Key: "scan", Name: "scan", Hosts: []model.Host{{
		IP: "192.168.1.42", MAC: &mac, OpenPorts: []uint16{22, 8096},
		Web: []model.WebService{{Port: 8096, URL: "http://192.168.1.42:8096/", Title: model.Ptr("Jellyfin")}},
	}}}
	host := model.Device{Key: "docker:192.168.1.42", Name: "master", Host: model.Ptr("192.168.1.42"), IPs: []string{"192.168.1.42"}, Vendor: model.Ptr("Docker")}
	service := func(name, image string, port uint16) model.Device {
		return model.Device{
			Key: "docker:192.168.1.42:" + name, Name: name, Vendor: model.Ptr("Docker"),
			Container: &model.Container{
				Name: &name, Image: &image, Stack: model.Ptr("homelab"), State: model.Ptr("running"),
				Replicas: model.Ptr(uint64(1)), Running: model.Ptr(uint64(1)),
				Ports: []model.ContainerPort{{Port: port, Protocol: model.Ptr(model.ContainerPortProtocolTcp)}},
			},
			Services: []model.Service{{Name: "8096/tcp"}},
			Neighbors: []model.Neighbor{{
				LocalPort: "ingress", Protocol: model.Ptr(model.NeighborProtocolOther),
				RemoteName: model.Ptr("master"), RemoteIP: model.Ptr("192.168.1.42"), RemotePlatform: model.Ptr("Docker"),
			}},
		}
	}
	e.fake.set([]model.Device{
		scan, host, service("homelab_jellyfin", "lscr.io/linuxserver/jellyfin:latest", 8096),
		service("homelab_kuma", "louislam/uptime-kuma:1", 3001),
	}, nil)
	s := e.collect(t)

	var apps []topology.Node
	for _, n := range s.Topology.Nodes {
		if n.Device != nil && n.Device.Container != nil {
			t.Errorf("a published container is no device node: %s", n.ID)
		}
		if n.Kind == topology.KindApp {
			apps = append(apps, n)
		}
	}
	if len(apps) != 2 {
		t.Fatalf("want Jellyfin and Uptime Kuma as apps, got %+v", apps)
	}
	for _, a := range apps {
		if a.Container == nil || a.IP != "192.168.1.42" {
			t.Errorf("app %s: container %+v, ip %q", a.Label, a.Container, a.IP)
		}
		switch a.Product {
		case "jellyfin":
			if model.Deref(a.Container.Name) != "homelab_jellyfin" || a.Web[0].URL != "http://192.168.1.42:8096/" {
				t.Errorf("jellyfin app = %+v", a)
			}
		case "uptime-kuma":
			if a.Label != "Uptime Kuma" || a.Web[0].URL != "http://192.168.1.42:3001/" {
				t.Errorf("kuma app = %+v", a)
			}
		default:
			t.Errorf("unexpected app %+v", a)
		}
	}
	if apps[0].ParentID != apps[1].ParentID || apps[0].ParentID == "" {
		t.Errorf("both apps hang from the host: %q, %q", apps[0].ParentID, apps[1].ParentID)
	}
}
