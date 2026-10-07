// Package models turns device model identifiers into the names people know:
// Apple identifiers ("iPhone14,2" → "iPhone 13 Pro") and Android model codes
// ("SM-S911B" → "Samsung Galaxy S23"). Lists: internal/models/gen (make models).
package models

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"strings"
	"sync"
)

//go:embed models.tsv.gz
var data []byte

var (
	once  sync.Once
	names map[string]string
)

func load() {
	names = map[string]string{}
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return
	}
	sc := bufio.NewScanner(zr)
	for sc.Scan() {
		if k, v, ok := strings.Cut(sc.Text(), "\t"); ok {
			names[k] = v
		}
	}
}

// Name returns the marketing name of a model identifier, if known. Apple
// identifiers match exactly; Android model codes in any case.
func Name(id string) (string, bool) {
	once.Do(load)
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	if n, ok := names[id]; ok {
		return n, true
	}
	n, ok := names[strings.ToUpper(id)]
	return n, ok
}
