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
	// Checked on the profile, not by counting allocations: with -race,
	// sync.Pool (which regexp uses) drops items on purpose, so matching
	// allocates now and then anyway.
	if p.sysDescr == nil {
		t.Fatal("the expression is not compiled when the profile is read")
	}
}
