package netscan

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"golang.org/x/net/dns/dnsmessage"
)

// mdnsAnswer builds the kind of reply an Apple TV sends about itself.
func mdnsAnswer(t *testing.T) []byte {
	t.Helper()
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{Response: true, Authoritative: true})
	_ = b.StartAnswers()
	hdr := func(name string, typ dnsmessage.Type) dnsmessage.ResourceHeader {
		return dnsmessage.ResourceHeader{Name: dnsmessage.MustNewName(name), Type: typ, Class: dnsmessage.ClassINET, TTL: 120}
	}
	_ = b.PTRResource(hdr("_airplay._tcp.local.", dnsmessage.TypePTR),
		dnsmessage.PTRResource{PTR: dnsmessage.MustNewName("Living Room._airplay._tcp.local.")})
	_ = b.PTRResource(hdr("_services._dns-sd._udp.local.", dnsmessage.TypePTR),
		dnsmessage.PTRResource{PTR: dnsmessage.MustNewName("_raop._tcp.local.")})
	_ = b.StartAdditionals()
	_ = b.SRVResource(hdr("Living Room._airplay._tcp.local.", dnsmessage.TypeSRV),
		dnsmessage.SRVResource{Port: 7000, Target: dnsmessage.MustNewName("Living-Room.local.")})
	_ = b.AResource(hdr("Living-Room.local.", dnsmessage.TypeA), dnsmessage.AResource{A: [4]byte{192, 168, 1, 40}})
	_ = b.TXTResource(hdr("Living Room._device-info._tcp.local.", dnsmessage.TypeTXT),
		dnsmessage.TXTResource{TXT: []string{"model=AppleTV11,1", "osxvers=21"}})
	_ = b.AAAAResource(hdr("Living-Room.local.", dnsmessage.TypeAAAA), dnsmessage.AAAAResource{})
	msg, err := b.Finish()
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestParseMDNS(t *testing.T) {
	a, ok := parseMDNS(mdnsAnswer(t))
	if !ok {
		t.Fatal("answer not parsed")
	}
	if !reflect.DeepEqual(a.Services, []string{"_airplay._tcp", "_raop._tcp"}) {
		t.Errorf("services = %v", a.Services)
	}
	if !reflect.DeepEqual(a.Hostnames, []string{"Living-Room.local"}) {
		t.Errorf("hostnames = %v", a.Hostnames)
	}
	if a.Model != "AppleTV11,1" {
		t.Errorf("model = %q", a.Model)
	}
	if _, ok := parseMDNS(mdnsQuery()); ok {
		t.Error("queries are not answers")
	}
}

func TestServiceType(t *testing.T) {
	cases := map[string]string{
		"Living Room._airplay._tcp.local": "_airplay._tcp",
		"_googlecast._tcp.local":          "_googlecast._tcp",
		"_sub._printer._tcp.local":        "_printer._tcp",
		"Living-Room.local":               "",
	}
	for in, want := range cases {
		if got := serviceType(in); got != want {
			t.Errorf("serviceType(%q) = %q, want %q", in, got, want)
		}
	}
}

const tvDescription = `<?xml version="1.0"?>
<root xmlns="urn:schemas-upnp-org:device-1-0">
  <device>
    <deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType>
    <friendlyName>[TV] Samsung Q60 Series (55)</friendlyName>
    <manufacturer>Samsung Electronics</manufacturer>
    <modelName>QN55Q60RAFXZA</modelName>
  </device>
</root>`

func TestSSDP(t *testing.T) {
	loc, server := parseSSDP([]byte("HTTP/1.1 200 OK\r\nCACHE-CONTROL: max-age=1800\r\nLOCATION: http://192.168.1.50:9197/dmr\r\nSERVER: Linux/4.1 UPnP/1.0\r\nST: upnp:rootdevice\r\n\r\n"))
	if loc != "http://192.168.1.50:9197/dmr" || server != "Linux/4.1 UPnP/1.0" {
		t.Fatalf("location=%q server=%q", loc, server)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(tvDescription))
	}))
	defer srv.Close()
	a, ok := fetchDescription(context.Background(), srv.Client(), srv.URL)
	if !ok {
		t.Fatal("description not parsed")
	}
	want := announce{
		Manufacturer: "Samsung Electronics", Model: "QN55Q60RAFXZA",
		Name: "[TV] Samsung Q60 Series (55)", Services: []string{"upnp:MediaRenderer"},
	}
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("announce = %+v", a)
	}
}
