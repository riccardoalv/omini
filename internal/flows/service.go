package flows

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/geoip"
	"github.com/riccardoalv/omini/internal/store"
)

// TopPerMinute bounds what is stored: the busiest conversations of each minute.
const TopPerMinute = 300

// Idle: listeners close when the integration has not asked for them for this
// long (it was disabled or deleted) — or for three rounds, when rounds are
// further apart (see Service.Round).
const Idle = 10 * time.Minute

// Service receives flow exports and stores a minute of conversations at a
// time. The flows integration turns it on (Configure) on every collection.
type Service struct {
	Store *store.Store
	Now   func() time.Time
	// Round is the time between collection rounds (nil: Idle alone), so
	// listeners stay open between rounds set further apart than Idle.
	Round func() time.Duration
	// Directory tells local from internet addresses and names them (nil: no
	// internet traffic per device is kept).
	Directory func() *Directory
	// Geo tells where an internet address is (nil: nowhere).
	Geo func(netip.Addr) geoip.Place

	mu        sync.Mutex
	ports     [2]int // netflow, sflow
	conns     []net.PacketConn
	stop      context.CancelFunc
	lastAsked time.Time
	minute    time.Time
	convs     map[convKey]*store.FlowRow
	probes    map[netip.Addr]map[probe]bool // this minute: the local services each local client reached
	scans     []Scan                        // the last hour's scans
	exporters map[netip.Addr]exporterStat
	templates *Templates
	wg        sync.WaitGroup
}

type convKey struct {
	a, b  netip.Addr
	proto uint8
	port  uint16
}

type exporterStat struct {
	Kind    string    `json:"kind"` // netflow | sflow
	Flows   uint64    `json:"flows"`
	LastAt  time.Time `json:"last_at"`
	Version string    `json:"version,omitempty"`
}

// Exporter is a device sending flows, for the UI.
type Exporter struct {
	IP string `json:"ip"`
	exporterStat
}

// idleAfter is how long the integration may go without asking before the
// listeners close.
func (s *Service) idleAfter() time.Duration {
	if s.Round == nil {
		return Idle
	}
	return max(Idle, 3*s.Round())
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Configure listens on the given UDP ports (0 = off), rebinding only when they
// change. It is called on every collection of the flows integration.
func (s *Service) Configure(ctx context.Context, netflowPort, sflowPort int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastAsked = s.now()
	want := [2]int{netflowPort, sflowPort}
	if s.stop != nil && s.ports == want {
		return nil
	}
	s.closeLocked()
	if s.templates == nil {
		s.templates = NewTemplates()
		s.convs = map[convKey]*store.FlowRow{}
		s.exporters = map[netip.Addr]exporterStat{}
	}
	runCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	var errs []error
	for i, port := range want {
		if port == 0 {
			continue
		}
		conn, err := net.ListenPacket("udp", ":"+strconv.Itoa(port))
		if err != nil {
			errs = append(errs, fmt.Errorf("cannot listen on UDP port %d: %w", port, err))
			continue
		}
		s.conns = append(s.conns, conn)
		s.wg.Add(1)
		go s.serve(conn, i == 1)
	}
	s.ports, s.stop = want, cancel
	s.wg.Add(1)
	go s.tick(runCtx)
	return errors.Join(errs...)
}

// Close stops listening and stores what was received.
func (s *Service) Close() {
	s.mu.Lock()
	s.closeLocked()
	s.mu.Unlock()
	s.wg.Wait()
	s.flush(true)
}

func (s *Service) closeLocked() {
	for _, c := range s.conns {
		c.Close()
	}
	s.conns = nil
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
}

// Addrs returns the addresses listened on (tests use port 0 → a random port).
func (s *Service) Addrs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, c := range s.conns {
		out = append(out, c.LocalAddr().String())
	}
	return out
}

func (s *Service) serve(conn net.PacketConn, sflow bool) {
	defer s.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, from, err := conn.ReadFrom(buf)
		if err != nil {
			return // closed
		}
		addr, _ := netip.ParseAddrPort(from.String())
		exporter := addr.Addr().Unmap()
		var (
			recs []Record
			kind = "netflow"
		)
		if sflow {
			kind = "sflow"
			recs, err = DecodeSFlow(buf[:n])
		} else {
			recs, err = DecodeNetFlow(buf[:n], exporter, s.templates)
		}
		if err != nil {
			slog.Debug("flow export not decoded", "from", exporter, "err", err)
		}
		version := ""
		if !sflow && n >= 2 {
			version = "v" + strconv.Itoa(int(buf[0])<<8|int(buf[1]))
		}
		s.Add(exporter, kind, version, recs)
	}
}

// Add sums records into the current minute.
func (s *Service) Add(exporter netip.Addr, kind, version string, recs []Record) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.convs == nil {
		s.convs = map[convKey]*store.FlowRow{}
		s.exporters = map[netip.Addr]exporterStat{}
	}
	if s.minute.IsZero() {
		s.minute = now.Truncate(time.Minute)
	}
	st := s.exporters[exporter]
	st.Kind, st.LastAt, st.Flows = kind, now, st.Flows+uint64(len(recs))
	if version != "" {
		st.Version = version
	}
	s.exporters[exporter] = st
	dir := s.directory()
	if s.probes == nil {
		s.probes = map[netip.Addr]map[probe]bool{}
	}
	for _, r := range recs {
		a, b, ab := r.Src.Unmap(), r.Dst.Unmap(), true
		if b.Less(a) {
			a, b, ab = b, a, false
		}
		// The service port: the lower of the two (a client's is ephemeral).
		port := r.DstPort
		if r.SrcPort != 0 && (port == 0 || r.SrcPort < port) {
			port = r.SrcPort
		}
		k := convKey{a, b, r.Proto, port}
		row := s.convs[k]
		if row == nil {
			row = &store.FlowRow{A: a.String(), B: b.String(), Proto: r.Proto, Port: port}
			s.convs[k] = row
		}
		if ab {
			row.BytesAB += r.Bytes
		} else {
			row.BytesBA += r.Bytes
		}
		row.Packets += r.Packets
		// The client is the side not on the service port.
		client, server := r.Src.Unmap(), r.Dst.Unmap()
		if port != r.DstPort {
			client, server = server, client
		}
		if dir != nil && dir.Local(client) && dir.Local(server) && !dir.WAN(server) {
			seen := s.probes[client]
			if seen == nil {
				seen = map[probe]bool{}
				s.probes[client] = seen
			}
			if len(seen) <= ScanTargets {
				seen[probe{server, port}] = true
			}
		}
	}
}

func (s *Service) tick(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.flush(false)
			s.mu.Lock()
			idle := s.now().Sub(s.lastAsked) > s.idleAfter()
			if idle {
				slog.Info("flow collector stopped: the flows integration is off")
				s.closeLocked()
			}
			s.mu.Unlock()
			if idle {
				return
			}
		}
	}
}

// flush stores the finished minute (or the current one when forced).
func (s *Service) flush(force bool) {
	s.mu.Lock()
	now := s.now()
	if s.minute.IsZero() || (!force && now.Truncate(time.Minute).Equal(s.minute)) {
		s.mu.Unlock()
		return
	}
	minute, convs, probes := s.minute, s.convs, s.probes
	s.minute, s.convs, s.probes = time.Time{}, map[convKey]*store.FlowRow{}, map[netip.Addr]map[probe]bool{}
	dir := s.directory()
	kept := s.scans[:0]
	for _, sc := range s.scans {
		if now.Sub(sc.At) <= time.Hour {
			kept = append(kept, sc)
		}
	}
	s.scans = kept
	for addr, seen := range probes {
		if len(seen) >= ScanTargets {
			s.scans = append(s.scans, Scan{Addr: addr, Node: dir.Node(addr), Targets: len(seen), At: minute})
		}
	}
	s.mu.Unlock()
	if dir != nil && s.Store != nil {
		usage, dests, places := internet(dir, s.Geo, convs)
		if len(usage) > 0 {
			if err := s.Store.SaveInternet(context.Background(), minute, usage, dests, places...); err != nil {
				slog.Warn("could not store internet traffic", "err", err)
			}
		}
	}
	rows := make([]store.FlowRow, 0, len(convs))
	for _, r := range convs {
		rows = append(rows, *r)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].BytesAB+rows[i].BytesBA > rows[j].BytesAB+rows[j].BytesBA })
	if len(rows) > TopPerMinute {
		rows = rows[:TopPerMinute]
	}
	if s.Store == nil || len(rows) == 0 {
		return
	}
	if err := s.Store.SaveFlows(context.Background(), minute, rows); err != nil {
		slog.Warn("could not store flows", "err", err)
	}
}

// Exporters lists the devices that sent flows, most recent first.
func (s *Service) Exporters() []Exporter {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Exporter, 0, len(s.exporters))
	for ip, st := range s.exporters {
		out = append(out, Exporter{IP: ip.String(), exporterStat: st})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastAt.After(out[j].LastAt) })
	return out
}

// Listening reports whether the service is receiving (the integration is on).
func (s *Service) Listening() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop != nil
}

// probe is a local service a local client reached: an address and a port.
type probe struct {
	addr netip.Addr
	port uint16
}

// ScanTargets is how many local services one client must reach within a
// minute to be scanning the network (a normal client reaches a handful).
const ScanTargets = 50

// Scan is a local device that reached many local services within a minute.
type Scan struct {
	Addr    netip.Addr
	Node    string // its node on the map ("" when unknown)
	Targets int
	At      time.Time
}

// Scans are the scans seen since a time.
func (s *Service) Scans(since time.Time) []Scan {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Scan
	for _, sc := range s.scans {
		if !sc.At.Before(since) {
			out = append(out, sc)
		}
	}
	return out
}

func (s *Service) directory() *Directory {
	if s.Directory == nil {
		return nil
	}
	return s.Directory()
}

// internet works out each device's internet traffic in a minute of
// conversations, and where it went: an address on the local network talking
// to one outside it. Traffic on a router's internet side is left out: it is
// the LAN's traffic again, after NAT.
func internet(dir *Directory, geo func(netip.Addr) geoip.Place, convs map[convKey]*store.FlowRow) ([]store.Usage, []store.Destination, []store.PlaceUsage) {
	usage := map[string]*store.Usage{}
	type destKey struct {
		node, dest string
		proto      uint8
		port       uint16
	}
	dests := map[destKey]*store.Destination{}
	places := map[[2]string]*store.PlaceUsage{}
	for k, row := range convs {
		if dir.WAN(k.a) || dir.WAN(k.b) {
			continue
		}
		la, lb := dir.Local(k.a), dir.Local(k.b)
		if la == lb {
			continue
		}
		local, remote, tx, rx := k.a, k.b, row.BytesAB, row.BytesBA
		if lb {
			local, remote, tx, rx = k.b, k.a, row.BytesBA, row.BytesAB
		}
		node := dir.Node(local)
		if node == "" {
			node = "ip:" + local.String()
		}
		u := usage[node]
		if u == nil {
			u = &store.Usage{Node: node}
			usage[node] = u
		}
		u.RX += rx
		u.TX += tx
		dest := dir.Name(local, remote)
		if dest == "" {
			dest = remote.String()
		}
		dk := destKey{node, dest, k.proto, k.port}
		d := dests[dk]
		if d == nil {
			d = &store.Destination{Node: node, Dest: dest, Proto: k.proto, Port: k.port, Addr: remote.String()}
			dests[dk] = d
		}
		d.RX += rx
		d.TX += tx
		var where geoip.Place
		if geo != nil {
			where = geo(remote)
		}
		if where.Org == "" {
			where.Org = dest // the name, else the address
		}
		pk := [2]string{where.Country, where.Org}
		p := places[pk]
		if p == nil {
			p = &store.PlaceUsage{Country: where.Country, Org: where.Org}
			places[pk] = p
		}
		p.RX += rx
		p.TX += tx
	}
	outU := make([]store.Usage, 0, len(usage))
	for _, u := range usage {
		outU = append(outU, *u)
	}
	// Each device's busiest destinations of the minute.
	byNode := map[string][]store.Destination{}
	for _, d := range dests {
		byNode[d.Node] = append(byNode[d.Node], *d)
	}
	var outD []store.Destination
	for _, list := range byNode {
		sort.Slice(list, func(i, j int) bool { return list[i].RX+list[i].TX > list[j].RX+list[j].TX })
		if len(list) > DestinationsPerMinute {
			list = list[:DestinationsPerMinute]
		}
		outD = append(outD, list...)
	}
	outP := make([]store.PlaceUsage, 0, len(places))
	for _, p := range places {
		outP = append(outP, *p)
	}
	return outU, outD, outP
}

// DestinationsPerMinute bounds what is kept of each device's minute.
const DestinationsPerMinute = 40
