// Command gen refreshes the app icon catalog (committed) and, with -bundle,
// downloads the SVG icons into the bundle embedded in release builds.
// Icons: Dashboard Icons by homarr-labs, Apache-2.0.
//
//	go run ./internal/appicons/gen            # catalog only
//	go run ./internal/appicons/gen -bundle    # catalog + icons (make icons)
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"
)

const (
	cdn      = "https://cdn.jsdelivr.net/gh/homarr-labs/dashboard-icons"
	maxBytes = 32 << 10 // larger icons (usually embedded bitmaps) are fetched on demand
)

type entry struct {
	Base       string   `json:"base"`
	Aliases    []string `json:"aliases"`
	Categories []string `json:"categories"`
}

// CatalogEntry is what Omini keeps per icon.
type CatalogEntry struct {
	Name       string   `json:"n"`
	Aliases    []string `json:"a,omitempty"`
	Categories []string `json:"c,omitempty"`
}

func main() {
	bundle := flag.Bool("bundle", false, "also download the SVG icons into the embedded bundle")
	flag.Parse()
	if err := run(*bundle); err != nil {
		log.Fatal(err)
	}
}

func get(url string) ([]byte, error) {
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func run(bundle bool) error {
	raw, err := get(cdn + "/metadata.json")
	if err != nil {
		return err
	}
	var meta map[string]entry
	if err := json.Unmarshal(raw, &meta); err != nil {
		return err
	}
	var catalog []CatalogEntry
	for name, e := range meta {
		if e.Base != "svg" {
			continue // only vector icons
		}
		catalog = append(catalog, CatalogEntry{Name: name, Aliases: e.Aliases, Categories: e.Categories})
	}
	sort.Slice(catalog, func(i, j int) bool { return catalog[i].Name < catalog[j].Name })
	if err := writeGzip("internal/appicons/catalog.json.gz", catalog); err != nil {
		return err
	}
	log.Printf("catalog: %d icons", len(catalog))
	if !bundle {
		return nil
	}

	type icon struct {
		name string
		data []byte
	}
	var (
		mu    sync.Mutex
		icons []icon
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 16)
	)
	for _, c := range catalog {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			data, err := get(cdn + "/svg/" + c.Name + ".svg")
			if err != nil || len(data) > maxBytes {
				return
			}
			mu.Lock()
			icons = append(icons, icon{c.Name, data})
			mu.Unlock()
		}()
	}
	wg.Wait()
	sort.Slice(icons, func(i, j int) bool { return icons[i].name < icons[j].name })

	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	tw := tar.NewWriter(zw)
	for _, ic := range icons {
		hdr := &tar.Header{Name: ic.name + ".svg", Mode: 0o644, Size: int64(len(ic.data))}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if _, err := tw.Write(ic.data); err != nil {
			return err
		}
	}
	if err := tw.Close(); err != nil {
		return err
	}
	if err := zw.Close(); err != nil {
		return err
	}
	if err := os.WriteFile("internal/appicons/bundle/icons.tar.gz", buf.Bytes(), 0o644); err != nil {
		return err
	}
	log.Printf("bundle: %d icons, %.1f MB", len(icons), float64(buf.Len())/1e6)
	return nil
}

func writeGzip(path string, v any) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	zw, _ := gzip.NewWriterLevel(f, gzip.BestCompression)
	enc := json.NewEncoder(zw)
	if err := enc.Encode(v); err != nil {
		return err
	}
	return zw.Close()
}
