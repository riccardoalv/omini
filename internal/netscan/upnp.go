package netscan

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// UPnP Internet Gateway Device (IGD): the port mappings a router's UPnP
// service holds, the ones devices opened by themselves. Omini only reads
// them (GetGenericPortMappingEntry), never adds or deletes one. NAT-PMP and
// PCP (UDP 5351, another protocol) are not read.

// igdServices are the IGD services that hold port mappings.
var igdServices = []string{
	"urn:schemas-upnp-org:service:WANIPConnection:2",
	"urn:schemas-upnp-org:service:WANIPConnection:1",
	"urn:schemas-upnp-org:service:WANPPPConnection:1",
}

// MaxPortMappings bounds the mappings read from one gateway.
const MaxPortMappings = 256

// igd is a gateway's port mapping service: its control URL and type.
type igd struct {
	Control string
	Service string
}

// upnpDevice is a device of a UPnP description, with its services and
// embedded devices (an IGD's WANDevice and WANConnectionDevice).
type upnpDevice struct {
	DeviceType   string        `xml:"deviceType"`
	FriendlyName string        `xml:"friendlyName"`
	Manufacturer string        `xml:"manufacturer"`
	ModelName    string        `xml:"modelName"`
	ModelNumber  string        `xml:"modelNumber"`
	Services     []upnpService `xml:"serviceList>service"`
	Devices      []upnpDevice  `xml:"deviceList>device"`
}

type upnpService struct {
	ServiceType string `xml:"serviceType"`
	ControlURL  string `xml:"controlURL"`
}

// findIGD is the first port mapping service of a description's device tree
// (by igdServices' order of preference), its control URL resolved against
// URLBase or the description's location. Only a control URL on the host
// that answered is taken: the scan talks to no other machine.
func findIGD(root upnpRoot, location string) (igd, bool) {
	var all []upnpService
	var walk func(d upnpDevice, depth int)
	walk = func(d upnpDevice, depth int) {
		all = append(all, d.Services...)
		if depth < 4 {
			for _, sub := range d.Devices {
				walk(sub, depth+1)
			}
		}
	}
	walk(root.Device, 0)
	base, err := url.Parse(location)
	if err != nil {
		return igd{}, false
	}
	if b := strings.TrimSpace(root.URLBase); b != "" {
		if u, err := url.Parse(b); err == nil && u.Hostname() == base.Hostname() {
			base = u
		}
	}
	for _, want := range igdServices {
		for _, s := range all {
			if strings.TrimSpace(s.ServiceType) != want {
				continue
			}
			ref, err := url.Parse(strings.TrimSpace(s.ControlURL))
			if err != nil || s.ControlURL == "" {
				continue
			}
			u := base.ResolveReference(ref)
			if u.Hostname() != base.Hostname() || (u.Scheme != "http" && u.Scheme != "https") {
				continue
			}
			return igd{Control: u.String(), Service: want}, true
		}
	}
	return igd{}, false
}

// portMappingEntry is GetGenericPortMappingEntry's answer.
type portMappingEntry struct {
	RemoteHost     string `xml:"NewRemoteHost"`
	ExternalPort   string `xml:"NewExternalPort"`
	Protocol       string `xml:"NewProtocol"`
	InternalPort   string `xml:"NewInternalPort"`
	InternalClient string `xml:"NewInternalClient"`
	Enabled        string `xml:"NewEnabled"`
	Description    string `xml:"NewPortMappingDescription"`
}

type soapEnvelope struct {
	Body struct {
		Entry *portMappingEntry `xml:"GetGenericPortMappingEntryResponse"`
	} `xml:"Body"`
}

// portMappings reads a gateway's port mappings by index, from 0 until the
// gateway answers an error (SpecifiedArrayIndexInvalid past the last one) or
// MaxPortMappings were read.
func portMappings(ctx context.Context, client *http.Client, g igd) []model.PortForward {
	var out []model.PortForward
	for i := 0; i < MaxPortMappings && ctx.Err() == nil; i++ {
		e, ok := portMapping(ctx, client, g, i)
		if !ok {
			break
		}
		if f, ok := e.forward(); ok {
			out = append(out, f)
		}
	}
	return out
}

func portMapping(ctx context.Context, client *http.Client, g igd, index int) (portMappingEntry, bool) {
	body := fmt.Sprintf(`<?xml version="1.0"?>`+
		`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">`+
		`<s:Body><u:GetGenericPortMappingEntry xmlns:u="%s"><NewPortMappingIndex>%d</NewPortMappingIndex>`+
		`</u:GetGenericPortMappingEntry></s:Body></s:Envelope>`, g.Service, index)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.Control, strings.NewReader(body))
	if err != nil {
		return portMappingEntry{}, false
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+g.Service+`#GetGenericPortMappingEntry"`)
	resp, err := client.Do(req)
	if err != nil {
		return portMappingEntry{}, false
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil || resp.StatusCode != http.StatusOK {
		return portMappingEntry{}, false
	}
	var env soapEnvelope
	if err := xml.NewDecoder(bytes.NewReader(raw)).Decode(&env); err != nil || env.Body.Entry == nil {
		return portMappingEntry{}, false
	}
	return *env.Body.Entry, true
}

// forward is the mapping as the model's port forward; false when it is not
// one (no port or protocol).
func (e portMappingEntry) forward() (model.PortForward, bool) {
	ext, err := strconv.ParseUint(strings.TrimSpace(e.ExternalPort), 10, 16)
	if err != nil || ext == 0 {
		return model.PortForward{}, false
	}
	var proto model.PortForwardProtocol
	switch strings.ToUpper(strings.TrimSpace(e.Protocol)) {
	case "TCP":
		proto = model.PortForwardProtocolTcp
	case "UDP":
		proto = model.PortForwardProtocolUdp
	default:
		return model.PortForward{}, false
	}
	f := model.PortForward{Protocol: proto, ExternalPort: uint16(ext), Upnp: model.Ptr(true)}
	if p, err := strconv.ParseUint(strings.TrimSpace(e.InternalPort), 10, 16); err == nil && p > 0 {
		f.InternalPort = model.Ptr(uint16(p))
	}
	if ip := strings.TrimSpace(e.InternalClient); ip != "" {
		f.InternalIP = model.Ptr(ip)
	}
	if src := strings.TrimSpace(e.RemoteHost); src != "" {
		f.Source = model.Ptr(src)
	}
	if d := strings.TrimSpace(e.Description); d != "" {
		f.Description = model.Ptr(d)
	}
	switch strings.TrimSpace(e.Enabled) {
	case "1", "true":
		f.Enabled = model.Ptr(true)
	case "0", "false":
		f.Enabled = model.Ptr(false)
	}
	return f, true
}

// igdTimeout is each port mapping request's.
const igdTimeout = 2 * time.Second

// readPortMappings reads a gateway's UPnP port mappings, in at most 15 s.
func readPortMappings(ctx context.Context, g igd) []model.PortForward {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	return portMappings(ctx, &http.Client{Timeout: igdTimeout}, g)
}
