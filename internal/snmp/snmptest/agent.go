// Package snmptest provides an in-process SNMP v2c agent for tests, so the
// SNMP integration can be tested without hardware or external simulators.
package snmptest

import (
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Agent answers Get, GetNext and GetBulk requests from a fixed set of variables.
type Agent struct {
	Community string
	Port      int

	conn net.PacketConn
	vars []variable
	wg   sync.WaitGroup
}

type variable struct {
	oid []int
	pdu gosnmp.SnmpPDU
}

// Start runs an agent on 127.0.0.1 (random UDP port) until the test ends.
func Start(t testing.TB, community string, pdus []gosnmp.SnmpPDU) *Agent {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	a := &Agent{Community: community, Port: conn.LocalAddr().(*net.UDPAddr).Port, conn: conn}
	for _, p := range pdus {
		p.Name = "." + strings.TrimPrefix(p.Name, ".")
		a.vars = append(a.vars, variable{oid: parseOID(p.Name), pdu: p})
	}
	sort.Slice(a.vars, func(i, j int) bool { return compareOID(a.vars[i].oid, a.vars[j].oid) < 0 })

	a.wg.Add(1)
	go a.serve(t)
	t.Cleanup(func() {
		conn.Close()
		a.wg.Wait()
	})
	return a
}

func (a *Agent) serve(t testing.TB) {
	defer a.wg.Done()
	decoder := &gosnmp.GoSNMP{Version: gosnmp.Version2c, Logger: gosnmp.NewLogger(nil)}
	buf := make([]byte, 65535)
	for {
		n, addr, err := a.conn.ReadFrom(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				t.Errorf("snmptest: read: %v", err)
			}
			return
		}
		req, err := decoder.SnmpDecodePacket(buf[:n])
		if err != nil || req.Community != a.Community {
			continue // like real agents: wrong community means no answer
		}
		resp := &gosnmp.SnmpPacket{
			Version:   gosnmp.Version2c,
			Community: req.Community,
			PDUType:   gosnmp.GetResponse,
			RequestID: req.RequestID,
			Variables: a.answer(req),
			Logger:    gosnmp.NewLogger(nil),
		}
		out, err := resp.MarshalMsg()
		if err != nil {
			t.Errorf("snmptest: marshal: %v", err)
			continue
		}
		if _, err := a.conn.WriteTo(out, addr); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Errorf("snmptest: write: %v", err)
		}
	}
}

func (a *Agent) answer(req *gosnmp.SnmpPacket) []gosnmp.SnmpPDU {
	var out []gosnmp.SnmpPDU
	switch req.PDUType {
	case gosnmp.GetRequest:
		for _, v := range req.Variables {
			out = append(out, a.get(v.Name))
		}
	case gosnmp.GetNextRequest:
		for _, v := range req.Variables {
			out = append(out, a.next(v.Name))
		}
	case gosnmp.GetBulkRequest:
		nonRep := int(req.NonRepeaters)
		for i, v := range req.Variables {
			if i < nonRep {
				out = append(out, a.next(v.Name))
			}
		}
		cursors := make([]string, 0, len(req.Variables))
		for i, v := range req.Variables {
			if i >= nonRep {
				cursors = append(cursors, v.Name)
			}
		}
		for r := 0; r < int(req.MaxRepetitions) && len(cursors) > 0; r++ {
			for i, c := range cursors {
				p := a.next(c)
				out = append(out, p)
				cursors[i] = p.Name
			}
		}
	}
	return out
}

func (a *Agent) get(name string) gosnmp.SnmpPDU {
	oid := parseOID(name)
	i := sort.Search(len(a.vars), func(i int) bool { return compareOID(a.vars[i].oid, oid) >= 0 })
	if i < len(a.vars) && compareOID(a.vars[i].oid, oid) == 0 {
		return a.vars[i].pdu
	}
	return gosnmp.SnmpPDU{Name: name, Type: gosnmp.NoSuchObject}
}

func (a *Agent) next(name string) gosnmp.SnmpPDU {
	oid := parseOID(name)
	i := sort.Search(len(a.vars), func(i int) bool { return compareOID(a.vars[i].oid, oid) > 0 })
	if i < len(a.vars) {
		return a.vars[i].pdu
	}
	return gosnmp.SnmpPDU{Name: name, Type: gosnmp.EndOfMibView}
}

func parseOID(s string) []int {
	parts := strings.Split(strings.TrimPrefix(s, "."), ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return out
		}
		out = append(out, n)
	}
	return out
}

func compareOID(a, b []int) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return len(a) - len(b)
}
