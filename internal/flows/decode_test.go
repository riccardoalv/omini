package flows

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"testing"
)

type buf struct{ bytes.Buffer }

func (b *buf) u8(v uint8)    { b.WriteByte(v) }
func (b *buf) u16(v uint16)  { _ = binary.Write(b, binary.BigEndian, v) }
func (b *buf) u32(v uint32)  { _ = binary.Write(b, binary.BigEndian, v) }
func (b *buf) ip(s string)   { a := netip.MustParseAddr(s).As4(); b.Write(a[:]) }
func (b *buf) zero(n int)    { b.Write(make([]byte, n)) }
func (b *buf) bytes() []byte { return b.Bytes() }

var exporter = netip.MustParseAddr("192.168.1.1")

func TestNetFlowV5(t *testing.T) {
	var b buf
	b.u16(5)
	b.u16(2) // count
	b.zero(18)
	b.u16(0x4000 | 10) // sampled 1 in 10
	for i, flow := range []struct {
		src, dst  string
		pkts, oct uint32
		sp, dp    uint16
		proto     uint8
	}{
		{"192.168.1.10", "142.250.0.1", 3, 1500, 51000, 443, 6},
		{"192.168.1.11", "1.1.1.1", 1, 80, 40000, 53, 17},
	} {
		_ = i
		b.ip(flow.src)
		b.ip(flow.dst)
		b.zero(8) // next hop, input, output
		b.u32(flow.pkts)
		b.u32(flow.oct)
		b.zero(8) // first, last
		b.u16(flow.sp)
		b.u16(flow.dp)
		b.u8(0)
		b.u8(0)
		b.u8(flow.proto)
		b.zero(9)
	}
	got, err := DecodeNetFlow(b.bytes(), exporter, NewTemplates())
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %+v", err, got)
	}
	want := Record{Src: netip.MustParseAddr("192.168.1.10"), Dst: netip.MustParseAddr("142.250.0.1"), Proto: 6, SrcPort: 51000, DstPort: 443, Bytes: 15000, Packets: 30}
	if got[0] != want || got[1].DstPort != 53 || got[1].Proto != 17 {
		t.Fatalf("records: %+v", got)
	}
	if _, err := DecodeNetFlow(b.bytes()[:40], exporter, NewTemplates()); err == nil {
		t.Fatal("truncated packet accepted")
	}
}

// v9 and IPFIX: a template, then data that uses it.
func templated(version uint16, withTemplate bool) []byte {
	var sets buf
	if withTemplate {
		var tpl buf
		tpl.u16(256)
		tpl.u16(7)
		for _, f := range [][2]uint16{{8, 4}, {12, 4}, {7, 2}, {11, 2}, {4, 1}, {1, 4}, {2, 4}} {
			tpl.u16(f[0])
			tpl.u16(f[1])
		}
		setID := uint16(0)
		if version == 10 {
			setID = 2
		}
		sets.u16(setID)
		sets.u16(uint16(4 + tpl.Len()))
		sets.Write(tpl.Bytes())
	}
	var data buf
	data.ip("10.0.20.5")
	data.ip("192.168.1.50")
	data.u16(5353)
	data.u16(8123)
	data.u8(6)
	data.u32(4096)
	data.u32(4)
	data.zero(3) // padding
	sets.u16(256)
	sets.u16(uint16(4 + data.Len()))
	sets.Write(data.Bytes())

	var b buf
	b.u16(version)
	if version == 9 {
		b.u16(2) // count
		b.zero(8)
		b.u32(1) // sequence
		b.u32(7) // source id
	} else {
		b.u16(uint16(16 + sets.Len()))
		b.zero(8)
		b.u32(7) // observation domain
	}
	b.Write(sets.Bytes())
	return b.bytes()
}

func TestNetFlowV9AndIPFIX(t *testing.T) {
	for _, v := range []uint16{9, 10} {
		tpl := NewTemplates()
		// Data before its template: skipped, no error.
		if got, err := DecodeNetFlow(templated(v, false), exporter, tpl); err != nil || len(got) != 0 {
			t.Fatalf("v%d without template: %v %+v", v, err, got)
		}
		got, err := DecodeNetFlow(templated(v, true), exporter, tpl)
		if err != nil || len(got) != 1 {
			t.Fatalf("v%d: %v %+v", v, err, got)
		}
		want := Record{Src: netip.MustParseAddr("10.0.20.5"), Dst: netip.MustParseAddr("192.168.1.50"), Proto: 6, SrcPort: 5353, DstPort: 8123, Bytes: 4096, Packets: 4}
		if got[0] != want {
			t.Fatalf("v%d: %+v", v, got[0])
		}
		// Later packets carry only data.
		if got, _ := DecodeNetFlow(templated(v, false), exporter, tpl); len(got) != 1 {
			t.Fatalf("v%d: template not remembered", v)
		}
	}
	if _, err := DecodeNetFlow([]byte{0, 7, 0, 0}, exporter, NewTemplates()); err == nil {
		t.Fatal("version 7 accepted")
	}
}

func TestSFlow(t *testing.T) {
	// A sampled TCP packet in a VLAN, 1 in 400.
	var frame buf
	frame.zero(12)
	frame.u16(0x8100)
	frame.u16(20)
	frame.u16(0x0800)
	frame.u8(0x45)
	frame.zero(8)
	frame.u8(6) // TCP
	frame.zero(2)
	frame.ip("192.168.1.20")
	frame.ip("192.168.1.5")
	frame.u16(445)
	frame.u16(50123)
	frame.zero(2) // header length 44: padded

	var rec buf
	rec.u32(1) // ethernet
	rec.u32(1500)
	rec.u32(4)
	rec.u32(uint32(frame.Len()))
	rec.Write(frame.Bytes())
	for rec.Len()%4 != 0 {
		rec.u8(0)
	}

	var sample buf
	sample.u32(1)   // sequence
	sample.u32(3)   // source id
	sample.u32(400) // sampling rate
	sample.zero(8)
	sample.zero(8) // input, output
	sample.u32(2)  // records: one skipped, one header
	sample.u32(1001)
	sample.u32(4)
	sample.u32(0)
	sample.u32(1)
	sample.u32(uint32(rec.Len()))
	sample.Write(rec.Bytes())

	var b buf
	b.u32(5)
	b.u32(1)
	b.ip("192.168.1.96")
	b.zero(12)
	b.u32(2) // samples: a counter sample (skipped) and a flow sample
	b.u32(2)
	b.u32(4)
	b.zero(4)
	b.u32(1)
	b.u32(uint32(sample.Len()))
	b.Write(sample.Bytes())

	got, err := DecodeSFlow(b.bytes())
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %+v", err, got)
	}
	want := Record{Src: netip.MustParseAddr("192.168.1.20"), Dst: netip.MustParseAddr("192.168.1.5"), Proto: 6, SrcPort: 445, DstPort: 50123, Bytes: 600000, Packets: 400}
	if got[0] != want {
		t.Fatalf("record: %+v", got[0])
	}
	if _, err := DecodeSFlow([]byte{0, 0, 0, 4}); err == nil {
		t.Fatal("sFlow v4 accepted")
	}
	if _, err := DecodeSFlow(b.bytes()[:60]); err == nil {
		t.Fatal("truncated datagram accepted")
	}
}
