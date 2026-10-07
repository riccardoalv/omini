// Command gen downloads the device model lists and writes them, compressed,
// next to the models package (run with `make models`):
//   - Apple identifiers ("iPhone14,2" → "iPhone 13 Pro"), from a community
//     list, plus extra.tsv (Apple TV, HomePod, Macs) maintained by hand;
//   - Android models ("SM-S911B" → "Samsung Galaxy S23"), from Google Play's
//     official list of supported devices.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf16"
)

const (
	appleURL  = "https://gist.githubusercontent.com/adamawolf/3048717/raw/Apple_mobile_device_types.txt"
	googleURL = "https://storage.googleapis.com/play_public/supported_devices.csv"
)

func main() {
	out := flag("-out", "internal/models")
	names := map[string]string{}

	apple := fetch(appleURL)
	sc := bufio.NewScanner(bytes.NewReader(apple))
	for sc.Scan() {
		id, name, ok := strings.Cut(sc.Text(), " : ")
		if !ok || strings.Contains(name, "Simulator") {
			continue
		}
		names[strings.TrimSpace(id)] = strings.TrimSpace(name)
	}
	extra, err := os.ReadFile(filepath.Join(out, "extra.tsv"))
	if err != nil {
		log.Fatal(err)
	}
	for _, line := range strings.Split(string(extra), "\n") {
		if id, name, ok := strings.Cut(line, "\t"); ok && !strings.HasPrefix(line, "#") {
			names[id] = name
		}
	}

	// Google's list is UTF-16: Retail Branding, Marketing Name, Device, Model.
	raw := fetch(googleURL)
	u16 := make([]uint16, 0, len(raw)/2)
	for i := 2; i+1 < len(raw); i += 2 { // skip the byte order mark
		u16 = append(u16, uint16(raw[i])|uint16(raw[i+1])<<8)
	}
	r := csv.NewReader(strings.NewReader(string(utf16.Decode(u16))))
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		log.Fatal(err)
	}
	for _, row := range rows[1:] {
		if len(row) < 4 {
			continue
		}
		brand, name, model := strings.TrimSpace(row[0]), strings.TrimSpace(row[1]), strings.TrimSpace(row[3])
		if name == "" || model == "" || strings.EqualFold(name, model) {
			continue
		}
		if brand != "" && !strings.HasPrefix(strings.ToLower(name), strings.ToLower(brand)) {
			name = brand + " " + name
		}
		key := strings.ToUpper(model)
		if _, taken := names[key]; !taken {
			names[key] = name
		}
	}

	keys := make([]string, 0, len(names))
	for k := range names {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	for _, k := range keys {
		fmt.Fprintf(zw, "%s\t%s\n", k, names[k])
	}
	zw.Close()
	if err := os.WriteFile(filepath.Join(out, "models.tsv.gz"), buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	log.Printf("%d models, %d KB", len(keys), buf.Len()/1024)
}

func flag(name, def string) string {
	for i, a := range os.Args {
		if a == name && i+1 < len(os.Args) {
			return os.Args[i+1]
		}
	}
	return def
}

func fetch(url string) []byte {
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "omini-models-generator")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	b, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, resp.Status)
	}
	if err != nil {
		log.Fatal(err)
	}
	return b
}
