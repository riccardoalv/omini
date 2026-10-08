// Package snmp implements the generic SNMP (v2c and v3) integration, which covers most
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

func dial(ctx context.Context, host string, port int, community string, v3 *V3, timeout time.Duration) (*client, error) {
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
	if v3 != nil {
		if err := v3.apply(g); err != nil {
			return nil, err
		}
	}
	if err := g.Connect(); err != nil {
		return nil, fmt.Errorf("connect %s:%d: %w", host, port, err)
	}
	return &client{g: g}, nil
}

// V3 holds SNMP v3 (USM) credentials: a user, optionally authenticated
// (MD5, SHA...) and encrypted (DES, AES...).
type V3 struct {
	User      string
	Auth      string // none | md5 | sha | sha224 | sha256 | sha384 | sha512
	AuthPass  string
	Priv      string // none | des | aes | aes192 | aes256 | aes192c | aes256c
	PrivPass  string
	ContextID string // context name, rarely needed
}

var authProtocols = map[string]gosnmp.SnmpV3AuthProtocol{
	"": gosnmp.NoAuth, "none": gosnmp.NoAuth, "md5": gosnmp.MD5, "sha": gosnmp.SHA,
	"sha224": gosnmp.SHA224, "sha256": gosnmp.SHA256, "sha384": gosnmp.SHA384, "sha512": gosnmp.SHA512,
}

var privProtocols = map[string]gosnmp.SnmpV3PrivProtocol{
	"": gosnmp.NoPriv, "none": gosnmp.NoPriv, "des": gosnmp.DES, "aes": gosnmp.AES,
	"aes192": gosnmp.AES192, "aes256": gosnmp.AES256, "aes192c": gosnmp.AES192C, "aes256c": gosnmp.AES256C,
}

// Check validates the credentials (protocols known, privacy only with authentication).
func (v *V3) Check() error {
	return v.apply(&gosnmp.GoSNMP{})
}

// apply configures a session for v3: the security level follows the
// protocols given (no auth, auth without privacy, auth and privacy).
func (v *V3) apply(g *gosnmp.GoSNMP) error {
	auth, ok := authProtocols[strings.ToLower(v.Auth)]
	if !ok {
		return fmt.Errorf("unknown SNMP v3 authentication %q", v.Auth)
	}
	priv, ok := privProtocols[strings.ToLower(v.Priv)]
	if !ok {
		return fmt.Errorf("unknown SNMP v3 privacy %q", v.Priv)
	}
	if v.User == "" {
		return fmt.Errorf("the SNMP v3 user is required")
	}
	flags := gosnmp.NoAuthNoPriv
	switch {
	case auth != gosnmp.NoAuth && priv != gosnmp.NoPriv:
		flags = gosnmp.AuthPriv
	case auth != gosnmp.NoAuth:
		flags = gosnmp.AuthNoPriv
	case priv != gosnmp.NoPriv:
		return fmt.Errorf("SNMP v3 privacy needs authentication")
	}
	g.Version = gosnmp.Version3
	g.SecurityModel = gosnmp.UserSecurityModel
	g.MsgFlags = flags
	g.ContextName = v.ContextID
	g.SecurityParameters = &gosnmp.UsmSecurityParameters{
		UserName:                 v.User,
		AuthenticationProtocol:   auth,
		AuthenticationPassphrase: v.AuthPass,
		PrivacyProtocol:          priv,
		PrivacyPassphrase:        v.PrivPass,
	}
	return nil
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
