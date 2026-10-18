package collector

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

// Configuration backups: each device's configuration as readable text, read
// once a day and after every change Omini makes, kept when it changed. They
// are for history and differences; Omini never restores them.

const (
	// BackupEvery is how often each integration's devices are backed up.
	BackupEvery = 24 * time.Hour
	// backupAtSetting + integration id: when its last backup ran.
	backupAtSetting = "backup_at:"
	// ChangedOutsideFor is how long a change made outside Omini is an alert.
	ChangedOutsideFor = 24 * time.Hour
)

// markBackupDue asks for a backup of the integration's devices after the
// round (Omini just changed one).
func (c *Collector) markBackupDue(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.backupDue == nil {
		c.backupDue = map[int64]bool{}
	}
	c.backupDue[id] = true
}

// runBackups backs up the integrations that are due.
func (c *Collector) runBackups(ctx context.Context) {
	list, err := c.store.ListIntegrations(ctx)
	if err != nil {
		return
	}
	now := c.opts.Now()
	for _, in := range list {
		impl, err := c.reg.Get(in.Type)
		if err != nil || !in.Enabled || !impl.Info().Backup {
			continue
		}
		b, ok := impl.(integration.Backuper)
		if !ok {
			continue
		}
		c.mu.Lock()
		due := c.backupDue[in.ID]
		delete(c.backupDue, in.ID)
		c.mu.Unlock()
		key := backupAtSetting + strconv.FormatInt(in.ID, 10)
		if last, ok, _ := c.store.GetSetting(ctx, key); ok && !due {
			if at, err := time.Parse(time.RFC3339, last); err == nil && now.Sub(at) < BackupEvery {
				continue
			}
		}
		if err := c.backupIntegration(ctx, in, b); err != nil {
			slog.Warn("configuration backup failed", "integration", in.Name, "err", err)
			continue // tried again after the next round
		}
		_ = c.store.SetSetting(ctx, key, now.UTC().Format(time.RFC3339))
	}
}

// BackupNow backs up one integration's devices now.
func (c *Collector) BackupNow(ctx context.Context, integrationID int64) error {
	in, err := c.store.GetIntegration(ctx, integrationID)
	if err != nil {
		return err
	}
	impl, err := c.reg.Get(in.Type)
	if err != nil {
		return err
	}
	b, ok := impl.(integration.Backuper)
	if !ok || !impl.Info().Backup {
		return fmt.Errorf("%w: %s keeps no configuration backups", ErrNotChangeable, in.Name)
	}
	return c.backupIntegration(ctx, in, b)
}

func (c *Collector) backupIntegration(ctx context.Context, in store.Integration, b integration.Backuper) error {
	impl, err := c.reg.Get(in.Type)
	if err != nil {
		return err
	}
	cfg, err := integration.OpenSecrets(c.box, impl.Info().Fields, in.Config)
	if err != nil {
		return err
	}
	limit := c.opts.Timeout
	if t, ok := b.(interface{ Timeout() time.Duration }); ok && t.Timeout() > limit {
		limit = t.Timeout()
	}
	ctx2, cancel := context.WithTimeout(integration.WithInstance(ctx, in.ID), limit)
	defer cancel()
	backups, err := b.Backup(ctx2, cfg)
	if err != nil {
		return err
	}
	nodes := map[string]string{} // device key → node
	for _, n := range c.State().Topology.Nodes {
		for _, r := range n.Reports {
			if r.Device != nil && r.IntegrationID == in.ID {
				nodes[r.Device.Key] = n.ID
			}
		}
	}
	for _, bk := range backups {
		id, ok := nodes[bk.Device]
		if !ok || bk.Content == "" {
			continue
		}
		if err := c.keepBackup(ctx, in.ID, id, bk); err != nil {
			return err
		}
	}
	return nil
}

// keepBackup stores a version, compressed and sealed, if it changed.
func (c *Collector) keepBackup(ctx context.Context, integrationID int64, nodeID string, bk model.ConfigBackup) error {
	sum := sha256.Sum256([]byte(bk.Content))
	var zipped bytes.Buffer
	zw := gzip.NewWriter(&zipped)
	if _, err := zw.Write([]byte(bk.Content)); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	sealed, err := c.box.Seal(zipped.String())
	if err != nil {
		return err
	}
	now := c.opts.Now()
	byOmini := false
	if prev, err := c.store.Backups(ctx, nodeID); err == nil && len(prev) > 0 {
		changes, _ := c.store.ConfigChanges(ctx, nodeID, 50)
		for _, ch := range changes {
			if ch.NodeID == nodeID && ch.Error == "" && ch.Kind != string(model.ChangeKindWolSend) && ch.At.After(prev[0].TakenAt) {
				byOmini = true
				break
			}
		}
	}
	added, err := c.store.AddBackup(ctx, store.Backup{
		NodeID: nodeID, IntegrationID: integrationID, TakenAt: now, Format: string(bk.Format),
		SHA256: hex.EncodeToString(sum[:]), Size: len(bk.Content), ByOmini: byOmini, Content: sealed,
	})
	if added {
		slog.Info("configuration backed up", "node", nodeID, "by_omini", byOmini)
	}
	return err
}

// BackupContent is a version's configuration, opened.
func (c *Collector) BackupContent(ctx context.Context, id int64) (store.Backup, string, error) {
	b, err := c.store.GetBackup(ctx, id)
	if err != nil {
		return b, "", err
	}
	zipped, err := c.box.Open(b.Content)
	if err != nil {
		return b, "", err
	}
	zr, err := gzip.NewReader(bytes.NewReader([]byte(zipped)))
	if err != nil {
		return b, "", err
	}
	plain, err := io.ReadAll(io.LimitReader(zr, 64<<20))
	return b, string(plain), err
}
