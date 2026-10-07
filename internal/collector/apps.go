package collector

import (
	"slices"
	"strconv"
	"strings"

	"github.com/riccardoalv/omini/internal/appicons"
	"github.com/riccardoalv/omini/internal/topology"
)

// appliances are products that are the device itself (its OS or firmware):
// their web interface is not a separate app node.
var appliances = map[string]bool{
	"opnsense": true, "pfsense": true, "proxmox": true, "truenas": true, "unraid": true,
	"openwrt": true, "mikrotik": true, "synology": true, "qnap": true, "homeassistant": true,
	"esphome": true, "tasmota": true,
}

// applianceHome are the ports where an appliance serves its own interface. A
// title naming the appliance on another port is an app (e.g. "OPNsense
// Exporter" on 8081 of an Ubuntu VM), not the device itself.
var applianceHome = map[string][]int{
	"opnsense": {80, 443}, "pfsense": {80, 443}, "truenas": {80, 443}, "unraid": {80, 443},
	"openwrt": {80, 443}, "mikrotik": {80, 443}, "proxmox": {8006}, "synology": {5000, 5001},
	"qnap": {8080, 443}, "homeassistant": {8123, 80, 443}, "esphome": {80, 6052}, "tasmota": {80},
}

// isAppliance reports whether the node really is the appliance its product
// names: evidence not coming from a web title, or a title on a home port.
func isAppliance(n *topology.Node, self string) bool {
	if !appliances[n.Product] {
		return false
	}
	homes := applianceHome[n.Product]
	fromTitle := false
	for _, w := range n.Web {
		if w.App != self {
			continue
		}
		fromTitle = true
		if slices.Contains(homes, w.Port) {
			return true
		}
	}
	return !fromTitle
}

// catalogName maps classifier product slugs to app catalog icon names.
var catalogName = map[string]string{
	"homeassistant": "home-assistant", "pihole": "pi-hole", "adguard": "adguard-home",
	"uptimekuma": "uptime-kuma", "paperlessngx": "paperless-ngx",
}

// portApps recognize apps on their usual port when the page has no useful
// title (single-page apps often have an empty or generic one).
var portApps = map[int]string{
	8096: "jellyfin", 8920: "jellyfin", 32400: "plex", 4533: "navidrome", 13378: "audiobookshelf",
	8181: "tautulli", 8989: "sonarr", 7878: "radarr", 8686: "lidarr", 8787: "readarr",
	9696: "prowlarr", 6767: "bazarr", 5055: "overseerr", 9091: "transmission", 8112: "deluge",
	6789: "nzbget", 8191: "flaresolverr", 8123: "home-assistant", 1880: "node-red", 6052: "esphome",
	8006: "proxmox", 9443: "portainer", 3001: "uptime-kuma", 19999: "netdata", 61208: "glances",
	10000: "webmin", 2283: "immich", 8384: "syncthing", 7575: "homarr", 5678: "n8n",
	9925: "mealie", 11434: "ollama",
}

func toCatalog(product string) string {
	if n, ok := catalogName[product]; ok {
		return n
	}
	return product
}

// expandApps recognizes the web apps on each node and adds one child node per
// app. A device that runs apps is then identified by its own system (e.g. an
// Ubuntu VM running Jellyfin and qBittorrent), unless the app is the device
// itself (OPNsense, Proxmox, TrueNAS...).
func expandApps(topo *topology.Topology) {
	var added []topology.Node
	var edges []topology.Edge
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		if n.Kind == topology.KindApp || len(n.Web) == 0 {
			continue
		}
		self := toCatalog(n.Product)
		for j := range n.Web {
			w := &n.Web[j]
			if app, ok := appicons.Match(w.Title); ok {
				w.App = app
			} else if app, ok := portApps[w.Port]; ok {
				w.App = app
			}
		}
		appliance := isAppliance(n, self)
		seen := map[string]bool{}
		var apps []topology.WebApp
		for _, w := range n.Web {
			if w.App == "" || (appliance && w.App == self && slices.Contains(applianceHome[n.Product], w.Port)) {
				continue
			}
			key := w.App + ":" + strconv.Itoa(w.Port)
			if seen[key] {
				continue
			}
			seen[key] = true
			apps = append(apps, w)
		}
		if len(apps) == 0 {
			continue
		}
		// The device runs apps: identify it by its own system, without the app evidence.
		if n.Product != "" && !appliance {
			reclassifyWithoutApps(n, apps)
		}
		for _, a := range apps {
			id := "app:" + n.ID + ":" + strconv.Itoa(a.Port)
			added = append(added, topology.Node{
				ID: id, Kind: topology.KindApp, Label: appLabel(a),
				Role: "app", Type: "app", Online: n.Online, ParentID: n.ID, Port: strconv.Itoa(a.Port),
				IP: n.IP, Product: a.App, Web: []topology.WebApp{a},
			})
			edges = append(edges, topology.Edge{
				ID: "e:" + n.ID + "|" + id, Source: n.ID, Target: id,
				SourcePort: strconv.Itoa(a.Port), Kind: topology.EdgeInferred,
			})
		}
	}
	topo.Nodes = append(topo.Nodes, added...)
	topo.Edges = append(topo.Edges, edges...)
}

// reclassifyWithoutApps classifies a node again ignoring its app titles and ports.
func reclassifyWithoutApps(n *topology.Node, apps []topology.WebApp) {
	appPorts := map[int]bool{}
	for _, a := range apps {
		appPorts[a.Port] = true
	}
	saved := *n
	n.Titles = nil
	n.OpenPorts = slices.DeleteFunc(slices.Clone(n.OpenPorts), func(p int) bool { return appPorts[p] })
	n.Product = ""
	classifyNode(n)
	n.Titles, n.OpenPorts = saved.Titles, saved.OpenPorts
}

// appLabel names an app node: the app name as written in the page title, or
// the whole title for variants such as "OPNsense Exporter".
func appLabel(w topology.WebApp) string {
	name := appicons.DisplayName(w.App, w.Title)
	if strings.Contains(strings.ToLower(w.Title), "exporter") && !strings.Contains(strings.ToLower(name), "exporter") {
		return name + " Exporter"
	}
	return name
}
