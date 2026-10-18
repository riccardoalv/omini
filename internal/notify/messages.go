package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// Texts of the alerts sent by the server, in the languages of the UI (which
// has its own copy, web/src/i18n: keep both in step). {name} is replaced by
// the alert's parameter of that name.
var texts = map[string]map[string][2]string{
	"en": {
		"device_offline":             {"{node} is offline", "Its integration could not reach it. {error}"},
		"integration_failed":         {"The {integration} integration is failing", "{error}"},
		"wan_down":                   {"Internet down on {node}", "Gateway {gateway} does not answer."},
		"wan_degraded":               {"Unstable internet on {node}", "Gateway {gateway}: {loss_pct}% loss, {rtt_ms} ms."},
		"duplicate_ip":               {"Duplicate IP {ip}", "Used by several devices at once: {devices}"},
		"update_pending":             {"Update available for {node}", "{latest} is available ({updates} updates)."},
		"update_pending_count":       {"Update available for {node}", "{updates} updates available."},
		"update_pending_latest":      {"Update available for {node}", "{latest} is available."},
		"disk_full":                  {"Disk almost full on {node}", "{mount} is {pct}% full."},
		"hot_cpu":                    {"{node} is running hot", "{sensor} at {celsius} °C."},
		"high_cpu":                   {"High CPU on {node}", "CPU at {pct}%."},
		"high_memory":                {"Memory almost full on {node}", "Memory at {pct}%."},
		"slow_uplink":                {"Slow uplink to {node}", "The link from {from} ({port}) runs at {speed}: below 1 Gbps."},
		"interface_errors":           {"Errors on {iface} of {node}", "{rx_errors} receive and {tx_errors} transmit errors in the last poll."},
		"weak_wifi":                  {"Weak Wi-Fi signal on {node}", "{signal_dbm} dBm on {ap}."},
		"saturated_link":             {"{iface} of {node} is saturated", "{pct}% of {speed} in use."},
		"unmanaged_switch":           {"Likely unmanaged switch on {parent}", "{macs} devices behind {port}."},
		"unknown_neighbor":           {"Unknown neighbor {node}", "Announced by LLDP on {parent} {port}, without an integration."},
		"new_device":                 {"New device: {node}", "{vendor} {mac} {ip}"},
		"fast_ethernet":              {"{node} is connected at {speed}", "Its link from {from} {port} runs at Fast Ethernet speed or less."},
		"integration_available":      {"{integration} found: {node}", "The {integration} integration would show its ports, health and what runs on it."},
		"link_flapping":              {"{iface} of {node} keeps going down", "{changes} up/down changes in the last hour."},
		"half_duplex":                {"{iface} of {node} is half-duplex", "The link negotiated half-duplex: collisions and slow traffic."},
		"device_rebooted":            {"{node} restarted", "Its uptime went back to zero."},
		"sfp_low_rx":                 {"Weak optical signal on {iface} of {node}", "Receiving {rx_dbm} dBm, below {limit_dbm} dBm."},
		"dhcp_pool_full":             {"DHCP pool almost full on {node}", "{network}: {used} of {total} addresses in use ({pct}%)."},
		"sfp_hot":                    {"Hot optical module on {iface} of {node}", "At {temp_c} °C, the limit is {limit_c} °C."},
		"lag_member_down":            {"{iface} of {lag} on {node} is down", "The LAG runs on fewer links: less bandwidth and no redundancy."},
		"stp_root_changed":           {"The spanning tree root changed for {node}", "It was {from}, now {to}."},
		"config_changed":             {"The configuration of {node} changed", "It changed outside Omini: see the differences in its panel, under Configuration."},
		"dns_bypass":                 {"{node} goes around your DNS server", "It asked DNS servers on the internet in the last hour: {servers}."},
		"upload_unusual":             {"{node} sent much more to the internet than usual", "{mb} MB in the last hour, against about {usual_mb} MB in a usual hour."},
		"lan_scan":                   {"{node} scanned your network", "It reached {targets} services on your network within a minute."},
		"infra_dynamic_ip":           {"{node} has no fixed IP", "It gets {ip} by DHCP: the address can change."},
		"iot_with_trusted":           {"IoT devices share a network with your servers and computers", "{network}: {iot_devices} beside {trusted_devices}."},
		"wifi_channel_conflict":      {"Wi-Fi channels of your access points overlap", "On 2.4 GHz: {aps}."},
		"wifi_channel_conflict_auto": {"Wi-Fi channels of your access points overlap", "On 2.4 GHz: {aps}. On automatic channel (left to their controller): {auto_aps}."},
		"port_forward":               {"Port {ports}/{protocol} open to {target}", "{node} forwards it from the internet to {internal}. {description}"},
		"upnp_forward":               {"{target} opened port {ports}/{protocol} by itself", "Through UPnP or NAT-PMP on {node}, to {internal}. {description}"},
		"firewall_states_full":       {"Firewall state table almost full on {node}", "{current} of {limit} states ({pct}%)."},
		"insecure_service":           {"{service} open on {node}", "Port {port}: passwords and data travel unencrypted."},
		"new_devices_burst":          {"{count} new devices in {minutes} minutes", "{devices}"},
		"discovery_limited":          {"Network discovery is limited here", "Not available where Omini runs: {limits}. See the README: what the network scan needs."},
		"dhcp_no_offer":              {"No DHCP server answers on {iface}", "Devices that join the network get no address."},
		"dhcp_rogue":                 {"Unknown DHCP server at {ip}", "It answers address requests on {iface} besides your router: devices may get a wrong address."},
		"dns_down":                   {"DNS {server} does not answer", "It could not resolve {name}: {error}."},
		"dns_slow":                   {"DNS {server} is slow", "It took {ms} ms to resolve a name."},
		"dns_local":                  {"DNS {server} does not resolve local names", "{name}: {error}."},
		"vlan_unknown":               {"VLAN {vlan} on {node} is not one of the network's", "It was created on the device ({name}), not in Omini."},
		"_resolved":                  {"Resolved: {title}", ""},
		"_subject":                   {"Omini: {title}", "Omini: {n} alerts"},
		"_test":                      {"Omini test message", "Notifications from Omini reach this channel."},
	},
	"pt-BR": {
		"device_offline":             {"{node} está offline", "A integração não conseguiu alcançá-lo. {error}"},
		"integration_failed":         {"A integração {integration} está falhando", "{error}"},
		"wan_down":                   {"Internet fora em {node}", "O gateway {gateway} não responde."},
		"wan_degraded":               {"Internet instável em {node}", "Gateway {gateway}: {loss_pct}% de perda, {rtt_ms} ms."},
		"duplicate_ip":               {"IP duplicado {ip}", "Usado por vários dispositivos ao mesmo tempo: {devices}"},
		"update_pending":             {"Atualização disponível para {node}", "{latest} disponível ({updates} atualizações)."},
		"update_pending_count":       {"Atualização disponível para {node}", "{updates} atualizações disponíveis."},
		"update_pending_latest":      {"Atualização disponível para {node}", "{latest} disponível."},
		"disk_full":                  {"Disco quase cheio em {node}", "{mount} está {pct}% cheio."},
		"hot_cpu":                    {"{node} está esquentando", "{sensor} a {celsius} °C."},
		"high_cpu":                   {"CPU alta em {node}", "CPU em {pct}%."},
		"high_memory":                {"Memória quase cheia em {node}", "Memória em {pct}%."},
		"slow_uplink":                {"Uplink lento para {node}", "O link a partir de {from} ({port}) roda a {speed}: abaixo de 1 Gbps."},
		"interface_errors":           {"Erros em {iface} de {node}", "{rx_errors} erros de recepção e {tx_errors} de transmissão na última coleta."},
		"weak_wifi":                  {"Sinal Wi-Fi fraco em {node}", "{signal_dbm} dBm em {ap}."},
		"saturated_link":             {"{iface} de {node} está saturada", "{pct}% de {speed} em uso."},
		"unmanaged_switch":           {"Provável switch não gerenciável em {parent}", "{macs} dispositivos atrás de {port}."},
		"unknown_neighbor":           {"Vizinho desconhecido {node}", "Anunciado por LLDP em {parent} {port}, sem integração."},
		"new_device":                 {"Dispositivo novo: {node}", "{vendor} {mac} {ip}"},
		"fast_ethernet":              {"{node} está conectado a {speed}", "O link a partir de {from} {port} roda em Fast Ethernet ou menos."},
		"integration_available":      {"{integration} encontrado: {node}", "A integração {integration} mostraria as portas, a saúde e o que roda nele."},
		"link_flapping":              {"{iface} de {node} está caindo e voltando", "{changes} quedas e retornos na última hora."},
		"half_duplex":                {"{iface} de {node} está em half-duplex", "O link negociou half-duplex: colisões e tráfego lento."},
		"device_rebooted":            {"{node} reiniciou", "O tempo ligado voltou a zero."},
		"sfp_low_rx":                 {"Sinal óptico fraco em {iface} de {node}", "Recebendo {rx_dbm} dBm, abaixo de {limit_dbm} dBm."},
		"dhcp_pool_full":             {"Pool DHCP quase cheio em {node}", "{network}: {used} de {total} endereços em uso ({pct}%)."},
		"sfp_hot":                    {"Módulo óptico quente em {iface} de {node}", "A {temp_c} °C, o limite é {limit_c} °C."},
		"lag_member_down":            {"{iface} do {lag} em {node} está fora", "O LAG funciona com menos links: menos banda e sem redundância."},
		"stp_root_changed":           {"A raiz do spanning tree mudou para {node}", "Era {from}, agora {to}."},
		"config_changed":             {"A configuração de {node} mudou", "Ela mudou fora do Omini: veja as diferenças no painel dele, em Configuração."},
		"dns_bypass":                 {"{node} contorna seu servidor DNS", "Na última hora ele consultou servidores DNS na internet: {servers}."},
		"upload_unusual":             {"{node} enviou muito mais para a internet que o normal", "{mb} MB na última hora, contra cerca de {usual_mb} MB numa hora normal."},
		"lan_scan":                   {"{node} varreu sua rede", "Ele acessou {targets} serviços da sua rede em um minuto."},
		"infra_dynamic_ip":           {"{node} não tem IP fixo", "Ele recebe {ip} por DHCP: o endereço pode mudar."},
		"iot_with_trusted":           {"Dispositivos IoT na mesma rede que seus servidores e computadores", "{network}: {iot_devices} junto de {trusted_devices}."},
		"wifi_channel_conflict":      {"Canais de Wi-Fi dos seus pontos de acesso se sobrepõem", "Em 2,4 GHz: {aps}."},
		"wifi_channel_conflict_auto": {"Canais de Wi-Fi dos seus pontos de acesso se sobrepõem", "Em 2,4 GHz: {aps}. Em canal automático (deixados com a controladora): {auto_aps}."},
		"port_forward":               {"Porta {ports}/{protocol} aberta para {target}", "{node} a encaminha da internet para {internal}. {description}"},
		"upnp_forward":               {"{target} abriu a porta {ports}/{protocol} sozinho", "Por UPnP ou NAT-PMP em {node}, para {internal}. {description}"},
		"firewall_states_full":       {"Tabela de states quase cheia em {node}", "{current} de {limit} states ({pct}%)."},
		"insecure_service":           {"{service} aberto em {node}", "Porta {port}: senhas e dados trafegam sem criptografia."},
		"new_devices_burst":          {"{count} dispositivos novos em {minutes} minutos", "{devices}"},
		"discovery_limited":          {"A descoberta da rede está limitada aqui", "Indisponível onde o Omini roda: {limits}. Veja no README o que o scan da rede precisa."},
		"dhcp_no_offer":              {"Nenhum servidor DHCP responde em {iface}", "Quem entra na rede não recebe endereço."},
		"dhcp_rogue":                 {"Servidor DHCP desconhecido em {ip}", "Ele responde pedidos de endereço em {iface} além do seu roteador: aparelhos podem receber um endereço errado."},
		"dns_down":                   {"O DNS {server} não responde", "Não resolveu {name}: {error}."},
		"dns_slow":                   {"O DNS {server} está lento", "Levou {ms} ms para resolver um nome."},
		"dns_local":                  {"O DNS {server} não resolve nomes locais", "{name}: {error}."},
		"vlan_unknown":               {"A VLAN {vlan} em {node} não é da rede", "Foi criada no equipamento ({name}), não no Omini."},
		"_resolved":                  {"Resolvido: {title}", ""},
		"_subject":                   {"Omini: {title}", "Omini: {n} alertas"},
		"_test":                      {"Mensagem de teste do Omini", "As notificações do Omini chegam a este canal."},
	},
}

func catalog(locale string) map[string][2]string {
	if t, ok := texts[locale]; ok {
		return t
	}
	return texts["en"]
}

// fill replaces {name} with the parameter's value; unknown names disappear.
func fill(s string, params map[string]string) string {
	var b strings.Builder
	for {
		i := strings.IndexByte(s, '{')
		j := strings.IndexByte(s[max(i, 0):], '}')
		if i < 0 || j < 0 {
			b.WriteString(s)
			break
		}
		b.WriteString(s[:i])
		b.WriteString(params[s[i+1:i+j]])
		s = s[i+j+1:]
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func formatSpeed(mbps float64) string {
	if mbps >= 1000 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", mbps/1000), ".0") + "G"
	}
	return fmt.Sprintf("%.0fM", mbps)
}

func formatRate(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.1f Gbps", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.1f Mbps", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.0f kbps", bps/1e3)
	}
	return fmt.Sprintf("%.0f bps", bps)
}

// params turns an alert's parameters into text.
func params(a store.Alert) map[string]string {
	out := map[string]string{}
	for k, v := range a.Params {
		if x, ok := number(v); ok {
			out[k] = strings.TrimSuffix(strings.TrimSuffix(fmt.Sprintf("%.1f", x), "0"), ".")
		} else if v != nil {
			out[k] = fmt.Sprint(v)
		}
	}
	if v, ok := number(a.Params["speed_mbps"]); ok {
		out["speed"] = formatSpeed(v)
	}
	for _, k := range []string{"rx_bps", "tx_bps"} {
		if v, ok := number(a.Params[k]); ok {
			out[k] = formatRate(v)
		}
	}
	for _, k := range []string{"loss_pct", "rtt_ms", "updates", "latest"} {
		if out[k] == "" {
			out[k] = "?"
		}
	}
	return out
}

// Describe returns an alert's title and detail in a language.
func Describe(a store.Alert, locale string) (title, detail string) {
	t := catalog(locale)
	rule := a.Rule
	switch {
	case rule == "update_pending" && a.Params["latest"] == nil:
		rule = "update_pending_count" // the device did not say which version
	case rule == "update_pending" && a.Params["updates"] == nil:
		rule = "update_pending_latest" // nor how many updates
	case rule == "wifi_channel_conflict" && a.Params["auto_aps"] != nil:
		rule = "wifi_channel_conflict_auto" // some are left to their controller
	}
	pair, ok := t[rule]
	if !ok {
		return a.Rule, ""
	}
	p := params(a)
	return fill(pair[0], p), fill(pair[1], p)
}

// short names a rule without its device, for a line under the device's name.
var short = map[string]map[string]string{
	"en": {
		"device_offline": "Offline", "integration_failed": "Integration failing", "wan_down": "Internet down",
		"wan_degraded": "Unstable internet", "duplicate_ip": "Duplicate IP", "update_pending": "Update available",
		"disk_full": "Disk almost full", "hot_cpu": "Running hot", "high_cpu": "High CPU",
		"high_memory": "Memory almost full", "slow_uplink": "Slow uplink", "interface_errors": "Interface errors",
		"weak_wifi": "Weak Wi-Fi signal", "saturated_link": "Saturated link",
		"unmanaged_switch": "Likely unmanaged switch", "unknown_neighbor": "Unknown neighbor",
		"fast_ethernet": "Fast Ethernet link", "integration_available": "Integration available", "link_flapping": "Link going down",
		"half_duplex": "Half-duplex link", "device_rebooted": "Restarted", "sfp_low_rx": "Weak optical signal",
		"dhcp_pool_full": "DHCP pool almost full", "infra_dynamic_ip": "No fixed IP", "iot_with_trusted": "IoT beside servers", "wifi_channel_conflict": "Wi-Fi channels overlap", "port_forward": "Port forwarded", "upnp_forward": "Port opened by UPnP", "config_changed": "Configuration changed", "dns_bypass": "DNS around your server", "upload_unusual": "Unusual upload", "lan_scan": "Network scan", "stp_root_changed": "Spanning tree root changed", "lag_member_down": "LAG member down", "sfp_hot": "Hot optical module", "firewall_states_full": "State table almost full", "insecure_service": "Insecure service",
		"discovery_limited": "Discovery limited", "dhcp_no_offer": "DHCP not answering", "dhcp_rogue": "Unknown DHCP server",
		"dns_down": "DNS not answering", "dns_slow": "Slow DNS", "dns_local": "Local names not resolving", "vlan_unknown": "VLAN outside the network's",
	},
	"pt-BR": {
		"device_offline": "Offline", "integration_failed": "Integração falhando", "wan_down": "Internet fora",
		"wan_degraded": "Internet instável", "duplicate_ip": "IP duplicado", "update_pending": "Atualização disponível",
		"disk_full": "Disco quase cheio", "hot_cpu": "Esquentando", "high_cpu": "CPU alta",
		"high_memory": "Memória quase cheia", "slow_uplink": "Uplink lento", "interface_errors": "Erros de interface",
		"weak_wifi": "Sinal Wi-Fi fraco", "saturated_link": "Link saturado",
		"unmanaged_switch": "Provável switch não gerenciável", "unknown_neighbor": "Vizinho desconhecido",
		"fast_ethernet": "Link Fast Ethernet", "integration_available": "Integração disponível", "link_flapping": "Link caindo",
		"half_duplex": "Link half-duplex", "device_rebooted": "Reiniciou", "sfp_low_rx": "Sinal óptico fraco",
		"dhcp_pool_full": "Pool DHCP quase cheio", "infra_dynamic_ip": "Sem IP fixo", "iot_with_trusted": "IoT junto de servidores", "wifi_channel_conflict": "Canais de Wi-Fi sobrepostos", "port_forward": "Porta encaminhada", "upnp_forward": "Porta aberta por UPnP", "config_changed": "Configuração alterada", "dns_bypass": "DNS fora do seu servidor", "upload_unusual": "Upload fora do normal", "lan_scan": "Varredura na rede", "stp_root_changed": "Raiz do spanning tree mudou", "lag_member_down": "Membro do LAG fora", "sfp_hot": "Módulo óptico quente", "firewall_states_full": "Tabela de states quase cheia", "insecure_service": "Serviço inseguro",
		"discovery_limited": "Descoberta limitada", "dhcp_no_offer": "DHCP não responde", "dhcp_rogue": "Servidor DHCP desconhecido",
		"dns_down": "DNS não responde", "dns_slow": "DNS lento", "dns_local": "Nomes locais não resolvem", "vlan_unknown": "VLAN fora da rede",
	},
}

// counts words the summary of a message: {singular, plural}.
var counts = map[string]map[string][2]string{
	"en": {
		"critical": {"{n} critical", "{n} critical"}, "warning": {"{n} warning", "{n} warnings"},
		"info": {"{n} notice", "{n} notices"}, "new": {"{n} new device", "{n} new devices"},
		"resolved": {"{n} resolved", "{n} resolved"},
	},
	"pt-BR": {
		"critical": {"{n} crítico", "{n} críticos"}, "warning": {"{n} aviso", "{n} avisos"},
		"info": {"{n} informação", "{n} informações"}, "new": {"{n} dispositivo novo", "{n} dispositivos novos"},
		"resolved": {"{n} resolvido", "{n} resolvidos"},
	},
}

func localeOf(m map[string]map[string]string, locale string) map[string]string {
	if t, ok := m[locale]; ok {
		return t
	}
	return m["en"]
}

func count(locale, key string, n int) string {
	c, ok := counts[locale]
	if !ok {
		c = counts["en"]
	}
	form := c[key][1]
	if n == 1 {
		form = c[key][0]
	}
	return fill(form, map[string]string{"n": fmt.Sprint(n)})
}

// Compose builds one message for several alert changes (a collection round):
// a group per device with what happened to it (most severe first), the new
// devices together, and what was resolved; the subject sums it up.
func Compose(changes []store.AlertChange, locale string) Message {
	return ComposeWith(changes, locale, Context{})
}

// ComposeWith is Compose with the map and Omini's address: each card then
// describes its device (type, make, address, where it is connected) and
// links to it.
func ComposeWith(changes []store.AlertChange, locale string, ctx Context) Message {
	rank := map[string]int{"critical": 0, "warning": 1, "info": 2}
	sorted := append([]store.AlertChange(nil), changes...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if a.Opened != b.Opened {
			return a.Opened
		}
		return rank[a.Alert.Severity] < rank[b.Alert.Severity]
	})
	t := catalog(locale)
	names := localeOf(short, locale)
	advice := localeOf(tips, locale)
	k := newCards(ctx, locale)
	m := Message{URL: strings.TrimRight(ctx.BaseURL, "/"), At: ctx.Now, Locale: locale}
	if m.At.IsZero() {
		m.At = time.Now()
	}

	var (
		groups   []*Group
		byKey    = map[string]*Group{}
		newDevs  = &Group{Severity: "info"}
		resolved = &Group{Severity: "resolved", Title: localeOf(map[string]map[string]string{
			"en": {"t": "Resolved"}, "pt-BR": {"t": "Resolvidos"},
		}, locale)["t"]}
		tally = map[string]int{}
	)
	for _, c := range sorted {
		title, detail := Describe(c.Alert, locale)
		m.Events = append(m.Events, Event{Opened: c.Opened, Alert: c.Alert, Title: title, Detail: detail})
		if !c.Opened {
			resolved.Lines = append(resolved.Lines, title)
			tally["resolved"]++
			continue
		}
		if c.Alert.Rule == "new_device" {
			p := params(c.Alert)
			line := p["node"]
			if d := k.card(c.Alert.NodeID); d != nil {
				line = strings.Join(nonEmpty(d.Name, strings.TrimSpace(d.Vendor+" "+d.Model), d.IP, d.ConnectedTo), " · ")
			} else if detail != "" {
				line += " · " + detail
			}
			newDevs.Lines = append(newDevs.Lines, line)
			newDevs.Tip = advice["new_device"]
			tally["new"]++
			continue
		}
		tally[c.Alert.Severity]++
		// One group per device; alerts without one stand alone.
		key, label := c.Alert.NodeID, fmt.Sprint(c.Alert.Params["node"])
		if key == "" || c.Alert.Params["node"] == nil || names[c.Alert.Rule] == "" {
			key, label = c.Alert.Key, title
		} else {
			key += "|" + label
		}
		g := byKey[key]
		if g == nil {
			g = &Group{Severity: c.Alert.Severity, Title: label, Tip: advice[c.Alert.Rule]}
			if label != title {
				g.Device = k.card(c.Alert.NodeID)
			}
			if g.Device != nil {
				g.URL = g.Device.URL
			} else {
				g.URL = nodeURL(ctx.BaseURL, c.Alert.NodeID)
			}
			byKey[key] = g
			groups = append(groups, g)
		}
		line := title
		if label != title {
			line = names[c.Alert.Rule]
		}
		g.Items = append(g.Items, Item{Severity: c.Alert.Severity, Label: line, Detail: detail})
		if detail != "" {
			line += ": " + strings.TrimSuffix(detail, ".")
		}
		g.Lines = append(g.Lines, line)
	}
	if n := len(newDevs.Lines); n > 0 {
		newDevs.Title = count(locale, "new", n)
		groups = append(groups, newDevs)
	}
	if len(resolved.Lines) > 0 {
		groups = append(groups, resolved)
	}
	for _, g := range groups {
		m.Groups = append(m.Groups, *g)
	}
	m.Text = textOf(m)

	if len(m.Events) == 1 {
		title := m.Events[0].Title
		if !m.Events[0].Opened {
			title = fill(t["_resolved"][0], map[string]string{"title": title})
		}
		m.Subject = fill(t["_subject"][0], map[string]string{"title": title})
		return m
	}
	var parts []string
	for _, k := range []string{"critical", "warning", "info", "new", "resolved"} {
		if tally[k] > 0 {
			parts = append(parts, count(locale, k, tally[k]))
		}
	}
	m.Subject = "Omini: " + strings.Join(parts, ", ")
	return m
}

// TestMessage is sent by the "Send a test" button: an example of what the
// alerts look like on the channel (a device with two problems, a new device),
// marked as a test; it carries no events, so nothing acts on it.
func TestMessage(locale string, now time.Time, base string) Message {
	l := localeOf(labels, locale)
	seen := now.Add(-3 * time.Minute)
	sample := topology.Topology{
		Nodes: []topology.Node{
			{ID: "example:switch", Kind: topology.KindDevice, Label: "core-switch", Online: true, Type: "switch"},
			{ID: "example:ap", Kind: topology.KindDevice, Label: "ap-living-room", Online: true, Type: "ap"},
			{
				ID: "example:nas", Kind: topology.KindClient, Label: "nas-01", Online: false, LastSeen: &seen,
				Type: "nas", Brand: "Synology", Model: "DS920+", OS: "DSM 7.2",
				IP: "192.168.1.20", MAC: "00:11:32:a4:5b:c6", ParentID: "example:switch", Port: "Port 4",
			},
			{
				ID: "example:phone", Kind: topology.KindClient, Label: "iPhone", Online: true,
				Type: "phone", Brand: "Apple", Model: "iPhone 15", IP: "192.168.1.57", MAC: "6a:3e:91:0c:22:7d",
				ParentID: "example:ap", SSID: "Home 5 GHz",
			},
		},
	}
	changes := []store.AlertChange{
		{Opened: true, Alert: store.Alert{
			Key: "example:offline", Rule: "device_offline", Severity: "critical", NodeID: "example:nas",
			Params: map[string]any{"node": "nas-01"},
		}},
		{Opened: true, Alert: store.Alert{
			Key: "example:disk", Rule: "disk_full", Severity: "warning", NodeID: "example:nas",
			Params: map[string]any{"node": "nas-01", "mount": "/volume1", "pct": 91.0},
		}},
		{Opened: true, Alert: store.Alert{
			Key: "example:new", Rule: "new_device", Severity: "info", NodeID: "example:phone",
			Params: map[string]any{"node": "iPhone"},
		}},
	}
	m := ComposeWith(changes, locale, Context{Topology: &sample, Now: now})
	// The example's links would lead nowhere: only the map's.
	for i := range m.Groups {
		m.Groups[i].URL = ""
		if d := m.Groups[i].Device; d != nil {
			d.URL = ""
		}
	}
	m.URL = strings.TrimRight(base, "/")
	m.Subject = catalog(locale)["_test"][0] + " · " + l["example"]
	m.Events, m.Test = nil, true
	m.Text = textOf(m)
	return m
}

// number reads a numeric parameter: float64 once stored (JSON), the rule's own
// type when just found.
func number(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint64:
		return float64(x), true
	case int32:
		return float64(x), true
	}
	return 0, false
}
