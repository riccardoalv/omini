// Package snmptest provides an in-process SNMP v2c agent for tests, so the
// SNMP integration can be tested without hardware or external simulators.
package snmptest

import (
	"errors"
	"net"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gosnmp/gosnmp"
)

// Agent answers Get, GetNext and GetBulk requests from a fixed set of variables,
// and Set requests with the write community (see AllowWrite).
type Agent struct {
	Community string
	Port      int
	write     string   // the community allowed to Set (empty: none)
	ignored   []string // OID prefixes whose Sets are answered but not applied
	drop      int      // requests left to drop (negative: all)

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

// AllowWrite lets a community change the agent's existing variables (Set);
// it reads the same variables as the agent's community. Set with another
// community answers noAccess, like a read-only community.
func (a *Agent) AllowWrite(community string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.write = community
	a.views[community] = a.views[a.Community]
}

// Drop leaves the next n requests unanswered, like packets lost on the way;
// a negative n drops every request until Drop(0).
func (a *Agent) Drop(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.drop = n
}

// IgnoreWrites makes Sets under an OID prefix answer noError without
// changing anything, like agents that accept a write and drop it.
func (a *Agent) IgnoreWrites(prefix string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.ignored = append(a.ignored, "."+strings.Trim(prefix, ".")+".")
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
		a.mu.Lock()
		dropped := a.drop != 0
		if a.drop > 0 {
			a.drop--
		}
		a.mu.Unlock()
		if dropped {
			continue // a lost packet
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
			Logger:    gosnmp.NewLogger(nil),
		}
		if req.PDUType == gosnmp.SetRequest {
			resp.Variables = req.Variables
			resp.Error, resp.ErrorIndex = a.set(req)
		} else {
			resp.Variables = answer(vars, req)
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

// vlanRowStatus is Q-BRIDGE-MIB dot1qVlanStaticRowStatus: setting it creates
// (createAndGo 4, createAndWait 5) or destroys (6) a VLAN's row.
const vlanRowStatus = ".1.3.6.1.2.1.17.7.1.4.3.1.5."

// set changes existing variables, with the write community only; a VLAN's
// row status also creates or destroys the row.
func (a *Agent) set(req *gosnmp.SnmpPacket) (gosnmp.SNMPError, uint8) {
	if a.write == "" || req.Community != a.write {
		return gosnmp.NoAccess, 1
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	vars := a.views[a.Community]
	find := func(name string) int {
		k := sort.Search(len(vars), func(j int) bool { return compareOID(vars[j].oid, parseOID(name)) >= 0 })
		if k == len(vars) || vars[k].pdu.Name != name {
			return -1
		}
		return k
	}
	put := func(name string, typ gosnmp.Asn1BER, value any) {
		if b, ok := value.([]byte); ok {
			value = slices.Clone(b) // the request's buffer is reused for the next one
		}
		if k := find(name); k >= 0 {
			vars[k].pdu.Type, vars[k].pdu.Value = typ, value
			return
		}
		vars = append(vars, variable{oid: parseOID(name), pdu: gosnmp.SnmpPDU{Name: name, Type: typ, Value: value}})
		sort.Slice(vars, func(i, j int) bool { return compareOID(vars[i].oid, vars[j].oid) < 0 })
	}
	for i, v := range req.Variables {
		name := "." + strings.TrimPrefix(v.Name, ".")
		if slices.ContainsFunc(a.ignored, func(p string) bool { return strings.HasPrefix(name, p) }) {
			continue
		}
		if vid, ok := strings.CutPrefix(name, vlanRowStatus); ok {
			status, _ := v.Value.(int)
			base := strings.TrimSuffix(vlanRowStatus, "5.")
			switch status {
			case 4, 5:
				for _, col := range []string{"1.", "2.", "4."} {
					if find(base+col+vid) < 0 {
						put(base+col+vid, gosnmp.OctetString, []byte{})
					}
				}
				put(name, gosnmp.Integer, map[int]int{4: 1, 5: 3}[status])
			case 6:
				vars = slices.DeleteFunc(vars, func(x variable) bool {
					return strings.HasPrefix(x.pdu.Name, base) && strings.HasSuffix(x.pdu.Name, "."+vid)
				})
			default:
				put(name, gosnmp.Integer, status)
			}
			continue
		}
		if find(name) < 0 {
			return gosnmp.NotWritable, uint8(i + 1) //nolint:gosec // a handful of variables
		}
		put(name, v.Type, v.Value)
	}
	a.views[a.Community] = vars
	a.views[a.write] = vars
	return gosnmp.NoError, 0
}
