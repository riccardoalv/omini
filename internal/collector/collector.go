// Package collector polls every enabled integration periodically, keeps the
// last result of each one, and rebuilds the topology and inventory from them.
package collector

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/classify"
	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
	"github.com/riccardoalv/omini/internal/store"
	"github.com/riccardoalv/omini/internal/topology"
)

// OfflineVisible is how long a client that left stays on the map (dimmed).
const OfflineVisible = time.Hour

// Status is the outcome of the last collection of one integration.
type Status struct {
	IntegrationID int64     `json:"integration_id"`
	OK            bool      `json:"ok"`
	Error         string    `json:"error,omitempty"`
	CollectedAt   time.Time `json:"collected_at"`
	DurationMs    int64     `json:"duration_ms"`
	Devices       int       `json:"devices"`
}

// State is everything the UI needs to draw the map.
type State struct {
	Topology    topology.Topology `json:"topology"`
	Statuses    []Status          `json:"statuses"`
	GeneratedAt time.Time         `json:"generated_at"`
}

type Options struct {
	Interval    time.Duration    // time between collection rounds
	Timeout     time.Duration    // per integration
	Concurrency int              // integrations collected in parallel
	Now         func() time.Time // for tests
}

type Collector struct {
	store *store.Store
	reg   *integration.Registry
	box   *secret.Box
	opts  Options

	mu    sync.RWMutex
	snaps map[int64]store.Snapshot
	state State

	round   sync.Mutex // one collection round at a time
	trigger chan struct{}
}

func New(st *store.Store, reg *integration.Registry, box *secret.Box, opts Options) *Collector {
	if opts.Interval == 0 {
		opts.Interval = time.Minute
	}
	if opts.Timeout == 0 {
		opts.Timeout = 45 * time.Second
	}
	if opts.Concurrency == 0 {
		opts.Concurrency = 8
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Collector{
		store: st, reg: reg, box: box, opts: opts,
		snaps:   map[int64]store.Snapshot{},
		trigger: make(chan struct{}, 1),
		state:   State{Topology: topology.Topology{Nodes: []topology.Node{}, Edges: []topology.Edge{}}, Statuses: []Status{}},
	}
}

// Run loads the last known state, then collects every Interval (or when
// Refresh is called) until ctx is done.
func (c *Collector) Run(ctx context.Context) error {
	if err := c.Load(ctx); err != nil {
		return err
	}
	c.collectLogged(ctx)
	ticker := time.NewTicker(c.opts.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.collectLogged(ctx)
		case <-c.trigger:
			c.collectLogged(ctx)
		}
	}
}

func (c *Collector) collectLogged(ctx context.Context) {
	if err := c.CollectNow(ctx); err != nil && ctx.Err() == nil {
		slog.Error("collection round failed", "err", err)
	}
}

// Refresh asks Run to start a collection round as soon as possible.
func (c *Collector) Refresh() {
	select {
	case c.trigger <- struct{}{}:
	default: // a round is already pending
	}
}

// State returns the current map state.
func (c *Collector) State() State {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.state
}

// Load restores the snapshots saved by previous runs, so the map is available
// immediately after a restart.
func (c *Collector) Load(ctx context.Context) error {
	snaps, err := c.store.ListSnapshots(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	for _, s := range snaps {
		c.snaps[s.IntegrationID] = s
	}
	c.mu.Unlock()
	return c.rebuild(ctx)
}

// CollectNow runs one collection round over all enabled integrations.
func (c *Collector) CollectNow(ctx context.Context) error {
	c.round.Lock()
	defer c.round.Unlock()

	all, err := c.store.ListIntegrations(ctx)
	if err != nil {
		return err
	}
	var enabled []store.Integration
	for _, in := range all {
		if in.Enabled {
			enabled = append(enabled, in)
		}
	}

	results := make([]store.Snapshot, len(enabled))
	sem := make(chan struct{}, c.opts.Concurrency)
	var wg sync.WaitGroup
	for i, in := range enabled {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = c.collectOne(ctx, in)
		}()
	}
	wg.Wait()

	snaps := make(map[int64]store.Snapshot, len(results))
	for _, r := range results {
		snaps[r.IntegrationID] = r
		if err := c.store.SaveSnapshot(ctx, r); err != nil {
			slog.Error("save snapshot", "integration", r.IntegrationID, "err", err)
		}
	}
	c.mu.Lock()
	c.snaps = snaps // disabled and deleted integrations drop out here
	c.mu.Unlock()
	return c.rebuild(ctx)
}

// ErrDisabled is returned when running an integration that is disabled.
var ErrDisabled = errors.New("integration is disabled")

// CollectIntegration runs one integration now (user request), skipping its
// caches, and returns its new status. The map is rebuilt right away.
func (c *Collector) CollectIntegration(ctx context.Context, id int64) (Status, error) {
	c.round.Lock()
	defer c.round.Unlock()
	in, err := c.store.GetIntegration(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if !in.Enabled {
		return Status{}, ErrDisabled
	}
	snap := c.collectOne(integration.WithForce(ctx), in)
	if err := c.store.SaveSnapshot(ctx, snap); err != nil {
		slog.Error("save snapshot", "integration", snap.IntegrationID, "err", err)
	}
	c.mu.Lock()
	c.snaps[id] = snap
	c.mu.Unlock()
	if err := c.rebuild(ctx); err != nil {
		return Status{}, err
	}
	return Status{
		IntegrationID: id, OK: snap.OK, Error: snap.Error, CollectedAt: snap.CollectedAt,
		DurationMs: snap.DurationMs, Devices: countDevices(snap.Devices),
	}, nil
}

func (c *Collector) collectOne(ctx context.Context, in store.Integration) store.Snapshot {
	start := c.opts.Now()
	devices, err := c.runIntegration(ctx, in)
	snap := store.Snapshot{
		IntegrationID: in.ID,
		CollectedAt:   start,
		DurationMs:    c.opts.Now().Sub(start).Milliseconds(),
		OK:            err == nil,
		Devices:       devices,
	}
	if err != nil {
		slog.Warn("integration collection failed", "integration", in.Name, "type", in.Type, "err", err)
		snap.Error = err.Error()
		// Keep the last known devices: they are shown as offline instead of vanishing.
		c.mu.RLock()
		snap.Devices = c.snaps[in.ID].Devices
		c.mu.RUnlock()
	}
	if snap.Devices == nil {
		snap.Devices = []model.Device{}
	}
	return snap
}

func (c *Collector) runIntegration(ctx context.Context, in store.Integration) (devices []model.Device, err error) {
	impl, err := c.reg.Get(in.Type)
	if err != nil {
		return nil, err
	}
	cfg, err := integration.OpenSecrets(c.box, impl.Info().Fields, in.Config)
	if err != nil {
		return nil, err
	}
	limit := c.opts.Timeout
	if t, ok := impl.(interface{ Timeout() time.Duration }); ok && t.Timeout() > limit {
		limit = t.Timeout() // plugins declare their own limit
	}
	ctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	defer func() {
		// A buggy integration must never take the whole collector down.
		if r := recover(); r != nil {
			devices, err = nil, fmt.Errorf("integration panicked: %v", r)
		}
	}()
	return impl.Collect(integration.WithInstance(ctx, in.ID), cfg)
}

func (c *Collector) rebuild(ctx context.Context) error {
	c.mu.RLock()
	snaps := make([]store.Snapshot, 0, len(c.snaps))
	for _, s := range c.snaps {
		snaps = append(snaps, s)
	}
	c.mu.RUnlock()
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].IntegrationID < snaps[j].IntegrationID })

	sources := make([]topology.Source, 0, len(snaps))
	statuses := make([]Status, 0, len(snaps))
	for _, s := range snaps {
		sources = append(sources, topology.Source{IntegrationID: s.IntegrationID, Online: s.OK, Devices: s.Devices})
		statuses = append(statuses, Status{
			IntegrationID: s.IntegrationID, OK: s.OK, Error: s.Error,
			CollectedAt: s.CollectedAt, DurationMs: s.DurationMs, Devices: countDevices(s.Devices),
		})
	}
	topo := topology.Build(sources)
	now := c.opts.Now()

	// Record what is present now (before user aliases are applied to labels).
	var seen []store.InventoryEntry
	for _, n := range topo.Nodes {
		if n.Online {
			seen = append(seen, store.InventoryEntry{
				ID: n.ID, Kind: string(n.Kind), Label: n.Label, MAC: n.MAC, IP: n.IP,
				Hostname: n.Hostname, Vendor: n.Vendor, ParentID: n.ParentID, Port: n.Port,
			})
		}
	}
	if err := c.store.MarkSeen(ctx, seen, now); err != nil {
		return fmt.Errorf("update inventory: %w", err)
	}
	inventory, err := c.store.ListInventory(ctx)
	if err != nil {
		return err
	}
	classifyNodes(&topo)
	expandApps(&topo)
	attachVMs(&topo)
	applyInventory(&topo, inventory, now)

	c.mu.Lock()
	c.state = State{Topology: topo, Statuses: statuses, GeneratedAt: now}
	c.mu.Unlock()
	return nil
}

// applyInventory applies user aliases and pins, and adds clients that left
// less than OfflineVisible ago as offline nodes.
func applyInventory(topo *topology.Topology, inventory []store.InventoryEntry, now time.Time) {
	present := make(map[string]bool, len(topo.Nodes))
	for i := range topo.Nodes {
		present[topo.Nodes[i].ID] = true
	}
	byID := make(map[string]store.InventoryEntry, len(inventory))
	for _, e := range inventory {
		byID[e.ID] = e
	}
	for i := range topo.Nodes {
		n := &topo.Nodes[i]
		if e, ok := byID[n.ID]; ok {
			if e.Alias != "" {
				n.Label = e.Alias
			}
			n.Pinned, n.Hidden = e.Pinned, e.Hidden
			applyOverrides(n, e)
		}
	}
	for _, e := range inventory {
		if e.Kind != string(topology.KindClient) || present[e.ID] || now.Sub(e.LastSeen) > OfflineVisible {
			continue
		}
		lastSeen := e.LastSeen
		n := topology.Node{
			ID: e.ID, Kind: topology.KindClient, Role: "client", Label: e.Label, Online: false,
			MAC: e.MAC, IP: e.IP, Hostname: e.Hostname, Vendor: e.Vendor, Pinned: e.Pinned, Hidden: e.Hidden,
			RandomMAC: model.MACAddress(e.MAC).IsRandomized(), LastSeen: &lastSeen,
		}
		if e.Alias != "" {
			n.Label = e.Alias
		}
		classifyNode(&n)
		applyOverrides(&n, e)
		if present[e.ParentID] {
			n.ParentID, n.Port = e.ParentID, e.Port
			topo.Edges = append(topo.Edges, topology.Edge{
				ID: "e:" + e.ParentID + "|" + e.ID, Source: e.ParentID, Target: e.ID,
				SourcePort: e.Port, Kind: topology.EdgeInferred,
			})
		}
		topo.Nodes = append(topo.Nodes, n)
	}
}

// classifyNodes fills type, OS, brand and product of every node.
func classifyNodes(topo *topology.Topology) {
	for i := range topo.Nodes {
		classifyNode(&topo.Nodes[i])
	}
}

func classifyNode(n *topology.Node) {
	in := classify.Input{
		Kind: string(n.Kind), Role: n.Role, Vendor: n.Vendor, Model: n.Model, Hostname: n.Hostname,
		OS: n.ReportedOS, RandomMAC: n.RandomMAC, OpenPorts: n.OpenPorts, Services: n.Services,
		Titles: n.Titles, Banners: n.Banners, TTL: n.TTL, Self: slices.Contains(n.Services, "omini"),
	}
	if in.Hostname == "" && n.Kind != topology.KindClient {
		in.Hostname = n.Label // managed devices: the name reported by the integration
	}
	r := classify.Classify(in)
	n.Type, n.OS, n.Brand, n.Product, n.Reasons = r.Type, r.OS, r.Brand, r.Product, r.Reasons
}

// applyOverrides applies the user's corrections of the classification.
func applyOverrides(n *topology.Node, e store.InventoryEntry) {
	if e.DeviceType != "" {
		n.Type = e.DeviceType
		n.Reasons = append([]string{"user"}, n.Reasons...)
	}
	if e.Icon != "" {
		n.Icon = e.Icon
	}
}

// countDevices counts devices and the hosts they report (a network scan
// reports a single gateway with every host under it), without counting a
// device twice when it also appears among the hosts.
func countDevices(devices []model.Device) int {
	n := len(devices)
	own := map[string]bool{}
	for _, d := range devices {
		for _, m := range d.MACs {
			own[string(m)] = true
		}
		for _, ip := range d.IPs {
			own[ip] = true
		}
	}
	for _, d := range devices {
		for _, h := range d.Hosts {
			if (h.MAC != nil && own[string(*h.MAC)]) || own[h.IP] {
				continue
			}
			n++
		}
	}
	return n
}
