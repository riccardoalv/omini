package notify

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
)

// Cooldown: an alert that opens again within this time after it was last
// notified is not notified again (a flapping link sends one message, not
// dozens). Its resolution is not notified either while it flaps.
const Cooldown = 30 * time.Minute

// Dispatcher sends alert changes to every enabled channel, in the background.
type Dispatcher struct {
	Store  *store.Store
	Box    *secret.Box
	Client *http.Client     // nil: a client with Timeout
	Now    func() time.Time // for tests

	mu       sync.Mutex
	notified map[string]time.Time // alert key → when its opening was last sent
	wg       sync.WaitGroup
}

var rank = map[string]int{"critical": 0, "warning": 1, "info": 2}

// Handle is given the alerts opened and resolved by a collection round
// (collector.Options.OnAlerts). It returns at once; delivery runs in the
// background.
func (d *Dispatcher) Handle(changes []store.AlertChange) {
	now := d.now()
	d.mu.Lock()
	if d.notified == nil {
		d.notified = map[string]time.Time{}
	}
	var send []store.AlertChange
	for _, c := range changes {
		last, seen := d.notified[c.Alert.Key]
		recent := seen && now.Sub(last) < Cooldown
		if c.Opened {
			if !recent {
				send = append(send, c)
			}
			d.notified[c.Alert.Key] = now
			continue
		}
		// Resolved: unless it is flapping (opened moments ago) or was dismissed.
		if !recent && !c.Alert.Dismissed {
			send = append(send, c)
		}
	}
	for k, at := range d.notified { // forget old keys
		if now.Sub(at) > 24*time.Hour {
			delete(d.notified, k)
		}
	}
	d.mu.Unlock()
	if len(send) == 0 {
		return
	}
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 2*Timeout)
		defer cancel()
		d.deliver(ctx, send)
	}()
}

// Wait waits for deliveries in progress (tests, shutdown).
func (d *Dispatcher) Wait() { d.wg.Wait() }

func (d *Dispatcher) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d *Dispatcher) deliver(ctx context.Context, changes []store.AlertChange) {
	channels, err := d.Store.ListNotifiers(ctx)
	if err != nil {
		slog.Warn("could not read the notification channels", "err", err)
		return
	}
	locale := d.Store.AdminLocale(ctx)
	for _, n := range channels {
		if !n.Enabled {
			continue
		}
		var mine []store.AlertChange
		for _, c := range changes {
			if rank[c.Alert.Severity] <= rank[n.MinSeverity] && (c.Opened || n.NotifyResolved) {
				mine = append(mine, c)
			}
		}
		if len(mine) == 0 {
			continue
		}
		err := d.Send(ctx, n, Compose(mine, locale))
		if err != nil {
			slog.Warn("notification not delivered", "channel", n.ID, "type", n.Type, "err", err)
		}
		if err := d.Store.NotifierSent(ctx, n.ID, d.now(), err); err != nil {
			slog.Warn("could not record a delivery", "err", err)
		}
	}
}

// Send delivers one message to a stored channel (secrets opened here).
func (d *Dispatcher) Send(ctx context.Context, n store.Notifier, m Message) error {
	kind, err := KindOf(n.Type)
	if err != nil {
		return err
	}
	cfg, err := integration.OpenSecrets(d.Box, kind.Fields, n.Config)
	if err != nil {
		return err
	}
	return SendWith(ctx, n.Type, cfg, d.Client, m)
}

// SendWith delivers one message with a configuration whose secrets are open.
func SendWith(ctx context.Context, typ string, cfg integration.Config, client *http.Client, m Message) error {
	s, err := New(typ, cfg, client)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return s.Send(ctx, m)
}
