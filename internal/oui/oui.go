// Package oui maps MAC address prefixes to vendor names, using the IEEE
// registry embedded at build time (regenerate with `make oui`).
package oui

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
)

//go:embed oui.tsv.gz
var data []byte

var (
	once    sync.Once
	vendors map[string]string
)

func load() {
	vendors = make(map[string]string, 40000)
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	s := bufio.NewScanner(zr)
	for s.Scan() {
		prefix, name, ok := strings.Cut(s.Text(), "\t")
		if ok {
			vendors[prefix] = name
		}
	}
}

// Lookup returns the vendor of a MAC (aa:bb:cc:dd:ee:ff), or "" when unknown.
// Randomized (locally administered) MACs have no vendor.
func Lookup(mac string) string {
	hex := strings.ToLower(strings.ReplaceAll(mac, ":", ""))
	if len(hex) < 6 {
		return ""
	}
	switch hex[1] {
	case '2', '6', 'a', 'e': // locally administered: not assigned by the IEEE
		return ""
	}
	once.Do(load)
	return vendors[hex[:6]]
}

// Count returns the number of known prefixes.
func Count() int {
	once.Do(load)
	return len(vendors)
}
