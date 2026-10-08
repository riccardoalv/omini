package notify

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

// Texts of the alerts sent by the server, in the languages of the UI (which
// has its own copy, web/src/i18n: keep both in step). {name} is replaced by
// the alert's parameter of that name.
var texts = map[string]map[string][2]string{
	"en": {
		"device_offline":     {"{node} is offline", "Its integration could not reach it. {error}"},
		"integration_failed": {"The {integration} integration is failing", "{error}"},
		"wan_down":           {"Internet down on {node}", "Gateway {gateway} does not answer."},
		"wan_degraded":       {"Unstable internet on {node}", "Gateway {gateway}: {loss_pct}% loss, {rtt_ms} ms."},
		"duplicate_ip":       {"Duplicate IP {ip}", "Used by several devices at once: {devices}"},
		"update_pending":     {"Update available for {node}", "{latest} is available ({updates} updates)."},
		"disk_full":          {"Disk almost full on {node}", "{mount} is {pct}% full."},
		"hot_cpu":            {"{node} is running hot", "{sensor} at {celsius} °C."},
		"high_cpu":           {"High CPU on {node}", "CPU at {pct}%."},
		"high_memory":        {"Memory almost full on {node}", "Memory at {pct}%."},
		"slow_uplink":        {"Slow uplink to {node}", "The link from {from} ({port}) runs at {speed}: below 1 Gbps."},
		"interface_errors":   {"Errors on {iface} of {node}", "{rx_errors} receive and {tx_errors} transmit errors in the last poll."},
		"weak_wifi":          {"Weak Wi-Fi signal on {node}", "{signal_dbm} dBm on {ap}."},
		"saturated_link":     {"{iface} of {node} is saturated", "{pct}% of {speed} in use."},
		"unmanaged_switch":   {"Likely unmanaged switch on {parent}", "{macs} devices behind {port}."},
		"unknown_neighbor":   {"Unknown neighbor {node}", "Announced by LLDP on {parent} {port}, without an integration."},
		"new_device":         {"New device: {node}", "{vendor} {mac} {ip}"},
		"_resolved":          {"Resolved: {title}", ""},
		"_subject":           {"Omini: {title}", "Omini: {n} alerts"},
		"_test":              {"Omini test message", "Notifications from Omini reach this channel."},
	},
	"pt-BR": {
		"device_offline":     {"{node} está offline", "A integração não conseguiu alcançá-lo. {error}"},
		"integration_failed": {"A integração {integration} está falhando", "{error}"},
		"wan_down":           {"Internet fora em {node}", "O gateway {gateway} não responde."},
		"wan_degraded":       {"Internet instável em {node}", "Gateway {gateway}: {loss_pct}% de perda, {rtt_ms} ms."},
		"duplicate_ip":       {"IP duplicado {ip}", "Usado por vários dispositivos ao mesmo tempo: {devices}"},
		"update_pending":     {"Atualização disponível para {node}", "{latest} disponível ({updates} atualizações)."},
		"disk_full":          {"Disco quase cheio em {node}", "{mount} está {pct}% cheio."},
		"hot_cpu":            {"{node} está esquentando", "{sensor} a {celsius} °C."},
		"high_cpu":           {"CPU alta em {node}", "CPU em {pct}%."},
		"high_memory":        {"Memória quase cheia em {node}", "Memória em {pct}%."},
		"slow_uplink":        {"Uplink lento para {node}", "O link a partir de {from} ({port}) roda a {speed}: abaixo de 1 Gbps."},
		"interface_errors":   {"Erros em {iface} de {node}", "{rx_errors} erros de recepção e {tx_errors} de transmissão na última coleta."},
		"weak_wifi":          {"Sinal Wi-Fi fraco em {node}", "{signal_dbm} dBm em {ap}."},
		"saturated_link":     {"{iface} de {node} está saturada", "{pct}% de {speed} em uso."},
		"unmanaged_switch":   {"Provável switch não gerenciável em {parent}", "{macs} dispositivos atrás de {port}."},
		"unknown_neighbor":   {"Vizinho desconhecido {node}", "Anunciado por LLDP em {parent} {port}, sem integração."},
		"new_device":         {"Dispositivo novo: {node}", "{vendor} {mac} {ip}"},
		"_resolved":          {"Resolvido: {title}", ""},
		"_subject":           {"Omini: {title}", "Omini: {n} alertas"},
		"_test":              {"Mensagem de teste do Omini", "As notificações do Omini chegam a este canal."},
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
	pair, ok := t[a.Rule]
	if !ok {
		return a.Rule, ""
	}
	p := params(a)
	return fill(pair[0], p), fill(pair[1], p)
}

var severityIcon = map[string]string{"critical": "🔴", "warning": "🟠", "info": "🔵"}

// Compose builds one message for several alert changes (a collection round),
// most severe first, opened before resolved.
func Compose(changes []store.AlertChange, locale string) Message {
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
	m := Message{}
	var lines []string
	for _, c := range sorted {
		title, detail := Describe(c.Alert, locale)
		ev := Event{Opened: c.Opened, Alert: c.Alert, Title: title, Detail: detail}
		m.Events = append(m.Events, ev)
		if c.Opened {
			line := severityIcon[c.Alert.Severity] + " " + title
			if detail != "" {
				line += "\n   " + detail
			}
			lines = append(lines, line)
		} else {
			lines = append(lines, "✅ "+fill(t["_resolved"][0], map[string]string{"title": title}))
		}
	}
	m.Text = strings.Join(lines, "\n")
	if len(m.Events) == 1 {
		title := m.Events[0].Title
		if !m.Events[0].Opened {
			title = fill(t["_resolved"][0], map[string]string{"title": title})
		}
		m.Subject = fill(t["_subject"][0], map[string]string{"title": title})
	} else {
		m.Subject = fill(t["_subject"][1], map[string]string{"n": fmt.Sprint(len(m.Events))})
	}
	return m
}

// TestMessage is sent by the "Send a test" button.
func TestMessage(locale string, now time.Time) Message {
	t := catalog(locale)["_test"]
	return Message{Subject: t[0], Text: t[1] + " (" + now.Format("2006-01-02 15:04") + ")"}
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
