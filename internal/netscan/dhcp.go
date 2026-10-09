package netscan

import (
	"context"
	"encoding/binary"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// DHCP fingerprints: what a device asks for when it requests an address (the
// parameter request list, option 55, in its own order) and what it says it
// is (vendor class, option 60) tell its operating system. Omini only watches
// the requests that reach this server (broadcasts on its own network): it
// never answers, never opens port 67, and never stands in the way of a DHCP
// server running here.

// FingerprintStore keeps the fingerprints across restarts: a device only
// asks for an address when it joins the network or renews its lease.
type FingerprintStore interface {
	DHCPFingerprints(ctx context.Context) (map[model.MACAddress]model.DhcpFingerprint, error)
	SaveDHCPFingerprint(ctx context.Context, mac model.MACAddress, fp model.DhcpFingerprint, at time.Time) error
}

// dhcpIdle: the watcher stops when the scan has not asked for it for this
// long (the integration was disabled or deleted, or its DHCP method off).
const dhcpIdle = time.Hour

type dhcpWatch struct {
	mu      sync.Mutex
	seen    map[model.MACAddress]model.DhcpFingerprint
	asked   time.Time
	running bool
	loaded  bool
}

// fingerprint returns what a MAC asked for, when it was seen.
func (w *dhcpWatch) fingerprint(m model.MACAddress) (model.DhcpFingerprint, bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	fp, ok := w.seen[m]
	return fp, ok
}

// watchDHCP starts the watcher if it is not running, and keeps it alive.
func (s *Integration) watchDHCP(ctx context.Context) {
	w := &s.dhcp
	w.mu.Lock()
	w.asked = s.clock()
	if w.seen == nil {
		w.seen = map[model.MACAddress]model.DhcpFingerprint{}
	}
	load := !w.loaded && s.Fingerprints != nil
	w.loaded = true
	start := !w.running
	w.running = true
	w.mu.Unlock()

	if load {
		saved, err := s.Fingerprints.DHCPFingerprints(ctx)
		if err != nil {
			slog.Warn("could not read the DHCP fingerprints", "err", err)
		}
		w.mu.Lock()
		for m, fp := range saved {
			if _, newer := w.seen[m]; !newer {
				w.seen[m] = fp
			}
		}
		w.mu.Unlock()
	}
	if !start {
		return
	}
	listen := s.ListenDHCP
	if listen == nil {
		listen = sniffDHCP
	}
	wctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer cancel()
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-wctx.Done():
				return
			case <-t.C:
				w.mu.Lock()
				idle := s.clock().Sub(w.asked) > dhcpIdle
				w.mu.Unlock()
				if idle {
					return
				}
			}
		}
	}()
	go func() {
		err := listen(wctx, func(packet []byte) { s.noteDHCP(packet) })
		cancel()
		if err != nil {
			// No permission (or not Linux): that does not change while Omini
			// runs, so it is not tried again.
			slog.Info("not watching DHCP requests", "err", err)
			return
		}
		w.mu.Lock()
		w.running = false // stopped for being idle: the next scan starts it again
		w.mu.Unlock()
	}()
}

// noteDHCP records a request read from the network (an IPv4 packet).
func (s *Integration) noteDHCP(packet []byte) {
	mac, fp, ok := parseDHCPRequest(packet)
	if !ok {
		return
	}
	w := &s.dhcp
	w.mu.Lock()
	old, had := w.seen[mac]
	w.seen[mac] = fp
	w.mu.Unlock()
	if (!had || !sameFingerprint(old, fp)) && s.Fingerprints != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.Fingerprints.SaveDHCPFingerprint(ctx, mac, fp, s.clock()); err != nil {
			slog.Warn("could not save a DHCP fingerprint", "err", err)
		}
	}
}

func (s *Integration) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

// parseDHCPRequest reads a DHCP request (DISCOVER, REQUEST or INFORM) from
// an IPv4 packet: the client's MAC and its fingerprint.
func parseDHCPRequest(p []byte) (model.MACAddress, model.DhcpFingerprint, bool) {
	var fp model.DhcpFingerprint
	if len(p) < 20 || p[0]>>4 != 4 || p[9] != 17 { // IPv4, UDP
		return "", fp, false
	}
	ihl := int(p[0]&0x0f) * 4
	if len(p) < ihl+8 || binary.BigEndian.Uint16(p[ihl+2:]) != 67 {
		return "", fp, false
	}
	b := p[ihl+8:]
	// BOOTP: op 1 (request), Ethernet (htype 1, hlen 6), then the magic cookie at 236.
	if len(b) < 240 || b[0] != 1 || b[1] != 1 || b[2] != 6 ||
		b[236] != 99 || b[237] != 130 || b[238] != 83 || b[239] != 99 {
		return "", fp, false
	}
	mac := model.MACFromBytes(b[28:34])
	if mac == "" {
		return "", fp, false
	}
	var msgType byte
	for i := 240; i < len(b); {
		code := b[i]
		if code == 255 {
			break
		}
		if code == 0 {
			i++
			continue
		}
		if i+1 >= len(b) || i+2+int(b[i+1]) > len(b) {
			break
		}
		val := b[i+2 : i+2+int(b[i+1])]
		switch code {
		case 53:
			if len(val) == 1 {
				msgType = val[0]
			}
		case 55:
			params := make([]string, len(val))
			for j, o := range val {
				params[j] = strconv.Itoa(int(o))
			}
			fp.Params = model.Ptr(strings.Join(params, ","))
		case 60:
			fp.VendorClass = printable(val)
		case 12:
			fp.Hostname = printable(val)
		}
		i += 2 + len(val)
	}
	switch msgType {
	case 1, 3, 8: // DISCOVER, REQUEST, INFORM
	default:
		return "", fp, false
	}
	if fp.Params == nil && fp.VendorClass == nil {
		return "", fp, false
	}
	return mac, fp, true
}

func sameFingerprint(a, b model.DhcpFingerprint) bool {
	return model.Deref(a.Params) == model.Deref(b.Params) && model.Deref(a.VendorClass) == model.Deref(b.VendorClass) &&
		model.Deref(a.Hostname) == model.Deref(b.Hostname)
}

// printable keeps a text option only when it is plain text.
func printable(b []byte) *string {
	s := strings.TrimRight(string(b), "\x00")
	if s == "" {
		return nil
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return nil
		}
	}
	return &s
}
