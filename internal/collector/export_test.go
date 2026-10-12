package collector

import "context"

// AfterRound runs what follows a round (names, paused devices, backups).
func (c *Collector) AfterRound(ctx context.Context) { c.afterRound(ctx) }
