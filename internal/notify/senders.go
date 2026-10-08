package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
)

// --- webhook ---

type webhook struct {
	url, format, secret string
	client              *http.Client
}

func newWebhook(cfg integration.Config, client *http.Client) (Sender, error) {
	w := &webhook{url: cfg.String("url"), format: cfg.String("format"), secret: cfg.String("secret"), client: client}
	if w.format == "" {
		w.format = "json"
	}
	u, err := url.Parse(w.url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, fmt.Errorf("%w: the webhook needs an http(s) URL", ErrConfig)
	}
	// A Slack, Discord or ntfy.sh address left as "json" (the default) gets its
	// own format: they refuse Omini's events ("Cannot send an empty message").
	if w.format == "json" {
		w.format = formatOf(u)
	}
	switch w.format {
	case "json", "slack", "discord", "ntfy":
	default:
		return nil, fmt.Errorf("%w: unknown format %q", ErrConfig, w.format)
	}
	return w, nil
}

// formatOf recognizes the services whose webhooks need their own format.
func formatOf(u *url.URL) string {
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "hooks.slack.com":
		return "slack"
	case (host == "discord.com" || host == "discordapp.com" || strings.HasSuffix(host, ".discord.com")) && strings.HasPrefix(u.Path, "/api/webhooks/"):
		return "discord"
	case host == "ntfy.sh":
		return "ntfy"
	}
	return "json"
}

func (w *webhook) Send(ctx context.Context, m Message) error {
	var (
		body        []byte
		contentType = "application/json"
		err         error
	)
	switch w.format {
	case "slack":
		body, err = json.Marshal(slackPayload(m))
	case "discord":
		body, err = json.Marshal(discordPayload(m))
	case "ntfy":
		body, contentType = []byte(m.Text), "text/plain; charset=utf-8"
	default:
		body, err = json.Marshal(m)
	}
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("User-Agent", "Omini")
	if w.format == "ntfy" {
		req.Header.Set("Title", m.Subject)
		sev := worst(m)
		req.Header.Set("Tags", ntfyTag[sev]+",omini")
		req.Header.Set("Priority", ntfyPriority[sev])
	}
	if w.secret != "" {
		mac := hmac.New(sha256.New, []byte(w.secret))
		mac.Write(body)
		req.Header.Set("X-Omini-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
	}
	return do(w.client, req)
}

// do sends a request and turns an error status into an error (with the start
// of the answer, which usually says why).
func do(client *http.Client, req *http.Request) error {
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", req.URL.Host, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("%s answered %d: %s", req.URL.Host, resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
	return nil
}

// --- Telegram ---

// TelegramAPI is Telegram's Bot API base URL (replaced in tests).
var TelegramAPI = "https://api.telegram.org"

type telegram struct {
	token, chat string
	client      *http.Client
}

func newTelegram(cfg integration.Config, client *http.Client) (Sender, error) {
	t := &telegram{token: strings.TrimSpace(cfg.String("token")), chat: strings.TrimSpace(cfg.String("chat_id")), client: client}
	if t.token == "" || t.chat == "" {
		return nil, fmt.Errorf("%w: the bot token and the chat id are required", ErrConfig)
	}
	return t, nil
}

func (t *telegram) Send(ctx context.Context, m Message) error {
	text := m.Subject + "\n\n" + m.Text
	if len(text) > 4000 { // Telegram's limit is 4096
		text = text[:3997] + "..."
	}
	form := url.Values{"chat_id": {t.chat}, "text": {text}, "disable_web_page_preview": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, TelegramAPI+"/bot"+t.token+"/sendMessage",
		strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if err := do(t.client, req); err != nil {
		// Never show the token (it is in the URL).
		return fmt.Errorf("telegram: %s", strings.ReplaceAll(err.Error(), t.token, "***"))
	}
	return nil
}

// --- e-mail ---

type email struct {
	host, security, username, password string
	port                               int
	from                               *mail.Address
	to                                 []*mail.Address
}

func newEmail(cfg integration.Config, _ *http.Client) (Sender, error) {
	e := &email{
		host: strings.TrimSpace(cfg.String("host")), port: cfg.Int("port", 587), security: cfg.String("security"),
		username: cfg.String("username"), password: cfg.String("password"),
	}
	if e.security == "" {
		e.security = "starttls"
	}
	from, err := mail.ParseAddress(cfg.String("from"))
	if err != nil {
		return nil, fmt.Errorf("%w: invalid sender address", ErrConfig)
	}
	to, err := mail.ParseAddressList(cfg.String("to"))
	if err != nil || len(to) == 0 {
		return nil, fmt.Errorf("%w: invalid recipient address", ErrConfig)
	}
	if e.host == "" || e.port <= 0 || e.port > 65535 {
		return nil, fmt.Errorf("%w: the SMTP server and port are required", ErrConfig)
	}
	e.from, e.to = from, to
	return e, nil
}

// Dial is how the e-mail sender connects (replaced in tests).
var Dial = func(ctx context.Context, addr string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", addr)
}

func (e *email) Send(ctx context.Context, m Message) error {
	addr := net.JoinHostPort(e.host, strconv.Itoa(e.port))
	conn, err := Dial(ctx, addr)
	if err != nil {
		return fmt.Errorf("could not reach %s: %w", addr, err)
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	tlsConf := &tls.Config{ServerName: e.host, MinVersion: tls.VersionTLS12}
	if e.security == "tls" {
		conn = tls.Client(conn, tlsConf)
	}
	c, err := smtp.NewClient(conn, e.host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp: %w", err)
	}
	defer c.Close()
	if e.security == "starttls" {
		if err := c.StartTLS(tlsConf); err != nil {
			return fmt.Errorf("smtp: STARTTLS: %w", err)
		}
	}
	if e.username != "" {
		if err := c.Auth(smtp.PlainAuth("", e.username, e.password, e.host)); err != nil {
			return fmt.Errorf("smtp: sign in: %w", err)
		}
	}
	if err := c.Mail(e.from.Address); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	for _, to := range e.to {
		if err := c.Rcpt(to.Address); err != nil {
			return fmt.Errorf("smtp: %s: %w", to.Address, err)
		}
	}
	wc, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if _, err := wc.Write(e.message(m)); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	if err := wc.Close(); err != nil {
		return fmt.Errorf("smtp: %w", err)
	}
	return c.Quit()
}

func (e *email) message(m Message) []byte {
	to := make([]string, len(e.to))
	for i, a := range e.to {
		to[i] = a.String()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "From: %s\r\n", e.from.String())
	fmt.Fprintf(&b, "To: %s\r\n", strings.Join(to, ", "))
	fmt.Fprintf(&b, "Subject: %s\r\n", mimeHeader(m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", time.Now().Format(time.RFC1123Z))
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	b.WriteString(strings.ReplaceAll(m.Text, "\n", "\r\n"))
	b.WriteString("\r\n")
	return []byte(b.String())
}

// mimeHeader encodes a header with non-ASCII characters (RFC 2047).
func mimeHeader(s string) string {
	for _, r := range s {
		if r > 127 {
			return "=?utf-8?q?" + qEncode(s) + "?="
		}
	}
	return s
}

func qEncode(s string) string {
	var b strings.Builder
	for _, c := range []byte(s) {
		switch {
		case c == ' ':
			b.WriteByte('_')
		case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c == '.', c == '-':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "=%02X", c)
		}
	}
	return b.String()
}

// --- message formats of chat services ---

// Card colors by severity (the UI's palette).
var cardColor = map[string]int{"critical": 0xf85149, "warning": 0xd29922, "info": 0x4c8dff, "resolved": 0x3fb950}

var (
	ntfyTag      = map[string]string{"critical": "rotating_light", "warning": "warning", "info": "information_source", "resolved": "white_check_mark", "": "bell"}
	ntfyPriority = map[string]string{"critical": "urgent", "warning": "high", "info": "default", "resolved": "low", "": "default"}
)

// worst is the most severe group of a message ("" for a plain message).
func worst(m Message) string {
	best := ""
	for _, rank := range []string{"critical", "warning", "info", "resolved"} {
		for _, g := range m.Groups {
			if g.Severity == rank && best == "" {
				best = rank
			}
		}
	}
	return best
}

func cut(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// discordPayload: the summary as the message, a colored card per group (at
// most 10, Discord's limit; the rest summed up in the last one).
func discordPayload(m Message) map[string]any {
	type embed struct {
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		Color       int    `json:"color"`
	}
	out := map[string]any{"username": "Omini", "content": cut("**"+m.Subject+"**", 2000)}
	if len(m.Groups) == 0 {
		out["content"] = cut("**"+m.Subject+"**\n"+m.Text, 2000)
		return out
	}
	var embeds []embed
	for i, g := range m.Groups {
		if i == 9 && len(m.Groups) > 10 {
			var rest []string
			for _, h := range m.Groups[9:] {
				rest = append(rest, h.Title)
			}
			embeds = append(embeds, embed{Title: fmt.Sprintf("+%d", len(m.Groups)-9), Description: cut(strings.Join(rest, "\n"), 4000), Color: 0x8b949e})
			break
		}
		embeds = append(embeds, embed{
			Title:       cut(g.Title, 256),
			Description: cut(strings.Join(g.Lines, "\n"), 4000),
			Color:       cardColor[g.Severity],
		})
	}
	out["embeds"] = embeds
	return out
}

// slackPayload: the summary as the message, a colored attachment per group.
func slackPayload(m Message) map[string]any {
	if len(m.Groups) == 0 {
		return map[string]any{"text": "*" + m.Subject + "*\n" + m.Text}
	}
	var atts []map[string]any
	for _, g := range m.Groups {
		atts = append(atts, map[string]any{
			"color": fmt.Sprintf("#%06x", cardColor[g.Severity]),
			"title": g.Title,
			"text":  strings.Join(g.Lines, "\n"),
		})
	}
	return map[string]any{"text": "*" + m.Subject + "*", "attachments": atts}
}
