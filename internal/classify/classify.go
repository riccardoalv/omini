// Package classify guesses what a device is (phone, NAS, Proxmox host...),
// its operating system, brand and, for homelab software, the product, from
// everything integrations learned about it. It is a pure function with
// ordered, table-driven rules; every conclusion records its evidence.
package classify

import (
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Device types.
const (
	Firewall       = "firewall"
	Router         = "router"
	Switch         = "switch"
	AccessPoint    = "ap"
	Server         = "server"
	NAS            = "nas"
	Hypervisor     = "hypervisor"
	VirtualMachine = "virtual_machine"
	Computer       = "computer"
	Phone          = "phone"
	Tablet         = "tablet"
	TV             = "tv"
	MediaPlayer    = "media_player"
	Speaker        = "speaker"
	Printer        = "printer"
	Camera         = "camera"
	SmartHome      = "smart_home"
	Appliance      = "appliance"
	AirConditioner = "air_conditioner"
	SolarInverter  = "solar_inverter"
	GameConsole    = "game_console"
	Wearable       = "wearable"
	IPPhone        = "ip_phone"
	UPS            = "ups"
	Segment        = "segment"
	Unknown        = "unknown"
)

// Input is what is known about a device.
type Input struct {
	Kind      string // device, unmanaged, segment, client
	Role      string // from an integration: router, switch, ap, firewall, server
	Vendor    string // MAC vendor or manufacturer
	Model     string
	Hostname  string
	OS        string // OS id reported directly (e.g. this server's /etc/os-release)
	RandomMAC bool
	OpenPorts []int
	Services  []string // mDNS/UPnP service types
	Titles    []string // web interface titles
	Banners   []string // e.g. SSH version strings
	TTL       int
	Self      bool // the Omini server itself
	Upstream  bool // seen on an internet uplink (WAN port or gateway address): the ISP's modem or router
}

// Result is the classification. Product is set for homelab software and
// appliances, which are shown with their own logo only.
type Result struct {
	Type    string   `json:"type"`
	OS      string   `json:"os,omitempty"`
	Brand   string   `json:"brand,omitempty"`
	Product string   `json:"product,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

type state struct {
	in  Input
	r   Result
	txt string // lowercase titles + hostname + model + banners, for keyword rules
}

func (s *state) set(field *string, value, reason string) {
	if *field == "" && value != "" {
		*field = value
		if reason != "" && !slices.Contains(s.r.Reasons, reason) {
			s.r.Reasons = append(s.r.Reasons, reason)
		}
	}
}

func (s *state) port(p int) bool { return slices.Contains(s.in.OpenPorts, p) }

func (s *state) service(prefix string) bool {
	for _, svc := range s.in.Services {
		if strings.HasPrefix(svc, prefix) {
			return true
		}
	}
	return false
}

// Classify applies the rules in order: specific products first, then the
// operating system, the brand and finally the device type.
func Classify(in Input) Result {
	s := &state{in: in}
	parts := append(slices.Clone(in.Titles), in.Hostname, in.Model)
	parts = append(parts, in.Banners...)
	s.txt = strings.ToLower(strings.Join(parts, " | "))

	if in.Kind == "segment" {
		s.r.Type = Segment
		return s.r
	}
	products(s)
	operatingSystem(s)
	brand(s)
	deviceType(s)
	if s.r.Type == "" {
		s.r.Type = Unknown
	}
	return s.r
}

// product rules: a keyword in the web title, hostname, model or banner, an
// advertised service or, as a weaker signal, a characteristic port.
type productRule struct {
	product  string
	typ      string
	os       string
	keywords []string
	services []string
	ports    []int
}

var productRules = []productRule{
	{product: "opnsense", typ: Firewall, os: "freebsd", keywords: []string{"opnsense"}},
	{product: "pfsense", typ: Firewall, os: "freebsd", keywords: []string{"pfsense"}},
	{product: "proxmox", typ: Hypervisor, os: "debian", keywords: []string{"proxmox"}, ports: []int{8006}},
	{product: "truenas", typ: NAS, keywords: []string{"truenas", "freenas"}},
	{product: "unraid", typ: NAS, keywords: []string{"unraid"}},
	{product: "homeassistant", typ: Server, keywords: []string{"home assistant", "homeassistant"}, services: []string{"_home-assistant._tcp"}, ports: []int{8123}},
	{product: "jellyfin", typ: Server, keywords: []string{"jellyfin"}, ports: []int{8096}},
	{product: "plex", typ: Server, keywords: []string{"plex media server", "plex"}, ports: []int{32400}},
	{product: "emby", typ: Server, keywords: []string{"emby"}},
	{product: "pihole", typ: Server, keywords: []string{"pi-hole", "pihole"}},
	{product: "adguard", typ: Server, keywords: []string{"adguard"}},
	{product: "nextcloud", typ: Server, keywords: []string{"nextcloud"}},
	{product: "portainer", typ: Server, keywords: []string{"portainer"}},
	{product: "grafana", typ: Server, keywords: []string{"grafana"}},
	{product: "frigate", typ: Server, keywords: []string{"frigate"}},
	{product: "immich", typ: Server, keywords: []string{"immich"}},
	{product: "paperlessngx", typ: Server, keywords: []string{"paperless"}},
	{product: "uptimekuma", typ: Server, keywords: []string{"uptime kuma"}},
	{product: "vaultwarden", typ: Server, keywords: []string{"vaultwarden"}},
	{product: "bitwarden", typ: Server, keywords: []string{"bitwarden"}},
	{product: "esphome", typ: SmartHome, keywords: []string{"esphome"}, services: []string{"_esphomelib._tcp"}},
	{product: "tasmota", typ: SmartHome, keywords: []string{"tasmota"}},
	{product: "openwrt", typ: Router, os: "openwrt", keywords: []string{"openwrt", "luci"}},
	{product: "mikrotik", typ: Router, keywords: []string{"mikrotik", "routeros"}}, //nolint:misspell // MikroTik RouterOS
	{product: "synology", typ: NAS, keywords: []string{"synology", "diskstation", "dsm "}},
	{product: "qnap", typ: NAS, keywords: []string{"qnap"}},
}

// typeRules recognize hardware without a logo of its own, by web title or model.
var typeRules = []struct {
	keywords []string
	typ      string
	brand    string
}{
	{[]string{"hc-swtg", "zx-swtg", "horaco"}, Switch, "horaco"},
	{[]string{"mercusys"}, AccessPoint, "mercusys"},
	// Solar inverters (and their Wi-Fi/LAN dongles) are named after the
	// series: Huawei "SUN2000-5KTL", Fronius "Symo", SMA "Sunny Boy"...
	{[]string{"sun2000", "sun5000", "sun600-", "sdongle"}, SolarInverter, "huawei"},
	{[]string{"fronius", "symo gen24", "primo gen24"}, SolarInverter, "fronius"},
	{[]string{"solaredge"}, SolarInverter, "solaredge"},
	{[]string{"sunny boy", "sunnyboy", "sunny tripower", "sma solar", "sma-inverter"}, SolarInverter, "sma"},
	{[]string{"growatt", "shinewifi", "shinelan"}, SolarInverter, "growatt"},
	{[]string{"goodwe"}, SolarInverter, "goodwe"},
	{[]string{"enphase", "envoy-s", "iq gateway"}, SolarInverter, "enphase"},
	{[]string{"sungrow", "winet-s"}, SolarInverter, "sungrow"},
	{[]string{"solarman", "deye"}, SolarInverter, "deye"},
	{[]string{"ginlong", "solis-"}, SolarInverter, "solis"},
}

// nameRules recognize consumer devices by a pattern in their name, model or
// web title (lowercase), some only for one brand ("echo" alone is a word,
// "Echo-Dot" from Amazon is a speaker). The first matching rule wins; a brand
// set by a rule only fills a brand not known yet.
var nameRules = []struct {
	re    *regexp.Regexp
	typ   string
	only  string // the rule applies only to devices of this brand
	brand string
}{
	// Streaming sticks and boxes before TVs: "Chromecast with Google TV".
	{re: regexp.MustCompile(`\bchromecast\b|\bgoogle[- ]?tv\b`), typ: MediaPlayer, brand: "google"},
	{re: regexp.MustCompile(`\bapple[- ]?tv\b`), typ: MediaPlayer, brand: "apple"},
	{re: regexp.MustCompile(`\bfire[- ]?(tv|stick)\b`), typ: MediaPlayer, brand: "amazon"},
	{re: regexp.MustCompile(`\broku\b`), typ: MediaPlayer, brand: "roku"},
	// Smart displays, then smart speakers.
	{re: regexp.MustCompile(`\bnest[- ]?hub\b|\bhome[- ]?hub\b|\becho[- ]?show\b`), typ: SmartHome},
	{re: regexp.MustCompile(`\bnest[- ]?(mini|audio)\b|\bgoogle[- ]?home\b|\bhome[- ]?mini\b|\bhomepod\b`), typ: Speaker},
	{re: regexp.MustCompile(`\becho\b`), typ: Speaker, only: "amazon"},
	// TVs: their OS, a "TV" in the name, or a Samsung model code ("QN85B", "UE55AU7100").
	{re: regexp.MustCompile(`\bwebos\b|\btizen\b|\bbravia\b|\bsmart[- ]?tv\b|\bandroid[- ]?tv\b|\btv\b|\boled\d{2}[a-z]`), typ: TV},
	{re: regexp.MustCompile(`\b(q[naeu]|u[nae])\d{2}[a-z]`), typ: TV, only: "samsung"},
	// IP phones: Yealink "SIP-T54W", Cisco "SEP" + MAC, Polycom "VVX".
	{re: regexp.MustCompile(`\bsip-[a-z]{1,2}\d|\bsep[0-9a-f]{12}\b|\bip[- ]phone\b|\bvvx[- ]?\d`), typ: IPPhone},
	// UPSes and their network cards (APC "Smart-UPS", "Back-UPS").
	{re: regexp.MustCompile(`\bups\b|\bsymmetra\b|\bpowerchute\b`), typ: UPS},
	// Air conditioning and its controllers (Daikin "intelligent Touch Manager").
	{re: regexp.MustCompile(`\bhvac\b|\bair[- ]?condition|\bintelligent touch manager\b|\bdaikin\b`), typ: AirConditioner},
	// Access control: badge and card readers (HID iCLASS, ZKTeco, Suprema).
	{re: regexp.MustCompile(`\bbadge\b|\bcard[- ]?reader\b|\bhid reader\b|\biclass\b|\baccess[- ]control\b`), typ: SmartHome},
	// Mobile broadband (LTE/5G) routers: Huawei "B535-232", "E5186s-22a", HiLink.
	{re: regexp.MustCompile(`\b(lte|4g|5g)[- ]?(cpe|router|modem)\b|\bhilink\b|\bmobile ?wi-?fi\b`), typ: Router},
	{re: regexp.MustCompile(`\b[be]\d{3,4}[a-z]{0,2}-\d{2,3}[a-z]?\b`), typ: Router, only: "huawei"},
}

// solarVendors are MAC vendors that only make solar equipment.
var solarVendors = []string{"fronius", "sma solar", "solaredge", "enphase", "growatt", "goodwe", "sungrow"}

func products(s *state) {
	for _, rule := range typeRules {
		for _, kw := range rule.keywords {
			if strings.Contains(s.txt, kw) {
				s.set(&s.r.Type, rule.typ, evidence(s, kw))
				s.set(&s.r.Brand, rule.brand, "")
			}
		}
	}
	// Strong evidence first (names, titles, services), then ports, so a
	// Jellyfin page title wins over a Proxmox port on the same address.
	for _, rule := range productRules {
		for _, kw := range rule.keywords {
			if strings.Contains(s.txt, kw) {
				applyProduct(s, rule, evidence(s, kw))
				return
			}
		}
		for _, svc := range rule.services {
			if s.service(svc) {
				applyProduct(s, rule, "mdns:"+svc)
				return
			}
		}
	}
	for _, rule := range productRules {
		for _, p := range rule.ports {
			if s.port(p) {
				applyProduct(s, rule, "port:"+strconv.Itoa(p))
				return
			}
		}
	}
}

func evidence(s *state, kw string) string {
	for _, t := range s.in.Titles {
		if strings.Contains(strings.ToLower(t), kw) {
			return "title:" + t
		}
	}
	if strings.Contains(strings.ToLower(s.in.Hostname), kw) {
		return "hostname:" + s.in.Hostname
	}
	for _, b := range s.in.Banners {
		if strings.Contains(strings.ToLower(b), kw) {
			return "banner:" + b
		}
	}
	return "model:" + s.in.Model
}

func applyProduct(s *state, rule productRule, reason string) {
	s.set(&s.r.Product, rule.product, reason)
	s.set(&s.r.Type, rule.typ, "")
	s.set(&s.r.OS, rule.os, "")
}

var osIDs = map[string]string{
	"nixos": "nixos", "debian": "debian", "ubuntu": "ubuntu", "fedora": "fedora", "arch": "archlinux",
	"raspbian": "raspberrypi", "alpine": "alpinelinux", "centos": "centos", "rhel": "redhat",
	"opensuse-leap": "opensuse", "opensuse-tumbleweed": "opensuse", "linuxmint": "linuxmint",
	"pop": "popos", "manjaro": "manjaro", "freebsd": "freebsd",
}

var reWindowsHost = regexp.MustCompile(`^(desktop|laptop|win)-[a-z0-9]{5,}`)

func operatingSystem(s *state) {
	if id, ok := osIDs[strings.ToLower(s.in.OS)]; ok {
		s.set(&s.r.OS, id, "os-release:"+s.in.OS)
	}
	for _, b := range s.in.Banners {
		lb := strings.ToLower(b)
		for kw, os := range map[string]string{
			"ubuntu": "ubuntu", "debian": "debian", "raspbian": "raspberrypi", "freebsd": "freebsd",
			"openbsd": "openbsd", "fedora": "fedora", "windows": "windows", "dropbear": "linux",
		} {
			if strings.Contains(lb, kw) {
				s.set(&s.r.OS, os, "banner:"+b)
			}
		}
	}
	host := strings.ToLower(s.in.Hostname)
	model := strings.ToLower(s.in.Model)
	switch {
	case strings.Contains(host, "nixos"):
		s.set(&s.r.OS, "nixos", "hostname:"+s.in.Hostname)
	case strings.Contains(host, "iphone") || strings.Contains(host, "ipad") || strings.HasPrefix(model, "iphone") || strings.HasPrefix(model, "ipad"):
		s.set(&s.r.OS, "ios", "hostname:"+s.in.Hostname)
	case strings.Contains(host, "macbook") || strings.Contains(host, "imac") || strings.HasPrefix(model, "mac"):
		s.set(&s.r.OS, "macos", "hostname:"+s.in.Hostname)
	case strings.HasPrefix(model, "appletv"):
		s.set(&s.r.OS, "tvos", "model:"+s.in.Model)
	case reWindowsHost.MatchString(host):
		s.set(&s.r.OS, "windows", "hostname:"+s.in.Hostname)
	case strings.Contains(host, "android") || strings.Contains(host, "galaxy") || strings.Contains(host, "pixel") || strings.Contains(host, "redmi"):
		s.set(&s.r.OS, "android", "hostname:"+s.in.Hostname)
	case strings.Contains(host, "raspberrypi"):
		s.set(&s.r.OS, "raspberrypi", "hostname:"+s.in.Hostname)
	}
	for _, svc := range []string{"_nearbypresence._tcp", "_androidtvremote2._tcp"} {
		if s.service(svc) {
			s.set(&s.r.OS, "android", "mdns:"+svc)
		}
	}
	for _, svc := range []string{"_companion-link._tcp", "_airplay._tcp", "_raop._tcp"} {
		if s.service(svc) && s.r.OS == "" {
			s.set(&s.r.OS, "apple", "mdns:"+svc) // refined to ios/macos below when possible
		}
	}
	if s.port(62078) { // iOS lockdownd: iPhone or iPad
		if s.r.OS == "" || s.r.OS == "apple" {
			s.r.OS = ""
			s.set(&s.r.OS, "ios", "port:62078")
		}
	}
	if (s.port(548) || s.service("_afpovertcp._tcp")) && (s.r.OS == "" || s.r.OS == "apple") {
		s.r.OS = ""
		s.set(&s.r.OS, "macos", "port:548")
	}
	if s.r.OS == "" && s.in.TTL > 64 && s.in.TTL <= 128 {
		s.set(&s.r.OS, "windows", "ttl:"+strconv.Itoa(s.in.TTL))
	}
}

// brands maps vendor name fragments (MAC registry or UPnP manufacturer) to brand slugs.
var brands = []struct{ fragment, brand string }{
	{"apple", "apple"},
	{"samsung", "samsung"},
	{"huawei", "huawei"},
	{"honor device", "honor"},
	{"xiaomi", "xiaomi"},
	{"beijing xiaomi", "xiaomi"},
	{"oneplus", "oneplus"},
	{"motorola", "motorola"},
	{"google", "google"},
	{"lg", "lg"},
	{"sony interactive", "playstation"},
	{"sony", "sony"},
	{"raspberry pi", "raspberrypi"},
	{"espressif", "espressif"},
	{"synology", "synology"},
	{"qnap", "qnap"},
	{"tp-link", "tplink"},
	{"mercusys", "mercusys"},
	{"ubiquiti", "ubiquiti"},
	{"mikrotik", "mikrotik"},
	{"routerboard", "mikrotik"},
	{"netgear", "netgear"},
	{"asustek", "asus"},
	{"intel", "intel"},
	{"realtek", "realtek"},
	{"dell", "dell"},
	{"lenovo", "lenovo"},
	{"hewlett packard", "hp"},
	{"hp inc", "hp"},
	{"amazon", "amazon"},
	{"sonos", "sonos"},
	{"signify", "philipshue"},
	{"philips lighting", "philipshue"},
	{"midea", "midea"},
	{"nintendo", "nintendo"},
	{"roku", "roku"},
	{"proxmox", "proxmox"},
	{"vmware", "vmware"},
	{"freebsd", "freebsd"},
	{"tuya", "tuya"},
	{"shelly", "shelly"},
	{"allterco", "shelly"},
	{"hikvision", "hikvision"},
	{"dahua", "dahua"},
	{"reolink", "reolink"},
	{"epson", "epson"},
	{"brother", "brother"},
	{"canon", "canon"},
	{"micro-star", "msi"},
	{"gigabyte", "gigabyte"},
	{"nvidia", "nvidia"},
	{"tenda", "tenda"},
	{"d-link", "dlink"},
	{"zte", "zte"},
	{"hongrui", "horaco"},
	{"motorola mobility", "motorola"},
	{"oppo", "oppo"},
	{"vivo mobile", "vivo"},
	{"realme", "realme"},
	{"american power conversion", "apc"},
	{"apc", "apc"},
	{"cyberpower", "cyberpower"},
	{"yealink", "yealink"},
	{"polycom", "poly"},
	{"snom", "snom"},
	{"fanvil", "fanvil"},
}

// containsWord reports whether frag appears in s as whole words: "lg" is in
// "LG Electronics" but not in "Algo Communication".
func containsWord(s, frag string) bool {
	alnum := func(b byte) bool { return b >= 'a' && b <= 'z' || b >= '0' && b <= '9' }
	for i := 0; ; {
		j := strings.Index(s[i:], frag)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(frag)
		if (start == 0 || !alnum(s[start-1])) && (end == len(s) || !alnum(s[end])) {
			return true
		}
		i = start + 1
	}
}

func brand(s *state) {
	v := strings.ToLower(s.in.Vendor)
	if v == "" {
		return
	}
	for _, b := range brands {
		if containsWord(v, b.fragment) {
			s.set(&s.r.Brand, b.brand, "vendor:"+s.in.Vendor)
			return
		}
	}
}

var (
	phoneBrands   = []string{"samsung", "huawei", "honor", "xiaomi", "oneplus", "motorola", "oppo", "vivo", "realme"}
	pcBrands      = []string{"intel", "realtek", "dell", "lenovo", "hp", "asus", "msi", "gigabyte", "nvidia"}
	networkBrands = []string{"tplink", "mercusys", "ubiquiti", "mikrotik", "netgear", "tenda", "dlink", "zte"}
	smartHome     = []string{"espressif", "tuya", "shelly", "philipshue"}
	cameraBrands  = []string{"hikvision", "dahua", "reolink"}
	printerBrands = []string{"epson", "brother", "canon"}
	ipPhoneBrands = []string{"yealink", "poly", "snom", "fanvil"}
	upsBrands     = []string{"apc", "cyberpower"}
	// Makers of access control only (badge readers, door controllers).
	accessControlVendors = []string{"hid global", "zkteco", "suprema", "paxton"}
	desktopOSes          = []string{"windows", "macos", "nixos", "ubuntu", "fedora", "archlinux", "linuxmint", "popos", "manjaro", "opensuse"}
	serverOSes           = []string{"debian", "freebsd", "alpinelinux", "centos", "redhat", "raspberrypi"}
	printerPorts         = []int{9100, 631, 515}
	printerService       = []string{"_ipp._tcp", "_ipps._tcp", "_printer._tcp", "_pdl-datastream._tcp"}
)

func deviceType(s *state) {
	r := &s.r
	in := s.in
	if in.Self {
		s.set(&r.Type, Computer, "this-server")
		return
	}
	switch in.Role {
	case Firewall, Router, Switch, AccessPoint, Server:
		s.set(&r.Type, in.Role, "integration:"+in.Role)
	}
	for _, rule := range nameRules {
		if r.Type != "" {
			break
		}
		if rule.only != "" && r.Brand != rule.only {
			continue
		}
		if m := rule.re.FindString(s.txt); m != "" {
			s.set(&r.Type, rule.typ, evidence(s, m))
			s.set(&r.Brand, rule.brand, "")
		}
	}
	if in.Upstream {
		s.set(&r.Type, Router, "wan") // the ISP's modem or router
	}
	if slices.Contains(ipPhoneBrands, r.Brand) {
		s.set(&r.Type, IPPhone, "vendor:"+in.Vendor)
	}
	if slices.Contains(upsBrands, r.Brand) {
		s.set(&r.Type, UPS, "vendor:"+in.Vendor)
	}
	for _, p := range printerPorts {
		if s.port(p) {
			s.set(&r.Type, Printer, "port:"+strconv.Itoa(p))
		}
	}
	for _, svc := range printerService {
		if s.service(svc) {
			s.set(&r.Type, Printer, "mdns:"+svc)
		}
	}
	if slices.Contains(printerBrands, r.Brand) {
		s.set(&r.Type, Printer, "")
	}
	if s.port(554) || slices.Contains(cameraBrands, r.Brand) {
		s.set(&r.Type, Camera, "port:554")
	}
	switch {
	case s.service("_googlecast._tcp"):
		s.set(&r.Type, MediaPlayer, "mdns:_googlecast._tcp")
		s.set(&r.Brand, "google", "")
	case s.service("_amzn-wplay._tcp"):
		s.set(&r.Type, MediaPlayer, "mdns:_amzn-wplay._tcp")
		s.set(&r.Brand, "amazon", "")
	case r.OS == "tvos":
		s.set(&r.Type, MediaPlayer, "")
	case s.service("upnp:MediaRenderer") && slices.Contains([]string{"samsung", "lg", "sony"}, r.Brand):
		s.set(&r.Type, TV, "upnp:MediaRenderer")
	case s.service("_sonos._tcp") || r.Brand == "sonos":
		s.set(&r.Type, Speaker, "mdns:_sonos._tcp")
	case s.service("_hue._tcp") || s.service("_hap._tcp") || s.service("_matter._tcp") || slices.Contains(smartHome, r.Brand):
		s.set(&r.Type, SmartHome, "")
	case slices.ContainsFunc(solarVendors, func(v string) bool { return strings.Contains(strings.ToLower(in.Vendor), v) }):
		s.set(&r.Type, SolarInverter, "vendor:"+in.Vendor)
	case strings.Contains(strings.ToLower(in.Vendor), "air-condition") || strings.Contains(strings.ToLower(in.Vendor), "air condition") ||
		strings.Contains(strings.ToLower(in.Vendor), "daikin"):
		s.set(&r.Type, AirConditioner, "vendor:"+in.Vendor)
	case slices.ContainsFunc(accessControlVendors, func(v string) bool { return strings.Contains(strings.ToLower(in.Vendor), v) }):
		s.set(&r.Type, SmartHome, "vendor:"+in.Vendor) // badge readers and door controllers
	case r.Brand == "midea":
		s.set(&r.Type, Appliance, "vendor:"+in.Vendor)
	case r.Brand == "nintendo" || r.Brand == "playstation":
		s.set(&r.Type, GameConsole, "vendor:"+in.Vendor)
	}
	host := strings.ToLower(in.Hostname)
	switch {
	case r.OS == "ios" && strings.Contains(host, "ipad"):
		s.set(&r.Type, Tablet, "hostname:"+in.Hostname)
	case strings.Contains(host, "watch"):
		s.set(&r.Type, Wearable, "hostname:"+in.Hostname)
	case r.OS == "ios":
		s.set(&r.Type, Phone, "")
	case r.OS == "android" && (strings.Contains(host, "tab") || strings.Contains(host, "pad")):
		s.set(&r.Type, Tablet, "hostname:"+in.Hostname)
	case r.OS == "android":
		s.set(&r.Type, Phone, "")
	}
	if r.Brand == "proxmox" || r.Brand == "vmware" {
		s.set(&r.Type, VirtualMachine, "vendor:"+in.Vendor)
	}
	if slices.Contains(networkBrands, r.Brand) && (s.port(80) || s.port(443)) {
		s.set(&r.Type, AccessPoint, "vendor:"+in.Vendor) // consumer Wi-Fi routers / APs
	}
	// Phones answer pings with TTL 64; 255 is typical of embedded/network gear.
	// Only for a device nothing else identified: a Huawei inverter or a
	// Samsung TV is no Android phone.
	if r.Type == "" && slices.Contains(phoneBrands, r.Brand) && len(in.OpenPorts) == 0 && in.TTL <= 64 {
		s.set(&r.Type, Phone, "vendor:"+in.Vendor)
		s.set(&r.OS, "android", "")
	}
	if r.Brand == "apple" && r.OS == "" {
		s.set(&r.OS, "apple", "")
	}
	if slices.Contains(desktopOSes, r.OS) || slices.Contains(pcBrands, r.Brand) {
		s.set(&r.Type, Computer, "")
	}
	if slices.Contains(serverOSes, r.OS) || r.Brand == "raspberrypi" || s.port(22) {
		s.set(&r.Type, Server, "")
	}
	if in.RandomMAC && len(in.OpenPorts) == 0 {
		s.set(&r.Type, Phone, "private-mac") // phones use private addresses by default
	}
}
