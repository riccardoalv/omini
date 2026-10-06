package model

import (
	"strings"
)

// NormMAC normalizes any common MAC notation (aa-bb-cc-dd-ee-ff, aabb.ccdd.eeff,
// AABBCCDDEEFF, raw 6 bytes) to aa:bb:cc:dd:ee:ff. It returns "" if s is not a MAC.
func NormMAC(s string) MACAddress {
	if len(s) == 6 {
		// Raw bytes, as returned by SNMP OCTET STRINGs.
		return MACFromBytes([]byte(s))
	}
	var hex strings.Builder
	for _, c := range strings.ToLower(s) {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') {
			hex.WriteRune(c)
		}
	}
	h := hex.String()
	if len(h) != 12 {
		return ""
	}
	return MACAddress(h[0:2] + ":" + h[2:4] + ":" + h[4:6] + ":" + h[6:8] + ":" + h[8:10] + ":" + h[10:12])
}

// MACFromBytes formats 6 raw bytes as a MAC. It returns "" for other lengths
// and for the all-zero MAC.
func MACFromBytes(b []byte) MACAddress {
	if len(b) != 6 {
		return ""
	}
	const digits = "0123456789abcdef"
	out := make([]byte, 0, 17)
	zero := true
	for i, x := range b {
		if i > 0 {
			out = append(out, ':')
		}
		if x != 0 {
			zero = false
		}
		out = append(out, digits[x>>4], digits[x&0x0f])
	}
	if zero {
		return ""
	}
	return MACAddress(out)
}

// IsRandomized reports whether the MAC has the locally administered bit set,
// which is what phones use for private/randomized addresses.
func (m MACAddress) IsRandomized() bool {
	if len(m) < 2 {
		return false
	}
	second := m[1]
	return second == '2' || second == '6' || second == 'a' || second == 'e'
}

// Ptr returns a pointer to v. Handy for the optional fields of generated types.
func Ptr[T any](v T) *T { return &v }

// Deref returns *p, or the zero value when p is nil.
func Deref[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}
