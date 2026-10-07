package nmapscan

import (
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// nmap's XML output (-oX), only what Omini uses.
type run struct {
	Hosts []host `xml:"host"`
}

type host struct {
	Status struct {
		State string `xml:"state,attr"`
	} `xml:"status"`
	Addresses []struct {
		Addr   string `xml:"addr,attr"`
		Type   string `xml:"addrtype,attr"`
		Vendor string `xml:"vendor,attr"`
	} `xml:"address"`
	Hostnames []struct {
		Name string `xml:"name,attr"`
	} `xml:"hostnames>hostname"`
	Ports []struct {
		ID    int `xml:"portid,attr"`
		State struct {
			State string `xml:"state,attr"`
		} `xml:"state"`
		Service struct {
			Name    string `xml:"name,attr"`
			Product string `xml:"product,attr"`
			Version string `xml:"version,attr"`
			Extra   string `xml:"extrainfo,attr"`
			OSType  string `xml:"ostype,attr"`
		} `xml:"service"`
	} `xml:"ports>port"`
	OS struct {
		Matches []struct {
			Name     string `xml:"name,attr"`
			Accuracy int    `xml:"accuracy,attr"`
			Classes  []struct {
				Family string `xml:"osfamily,attr"`
				Vendor string `xml:"vendor,attr"`
				Type   string `xml:"type,attr"`
			} `xml:"osclass"`
		} `xml:"osmatch"`
	} `xml:"os"`
}

// osFamilies maps nmap's OS families to Omini's OS names.
var osFamilies = map[string]string{
	"linux": "linux", "windows": "windows", "ios": "ios", "mac os x": "macos", "macos": "macos",
	"android": "android", "freebsd": "freebsd", "openbsd": "openbsd",
	"routeros": "routeros", //nolint:misspell // MikroTik RouterOS
}

// parse reads the hosts that were up.
func parse(out []byte) ([]model.Host, error) {
	var r run
	if err := xml.Unmarshal(out, &r); err != nil {
		return nil, fmt.Errorf("unexpected nmap output: %w", err)
	}
	var hosts []model.Host
	for _, h := range r.Hosts {
		if h.Status.State != "up" {
			continue
		}
		mh := model.Host{Sources: []string{"nmap"}}
		for _, a := range h.Addresses {
			switch a.Type {
			case "ipv4":
				mh.IP = a.Addr
			case "mac":
				mac := model.MACAddress(strings.ToLower(a.Addr))
				mh.MAC = &mac
				if a.Vendor != "" {
					mh.Manufacturer = model.Ptr(a.Vendor)
				}
			}
		}
		if mh.IP == "" {
			continue
		}
		for _, n := range h.Hostnames {
			if n.Name != "" {
				mh.Hostnames = append(mh.Hostnames, n.Name)
			}
		}
		for _, p := range h.Ports {
			if p.State.State != "open" {
				continue
			}
			mh.OpenPorts = append(mh.OpenPorts, uint16(p.ID))
			// "OpenSSH 9.6p1 Ubuntu 3ubuntu13.5": the classifier reads systems in it.
			if banner := strings.Join(nonEmpty(p.Service.Product, p.Service.Version, p.Service.Extra), " "); banner != "" {
				mh.Banners = append(mh.Banners, banner)
			}
			if mh.OS == nil && p.Service.OSType != "" {
				if os := osFamilies[strings.ToLower(p.Service.OSType)]; os != "" {
					mh.OS = model.Ptr(os)
				}
			}
		}
		// The best OS guess, when nmap is confident.
		if len(h.OS.Matches) > 0 && h.OS.Matches[0].Accuracy >= 90 {
			m := h.OS.Matches[0]
			for _, c := range m.Classes {
				if os := osFamilies[strings.ToLower(c.Family)]; os != "" {
					mh.OS = model.Ptr(os)
					break
				}
			}
		}
		hosts = append(hosts, mh)
	}
	return hosts, nil
}

func nonEmpty(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
