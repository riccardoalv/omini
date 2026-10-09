package netscan

import (
	"context"
	"encoding/binary"
	"sync"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// dhcpPacket builds an IPv4/UDP packet carrying a BOOTP message.
func dhcpPacket(op byte, dstPort uint16, mac []byte, options ...[]byte) []byte {
	bootp := make([]byte, 240)
	bootp[0], bootp[1], bootp[2] = op, 1, 6
	copy(bootp[28:], mac)
	copy(bootp[236:], []byte{99, 130, 83, 99})
	for _, o := range options {
		bootp = append(bootp, o...)
	}
	bootp = append(bootp, 255)
	udp := make([]byte, 8)
	binary.BigEndian.PutUint16(udp[0:], 68)
	binary.BigEndian.PutUint16(udp[2:], dstPort)
	binary.BigEndian.PutUint16(udp[4:], uint16(8+len(bootp)))
	ip := make([]byte, 20)
	ip[0], ip[9] = 0x45, 17
	return append(append(ip, udp...), bootp...)
}

func opt(code byte, val ...byte) []byte { return append([]byte{code, byte(len(val))}, val...) }

var phoneMAC = []byte{0xda, 0xa1, 0x19, 0x00, 0x00, 0x01}

func androidDiscover() []byte {
	return dhcpPacket(1, 67, phoneMAC,
		opt(53, 1),
		opt(55, 1, 3, 6, 15, 26, 28, 51, 58, 59, 43, 114, 108),
		opt(60, []byte("android-dhcp-14")...),
		opt(12, []byte("moto-edge-70")...),
	)
}

func TestParseDHCPRequest(t *testing.T) {
	mac, fp, ok := parseDHCPRequest(androidDiscover())
	if !ok || mac != "da:a1:19:00:00:01" {
		t.Fatalf("parsed %v %q", ok, mac)
	}
	if model.Deref(fp.Params) != "1,3,6,15,26,28,51,58,59,43,114,108" ||
		model.Deref(fp.VendorClass) != "android-dhcp-14" || model.Deref(fp.Hostname) != "moto-edge-70" {
		t.Fatalf("fingerprint: %+v", fp)
	}
	for name, p := range map[string][]byte{
		"a server's reply":    dhcpPacket(2, 68, phoneMAC, opt(53, 2), opt(55, 1, 3)),
		"an offer":            dhcpPacket(1, 67, phoneMAC, opt(53, 2), opt(55, 1, 3)),
		"another port":        dhcpPacket(1, 68, phoneMAC, opt(53, 1), opt(55, 1, 3)),
		"no fingerprint":      dhcpPacket(1, 67, phoneMAC, opt(53, 1)),
		"cut short":           androidDiscover()[:200],
		"a broken option len": dhcpPacket(1, 67, phoneMAC, opt(53, 1), []byte{55, 200, 1}),
	} {
		if _, _, ok := parseDHCPRequest(p); ok {
			t.Errorf("%s was taken for a request", name)
		}
	}
}

type memFingerprints struct {
	mu    sync.Mutex
	saved map[model.MACAddress]model.DhcpFingerprint
	saves int
}

func (m *memFingerprints) DHCPFingerprints(context.Context) (map[model.MACAddress]model.DhcpFingerprint, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[model.MACAddress]model.DhcpFingerprint{}
	for k, v := range m.saved {
		out[k] = v
	}
	return out, nil
}

func (m *memFingerprints) SaveDHCPFingerprint(_ context.Context, mac model.MACAddress, fp model.DhcpFingerprint, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saved[mac] = fp
	m.saves++
	return nil
}

// The watcher keeps what each device asked for (saved once, not on every
// request), and the scan's host carries it, with the name the device gave.
func TestWatchDHCP(t *testing.T) {
	store := &memFingerprints{saved: map[model.MACAddress]model.DhcpFingerprint{
		"aa:00:00:00:00:02": {VendorClass: model.Ptr("MSFT 5.0")}, // from before a restart
	}}
	heard := make(chan struct{})
	s := &Integration{Fingerprints: store, ListenDHCP: func(ctx context.Context, packet func([]byte)) error {
		packet(androidDiscover())
		packet(androidDiscover()) // the same request again: nothing new to save
		close(heard)
		<-ctx.Done()
		return nil
	}}
	s.watchDHCP(context.Background())
	<-heard

	h := &hostAcc{mac: "da:a1:19:00:00:01"}
	mh := s.toHost(h)
	if mh.Dhcp == nil || model.Deref(mh.Dhcp.VendorClass) != "android-dhcp-14" {
		t.Fatalf("host fingerprint: %+v", mh.Dhcp)
	}
	if len(mh.Hostnames) == 0 || mh.Hostnames[0] != "moto-edge-70" {
		t.Fatalf("hostnames: %v", mh.Hostnames)
	}
	if fp, ok := s.dhcp.fingerprint("aa:00:00:00:00:02"); !ok || model.Deref(fp.VendorClass) != "MSFT 5.0" {
		t.Fatalf("the saved fingerprint was not loaded: %+v", fp)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.saves != 1 {
		t.Fatalf("saved %d times, want 1", store.saves)
	}
}
