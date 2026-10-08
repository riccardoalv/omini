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

	conn  net.PacketConn
	mu    sync.RWMutex
	views map[string][]variable // community → its variables
	wg    sync.WaitGroup
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
	a := &Agent{Community: community, Port: conn.LocalAddr().(*net.UDPAddr).Port, conn: conn, views: map[string][]variable{}}
	a.AddCommunity(community, pdus)

	a.wg.Add(1)
	go a.serve(t)
	t.Cleanup(func() {
		conn.Close()
		a.wg.Wait()
	})
	return a
}

// AddCommunity answers another community with its own variables (e.g. the
// MAC table of one VLAN, read as "community@vlan").
func (a *Agent) AddCommunity(community string, pdus []gosnmp.SnmpPDU) {
	vars := make([]variable, 0, len(pdus))
	for _, p := range pdus {
		p.Name = "." + strings.TrimPrefix(p.Name, ".")
		vars = append(vars, variable{oid: parseOID(p.Name), pdu: p})
	}
	sort.Slice(vars, func(i, j int) bool { return compareOID(vars[i].oid, vars[j].oid) < 0 })
	a.mu.Lock()
	a.views[community] = vars
	a.mu.Unlock()
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
		if err != nil {
			continue
		}
		a.mu.RLock()
		vars, ok := a.views[req.Community]
		a.mu.RUnlock()
		if !ok {
			continue // like real agents: wrong community means no answer
		}
		resp := &gosnmp.SnmpPacket{
			Version:   gosnmp.Version2c,
			Community: req.Community,
			PDUType:   gosnmp.GetResponse,
			RequestID: req.RequestID,
			Variables: answer(vars, req),
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

func answer(vars []variable, req *gosnmp.SnmpPacket) []gosnmp.SnmpPDU {
	var out []gosnmp.SnmpPDU
	switch req.PDUType {
	case gosnmp.GetRequest:
		for _, v := range req.Variables {
			out = append(out, get(vars, v.Name))
		}
	case gosnmp.GetNextRequest:
		for _, v := range req.Variables {
			out = append(out, next(vars, v.Name))
		}
	case gosnmp.GetBulkRequest:
		nonRep := int(req.NonRepeaters)
		for i, v := range req.Variables {
			if i < nonRep {
				out = append(out, next(vars, v.Name))
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
				p := next(vars, c)
				out = append(out, p)
				cursors[i] = p.Name
			}
		}
	}
	return out
}

func get(vars []variable, name string) gosnmp.SnmpPDU {
	oid := parseOID(name)
	i := sort.Search(len(vars), func(i int) bool { return compareOID(vars[i].oid, oid) >= 0 })
	if i < len(vars) && compareOID(vars[i].oid, oid) == 0 {
		return vars[i].pdu
	}
	return gosnmp.SnmpPDU{Name: name, Type: gosnmp.NoSuchObject}
}

func next(vars []variable, name string) gosnmp.SnmpPDU {
	oid := parseOID(name)
	i := sort.Search(len(vars), func(i int) bool { return compareOID(vars[i].oid, oid) > 0 })
	if i < len(vars) {
		return vars[i].pdu
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
