package notify

import (
	"bufio"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
)

func alert(rule, sev string, params map[string]any) store.Alert {
	return store.Alert{Key: rule + "|x", Rule: rule, Severity: sev, NodeID: "dev:x", Params: params}
}

func TestCompose(t *testing.T) {
	m := Compose([]store.AlertChange{
		{Alert: alert("high_cpu", "warning", map[string]any{"node": "fw", "pct": 91.0})},
		{Alert: alert("slow_uplink", "warning", map[string]any{"node": "AP", "from": "sw", "port": "Port 3", "speed_mbps": uint64(100)}), Opened: true},
		{Alert: alert("device_offline", "critical", map[string]any{"node": "Switch", "error": "timeout"}), Opened: true},
	}, "en")
	if m.Subject != "Omini: 1 critical, 1 warning, 1 resolved" {
		t.Fatalf("subject: %q", m.Subject)
	}
	want := "CRITICAL · Switch\n  - Offline: Its integration could not reach it. timeout\n" +
		"  What to do: Check that it is powered and connected; if it is, check the integration's address and credentials.\n\n" +
		"WARNING · AP\n  - Slow uplink: The link from sw (Port 3) runs at 100M: below 1 Gbps\n" +
		"  What to do: Check the cable (all 8 wires) and both ports: they should reach 1 Gbps.\n\n" +
		"Resolved\n  - High CPU on fw"
	if m.Text != want {
		t.Fatalf("text:\n%s\nwant:\n%s", m.Text, want)
	}
	one := Compose([]store.AlertChange{{Alert: alert("disk_full", "critical", map[string]any{"node": "NAS", "mount": "/", "pct": 93.25}), Opened: true}}, "pt-BR")
	if one.Subject != "Omini: Disco quase cheio em NAS" || !strings.Contains(one.Text, "/ está 93.2% cheio") {
		t.Fatalf("pt-BR: %+v", one)
	}
}

func TestEveryRuleHasTexts(t *testing.T) {
	for _, locale := range []string{"en", "pt-BR"} {
		for _, rule := range []string{
			"device_offline", "integration_failed", "wan_down", "wan_degraded", "duplicate_ip", "update_pending",
			"disk_full", "hot_cpu", "high_cpu", "high_memory", "slow_uplink", "interface_errors", "weak_wifi",
			"saturated_link", "unmanaged_switch", "unknown_neighbor", "new_device",
			"fast_ethernet", "integration_available", "link_flapping", "half_duplex", "device_rebooted", "sfp_low_rx",
			"dhcp_pool_full", "firewall_states_full", "insecure_service", "new_devices_burst", "discovery_limited",
		} {
			if _, ok := texts[locale][rule]; !ok {
				t.Errorf("%s: no text for %s", locale, rule)
			}
		}
	}
}

func TestWebhookFormats(t *testing.T) {
	var (
		mu   sync.Mutex
		got  []*http.Request
		body []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		got, body = append(got, r), append(body, string(b))
		mu.Unlock()
	}))
	defer srv.Close()
	m := Message{Subject: "Omini: test", Text: "hello"}
	for _, format := range []string{"json", "slack", "discord", "ntfy"} {
		cfg := integration.Config{"url": srv.URL, "format": format, "secret": "s3"}
		if err := SendWith(context.Background(), "webhook", cfg, nil, m); err != nil {
			t.Fatalf("%s: %v", format, err)
		}
	}
	var j Message
	if err := json.Unmarshal([]byte(body[0]), &j); err != nil || j.Text != "hello" {
		t.Fatalf("json: %s", body[0])
	}
	mac := hmac.New(sha256.New, []byte("s3"))
	mac.Write([]byte(body[0]))
	if got[0].Header.Get("X-Omini-Signature") != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatal("signature")
	}
	if body[1] != `{"text":"*Omini: test*\nhello"}` || body[2] != `{"content":"**Omini: test**\nhello","username":"Omini"}` {
		t.Fatalf("slack/discord: %s %s", body[1], body[2])
	}
	if body[3] != "hello" || got[3].Header.Get("Title") != "Omini: test" {
		t.Fatalf("ntfy: %s %v", body[3], got[3].Header)
	}
	if err := SendWith(context.Background(), "webhook", integration.Config{"url": "ftp://x"}, nil, m); err == nil {
		t.Fatal("ftp accepted")
	}
}

func TestWebhookErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "invalid token", http.StatusForbidden)
	}))
	defer srv.Close()
	err := SendWith(context.Background(), "webhook", integration.Config{"url": srv.URL}, nil, Message{})
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "invalid token") {
		t.Fatalf("err = %v", err)
	}
}

func TestTelegramHidesTheToken(t *testing.T) {
	var form string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.URL.Path + "?" + r.PostForm.Encode()
		if r.PostForm.Get("chat_id") == "bad" {
			http.Error(w, `{"ok":false,"description":"chat not found"}`, http.StatusBadRequest)
		}
	}))
	defer srv.Close()
	old := TelegramAPI
	TelegramAPI = srv.URL
	defer func() { TelegramAPI = old }()
	cfg := integration.Config{"token": "123:SECRET", "chat_id": "42"}
	if err := SendWith(context.Background(), "telegram", cfg, nil, Message{Subject: "S", Text: "T"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(form, "/bot123:SECRET/sendMessage?") || !strings.Contains(form, "chat_id=42") || !strings.Contains(form, "text=%3Cb%3ES%3C%2Fb%3E%0A%0AT") || !strings.Contains(form, "parse_mode=HTML") {
		t.Fatalf("request: %s", form)
	}
	cfg["chat_id"] = "bad"
	err := SendWith(context.Background(), "telegram", cfg, nil, Message{})
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "chat not found") {
		t.Fatalf("err = %v", err)
	}
}

// fakeSMTP answers like a mail server without TLS and records the message.
func fakeSMTP(t *testing.T) (addr string, received chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	received = make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		w := func(s string) { _, _ = c.Write([]byte(s + "\r\n")) }
		w("220 test ESMTP")
		var data strings.Builder
		inData := false
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			if inData {
				if line == ".\r\n" {
					inData = false
					w("250 queued")
					received <- data.String()
					continue
				}
				data.WriteString(line)
				continue
			}
			switch cmd := strings.ToUpper(strings.TrimSpace(line)); {
			case strings.HasPrefix(cmd, "EHLO"):
				w("250 test")
			case strings.HasPrefix(cmd, "DATA"):
				inData = true
				w("354 go")
			case strings.HasPrefix(cmd, "QUIT"):
				w("221 bye")
				return
			default:
				w("250 ok")
			}
		}
	}()
	return ln.Addr().String(), received
}

func TestEmail(t *testing.T) {
	addr, received := fakeSMTP(t)
	host, port, _ := net.SplitHostPort(addr)
	cfg := integration.Config{
		"host": host, "port": mustFloat(port), "security": "none",
		"from": "Omini <omini@example.org>", "to": "admin@example.org, ops@example.org",
	}
	err := SendWith(context.Background(), "email", cfg, nil, Message{Subject: "Omini: Memória quase cheia", Text: "line 1\nline 2"})
	if err != nil {
		t.Fatal(err)
	}
	msg := <-received
	for _, want := range []string{"To: <admin@example.org>, <ops@example.org>", "Subject: =?utf-8?q?", "line 1\r\nline 2"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("message lacks %q:\n%s", want, msg)
		}
	}
	if _, err := New("email", integration.Config{"host": "x", "from": "nope", "to": "a@b.c"}, nil); err == nil {
		t.Fatal("bad sender accepted")
	}
}

func TestDispatcher(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	box, _ := secret.New(make([]byte, 32))
	var (
		mu       sync.Mutex
		messages []Message
	)
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		var m Message
		_ = json.NewDecoder(r.Body).Decode(&m)
		mu.Lock()
		messages = append(messages, m)
		mu.Unlock()
	}))
	defer srv.Close()
	kind, _ := KindOf("webhook")
	sealed, _ := integration.SealSecrets(box, kind.Fields, integration.Config{"url": srv.URL, "secret": "x"})
	n, err := st.CreateNotifier(ctx, store.Notifier{Type: "webhook", Config: sealed, MinSeverity: "warning", NotifyResolved: true, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = st.CreateNotifier(ctx, store.Notifier{Type: "webhook", Config: sealed, MinSeverity: "critical", Enabled: false})

	now := time.Unix(1_790_000_000, 0)
	d := &Dispatcher{Store: st, Box: box, Now: func() time.Time { return now }}
	cpu := alert("high_cpu", "warning", map[string]any{"node": "fw", "pct": 91.0})
	d.Handle([]store.AlertChange{
		{Alert: cpu, Opened: true},
		{Alert: alert("unmanaged_switch", "info", map[string]any{"parent": "sw"}), Opened: true}, // below warning
	})
	d.Wait()
	// It flaps: resolved and opened again within the cooldown — nothing sent.
	now = now.Add(5 * time.Minute)
	d.Handle([]store.AlertChange{{Alert: cpu}})
	d.Handle([]store.AlertChange{{Alert: cpu, Opened: true}})
	d.Wait()
	// Resolved for good, later.
	now = now.Add(time.Hour)
	d.Handle([]store.AlertChange{{Alert: cpu}})
	d.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(messages) != 2 || len(messages[0].Events) != 1 || !messages[0].Events[0].Opened || messages[1].Events[0].Opened {
		t.Fatalf("messages: %+v", messages)
	}
	got, _ := st.GetNotifier(ctx, n.ID)
	if got.LastSentAt == nil || got.LastError != "" {
		t.Fatalf("delivery not recorded: %+v", got)
	}
}

func mustFloat(s string) float64 {
	var f float64
	_ = json.Unmarshal([]byte(s), &f)
	return f
}

// A Discord, Slack or ntfy.sh address saved with the default "json" format
// still gets the format its service needs (Discord refused Omini's events
// with "Cannot send an empty message").
func TestWebhookFormatFromTheAddress(t *testing.T) {
	for address, want := range map[string]string{
		"https://discord.com/api/webhooks/1/abc":        "discord",
		"https://discordapp.com/api/webhooks/1/abc":     "discord",
		"https://hooks.slack.com/services/T/B/x":        "slack",
		"https://ntfy.sh/omini-home":                    "ntfy",
		"https://homeassistant.local/api/webhook/omini": "json",
	} {
		s, err := newWebhook(integration.Config{"url": address, "format": "json"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := s.(*webhook).format; got != want {
			t.Errorf("%s: format %q, want %q", address, got, want)
		}
	}
	// A format picked on purpose is kept.
	s, _ := newWebhook(integration.Config{"url": "https://ntfy.sh/x", "format": "slack"}, nil)
	if s.(*webhook).format != "slack" {
		t.Error("a chosen format was replaced")
	}
}

// A round with several alerts on one device and new devices: a card per
// device, the new devices together, a summary for a subject.
func TestComposeGroupsByDevice(t *testing.T) {
	on := func(id, rule, sev string, p map[string]any) store.AlertChange {
		return store.AlertChange{Alert: store.Alert{Key: rule + "|" + id, Rule: rule, Severity: sev, NodeID: id, Params: p}, Opened: true}
	}
	m := Compose([]store.AlertChange{
		on("pve", "high_memory", "warning", map[string]any{"node": "proxmox", "pct": 98.3}),
		on("pve", "update_pending", "warning", map[string]any{"node": "proxmox", "latest": "9.2.20", "updates": 258.0}),
		on("vm1", "new_device", "info", map[string]any{"node": "haos", "vendor": "Proxmox Server Solutions", "mac": "bc:24:11:5a:a5:b2"}),
		on("vm2", "new_device", "info", map[string]any{"node": "master", "vendor": "Proxmox Server Solutions", "mac": "bc:24:11:8d:d7:e5"}),
	}, "en")
	if m.Subject != "Omini: 2 warnings, 2 new devices" {
		t.Fatalf("subject: %q", m.Subject)
	}
	if len(m.Groups) != 2 || m.Groups[0].Title != "proxmox" || len(m.Groups[0].Lines) != 2 ||
		m.Groups[0].Lines[0] != "Memory almost full: Memory at 98.3%" ||
		m.Groups[1].Title != "2 new devices" || m.Groups[1].Lines[0] != "haos · Proxmox Server Solutions bc:24:11:5a:a5:b2" {
		t.Fatalf("groups: %+v", m.Groups)
	}

	d := discordPayload(m)
	embeds := d["embeds"]
	b, _ := json.Marshal(embeds)
	if !strings.Contains(string(b), `"title":"proxmox"`) || !strings.Contains(string(b), `"color":13801762`) ||
		d["content"] != "**Omini: 2 warnings, 2 new devices**" {
		t.Fatalf("discord: %v %s", d["content"], b)
	}
	s := slackPayload(m)
	if atts := s["attachments"].([]map[string]any); len(atts) != 2 || atts[0]["color"] != "#d29922" {
		t.Fatalf("slack: %+v", s)
	}

	pt := Compose([]store.AlertChange{
		on("pve", "high_memory", "warning", map[string]any{"node": "proxmox", "pct": 98.3}),
		on("vm1", "new_device", "info", map[string]any{"node": "haos"}),
	}, "pt-BR")
	if pt.Subject != "Omini: 1 aviso, 1 dispositivo novo" || pt.Groups[0].Lines[0] != "Memória quase cheia: Memória em 98.3%" {
		t.Fatalf("pt-BR: %q %+v", pt.Subject, pt.Groups)
	}
}

func TestEveryRuleHasAShortName(t *testing.T) {
	for _, locale := range []string{"en", "pt-BR"} {
		for rule := range texts["en"] {
			if strings.HasPrefix(rule, "_") || strings.HasSuffix(rule, "_count") || rule == "new_device" || rule == "new_devices_burst" {
				continue
			}
			if short[locale][rule] == "" {
				t.Errorf("%s: no short name for %s", locale, rule)
			}
		}
	}
}

func TestUpdateWithoutVersion(t *testing.T) {
	_, detail := Describe(alert("update_pending", "warning", map[string]any{"node": "bkp-01", "updates": 7.0}), "en")
	if detail != "7 updates available." {
		t.Fatalf("detail: %q", detail)
	}
}
