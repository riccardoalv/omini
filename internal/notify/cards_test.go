package notify

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// A NAS on a switch port with two alerts, a new phone on Wi-Fi.
func sampleRound(t *testing.T) Message {
	t.Helper()
	seen := time.Date(2026, 10, 8, 9, 30, 0, 0, time.UTC)
	topo := topology.Topology{
		Nodes: []topology.Node{
			{
				ID: "sw", Kind: topology.KindDevice, Label: "core-sw1", Online: true, Type: "switch",
				PortLabels: map[string]string{"Te2/0/4": "Rack NAS"},
			},
			{ID: "ap", Kind: topology.KindDevice, Label: "ap-lobby", Online: true, Type: "ap"},
			{
				ID: "nas", Kind: topology.KindDevice, Label: "nas-01", Online: false, LastSeen: &seen,
				Type: "nas", Brand: "Synology", OS: "DSM 7.2",
				Device: &model.Device{Model: model.Ptr("DS920+"), MACs: []model.MACAddress{"00:11:32:aa:bb:cc"}, IPs: []string{"10.0.0.20"}},
			},
			{ID: "cam", Kind: topology.KindClient, Label: "cam-<door>", Online: true, Type: "camera", Brand: "Reolink", IP: "10.0.40.9", ParentID: "ap", SSID: "IOT"},
		},
		Edges: []topology.Edge{
			{ID: "1", Source: "sw", Target: "nas", SourcePort: "Te2/0/4", Kind: topology.EdgeFDB},
			{ID: "2", Source: "ap", Target: "cam", SourcePort: "IOT", Kind: topology.EdgeWifi},
		},
	}
	at := time.Date(2026, 10, 8, 9, 35, 0, 0, time.UTC)
	return ComposeWith([]store.AlertChange{
		{Opened: true, Alert: store.Alert{
			Key: "a", Rule: "device_offline", Severity: "critical", NodeID: "nas",
			Params: map[string]any{"node": "nas-01", "error": "10.0.0.20 did not answer in time"},
		}},
		{Opened: true, Alert: store.Alert{
			Key: "b", Rule: "disk_full", Severity: "warning", NodeID: "nas",
			Params: map[string]any{"node": "nas-01", "mount": "/volume1", "pct": 91.0},
		}},
		{Opened: true, Alert: store.Alert{
			Key: "c", Rule: "weak_wifi", Severity: "warning", NodeID: "cam",
			Params: map[string]any{"node": "cam-<door>", "signal_dbm": -81.0, "ap": "ap-lobby"},
		}},
	}, "en", Context{Topology: &topo, BaseURL: "http://omini.lan:8080/", Now: at})
}

func TestCardsDescribeTheDevice(t *testing.T) {
	m := sampleRound(t)
	if m.URL != "http://omini.lan:8080" || len(m.Groups) != 2 {
		t.Fatalf("message: %+v", m)
	}
	nas := m.Groups[0]
	d := nas.Device
	if d == nil || d.Name != "nas-01" || d.Type != "NAS / storage" || d.Vendor != "Synology" || d.Model != "DS920+" ||
		d.IP != "10.0.0.20" || d.MAC != "00:11:32:aa:bb:cc" || d.ConnectedTo != "core-sw1 · Rack NAS" || d.LastSeen == nil {
		t.Fatalf("NAS card: %+v", d)
	}
	if nas.URL != "http://omini.lan:8080/?node=nas" || len(nas.Items) != 2 || nas.Items[0].Label != "Offline" ||
		!strings.HasPrefix(nas.Tip, "Check that it is powered") {
		t.Fatalf("NAS group: %+v", nas)
	}
	// A Wi-Fi client: connected to the access point and its network.
	if c := m.Groups[1].Device; c == nil || c.ConnectedTo != "ap-lobby · IOT" || c.LastSeen != nil {
		t.Fatalf("camera card: %+v", c)
	}
	if !strings.Contains(m.Text, "Make and model: Synology DS920+") || !strings.Contains(m.Text, "Open in Omini: http://omini.lan:8080/?node=nas") {
		t.Fatalf("text:\n%s", m.Text)
	}
}

func TestDiscordCards(t *testing.T) {
	raw, _ := json.Marshal(discordPayload(sampleRound(t)))
	var p struct {
		Content string `json:"content"`
		Embeds  []struct {
			Author    map[string]string `json:"author"`
			Title     string            `json:"title"`
			URL       string            `json:"url"`
			Color     int               `json:"color"`
			Timestamp string            `json:"timestamp"`
			Fields    []struct {
				Name, Value string
				Inline      bool
			} `json:"fields"`
		} `json:"embeds"`
	}
	_ = json.Unmarshal(raw, &p)
	if !strings.Contains(p.Content, "[Open the map](http://omini.lan:8080)") || len(p.Embeds) != 2 {
		t.Fatalf("payload: %s", raw)
	}
	e := p.Embeds[0]
	if e.Author["name"] != "Critical" || e.URL != "http://omini.lan:8080/?node=nas" || e.Color != 0xf85149 || e.Timestamp != "2026-10-08T09:35:00Z" {
		t.Fatalf("embed: %+v", e)
	}
	got := map[string]string{}
	for _, f := range e.Fields {
		got[f.Name] = f.Value
	}
	if got["Offline since"] != "<t:1791451800:f> (<t:1791451800:R>)" {
		t.Fatalf("offline since: %q", got["Offline since"])
	}
	if got["IP address"] != "`10.0.0.20`" || got["Connected to"] != "core-sw1 · Rack NAS" || !strings.HasPrefix(got["What to do"], "Check") {
		t.Fatalf("fields: %v", got)
	}
	if strings.ContainsAny(string(raw), "🔴🟠🟡🟢") {
		t.Fatal("no emoji in the cards")
	}
}

func TestSlackCards(t *testing.T) {
	p := slackPayload(sampleRound(t))
	atts := p["attachments"].([]map[string]any)
	if len(atts) != 2 || atts[0]["title_link"] != "http://omini.lan:8080/?node=nas" || atts[0]["author_name"] != "Critical" {
		t.Fatalf("attachments: %v", atts)
	}
	if fields := atts[0]["fields"].([]map[string]any); len(fields) < 5 {
		t.Fatalf("fields: %v", fields)
	}
}

func TestTelegramCards(t *testing.T) {
	text := telegramText(sampleRound(t))
	for _, want := range []string{
		"<b>nas-01</b>  ·  <i>Critical</i>", "• <b>Offline</b>: Its integration could not reach it.",
		"IP address: <code>10.0.0.20</code>", `<a href="http://omini.lan:8080/?node=nas">Open in Omini</a>`,
		"<b>cam-&lt;door&gt;</b>", // names are escaped
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in:\n%s", want, text)
		}
	}
}

func TestNtfyCards(t *testing.T) {
	m := sampleRound(t)
	if body := ntfyText(m); !strings.Contains(body, "**nas-01** · Critical") || !strings.Contains(body, "> What to do:") {
		t.Fatalf("body:\n%s", body)
	}
	if ntfyClick(m) != "http://omini.lan:8080" {
		t.Fatalf("two devices: the map, got %q", ntfyClick(m))
	}
	m.Groups = m.Groups[:1]
	if ntfyClick(m) != "http://omini.lan:8080/?node=nas" {
		t.Fatalf("one device: its page, got %q", ntfyClick(m))
	}
}

func TestEmailCards(t *testing.T) {
	page := emailHTML(sampleRound(t))
	for _, want := range []string{"border-left:5px solid #f85149", "Synology DS920+", "cam-&lt;door&gt;", `href="http://omini.lan:8080/?node=nas"`, "What to do"} {
		if !strings.Contains(page, want) {
			t.Fatalf("missing %q", want)
		}
	}
}

func TestTestMessageIsAnExample(t *testing.T) {
	m := TestMessage("pt-BR", time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC), "http://omini.lan:8080")
	if !m.Test || len(m.Events) != 0 {
		t.Fatalf("a test carries no events: %+v", m.Events)
	}
	if len(m.Groups) != 2 || m.Groups[0].Device == nil || m.Groups[0].Device.ConnectedTo != "core-switch · Port 4" {
		t.Fatalf("groups: %+v", m.Groups)
	}
	if m.Groups[0].URL != "" || m.URL != "http://omini.lan:8080" || !strings.Contains(m.Subject, "exemplo") {
		t.Fatalf("links and subject: %q %q %q", m.Groups[0].URL, m.URL, m.Subject)
	}
	if !strings.Contains(m.Text, "Marca e modelo: Synology DS920+") {
		t.Fatalf("text:\n%s", m.Text)
	}
}
