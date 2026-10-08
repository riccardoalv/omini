package notify

import (
	"net/url"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/topology"
)

// DeviceCard is what Omini knows about the device an alert is about, so a
// message says which machine it is and where to find it.
type DeviceCard struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Type        string     `json:"type,omitempty"`   // in the reader's language
	Vendor      string     `json:"vendor,omitempty"` // brand, else the MAC's vendor
	Model       string     `json:"model,omitempty"`
	OS          string     `json:"os,omitempty"`
	IP          string     `json:"ip,omitempty"`
	MAC         string     `json:"mac,omitempty"`
	ConnectedTo string     `json:"connected_to,omitempty"` // "core-sw1 · Port 3", "ap-lobby · IOT"
	Online      bool       `json:"online"`
	LastSeen    *time.Time `json:"last_seen,omitempty"` // offline: when it was last present
	URL         string     `json:"url,omitempty"`       // the device on Omini's map
}

// Item is one alert of a card.
type Item struct {
	Severity string `json:"severity"`
	Label    string `json:"label"`  // what happened ("Memory almost full")
	Detail   string `json:"detail"` // the numbers ("Memory at 98%.")
}

// Context is what a message can say beyond the alerts: the map (the devices'
// details) and Omini's address (links). Both optional.
type Context struct {
	Topology *topology.Topology
	BaseURL  string // e.g. http://omini.lan:8080
	Now      time.Time
}

// nodeURL links a node on the map ("" without Omini's address).
func nodeURL(base, id string) string {
	if base == "" || id == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/?node=" + url.QueryEscape(id)
}

// cards indexes a map's nodes and where each hangs from.
type cards struct {
	nodes  map[string]topology.Node
	uplink map[string]topology.Edge
	base   string
	locale string
}

func newCards(c Context, locale string) *cards {
	k := &cards{nodes: map[string]topology.Node{}, uplink: map[string]topology.Edge{}, base: c.BaseURL, locale: locale}
	if c.Topology == nil {
		return k
	}
	for _, n := range c.Topology.Nodes {
		k.nodes[n.ID] = n
	}
	for _, e := range c.Topology.Edges {
		if _, ok := k.uplink[e.Target]; !ok && e.Kind != topology.EdgeVPN {
			k.uplink[e.Target] = e
		}
	}
	return k
}

// card describes a node (nil when the map does not have it).
func (k *cards) card(id string) *DeviceCard {
	n, ok := k.nodes[id]
	if !ok {
		return nil
	}
	c := &DeviceCard{
		ID: n.ID, Name: n.Label, OS: n.OS, IP: n.IP, MAC: n.MAC,
		Online: n.Online, LastSeen: n.LastSeen, URL: nodeURL(k.base, n.ID),
	}
	if t := n.Type; t != "" && t != "unknown" {
		c.Type = localeOf(typeNames, k.locale)[t]
	} else if n.Kind == topology.KindSegment || n.Kind == topology.KindUnmanaged {
		c.Type = localeOf(typeNames, k.locale)["segment"]
	}
	c.Vendor = n.Brand
	if c.Vendor == "" {
		c.Vendor = n.Vendor
	}
	c.Model = n.Model
	if c.Model == "" && n.Device != nil && n.Device.Model != nil {
		c.Model = *n.Device.Model
	}
	if c.MAC == "" && n.Device != nil && len(n.Device.MACs) > 0 {
		c.MAC = string(n.Device.MACs[0])
	}
	if c.IP == "" && n.Device != nil && len(n.Device.IPs) > 0 {
		c.IP = n.Device.IPs[0]
	}
	c.ConnectedTo = k.connectedTo(n)
	if n.Online {
		c.LastSeen = nil
	}
	return c
}

// connectedTo names where a node hangs: its parent and the port (or the
// Wi-Fi network) it uses.
func (k *cards) connectedTo(n topology.Node) string {
	parent, port := n.ParentID, n.Port
	if n.SSID != "" {
		port = n.SSID
	}
	if parent == "" {
		if e, ok := k.uplink[n.ID]; ok {
			parent = e.Source
			if port == "" {
				port = e.SourcePort
			}
		}
	}
	if parent == "" || strings.HasPrefix(parent, "wan:") {
		return ""
	}
	p, ok := k.nodes[parent]
	if !ok {
		return ""
	}
	name := p.Label
	if l := p.PortLabels[port]; l != "" {
		port = l
	}
	if port == "" {
		return name
	}
	return name + " · " + port
}

// typeNames name the device types (the UI's "types", kept in step).
var typeNames = map[string]map[string]string{
	"en": {
		"solar_inverter": "Solar inverter", "ip_phone": "IP phone", "ups": "UPS", "wan": "Internet uplink",
		"app": "App", "air_conditioner": "Air conditioner", "firewall": "Firewall", "router": "Router",
		"switch": "Switch", "ap": "Access point", "server": "Server", "nas": "NAS / storage",
		"hypervisor": "Hypervisor", "virtual_machine": "Virtual machine", "computer": "Computer",
		"phone": "Phone", "tablet": "Tablet", "tv": "TV", "media_player": "Media player", "speaker": "Speaker",
		"printer": "Printer", "camera": "Camera", "smart_home": "Smart home device", "appliance": "Appliance",
		"game_console": "Game console", "wearable": "Wearable", "segment": "Unmanaged segment",
	},
	"pt-BR": {
		"solar_inverter": "Inversor solar", "ip_phone": "Telefone IP", "ups": "Nobreak", "wan": "Link de internet",
		"app": "Aplicativo", "air_conditioner": "Ar-condicionado", "firewall": "Firewall", "router": "Roteador",
		"switch": "Switch", "ap": "Access point", "server": "Servidor", "nas": "NAS / armazenamento",
		"hypervisor": "Hypervisor", "virtual_machine": "Máquina virtual", "computer": "Computador",
		"phone": "Celular", "tablet": "Tablet", "tv": "TV", "media_player": "Media player", "speaker": "Caixa de som",
		"printer": "Impressora", "camera": "Câmera", "smart_home": "Casa inteligente", "appliance": "Eletrodoméstico",
		"game_console": "Videogame", "wearable": "Vestível", "segment": "Segmento não gerenciado",
	},
}

// tips say what to do about each rule (the UI's "insights.tips", kept in step).
var tips = map[string]map[string]string{
	"en": {
		"device_offline":        "Check that it is powered and connected; if it is, check the integration's address and credentials.",
		"integration_failed":    `Open the integration and press "Test connection": the message says what is wrong.`,
		"wan_down":              "Check the modem and the cable to it, or call your provider.",
		"wan_degraded":          "Loss or high latency: restart the modem, or check the line with your provider.",
		"duplicate_ip":          "Two devices use the same address: give one a DHCP reservation or another static IP.",
		"update_pending":        "Install the update from the device's own interface when it suits you.",
		"disk_full":             "Delete old files, logs or snapshots, or grow the disk.",
		"hot_cpu":               "Check the fans and the airflow around it.",
		"high_cpu":              "See which process uses it on the device itself; it may be a short peak.",
		"high_memory":           "See what uses the memory; a cache that frees itself is not a problem.",
		"slow_uplink":           "Check the cable (all 8 wires) and both ports: they should reach 1 Gbps.",
		"interface_errors":      "Usually a bad cable or connector: replace the cable first.",
		"weak_wifi":             "Move it closer to the access point, or add one near it.",
		"saturated_link":        "The link is full: find who uses it in Flows, or upgrade the link.",
		"unmanaged_switch":      "Several devices behind one port: a small switch or hub is there.",
		"unknown_neighbor":      "A device announces itself but Omini has no integration for it.",
		"new_device":            "If you do not know it, look at its vendor and where it is connected.",
		"fast_ethernet":         "Many cameras, printers and TVs only do 100 Mbps; otherwise check the cable (all 8 wires) and the port.",
		"integration_available": "Add the integration in Integrations → Add integration.",
		"link_flapping":         "Replace the cable, or fix the speed on both ends instead of auto-negotiation.",
		"half_duplex":           "Set both ends to auto-negotiation (or to the same fixed speed and full-duplex).",
		"device_rebooted":       "A power cut, a crash or an update: check its logs if it was not planned.",
		"sfp_low_rx":            "Clean the fiber connectors and check the fiber is not bent; check the module on the other end.",
		"dhcp_pool_full":        "Widen the DHCP range or shorten the lease time.",
		"firewall_states_full":  "Raise the state table limit, or look for a device opening many connections.",
		"insecure_service":      "Turn it off on the device, or use SSH, SFTP or HTTPS instead.",
		"new_devices_burst":     "A visitor, a new appliance... or someone who should not be on your network.",
		"discovery_limited":     "See Integrations → Network scan: what is missing and how to fix it (usually network_mode: host).",
	},
	"pt-BR": {
		"device_offline":        "Veja se está ligado e conectado; se estiver, confira o endereço e as credenciais da integração.",
		"integration_failed":    `Abra a integração e clique em "Testar conexão": a mensagem diz o que está errado.`,
		"wan_down":              "Confira o modem e o cabo até ele, ou ligue para o provedor.",
		"wan_degraded":          "Perda ou latência alta: reinicie o modem ou verifique a linha com o provedor.",
		"duplicate_ip":          "Dois dispositivos usam o mesmo endereço: dê a um deles uma reserva de DHCP ou outro IP fixo.",
		"update_pending":        "Instale a atualização pela interface do próprio equipamento quando for oportuno.",
		"disk_full":             "Apague arquivos, logs ou snapshots antigos, ou aumente o disco.",
		"hot_cpu":               "Confira os ventiladores e a circulação de ar em volta.",
		"high_cpu":              "Veja no próprio equipamento qual processo está usando; pode ser um pico passageiro.",
		"high_memory":           "Veja o que está usando a memória; cache que se libera sozinho não é problema.",
		"slow_uplink":           "Confira o cabo (os 8 fios) e as duas portas: devem chegar a 1 Gbps.",
		"interface_errors":      "Quase sempre é cabo ou conector ruim: troque o cabo primeiro.",
		"weak_wifi":             "Aproxime do access point, ou coloque um perto dele.",
		"saturated_link":        "O link está cheio: veja quem está usando em Fluxos, ou aumente o link.",
		"unmanaged_switch":      "Vários dispositivos atrás de uma porta: há um switch pequeno ou hub ali.",
		"unknown_neighbor":      "Um equipamento se anuncia, mas o Omini não tem integração para ele.",
		"new_device":            "Se não reconhecer, veja o fabricante e onde ele está conectado.",
		"fast_ethernet":         "Muitas câmeras, impressoras e TVs só fazem 100 Mbps; se não for o caso, confira o cabo (os 8 fios) e a porta.",
		"integration_available": "Adicione a integração em Integrações → Adicionar integração.",
		"link_flapping":         "Troque o cabo, ou fixe a velocidade nas duas pontas em vez da autonegociação.",
		"half_duplex":           "Deixe as duas pontas em autonegociação (ou na mesma velocidade fixa, full-duplex).",
		"device_rebooted":       "Queda de energia, travamento ou atualização: veja os logs dele se não foi planejado.",
		"sfp_low_rx":            "Limpe os conectores da fibra e veja se ela não está dobrada; confira o módulo da outra ponta.",
		"dhcp_pool_full":        "Aumente a faixa do DHCP ou diminua o tempo de lease.",
		"firewall_states_full":  "Aumente o limite da tabela de states, ou procure um dispositivo abrindo muitas conexões.",
		"insecure_service":      "Desligue no equipamento, ou use SSH, SFTP ou HTTPS no lugar.",
		"new_devices_burst":     "Uma visita, um aparelho novo... ou alguém que não deveria estar na sua rede.",
		"discovery_limited":     "Veja em Integrações → Network scan o que falta e como resolver (quase sempre network_mode: host).",
	},
}

// labels name the parts of a card.
var labels = map[string]map[string]string{
	"en": {
		"critical": "Critical", "warning": "Warning", "info": "Notice", "resolved": "Resolved",
		"type": "Type", "vendor": "Make and model", "os": "System", "ip": "IP address", "mac": "MAC",
		"connected": "Connected to", "offline_since": "Offline since", "tip": "What to do",
		"open": "Open in Omini", "open_map": "Open the map", "example": "example",
	},
	"pt-BR": {
		"critical": "Crítico", "warning": "Aviso", "info": "Informação", "resolved": "Resolvido",
		"type": "Tipo", "vendor": "Marca e modelo", "os": "Sistema", "ip": "Endereço IP", "mac": "MAC",
		"connected": "Conectado a", "offline_since": "Offline desde", "tip": "O que fazer",
		"open": "Abrir no Omini", "open_map": "Abrir o mapa", "example": "exemplo",
	},
}

// facts are a card's details as label/value pairs, in reading order.
func (d *DeviceCard) facts(locale string) [][2]string {
	if d == nil {
		return nil
	}
	l := localeOf(labels, locale)
	var out [][2]string
	add := func(key, v string) {
		if v != "" {
			out = append(out, [2]string{l[key], v})
		}
	}
	kind, maker := d.Type, strings.Join(nonEmpty(d.Vendor, d.Model), " ")
	if kind != "" && d.Model == "" && d.Vendor != "" {
		// A brand alone reads better next to the type: "Switch · Cisco".
		kind, maker = kind+" · "+d.Vendor, ""
	}
	add("type", kind)
	add("vendor", maker)
	add("ip", d.IP)
	add("mac", d.MAC)
	add("connected", d.ConnectedTo)
	add("os", d.OS)
	if d.LastSeen != nil {
		add("offline_since", d.LastSeen.Local().Format("2006-01-02 15:04"))
	}
	return out
}

func nonEmpty(s ...string) []string {
	var out []string
	for _, v := range s {
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
