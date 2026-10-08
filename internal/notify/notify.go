// Package notify sends alerts to the channels the user configured: a webhook
// (plain JSON, Slack, Discord or ntfy), a Telegram bot or e-mail.
package notify

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

// Event is one alert opened or resolved, with its text in the user's language.
type Event struct {
	Opened bool        `json:"opened"`
	Alert  store.Alert `json:"alert"`
	Title  string      `json:"title"`
	Detail string      `json:"detail"`
}

// Message is what a channel delivers: a subject and a text, plus the events
// for channels that take structured data (JSON webhook).
type Message struct {
	Subject string  `json:"subject"`
	Text    string  `json:"text"`
	Events  []Event `json:"events"`
	// Groups is the message as cards: a device and what happened to it, the
	// new devices together, what was resolved (Discord embeds, Slack attachments).
	Groups []Group `json:"groups,omitempty"`
	// URL is Omini's map (empty when Omini's address is not known).
	URL string `json:"url,omitempty"`
	// At is when the round that found the changes ended.
	At time.Time `json:"at"`
	// Locale is the language of the texts (labels of the cards).
	Locale string `json:"locale,omitempty"`
	// Test marks the example sent by "Send a test" (it has no events).
	Test bool `json:"test,omitempty"`
}

// Group is one card of a message.
type Group struct {
	Severity string   `json:"severity"` // critical | warning | info | resolved
	Title    string   `json:"title"`
	Lines    []string `json:"lines"`
	// Items are the card's alerts, one per line of Lines (none for the new
	// devices and resolved cards, whose lines are names).
	Items []Item `json:"items,omitempty"`
	// Device is the device the card is about, as Omini knows it.
	Device *DeviceCard `json:"device,omitempty"`
	// Tip says what to do (the most severe alert's).
	Tip string `json:"tip,omitempty"`
	// URL opens the card's device on Omini's map.
	URL string `json:"url,omitempty"`
}

// Sender delivers messages to one channel.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// Kind is a type of channel: its form and how to build a sender from it.
type Kind struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Fields []model.FormField `json:"fields"`
	build  func(cfg integration.Config, client *http.Client) (Sender, error)
}

// Timeout bounds one delivery.
const Timeout = 15 * time.Second

func str(s string) *string { return &s }

// Kinds are the channels Omini can send to.
var Kinds = []Kind{
	{
		Type: "webhook", Name: "Webhook",
		Fields: []model.FormField{
			{Key: "url", Type: model.FormFieldTypeURL, Label: str("URL"), Required: true},
			{
				Key: "format", Type: model.FormFieldTypeSelect, Label: str("Format"), Default: "json",
				Options: []string{"json", "slack", "discord", "ntfy"},
				Help:    str("json: Omini's events; slack and discord: their incoming webhooks; ntfy: a topic URL."),
			},
			{
				Key: "secret", Type: model.FormFieldTypeSecret, Label: str("Signing secret"),
				Help: str("Optional: requests carry X-Omini-Signature (sha256 HMAC of the body)."),
			},
		},
		build: newWebhook,
	},
	{
		Type: "telegram", Name: "Telegram",
		Fields: []model.FormField{
			{
				Key: "token", Type: model.FormFieldTypeSecret, Label: str("Bot token"), Required: true,
				Help: str("Create a bot with @BotFather and paste its token."),
			},
			{
				Key: "chat_id", Type: model.FormFieldTypeString, Label: str("Chat ID"), Required: true,
				Help: str("Your user, group or channel id (message @userinfobot to get yours)."),
			},
		},
		build: newTelegram,
	},
	{
		Type: "email", Name: "E-mail",
		Fields: []model.FormField{
			{Key: "host", Type: model.FormFieldTypeHost, Label: str("SMTP server"), Required: true},
			{Key: "port", Type: model.FormFieldTypeInt, Label: str("Port"), Default: 587},
			{
				Key: "security", Type: model.FormFieldTypeSelect, Label: str("Security"), Default: "starttls",
				Options: []string{"starttls", "tls", "none"},
			},
			{Key: "username", Type: model.FormFieldTypeString, Label: str("Username")},
			{Key: "password", Type: model.FormFieldTypeSecret, Label: str("Password")},
			{Key: "from", Type: model.FormFieldTypeString, Label: str("From"), Required: true},
			{
				Key: "to", Type: model.FormFieldTypeString, Label: str("To"), Required: true,
				Help: str("One or more addresses, separated by commas."),
			},
		},
		build: newEmail,
	},
}

// KindOf returns the channel type.
func KindOf(typ string) (Kind, error) {
	for _, k := range Kinds {
		if k.Type == typ {
			return k, nil
		}
	}
	return Kind{}, fmt.Errorf("unknown notification channel %q", typ)
}

// ErrConfig is wrapped by configuration errors (shown to the user).
var ErrConfig = errors.New("invalid configuration")

// New builds a sender for a channel; cfg has its secrets opened.
func New(typ string, cfg integration.Config, client *http.Client) (Sender, error) {
	k, err := KindOf(typ)
	if err != nil {
		return nil, err
	}
	if client == nil {
		client = &http.Client{Timeout: Timeout}
	}
	return k.build(cfg, client)
}
