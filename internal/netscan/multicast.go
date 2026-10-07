package netscan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

// announce is what a device told us about itself over mDNS or SSDP.
type announce struct {
	Hostnames    []string
	Services     []string
	Manufacturer string
	Model        string
	Name         string // friendly name (UPnP)
}

func (a *announce) merge(b announce) {
	a.Hostnames = appendUnique(a.Hostnames, b.Hostnames...)
	a.Services = appendUnique(a.Services, b.Services...)
	if a.Manufacturer == "" {
		a.Manufacturer = b.Manufacturer
	}
	if a.Model == "" {
		a.Model = b.Model
	}
	if a.Name == "" {
		a.Name = b.Name
	}
}

// mdnsTypes are asked explicitly, besides the generic service enumeration.
var mdnsTypes = []string{
	"_services._dns-sd._udp", "_device-info._tcp", "_workstation._tcp", "_http._tcp", "_ssh._tcp",
	"_smb._tcp", "_afpovertcp._tcp", "_airplay._tcp", "_raop._tcp", "_companion-link._tcp",
	"_googlecast._tcp", "_androidtvremote2._tcp", "_amzn-wplay._tcp", "_spotify-connect._tcp",
	"_sonos._tcp", "_homekit._tcp", "_hap._tcp", "_hue._tcp", "_matter._tcp", "_home-assistant._tcp",
	"_esphomelib._tcp", "_mqtt._tcp", "_ipp._tcp", "_ipps._tcp", "_printer._tcp",
	"_pdl-datastream._tcp", "_scanner._tcp", "_rfb._tcp", "_nvstream._tcp", "_miio._udp",
}

var mdnsGroup = netip.MustParseAddrPort("224.0.0.251:5353")

// mdnsQuery builds a one-shot query. Sent from a port other than 5353, it
// gets unicast replies (RFC 6762 "legacy unicast"), so we do not need to bind
// port 5353, which is usually taken by avahi.
func mdnsQuery() []byte {
	b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
	_ = b.StartQuestions()
	for _, t := range mdnsTypes {
		name := dnsmessage.MustNewName(t + ".local.")
		_ = b.Question(dnsmessage.Question{Name: name, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET})
	}
	msg, _ := b.Finish()
	return msg
}

// browseMDNS sends mDNS queries and collects answers for wait.
func browseMDNS(ctx context.Context, wait time.Duration) map[netip.Addr]announce {
	out := map[netip.Addr]announce{}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return out
	}
	defer conn.Close()
	var mu sync.Mutex
	handle := func(src netip.Addr, b []byte) {
		if a, ok := parseMDNS(b); ok {
			mu.Lock()
			cur := out[src]
			cur.merge(a)
			out[src] = cur
			mu.Unlock()
		}
	}
	// Some responders answer on the multicast group instead of unicast: listen
	// there too (port 5353 is shared with avahi through SO_REUSEPORT).
	var wg sync.WaitGroup
	if group := joinGroup(ctx, mdnsGroup); group != nil {
		defer group.Close()
		wg.Add(1)
		go func() { defer wg.Done(); collect(ctx, group, wait, handle) }()
	}
	query := mdnsQuery()
	for i := 0; i < 2; i++ { // twice: UDP may drop the first one
		_, _ = conn.WriteToUDPAddrPort(query, mdnsGroup)
	}
	collect(ctx, conn, wait, handle)
	wg.Wait()
	return out
}

// joinGroup listens on a multicast group's port on every multicast-capable
// interface. It returns nil when that is not possible.
func joinGroup(ctx context.Context, group netip.AddrPort) *net.UDPConn {
	lc := net.ListenConfig{Control: reusePort}
	pc, err := lc.ListenPacket(ctx, "udp4", fmt.Sprintf("0.0.0.0:%d", group.Port()))
	if err != nil {
		return nil
	}
	conn := pc.(*net.UDPConn)
	p := ipv4.NewPacketConn(conn)
	ifaces, _ := net.Interfaces()
	joined := false
	for i := range ifaces {
		ifc := &ifaces[i]
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagMulticast == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		if p.JoinGroup(ifc, &net.UDPAddr{IP: group.Addr().AsSlice()}) == nil {
			joined = true
		}
	}
	if !joined {
		conn.Close()
		return nil
	}
	return conn
}

// parseMDNS extracts what a responder says about itself: its .local names,
// the service types it offers and its model (device-info TXT record).
func parseMDNS(b []byte) (announce, bool) {
	var p dnsmessage.Parser
	h, err := p.Start(b)
	if err != nil || !h.Response {
		return announce{}, false
	}
	_ = p.SkipAllQuestions()
	var a announce
	for {
		rh, err := p.AnswerHeader()
		if errors.Is(err, dnsmessage.ErrSectionDone) {
			break
		}
		if err != nil {
			return a, false
		}
		readRecord(&p, rh, &a, p.SkipAnswer)
	}
	_ = p.SkipAllAuthorities()
	for {
		rh, err := p.AdditionalHeader()
		if err != nil {
			break
		}
		readRecord(&p, rh, &a, p.SkipAdditional)
	}
	return a, len(a.Hostnames)+len(a.Services) > 0 || a.Model != ""
}

// readRecord reads one resource record; skip skips unknown types in the current section.
func readRecord(p *dnsmessage.Parser, rh dnsmessage.ResourceHeader, a *announce, skip func() error) {
	name := strings.TrimSuffix(rh.Name.String(), ".")
	switch rh.Type {
	case dnsmessage.TypeA:
		_, _ = p.AResource()
		a.Hostnames = appendUnique(a.Hostnames, name)
	case dnsmessage.TypePTR:
		r, err := p.PTRResource()
		if err != nil {
			return
		}
		if t := serviceType(name); t != "" && t != "_dns-sd._udp" { // not the enumeration itself
			a.Services = appendUnique(a.Services, t)
		}
		if name == "_services._dns-sd._udp.local" { // enumeration: the target is a type
			if t := serviceType(strings.TrimSuffix(r.PTR.String(), ".")); t != "" {
				a.Services = appendUnique(a.Services, t)
			}
		}
	case dnsmessage.TypeTXT:
		r, err := p.TXTResource()
		if err != nil {
			return
		}
		if strings.Contains(name, "._device-info._tcp") {
			for _, kv := range r.TXT {
				if v, ok := strings.CutPrefix(kv, "model="); ok {
					a.Model = v
				}
			}
		}
	case dnsmessage.TypeSRV:
		r, err := p.SRVResource()
		if err != nil {
			return
		}
		a.Hostnames = appendUnique(a.Hostnames, strings.TrimSuffix(r.Target.String(), "."))
		if t := serviceType(name); t != "" {
			a.Services = appendUnique(a.Services, t)
		}
	default:
		_ = skip()
	}
}

// serviceType extracts "_airplay._tcp" from "Living Room._airplay._tcp.local".
func serviceType(name string) string {
	name = strings.TrimSuffix(name, ".local")
	labels := strings.Split(name, ".")
	for i := 0; i+1 < len(labels); i++ {
		if strings.HasPrefix(labels[i], "_") && (labels[i+1] == "_tcp" || labels[i+1] == "_udp") {
			return labels[i] + "." + labels[i+1]
		}
	}
	return ""
}

var ssdpGroup = netip.MustParseAddrPort("239.255.255.250:1900")

const ssdpSearch = "M-SEARCH * HTTP/1.1\r\n" +
	"HOST: 239.255.255.250:1900\r\n" +
	"MAN: \"ssdp:discover\"\r\n" +
	"MX: 2\r\n" +
	"ST: ssdp:all\r\n\r\n"

// browseSSDP finds UPnP devices (TVs, routers, media players, consoles) and
// reads their device descriptions (manufacturer, model, friendly name).
func browseSSDP(ctx context.Context, wait time.Duration, allowed func(netip.Addr) bool) map[netip.Addr]announce {
	out := map[netip.Addr]announce{}
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{})
	if err != nil {
		return out
	}
	defer conn.Close()
	for i := 0; i < 2; i++ {
		_, _ = conn.WriteToUDPAddrPort([]byte(ssdpSearch), ssdpGroup)
	}
	locations := map[netip.Addr]string{}
	collect(ctx, conn, wait, func(src netip.Addr, b []byte) {
		loc, server := parseSSDP(b)
		if loc != "" && locations[src] == "" {
			locations[src] = loc
		}
		if server != "" {
			cur := out[src]
			cur.merge(announce{Services: []string{"upnp"}})
			out[src] = cur
		}
	})
	client := &http.Client{Timeout: 2 * time.Second}
	for ip, loc := range locations {
		u, err := url.Parse(loc)
		if err != nil || !allowed(ip) || u.Hostname() != ip.String() {
			continue // only fetch descriptions from the device that answered
		}
		if a, ok := fetchDescription(ctx, client, loc); ok {
			cur := out[ip]
			cur.merge(a)
			out[ip] = cur
		}
	}
	return out
}

// parseSSDP returns the LOCATION and SERVER headers of an SSDP response.
func parseSSDP(b []byte) (location, server string) {
	s := bufio.NewScanner(bytes.NewReader(b))
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(k)) {
		case "LOCATION":
			location = strings.TrimSpace(v)
		case "SERVER":
			server = strings.TrimSpace(v)
		}
	}
	return location, server
}

type upnpRoot struct {
	Device struct {
		DeviceType   string `xml:"deviceType"`
		FriendlyName string `xml:"friendlyName"`
		Manufacturer string `xml:"manufacturer"`
		ModelName    string `xml:"modelName"`
		ModelNumber  string `xml:"modelNumber"`
	} `xml:"device"`
}

func fetchDescription(ctx context.Context, client *http.Client, loc string) (announce, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc, nil)
	if err != nil {
		return announce{}, false
	}
	resp, err := client.Do(req)
	if err != nil {
		return announce{}, false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<10))
	if err != nil {
		return announce{}, false
	}
	return parseDescription(body)
}

func parseDescription(body []byte) (announce, bool) {
	var root upnpRoot
	if err := xml.Unmarshal(body, &root); err != nil {
		return announce{}, false
	}
	d := root.Device
	model := strings.TrimSpace(strings.TrimSpace(d.ModelName) + " " + strings.TrimSpace(d.ModelNumber))
	a := announce{
		Manufacturer: strings.TrimSpace(d.Manufacturer),
		Model:        model,
		Name:         strings.TrimSpace(d.FriendlyName),
	}
	if t := upnpDeviceType(d.DeviceType); t != "" {
		a.Services = []string{"upnp:" + t}
	}
	return a, a.Manufacturer != "" || a.Model != "" || a.Name != ""
}

// upnpDeviceType extracts "MediaRenderer" from "urn:schemas-upnp-org:device:MediaRenderer:1".
func upnpDeviceType(urn string) string {
	parts := strings.Split(urn, ":")
	if len(parts) >= 5 && parts[2] == "device" {
		return parts[3]
	}
	return ""
}

// collect reads datagrams for wait, calling fn with the sender address.
func collect(ctx context.Context, conn *net.UDPConn, wait time.Duration, fn func(netip.Addr, []byte)) {
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 9000)
	for {
		n, src, err := conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		fn(src.Addr().Unmap(), append([]byte(nil), buf[:n]...))
	}
}

func appendUnique(list []string, items ...string) []string {
	for _, it := range items {
		if it == "" {
			continue
		}
		found := false
		for _, x := range list {
			if strings.EqualFold(x, it) {
				found = true
				break
			}
		}
		if !found {
			list = append(list, it)
		}
	}
	return list
}
