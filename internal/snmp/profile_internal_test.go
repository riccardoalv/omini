package snmp

import "testing"

// Matching runs for every profile, device and round: the sys_descr regular
// expression is compiled once, when the profile is read.
func TestSysDescrIsCompiledOnce(t *testing.T) {
	p, err := ParseProfile([]byte("id: x\nmatch:\n  sys_descr: 'routeros|mikrotik'\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !p.matches(nil, "", "RouterOS RB4011") || p.matches(nil, "", "Linux") {
		t.Fatal("sys_descr does not match as it should")
	}
	if n := testing.AllocsPerRun(100, func() { p.matches(nil, "", "RouterOS RB4011") }); n != 0 {
		t.Fatalf("matching allocates %v times: the expression is compiled on every match", n)
	}
}
