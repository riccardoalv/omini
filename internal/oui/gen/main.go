// Command gen downloads the IEEE MA-L registry and writes the compact,
// gzipped OUI table embedded by package oui. Run with: make oui
package main

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
)

const source = "https://standards-oui.ieee.org/oui/oui.csv"

// Legal suffixes dropped from vendor names ("Apple, Inc." → "Apple").
var suffix = regexp.MustCompile(`(?i)[,.]?\s+(inc|ltd|llc|co|corp|corporation|limited|gmbh|ag|sa|s\.a|bv|b\.v|oy|ab|as|srl|spa|pty|plc|kg|company|technologies|technology|electronics|incorporated)\.?\s*$`)

func clean(name string) string {
	name = strings.Join(strings.Fields(name), " ")
	for i := 0; i < 3; i++ {
		name = strings.TrimRight(suffix.ReplaceAllString(name, ""), " ,.")
	}
	return name
}

func main() {
	out := "internal/oui/oui.tsv.gz"
	if len(os.Args) > 1 {
		out = os.Args[1]
	}
	n, err := run(out)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("wrote %d prefixes to %s", n, out)
}

func run(out string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, source, nil)
	if err != nil {
		return 0, err
	}
	// The IEEE site rejects Go's default User-Agent.
	req.Header.Set("User-Agent", "Mozilla/5.0 (omini oui generator; +https://github.com/riccardoalv/omini)")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("download %s: %s", source, resp.Status)
	}
	r := csv.NewReader(resp.Body)
	r.FieldsPerRecord = -1
	rows := map[string]string{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, err
		}
		if len(rec) < 3 || rec[0] != "MA-L" || len(rec[1]) != 6 {
			continue
		}
		if name := clean(rec[2]); name != "" && !strings.EqualFold(name, "private") {
			rows[strings.ToLower(rec[1])] = name
		}
	}
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	f, err := os.Create(out)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	zw.ModTime = zw.ModTime.UTC() // zero time: reproducible output
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, rows[k])
	}
	if err := zw.Close(); err != nil {
		return 0, err
	}
	return len(keys), nil
}
