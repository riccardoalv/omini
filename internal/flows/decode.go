// Package flows receives flow exports — NetFlow v5 and v9, IPFIX and sFlow v5
// — from routers, firewalls and switches, and sums them into conversations:
// who talks to whom, how much, on which ports. Omini only listens: it never
// configures the exporters.
package flows

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"
)

// Record is one flow as an exporter reported it (bytes and packets already
// scaled by the sampling rate when the exporter samples).
type Record struct {
	Src, Dst         netip.Addr
	Proto            uint8 // 6 TCP, 17 UDP, 1 ICMP...
	SrcPort, DstPort uint16
	Bytes, Packets   uint64
}

var errShort = errors.New("truncated packet")

// Templates remembers NetFlow v9 / IPFIX templates per exporter and
// observation domain (they are sent every few minutes, data references them).
type Templates struct {
	mu   sync.Mutex
	byID map[templateKey]template
}

type templateKey struct {
	exporter netip.Addr
	domain   uint32
	id       uint16
}

type field struct {
	typ, length uint16
	enterprise  bool
}

type template struct {
	fields []field
	at     time.Time
}

// NewTemplates returns an empty template cache.
func NewTemplates() *Templates { return &Templates{byID: map[templateKey]template{}} }

func (t *Templates) set(k templateKey, fields []field) {
	t.mu.Lock()
	t.byID[k] = template{fields: fields, at: time.Now()}
	t.mu.Unlock()
}

func (t *Templates) get(k templateKey) ([]field, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	tpl, ok := t.byID[k]
	return tpl.fields, ok
}

// DecodeNetFlow decodes a NetFlow v5, v9 or IPFIX (v10) packet. Data whose
// template has not been received yet is skipped (it is sent again soon).
func DecodeNetFlow(b []byte, exporter netip.Addr, templates *Templates) ([]Record, error) {
	if len(b) < 2 {
		return nil, errShort
	}
	switch v := binary.BigEndian.Uint16(b); v {
	case 5:
		return decodeV5(b)
	case 9:
		return decodeV9(b, exporter, templates)
	case 10:
		return decodeIPFIX(b, exporter, templates)
	default:
		return nil, fmt.Errorf("unsupported NetFlow version %d", v)
	}
}

func decodeV5(b []byte) ([]Record, error) {
	const header, size = 24, 48
	if len(b) < header {
		return nil, errShort
	}
	count := int(binary.BigEndian.Uint16(b[2:]))
	// Sampling: 2 bits of mode, 14 of interval (0 = not sampled).
	rate := uint64(binary.BigEndian.Uint16(b[22:]) & 0x3fff)
	if rate == 0 {
		rate = 1
	}
	if len(b) < header+count*size {
		return nil, errShort
	}
	out := make([]Record, 0, count)
	for i := range count {
		r := b[header+i*size:]
		out = append(out, Record{
			Src:     netip.AddrFrom4([4]byte(r[0:4])),
			Dst:     netip.AddrFrom4([4]byte(r[4:8])),
			Packets: uint64(binary.BigEndian.Uint32(r[16:])) * rate,
			Bytes:   uint64(binary.BigEndian.Uint32(r[20:])) * rate,
			SrcPort: binary.BigEndian.Uint16(r[32:]),
			DstPort: binary.BigEndian.Uint16(r[34:]),
			Proto:   r[38],
		})
	}
	return out, nil
}

func decodeV9(b []byte, exporter netip.Addr, templates *Templates) ([]Record, error) {
	const header = 20
	if len(b) < header {
		return nil, errShort
	}
	domain := binary.BigEndian.Uint32(b[16:]) // source id
	return decodeSets(b[header:], exporter, domain, templates, 0, 1)
}

func decodeIPFIX(b []byte, exporter netip.Addr, templates *Templates) ([]Record, error) {
	const header = 16
	if len(b) < header {
		return nil, errShort
	}
	length := int(binary.BigEndian.Uint16(b[2:]))
	if length < header || length > len(b) {
		return nil, errShort
	}
	domain := binary.BigEndian.Uint32(b[12:])
	return decodeSets(b[header:length], exporter, domain, templates, 2, 3)
}

// decodeSets walks the flowsets (v9) / sets (IPFIX): template sets define
// records, data sets (id ≥ 256) use them. Options templates are skipped.
func decodeSets(b []byte, exporter netip.Addr, domain uint32, templates *Templates, tplSet, optSet uint16) ([]Record, error) {
	var out []Record
	for len(b) >= 4 {
		id := binary.BigEndian.Uint16(b)
		length := int(binary.BigEndian.Uint16(b[2:]))
		if length < 4 || length > len(b) {
			return out, errShort
		}
		body := b[4:length]
		b = b[length:]
		switch {
		case id == tplSet:
			for len(body) >= 4 {
				tid := binary.BigEndian.Uint16(body)
				count := int(binary.BigEndian.Uint16(body[2:]))
				body = body[4:]
				fields := make([]field, 0, count)
				for range count {
					if len(body) < 4 {
						return out, errShort
					}
					f := field{typ: binary.BigEndian.Uint16(body), length: binary.BigEndian.Uint16(body[2:])}
					body = body[4:]
					if tplSet == 2 && f.typ&0x8000 != 0 { // IPFIX enterprise field: 4 more bytes
						if len(body) < 4 {
							return out, errShort
						}
						f.enterprise, f.typ = true, f.typ&0x7fff
						body = body[4:]
					}
					fields = append(fields, f)
				}
				templates.set(templateKey{exporter, domain, tid}, fields)
			}
		case id == optSet || id < 256:
			// options templates and reserved sets: not needed
		default:
			fields, ok := templates.get(templateKey{exporter, domain, id})
			if !ok {
				continue
			}
			out = append(out, dataRecords(body, fields)...)
		}
	}
	return out, nil
}

// Information elements read (IANA IPFIX numbers, the same as NetFlow v9's).
const (
	ieOctetDelta  = 1
	iePacketDelta = 2
	ieProtocol    = 4
	ieSrcPort     = 7
	ieSrcIPv4     = 8
	ieDstPort     = 11
	ieDstIPv4     = 12
	ieOutBytes    = 23
	ieOutPkts     = 24
	ieSrcIPv6     = 27
	ieDstIPv6     = 28
	ieSampling    = 34
	ieOctetTotal  = 85
	iePacketTotal = 86
)

func dataRecords(body []byte, fields []field) []Record {
	size := 0
	for _, f := range fields {
		if f.length == 0xffff { // variable length: not used by the fields we read
			return nil
		}
		size += int(f.length)
	}
	if size == 0 {
		return nil
	}
	var out []Record
	for len(body) >= size {
		rec := body[:size]
		body = body[size:]
		var (
			r    Record
			rate uint64 = 1
			off  int
		)
		for _, f := range fields {
			v := rec[off : off+int(f.length)]
			off += int(f.length)
			if f.enterprise {
				continue
			}
			switch f.typ {
			case ieSrcIPv4:
				if len(v) == 4 {
					r.Src = netip.AddrFrom4([4]byte(v))
				}
			case ieDstIPv4:
				if len(v) == 4 {
					r.Dst = netip.AddrFrom4([4]byte(v))
				}
			case ieSrcIPv6:
				if len(v) == 16 {
					r.Src = netip.AddrFrom16([16]byte(v))
				}
			case ieDstIPv6:
				if len(v) == 16 {
					r.Dst = netip.AddrFrom16([16]byte(v))
				}
			case ieProtocol:
				r.Proto = byte(uintOf(v))
			case ieSrcPort:
				r.SrcPort = uint16(uintOf(v))
			case ieDstPort:
				r.DstPort = uint16(uintOf(v))
			case ieOctetDelta, ieOctetTotal, ieOutBytes:
				if r.Bytes == 0 {
					r.Bytes = uintOf(v)
				}
			case iePacketDelta, iePacketTotal, ieOutPkts:
				if r.Packets == 0 {
					r.Packets = uintOf(v)
				}
			case ieSampling:
				if s := uintOf(v); s > 0 {
					rate = s
				}
			}
		}
		if !r.Src.IsValid() || !r.Dst.IsValid() {
			continue
		}
		r.Bytes *= rate
		r.Packets *= rate
		out = append(out, r)
	}
	return out
}

func uintOf(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = v<<8 | uint64(x)
	}
	return v
}

// DecodeSFlow decodes an sFlow v5 datagram: flow samples with a raw packet
// header (Ethernet, 802.1Q, IPv4/IPv6, TCP/UDP), scaled by their sampling rate.
// Counter samples are skipped (Omini reads interface counters elsewhere).
func DecodeSFlow(b []byte) ([]Record, error) {
	r := reader{b: b}
	if r.u32() != 5 {
		return nil, errors.New("not an sFlow v5 datagram")
	}
	switch r.u32() { // agent address
	case 1:
		r.skip(4)
	case 2:
		r.skip(16)
	default:
		return nil, errors.New("bad sFlow agent address")
	}
	r.skip(12) // sub-agent id, sequence, uptime
	samples := r.u32()
	var out []Record
	for i := uint32(0); i < samples && r.err == nil; i++ {
		tag, length := r.u32(), int(r.u32())
		body := reader{b: r.take(length)}
		switch tag {
		case 1, 3: // flow sample, expanded flow sample
			out = append(out, flowSample(&body, tag == 3)...)
		}
	}
	if r.err != nil {
		return out, r.err
	}
	return out, nil
}

func flowSample(r *reader, expanded bool) []Record {
	r.skip(4) // sequence
	if expanded {
		r.skip(8) // source id type + index
	} else {
		r.skip(4)
	}
	rate := uint64(r.u32())
	if rate == 0 {
		rate = 1
	}
	r.skip(8) // sample pool, drops
	if expanded {
		r.skip(16) // input and output: format + value
	} else {
		r.skip(8)
	}
	records := r.u32()
	var out []Record
	for i := uint32(0); i < records && r.err == nil; i++ {
		tag, length := r.u32(), int(r.u32())
		body := reader{b: r.take(length)}
		if tag != 1 { // raw packet header
			continue
		}
		proto := body.u32()
		frame := uint64(body.u32())
		body.skip(4) // stripped
		hlen := int(body.u32())
		header := body.take(hlen)
		if proto != 1 || body.err != nil { // Ethernet only
			continue
		}
		if rec, ok := parseEthernet(header); ok {
			rec.Bytes, rec.Packets = frame*rate, rate
			out = append(out, rec)
		}
	}
	return out
}

func parseEthernet(b []byte) (Record, bool) {
	if len(b) < 14 {
		return Record{}, false
	}
	etherType := binary.BigEndian.Uint16(b[12:])
	b = b[14:]
	for etherType == 0x8100 || etherType == 0x88a8 { // VLAN tags
		if len(b) < 4 {
			return Record{}, false
		}
		etherType = binary.BigEndian.Uint16(b[2:])
		b = b[4:]
	}
	var (
		r       Record
		payload []byte
	)
	switch etherType {
	case 0x0800:
		if len(b) < 20 {
			return Record{}, false
		}
		ihl := int(b[0]&0x0f) * 4
		if ihl < 20 || len(b) < ihl {
			return Record{}, false
		}
		r.Proto = b[9]
		r.Src, r.Dst = netip.AddrFrom4([4]byte(b[12:16])), netip.AddrFrom4([4]byte(b[16:20]))
		payload = b[ihl:]
	case 0x86dd:
		if len(b) < 40 {
			return Record{}, false
		}
		r.Proto = b[6]
		r.Src, r.Dst = netip.AddrFrom16([16]byte(b[8:24])), netip.AddrFrom16([16]byte(b[24:40]))
		payload = b[40:]
	default:
		return Record{}, false
	}
	if (r.Proto == 6 || r.Proto == 17) && len(payload) >= 4 {
		r.SrcPort, r.DstPort = binary.BigEndian.Uint16(payload), binary.BigEndian.Uint16(payload[2:])
	}
	return r, true
}

// reader reads big-endian XDR fields, remembering the first error.
type reader struct {
	b   []byte
	err error
}

func (r *reader) take(n int) []byte {
	if r.err != nil || n < 0 || n > len(r.b) {
		r.err = errShort
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	// XDR opaque data is padded to 4 bytes.
	if pad := (4 - n%4) % 4; pad > 0 && pad <= len(r.b) {
		r.b = r.b[pad:]
	}
	return out
}

func (r *reader) skip(n int) { r.take(n) }

func (r *reader) u32() uint32 {
	b := r.take(4)
	if len(b) < 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
