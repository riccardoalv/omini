// Package snmp implements the generic SNMP v2c integration, which covers most
// managed switches, routers and firewalls through standard MIBs.
package snmp

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/gosnmp/gosnmp"
)

type client struct {
	g *gosnmp.GoSNMP
}

func dial(ctx context.Context, host string, port int, community string, timeout time.Duration) (*client, error) {
	g := &gosnmp.GoSNMP{
		Target:         host,
		Port:           uint16(port),
		Community:      community,
		Version:        gosnmp.Version2c,
		Timeout:        timeout,
		Retries:        1,
		MaxRepetitions: 25,
		Context:        ctx,
	}
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("connect %s:%d: %w", host, port, err)
	}
	return &client{g: g}, nil
}

func (c *client) close() {
	if c.g.Conn != nil {
		c.g.Conn.Close()
	}
}

// get fetches scalar OIDs. Missing OIDs are simply absent from the result.
func (c *client) get(oids ...string) (map[string]gosnmp.SnmpPDU, error) {
	res, err := c.g.Get(oids)
	if err != nil {
		return nil, err
	}
	out := make(map[string]gosnmp.SnmpPDU, len(res.Variables))
	for _, v := range res.Variables {
		switch v.Type {
		case gosnmp.NoSuchObject, gosnmp.NoSuchInstance, gosnmp.EndOfMibView, gosnmp.Null:
			continue
		}
		out[strings.TrimPrefix(v.Name, ".")] = v
	}
	return out, nil
}

// walk returns every value under base, keyed by the OID suffix after base
// (e.g. walking an ifTable entry yields keys like "2.17" = column 2, ifIndex 17).
// A table the agent does not implement yields an empty map, not an error.
func (c *client) walk(base string) (map[string]gosnmp.SnmpPDU, error) {
	pdus, err := c.g.BulkWalkAll(base)
	if err != nil {
		return nil, err
	}
	prefix := "." + base + "."
	out := make(map[string]gosnmp.SnmpPDU, len(pdus))
	for _, p := range pdus {
		if strings.HasPrefix(p.Name, prefix) {
			out[p.Name[len(prefix):]] = p
		}
	}
	return out, nil
}

// table splits walk results of a table entry into column -> index -> pdu.
func table(rows map[string]gosnmp.SnmpPDU) map[int]map[string]gosnmp.SnmpPDU {
	out := map[int]map[string]gosnmp.SnmpPDU{}
	for suffix, p := range rows {
		col, idx, ok := strings.Cut(suffix, ".")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(col)
		if err != nil {
			continue
		}
		if out[n] == nil {
			out[n] = map[string]gosnmp.SnmpPDU{}
		}
		out[n][idx] = p
	}
	return out
}

func pduBytes(p gosnmp.SnmpPDU) []byte {
	switch v := p.Value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}

// pduString returns a printable string; binary values are hex encoded.
func pduString(p gosnmp.SnmpPDU) string {
	switch v := p.Value.(type) {
	case []byte:
		s := strings.TrimRight(string(v), "\x00")
		if isPrintable(s) {
			return strings.TrimSpace(s)
		}
		return hex.EncodeToString(v)
	case string:
		return v
	case nil:
		return ""
	}
	return fmt.Sprint(p.Value)
}

func pduUint(p gosnmp.SnmpPDU) (uint64, bool) {
	switch p.Value.(type) {
	case []byte, string, nil:
		return 0, false
	}
	b := gosnmp.ToBigInt(p.Value)
	if b == nil || b.Sign() < 0 || b.Cmp(new(big.Int).SetUint64(^uint64(0))) > 0 {
		return 0, false
	}
	return b.Uint64(), true
}

func pduInt(p gosnmp.SnmpPDU) (int64, bool) {
	switch p.Value.(type) {
	case []byte, string, nil:
		return 0, false
	}
	b := gosnmp.ToBigInt(p.Value)
	if b == nil || !b.IsInt64() {
		return 0, false
	}
	return b.Int64(), true
}

func isPrintable(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			if r != '\t' && r != '\n' && r != '\r' {
				return false
			}
		}
	}
	return true
}

// splitOID parses "1.2.3" into integers.
func splitOID(s string) []int {
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}
