package models

import "testing"

func TestName(t *testing.T) {
	for id, want := range map[string]string{
		"iPhone14,2":     "iPhone 13 Pro",
		"AppleTV11,1":    "Apple TV 4K (2nd generation)",
		"MacBookPro18,3": `MacBook Pro 14" (M1 Pro, 2021)`,
		"SM-S911B":       "Samsung Galaxy S23",
		"sm-s911b":       "Samsung Galaxy S23", // Android codes in any case
	} {
		if got, ok := Name(id); !ok || got != want {
			t.Errorf("Name(%q) = %q, %v; want %q", id, got, ok, want)
		}
	}
	// Apple ids are exact; words without a digit are no model codes.
	for _, id := range []string{"", "Living room TV", "iphone14,2", "Chromecast", "chromecast", "Access"} {
		if got, ok := Name(id); ok {
			t.Errorf("Name(%q) = %q, want unknown", id, got)
		}
	}
}
