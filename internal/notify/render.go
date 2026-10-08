package notify

import (
	"fmt"
	"html"
	"strings"
)

// How a message looks on each channel: the same cards (a device, what
// happened to it, its details, what to do and a link to it), drawn with what
// each service offers. No emoji: the color says the severity.

func severityLabel(m Message, sev string) string { return localeOf(labels, m.Locale)[sev] }

// badge is the severity shown on a card: none on the cards whose title says
// it already (resolved, new devices).
func badge(m Message, g Group) string {
	if len(g.Items) == 0 {
		return ""
	}
	return severityLabel(m, g.Severity)
}

// itemLines are a card's alerts as "label: detail" (or its plain lines).
func itemLines(g Group, bold func(string) string) []string {
	if len(g.Items) == 0 {
		return g.Lines
	}
	out := make([]string, len(g.Items))
	for i, it := range g.Items {
		out[i] = bold(it.Label)
		if d := strings.TrimSuffix(it.Detail, "."); d != "" {
			out[i] += ": " + d
		}
	}
	return out
}

// textOf is the message as plain text (e-mail's text part, the JSON
// webhook's "text").
func textOf(m Message) string {
	l := localeOf(labels, m.Locale)
	var b strings.Builder
	for i, g := range m.Groups {
		if i > 0 {
			b.WriteString("\n")
		}
		if sev := badge(m, g); sev != "" {
			fmt.Fprintf(&b, "%s · %s\n", strings.ToUpper(sev), g.Title)
		} else {
			b.WriteString(g.Title + "\n")
		}
		for _, line := range itemLines(g, func(s string) string { return s }) {
			b.WriteString("  - " + line + "\n")
		}
		for _, f := range g.Device.facts(m.Locale) {
			fmt.Fprintf(&b, "  %s: %s\n", f[0], f[1])
		}
		if g.Tip != "" {
			fmt.Fprintf(&b, "  %s: %s\n", l["tip"], g.Tip)
		}
		if g.URL != "" {
			fmt.Fprintf(&b, "  %s: %s\n", l["open"], g.URL)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- Discord: an embed per card ---

func discordPayload(m Message) map[string]any {
	type field struct {
		Name   string `json:"name"`
		Value  string `json:"value"`
		Inline bool   `json:"inline"`
	}
	type embed struct {
		Author      map[string]string `json:"author,omitempty"`
		Title       string            `json:"title"`
		URL         string            `json:"url,omitempty"`
		Description string            `json:"description,omitempty"`
		Color       int               `json:"color"`
		Fields      []field           `json:"fields,omitempty"`
		Footer      map[string]string `json:"footer,omitempty"`
		Timestamp   string            `json:"timestamp,omitempty"`
	}
	l := localeOf(labels, m.Locale)
	content := "**" + m.Subject + "**"
	if m.URL != "" {
		content += "  ·  [" + l["open_map"] + "](" + m.URL + ")"
	}
	out := map[string]any{"username": "Omini", "content": cut(content, 2000)}
	if len(m.Groups) == 0 {
		out["content"] = cut("**"+m.Subject+"**\n"+m.Text, 2000)
		return out
	}
	stamp := m.At.UTC().Format("2006-01-02T15:04:05Z")
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
		lines := itemLines(g, func(s string) string { return "**" + s + "**" })
		if len(g.Items) == 0 {
			for j, s := range lines {
				lines[j] = "• " + s
			}
		}
		e := embed{
			Title:       cut(g.Title, 256),
			URL:         g.URL,
			Description: cut(strings.Join(lines, "\n"), 4000),
			Color:       cardColor[g.Severity],
			Footer:      map[string]string{"text": "Omini"},
			Timestamp:   stamp,
		}
		if sev := badge(m, g); sev != "" {
			e.Author = map[string]string{"name": sev}
		}
		for _, f := range g.Device.facts(m.Locale) {
			v := f[1]
			switch f[0] {
			case l["ip"], l["mac"]:
				v = "`" + v + "`"
			case l["offline_since"]: // in the reader's time zone, and how long ago
				v = fmt.Sprintf("<t:%d:f> (<t:%d:R>)", g.Device.LastSeen.Unix(), g.Device.LastSeen.Unix())
			}
			e.Fields = append(e.Fields, field{Name: f[0], Value: cut(v, 1024), Inline: true})
		}
		if g.Tip != "" {
			e.Fields = append(e.Fields, field{Name: l["tip"], Value: cut(g.Tip, 1024)})
		}
		embeds = append(embeds, e)
	}
	out["embeds"] = embeds
	return out
}

// --- Slack: an attachment per card ---

func slackPayload(m Message) map[string]any {
	l := localeOf(labels, m.Locale)
	head := "*" + m.Subject + "*"
	if m.URL != "" {
		head += "  ·  <" + m.URL + "|" + l["open_map"] + ">"
	}
	if len(m.Groups) == 0 {
		return map[string]any{"text": "*" + m.Subject + "*\n" + m.Text}
	}
	var atts []map[string]any
	for _, g := range m.Groups {
		lines := itemLines(g, func(s string) string { return "*" + s + "*" })
		if len(g.Items) == 0 {
			for j, s := range lines {
				lines[j] = "• " + s
			}
		}
		a := map[string]any{
			"color":     fmt.Sprintf("#%06x", cardColor[g.Severity]),
			"title":     g.Title,
			"text":      strings.Join(lines, "\n"),
			"footer":    "Omini",
			"ts":        m.At.Unix(),
			"mrkdwn_in": []string{"text", "fields"},
		}
		if g.URL != "" {
			a["title_link"] = g.URL
		}
		if sev := badge(m, g); sev != "" {
			a["author_name"] = sev
		}
		var fields []map[string]any
		for _, f := range g.Device.facts(m.Locale) {
			fields = append(fields, map[string]any{"title": f[0], "value": f[1], "short": true})
		}
		if g.Tip != "" {
			fields = append(fields, map[string]any{"title": l["tip"], "value": g.Tip, "short": false})
		}
		if len(fields) > 0 {
			a["fields"] = fields
		}
		atts = append(atts, a)
	}
	return map[string]any{"text": head, "attachments": atts}
}

// --- Telegram: HTML text ---

func telegramText(m Message) string {
	l := localeOf(labels, m.Locale)
	esc := html.EscapeString
	if len(m.Groups) == 0 {
		return "<b>" + esc(m.Subject) + "</b>\n\n" + esc(m.Text)
	}
	var cards []string
	for _, g := range m.Groups {
		var b strings.Builder
		if sev := badge(m, g); sev != "" {
			fmt.Fprintf(&b, "<b>%s</b>  ·  <i>%s</i>\n", esc(g.Title), esc(sev))
		} else {
			fmt.Fprintf(&b, "<b>%s</b>\n", esc(g.Title))
		}
		for _, line := range itemLines(g, func(s string) string { return "\x00" + s + "\x01" }) {
			line = esc(line)
			line = strings.NewReplacer("\x00", "<b>", "\x01", "</b>").Replace(line)
			b.WriteString("• " + line + "\n")
		}
		if facts := g.Device.facts(m.Locale); len(facts) > 0 {
			b.WriteString("\n")
			for _, f := range facts {
				v := esc(f[1])
				if f[0] == l["ip"] || f[0] == l["mac"] {
					v = "<code>" + v + "</code>"
				}
				fmt.Fprintf(&b, "%s: %s\n", esc(f[0]), v)
			}
		}
		if g.Tip != "" {
			fmt.Fprintf(&b, "\n<i>%s: %s</i>\n", esc(l["tip"]), esc(g.Tip))
		}
		if g.URL != "" {
			fmt.Fprintf(&b, "<a href=\"%s\">%s</a>\n", esc(g.URL), esc(l["open"]))
		}
		cards = append(cards, strings.TrimRight(b.String(), "\n"))
	}
	out := "<b>" + esc(m.Subject) + "</b>"
	for i, c := range cards {
		next := out + "\n\n" + c
		if len(next) > 4000 { // Telegram's limit is 4096
			out += fmt.Sprintf("\n\n+%d", len(cards)-i)
			break
		}
		out = next
	}
	return out
}

// --- ntfy: Markdown ---

func ntfyText(m Message) string {
	l := localeOf(labels, m.Locale)
	if len(m.Groups) == 0 {
		return m.Text
	}
	var b strings.Builder
	for i, g := range m.Groups {
		if i > 0 {
			b.WriteString("\n")
		}
		if sev := badge(m, g); sev != "" {
			fmt.Fprintf(&b, "**%s** · %s\n", g.Title, sev)
		} else {
			fmt.Fprintf(&b, "**%s**\n", g.Title)
		}
		for _, line := range itemLines(g, func(s string) string { return "**" + s + "**" }) {
			b.WriteString("- " + line + "\n")
		}
		var facts []string
		for _, f := range g.Device.facts(m.Locale) {
			facts = append(facts, f[1])
		}
		if len(facts) > 0 {
			b.WriteString(strings.Join(facts, " · ") + "\n")
		}
		if g.Tip != "" {
			fmt.Fprintf(&b, "> %s: %s\n", l["tip"], g.Tip)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ntfyClick is where tapping the notification goes: the only device, else the map.
func ntfyClick(m Message) string {
	if len(m.Groups) == 1 && m.Groups[0].URL != "" {
		return m.Groups[0].URL
	}
	return m.URL
}

// --- e-mail: HTML ---

var cardCSS = map[string]string{"critical": "#f85149", "warning": "#d29922", "info": "#4c8dff", "resolved": "#3fb950"}

func emailHTML(m Message) string {
	l := localeOf(labels, m.Locale)
	esc := html.EscapeString
	var b strings.Builder
	b.WriteString(`<!doctype html><html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width"></head>`)
	b.WriteString(`<body style="margin:0;padding:24px 12px;background:#f3f4f6;font-family:-apple-system,Segoe UI,Helvetica,Arial,sans-serif;color:#1f2328">`)
	b.WriteString(`<table role="presentation" width="100%" cellspacing="0" cellpadding="0"><tr><td align="center">`)
	b.WriteString(`<table role="presentation" width="600" cellspacing="0" cellpadding="0" style="max-width:600px;width:100%">`)
	fmt.Fprintf(&b, `<tr><td style="padding:0 4px 16px"><div style="font-size:13px;color:#57606a;letter-spacing:.04em">OMINI</div>`+
		`<div style="font-size:20px;font-weight:600;margin-top:4px">%s</div></td></tr>`, esc(m.Subject))
	if len(m.Groups) == 0 {
		fmt.Fprintf(&b, `<tr><td style="background:#fff;border-radius:10px;padding:18px 20px;font-size:14px;line-height:1.5">%s</td></tr>`,
			strings.ReplaceAll(esc(m.Text), "\n", "<br>"))
	}
	for _, g := range m.Groups {
		color := cardCSS[g.Severity]
		b.WriteString(`<tr><td style="padding-bottom:14px">`)
		fmt.Fprintf(&b, `<table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="background:#fff;border-radius:10px;border-left:5px solid %s">`, color)
		b.WriteString(`<tr><td style="padding:16px 20px">`)
		if sev := badge(m, g); sev != "" {
			fmt.Fprintf(&b, `<span style="display:inline-block;font-size:11px;font-weight:600;text-transform:uppercase;letter-spacing:.05em;color:%s">%s</span>`,
				color, esc(sev))
		}
		title := esc(g.Title)
		if g.URL != "" {
			title = fmt.Sprintf(`<a href="%s" style="color:#1f2328;text-decoration:none">%s</a>`, esc(g.URL), title)
		}
		fmt.Fprintf(&b, `<div style="font-size:17px;font-weight:600;margin:4px 0 10px">%s</div>`, title)
		b.WriteString(`<ul style="margin:0 0 12px;padding-left:18px;font-size:14px;line-height:1.55">`)
		for _, line := range itemLines(g, func(s string) string { return "\x00" + s + "\x01" }) {
			line = strings.NewReplacer("\x00", "<b>", "\x01", "</b>").Replace(esc(line))
			b.WriteString("<li>" + line + "</li>")
		}
		b.WriteString(`</ul>`)
		if facts := g.Device.facts(m.Locale); len(facts) > 0 {
			b.WriteString(`<table role="presentation" cellspacing="0" cellpadding="0" style="font-size:13px;margin-bottom:12px">`)
			for _, f := range facts {
				v := esc(f[1])
				if f[0] == l["ip"] || f[0] == l["mac"] {
					v = `<span style="font-family:ui-monospace,Menlo,Consolas,monospace">` + v + `</span>`
				}
				fmt.Fprintf(&b, `<tr><td style="color:#57606a;padding:2px 16px 2px 0;white-space:nowrap">%s</td><td style="padding:2px 0">%s</td></tr>`, esc(f[0]), v)
			}
			b.WriteString(`</table>`)
		}
		if g.Tip != "" {
			fmt.Fprintf(&b, `<div style="background:#f6f8fa;border-radius:8px;padding:10px 12px;font-size:13px;line-height:1.5;margin-bottom:12px">`+
				`<b>%s</b><br>%s</div>`, esc(l["tip"]), esc(g.Tip))
		}
		if g.URL != "" {
			fmt.Fprintf(&b, `<a href="%s" style="display:inline-block;background:#1f6feb;color:#fff;text-decoration:none;font-size:13px;font-weight:600;padding:8px 14px;border-radius:6px">%s</a>`,
				esc(g.URL), esc(l["open"]))
		}
		b.WriteString(`</td></tr></table></td></tr>`)
	}
	foot := "Omini · " + m.At.Local().Format("2006-01-02 15:04")
	if m.URL != "" {
		foot = fmt.Sprintf(`<a href="%s" style="color:#57606a">Omini</a> · %s`, esc(m.URL), m.At.Local().Format("2006-01-02 15:04"))
	}
	fmt.Fprintf(&b, `<tr><td style="padding:6px 4px;font-size:12px;color:#57606a">%s</td></tr>`, foot)
	b.WriteString(`</table></td></tr></table></body></html>`)
	return b.String()
}
