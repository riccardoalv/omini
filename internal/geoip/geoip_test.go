package geoip

import (
	"io/fs"
	"net/netip"
	"testing"
)

func TestLookup(t *testing.T) {
	if _, err := fs.Stat(dataFS, "data/"+countryFile); err != nil {
		t.Skip("no embedded databases (run make geoip)")
	}
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil || db == nil {
		t.Fatalf("open: %v %v", db, err)
	}
	defer db.Close()
	p := db.Lookup(netip.MustParseAddr("8.8.8.8"))
	if p.Country != "US" || p.Org == "" {
		t.Fatalf("8.8.8.8 = %+v", p)
	}
	if p := db.Lookup(netip.MustParseAddr("192.168.1.10")); p.Country != "" {
		t.Errorf("a private address has a place: %+v", p)
	}
	// Opening again reuses what was unpacked.
	again, err := Open(dir)
	if err != nil || again.Lookup(netip.MustParseAddr("8.8.8.8")).Country != "US" {
		t.Fatalf("reopen: %v", err)
	}
	again.Close()
}

func TestNoDatabaseKnowsNoPlaces(t *testing.T) {
	var db *DB
	if db.Enabled() || db.Lookup(netip.MustParseAddr("8.8.8.8")) != (Place{}) {
		t.Fatal("a nil database knows places")
	}
}
