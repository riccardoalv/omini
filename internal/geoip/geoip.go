// Package geoip tells where an internet address is (its country) and who
// runs it (its network's owner: Google, Netflix, Amazon), from DB-IP's free
// Lite databases (CC BY 4.0, https://db-ip.com). Nothing is asked of any
// service: the databases are downloaded at build time (make geoip) and
// embedded; a build without them simply knows no places.
package geoip

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"strings"

	"github.com/oschwald/maxminddb-golang/v2"
)

// Attribution is the credit DB-IP's license asks for, shown where places are.
const Attribution = "IP geolocation by DB-IP (db-ip.com)"

//go:embed all:data
var dataFS embed.FS

const (
	countryFile = "dbip-country-lite.mmdb.gz"
	asnFile     = "dbip-asn-lite.mmdb.gz"
)

// Place is where an address is and who runs it ("" when unknown).
type Place struct {
	Country string `json:"country,omitempty"` // ISO 3166 alpha-2, e.g. "US"
	Org     string `json:"org,omitempty"`     // the network's owner, e.g. "Google LLC"
}

// DB looks addresses up. A nil DB knows no places.
type DB struct {
	country *maxminddb.Reader
	asn     *maxminddb.Reader
}

// Open unpacks the embedded databases into dir (once per version) and opens
// them. Without embedded databases it returns nil and no error.
func Open(dir string) (*DB, error) {
	db := &DB{}
	var err error
	if db.country, err = open(dir, countryFile); err != nil {
		return nil, err
	}
	if db.asn, err = open(dir, asnFile); err != nil {
		return nil, err
	}
	if db.country == nil && db.asn == nil {
		return nil, nil
	}
	return db, nil
}

func open(dir, name string) (*maxminddb.Reader, error) {
	gz, err := fs.ReadFile(dataFS, "data/"+name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(gz)
	path := filepath.Join(dir, strings.TrimSuffix(name, ".gz"))
	stamp := path + ".sha256"
	if have, err := os.ReadFile(stamp); err != nil || string(have) != hex.EncodeToString(sum[:]) {
		if err := unpack(gz, path); err != nil {
			return nil, fmt.Errorf("unpacking %s: %w", name, err)
		}
		if err := os.WriteFile(stamp, []byte(hex.EncodeToString(sum[:])), 0o600); err != nil {
			return nil, err
		}
	}
	return maxminddb.Open(path)
}

func unpack(gz []byte, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	r, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp) //nolint:gosec // a path under the data directory
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil { //nolint:gosec // our own embedded database
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Lookup tells where an address is ("" fields when unknown).
func (db *DB) Lookup(a netip.Addr) Place {
	var p Place
	if db == nil || !a.IsValid() {
		return p
	}
	a = a.Unmap()
	if db.country != nil {
		var rec struct {
			Country struct {
				ISOCode string `maxminddb:"iso_code"`
			} `maxminddb:"country"`
		}
		if err := db.country.Lookup(a).Decode(&rec); err == nil {
			p.Country = rec.Country.ISOCode
		}
	}
	if db.asn != nil {
		var rec struct {
			Org string `maxminddb:"autonomous_system_organization"`
		}
		if err := db.asn.Lookup(a).Decode(&rec); err == nil {
			p.Org = rec.Org
		}
	}
	return p
}

// Enabled reports whether places are known.
func (db *DB) Enabled() bool { return db != nil }

// Close releases the databases.
func (db *DB) Close() {
	if db == nil {
		return
	}
	for _, r := range []*maxminddb.Reader{db.country, db.asn} {
		if r != nil {
			_ = r.Close()
		}
	}
}
