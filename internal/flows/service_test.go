package flows

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/store"
)

func freeUDPPort(t *testing.T) int {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).Port
}

func TestServiceReceivesAndStores(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "omini.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := &Service{Store: st}
	in := New(svc)
	nf := freeUDPPort(t)
	cfg := integration.Config{"netflow_port": float64(nf), "sflow_port": float64(0)}
	if devices, err := in.Collect(ctx, cfg); err != nil || len(devices) != 0 {
		t.Fatalf("collect: %v %v", devices, err)
	}

	// A v5 export: phone → server on 8123, and the answer back.
	var b buf
	b.u16(5)
	b.u16(2)
	b.zero(20)
	for _, f := range [][2]string{{"192.168.1.50", "192.168.1.20"}, {"192.168.1.20", "192.168.1.50"}} {
		b.ip(f[0])
		b.ip(f[1])
		b.zero(8)
		b.u32(10)
		if f[0] == "192.168.1.50" {
			b.u32(1000)
		} else {
			b.u32(50000)
		}
		b.zero(8)
		if f[0] == "192.168.1.50" {
			b.u16(51000)
			b.u16(8123)
		} else {
			b.u16(8123)
			b.u16(51000)
		}
		b.zero(2)
		b.u8(6)
		b.zero(9)
	}
	conn, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", itoa(nf)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(b.bytes()); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	deadline := time.Now().Add(3 * time.Second)
	for len(svc.Exporters()) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	ex := svc.Exporters()
	if len(ex) != 1 || ex[0].IP != "127.0.0.1" || ex[0].Flows != 2 || ex[0].Version != "v5" {
		t.Fatalf("exporters: %+v", ex)
	}
	if msg, err := in.Test(ctx, cfg); err != nil || msg != "Listening; receiving from 1 exporter(s)" {
		t.Fatalf("test: %q %v", msg, err)
	}
	svc.Close()

	convs, err := st.Conversations(ctx, time.Now().Add(-time.Hour), "192.168.1.20", 10)
	if err != nil || len(convs) != 1 {
		t.Fatalf("conversations: %+v %v", convs, err)
	}
	c := convs[0]
	if c.A != "192.168.1.20" || c.B != "192.168.1.50" || c.BytesAB != 50000 || c.BytesBA != 1000 ||
		len(c.Ports) != 1 || c.Ports[0].Port != 8123 || c.Ports[0].Proto != 6 {
		t.Fatalf("conversation: %+v", c)
	}
}

func TestPorts(t *testing.T) {
	in := New(&Service{})
	for cfg, ok := range map[*integration.Config]bool{
		{"netflow_port": float64(2055), "sflow_port": float64(6343)}: true,
		{"netflow_port": float64(0), "sflow_port": float64(6343)}:    true,
		{"netflow_port": float64(0), "sflow_port": float64(0)}:       false,
		{"netflow_port": float64(70000)}:                             false,
		{"netflow_port": float64(6343), "sflow_port": float64(6343)}: false,
	} {
		if err := in.Validate(*cfg); (err == nil) != ok {
			t.Errorf("%v: err = %v", *cfg, err)
		}
	}
}

func TestAddKeepsDirectionAndTheServicePort(t *testing.T) {
	svc := &Service{Now: func() time.Time { return time.Unix(1_790_000_000, 0) }}
	a, b := netip.MustParseAddr("10.0.0.9"), netip.MustParseAddr("10.0.0.2")
	svc.Add(a, "sflow", "", []Record{
		{Src: a, Dst: b, Proto: 17, SrcPort: 40000, DstPort: 53, Bytes: 100},
		{Src: b, Dst: a, Proto: 17, SrcPort: 53, DstPort: 40000, Bytes: 300},
	})
	if len(svc.convs) != 1 {
		t.Fatalf("convs: %+v", svc.convs)
	}
	for k, r := range svc.convs {
		if k.port != 53 || r.A != "10.0.0.2" || r.BytesAB != 300 || r.BytesBA != 100 {
			t.Fatalf("row: %+v %+v", k, r)
		}
	}
}

func itoa(n int) string {
	return netip.AddrPortFrom(netip.IPv4Unspecified(), uint16(n)).String()[len("0.0.0.0:"):]
}
