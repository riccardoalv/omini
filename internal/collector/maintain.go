package collector

import (
	"context"
	"log/slog"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
)

// afterRound keeps what Omini set on the devices in step with the network
// after a round: DNS names of renamed devices, paused devices that changed
// address, and the configuration backups that are due.
func (c *Collector) afterRound(ctx context.Context) {
	c.syncFlowNames(ctx)
	c.syncDNSNames(ctx)
	c.syncPaused(ctx)
	c.runBackups(ctx)
}

// byMAC finds the node of a MAC on the map.
func (c *Collector) byMAC(mac string) (string, bool) {
	for _, n := range c.State().Topology.Nodes {
		if string(model.NormMAC(n.MAC)) == mac {
			return n.ID, true
		}
	}
	return "", false
}

// syncDNSNames updates the names Omini set: a renamed device, a freed
// address, a name turned off. New names come from fixing an address.
func (c *Collector) syncDNSNames(ctx context.Context) {
	applied, err := c.store.SettingsWithPrefix(ctx, dnsAppliedSetting)
	if err != nil {
		slog.Warn("could not read the DNS names Omini set", "err", err)
		return
	}
	for key, v := range applied {
		if v == "" {
			continue
		}
		id, ok := c.byMAC(strings.TrimPrefix(key, dnsAppliedSetting))
		if !ok {
			continue // not on the map now: its name stays until it is back
		}
		key := "dns|" + id
		if !c.retry.due(key, c.opts.Now()) {
			continue // it refused lately: tried again later
		}
		err := c.SyncDNSName(ctx, id)
		c.retry.done(key, c.opts.Now(), err)
		if err != nil {
			slog.Warn("could not update a DNS name", "node", id, "err", err)
		}
	}
}
