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

	mu        sync.Mutex
	ports     [2]int // netflow, sflow
	conns     []net.PacketConn
	stop      context.CancelFunc
	lastAsked time.Time
	minute    time.Time
	convs     map[convKey]*store.FlowRow
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
	minute, convs := s.minute, s.convs
	s.minute, s.convs = time.Time{}, map[convKey]*store.FlowRow{}
	s.mu.Unlock()
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
