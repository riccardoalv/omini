package netscan

import (
	"context"
	"encoding/binary"
	"net"
	"net/netip"
	"testing"
	"time"
)

func TestScanPorts(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	open := ln.Addr().(*net.TCPAddr).Port

	closedLn, _ := net.Listen("tcp4", "127.0.0.1:0")
	closed := closedLn.Addr().(*net.TCPAddr).Port
	closedLn.Close()

	ip := netip.MustParseAddr("127.0.0.1")
	got := scanPorts(context.Background(), ip, []int{open, closed}, time.Second)
	if len(got) != 1 || got[0] != open {
		t.Fatalf("open ports = %v, want [%d]", got, open)
	}
	if dialPort(context.Background(), ip, closed, time.Second) != portClosed {
		t.Error("a refused connection means the host is up (closed port)")
	}
	if !tcpAlive(context.Background(), ip, []int{closed}, time.Second) {
		t.Error("host with only refused ports should be alive")
	}
}

// nbstatResponse builds a NetBIOS node status answer with the given names.
func nbstatResponse(names ...struct {
	name   string
	suffix byte
	group  bool
},
) []byte {
	b := make([]byte, 56)
	b = append(b, byte(len(names)))
	for _, n := range names {
		entry := make([]byte, 18)
		copy(entry, []byte(n.name + "               ")[:15])
		entry[15] = n.suffix
		if n.group {
			binary.BigEndian.PutUint16(entry[16:], 0x8000)
		}
		b = append(b, entry...)
	}
	return b
}

func TestParseNBSTAT(t *testing.T) {
	type name = struct {
		name   string
		suffix byte
		group  bool
	}
	resp := nbstatResponse(name{"WORKGROUP", 0x00, true}, name{"DESKTOP-4F2K", 0x00, false}, name{"DESKTOP-4F2K", 0x20, false})
	if got := parseNBSTAT(resp); got != "DESKTOP-4F2K" {
		t.Fatalf("name = %q", got)
	}
	if got := parseNBSTAT([]byte{1, 2, 3}); got != "" {
		t.Fatalf("short packet should give no name, got %q", got)
	}
}

func TestReverseDNSOfLoopback(t *testing.T) {
	// The test machine always resolves 127.0.0.1; this checks the trailing dot is removed.
	for _, n := range reverseDNS(context.Background(), netip.MustParseAddr("127.0.0.1"), netip.Addr{}, 2*time.Second) {
		if n == "" || n[len(n)-1] == '.' {
			t.Errorf("bad name %q", n)
		}
	}
}
