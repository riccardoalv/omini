package netscan

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

func TestIGDFromTheDescription(t *testing.T) {
	body, err := os.ReadFile("testdata/igd.xml")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := parseDescription(body, "http://192.168.1.1:5000/rootDesc.xml")
	if !ok {
		t.Fatal("description not parsed")
	}
	want := igd{Control: "http://192.168.1.1:5000/ctl/IPConn", Service: "urn:schemas-upnp-org:service:WANIPConnection:1"}
	if a.IGD != want || a.Manufacturer != "OpenWrt" || !reflect.DeepEqual(a.Services, []string{"upnp:InternetGatewayDevice"}) {
		t.Fatalf("announce = %+v", a)
	}

	// A control URL on another host is not followed.
	other := strings.Replace(string(body), "<controlURL>/ctl/IPConn", "<controlURL>http://203.0.113.9/ctl/IPConn", 1)
	if a, _ := parseDescription([]byte(other), "http://192.168.1.1:5000/rootDesc.xml"); a.IGD != (igd{}) {
		t.Errorf("control URL elsewhere: %+v", a.IGD)
	}
	// A TV has no port mappings.
	if a, _ := parseDescription([]byte(tvDescription), "http://192.168.1.50:9197/dmr"); a.IGD != (igd{}) {
		t.Errorf("TV: %+v", a.IGD)
	}
}

const soapFault = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault>` +
	`<faultcode>s:Client</faultcode><faultstring>UPnPError</faultstring><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0">` +
	`<errorCode>713</errorCode><errorDescription>SpecifiedArrayIndexInvalid</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`

// igdServer is a gateway with these mappings (protocol, external port,
// client, internal port, enabled, description), answering 713 past the last
// one; endless answers the same mapping at every index.
func igdServer(t *testing.T, mappings [][6]string, endless bool) (*httptest.Server, *int) {
	t.Helper()
	asked := new(int)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/ctl/IPConn" ||
			r.Header.Get("SOAPAction") != `"urn:schemas-upnp-org:service:WANIPConnection:1#GetGenericPortMappingEntry"` {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		*asked++
		b, _ := io.ReadAll(r.Body)
		var i int
		s := string(b)
		if k := strings.Index(s, "<NewPortMappingIndex>"); k >= 0 {
			_, _ = fmt.Sscanf(s[k+len("<NewPortMappingIndex>"):], "%d", &i)
		}
		if endless {
			i = 0
		}
		if i >= len(mappings) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(soapFault))
			return
		}
		m := mappings[i]
		fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body>`+
			`<u:GetGenericPortMappingEntryResponse xmlns:u="urn:schemas-upnp-org:service:WANIPConnection:1">`+
			`<NewRemoteHost></NewRemoteHost><NewExternalPort>%s</NewExternalPort><NewProtocol>%s</NewProtocol>`+
			`<NewInternalPort>%s</NewInternalPort><NewInternalClient>%s</NewInternalClient><NewEnabled>%s</NewEnabled>`+
			`<NewPortMappingDescription>%s</NewPortMappingDescription><NewLeaseDuration>0</NewLeaseDuration>`+
			`</u:GetGenericPortMappingEntryResponse></s:Body></s:Envelope>`, m[1], m[0], m[3], m[2], m[4], m[5])
	}))
	t.Cleanup(srv.Close)
	return srv, asked
}

func TestPortMappings(t *testing.T) {
	srv, asked := igdServer(t, [][6]string{
		{"TCP", "32400", "192.168.1.20", "32400", "1", "Plex Media Server"},
		{"UDP", "3074", "192.168.1.30", "3074", "0", "Xbox"},
		{"ICMP", "1", "192.168.1.30", "1", "1", "not a port"},
	}, false)
	g := igd{Control: srv.URL + "/ctl/IPConn", Service: "urn:schemas-upnp-org:service:WANIPConnection:1"}
	got := portMappings(context.Background(), srv.Client(), g)
	yes := true
	no := false
	want := []model.PortForward{
		{
			Protocol: model.PortForwardProtocolTcp, ExternalPort: 32400, InternalIP: model.Ptr("192.168.1.20"),
			InternalPort: model.Ptr(uint16(32400)), Description: model.Ptr("Plex Media Server"), Enabled: &yes, Upnp: &yes,
		},
		{
			Protocol: model.PortForwardProtocolUdp, ExternalPort: 3074, InternalIP: model.Ptr("192.168.1.30"),
			InternalPort: model.Ptr(uint16(3074)), Description: model.Ptr("Xbox"), Enabled: &no, Upnp: &yes,
		},
	}
	if !reflect.DeepEqual(got, want) {
		g, _ := json.Marshal(got)
		t.Fatalf("mappings = %s", g)
	}
	if *asked != 4 { // three mappings and the error past them
		t.Errorf("asked %d times", *asked)
	}

	// A gateway that never says it is done is read up to the cap.
	srv, asked = igdServer(t, [][6]string{{"TCP", "80", "192.168.1.2", "80", "1", "web"}}, true)
	g.Control = srv.URL + "/ctl/IPConn"
	if got := portMappings(context.Background(), srv.Client(), g); len(got) != MaxPortMappings || *asked != MaxPortMappings {
		t.Errorf("endless gateway: %d mappings, %d requests", len(got), *asked)
	}
}

// TestGatewayGetsItsPortMappings: the scan's gateway device carries the
// mappings of the IGD service SSDP found on it.
func TestGatewayGetsItsPortMappings(t *testing.T) {
	srv, _ := igdServer(t, [][6]string{{"TCP", "8443", "127.0.0.2", "443", "1", "nas"}}, false)
	gw := netip.MustParseAddr("127.0.0.1")
	s := &Integration{
		ARPPath: "testdata/arp", RoutePath: "testdata/route", Ports: []int{}, SettleTime: time.Millisecond,
		NoPing: true, NoWebTitles: true, SNMPTimeout: 100 * time.Millisecond,
		Locals: func() []localNet { return nil },
		ssdp: func(context.Context, time.Duration, func(netip.Addr) bool) map[netip.Addr]announce {
			return map[netip.Addr]announce{gw: {
				Services: []string{"upnp:InternetGatewayDevice"},
				IGD:      igd{Control: srv.URL + "/ctl/IPConn", Service: "urn:schemas-upnp-org:service:WANIPConnection:1"},
			}}
		},
	}
	devices, err := s.Collect(context.Background(), integration.Config{
		"subnets": "127.0.0.0/30", "mdns": false, "snmp": false, "dns": false, "netbios": false,
		"dhcp": false, "dhcp_check": false, "dns_check": false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].Key != "gw:127.0.0.1" {
		t.Fatalf("devices = %+v", devices)
	}
	f := devices[0].PortForwards
	if len(f) != 1 || f[0].ExternalPort != 8443 || model.Deref(f[0].InternalIP) != "127.0.0.2" || !model.Deref(f[0].Upnp) {
		t.Errorf("port forwards = %+v", f)
	}
}
