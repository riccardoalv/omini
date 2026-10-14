// Command gen downloads DB-IP's free Lite databases (country, and the
// network's owner) into internal/geoip/data, embedded in release builds.
// Databases: DB-IP Lite, CC BY 4.0 (https://db-ip.com).
//
//	go run ./internal/geoip/gen    # make geoip
package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const base = "https://download.db-ip.com/free/"

func main() {
	out := filepath.Join("internal", "geoip", "data")
	for _, kind := range []string{"country", "asn"} {
		if err := fetch(kind, out); err != nil {
			log.Fatal(err)
		}
	}
}

// fetch gets this month's database, or last month's early in the month.
func fetch(kind, out string) error {
	now := time.Now().UTC()
	for _, month := range []time.Time{now, now.AddDate(0, -1, 0)} {
		url := fmt.Sprintf("%sdbip-%s-lite-%s.mmdb.gz", base, kind, month.Format("2006-01"))
		resp, err := http.Get(url) //nolint:gosec,noctx // a fixed host, at build time
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		path := filepath.Join(out, "dbip-"+kind+"-lite.mmdb.gz")
		f, err := os.Create(path) //nolint:gosec // our own folder
		if err != nil {
			_ = resp.Body.Close()
			return err
		}
		n, err := io.Copy(f, resp.Body)
		_ = resp.Body.Close()
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		log.Printf("%s: %d bytes from %s", path, n, url)
		return nil
	}
	return fmt.Errorf("no %s database found at %s", kind, base)
}
