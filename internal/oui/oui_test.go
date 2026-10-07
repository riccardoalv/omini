package oui

import "testing"

func TestLookup(t *testing.T) {
	cases := map[string]string{
		"f0:18:98:00:00:02": "Apple",
		"b8:27:eb:12:34:56": "Raspberry Pi Foundation",
		"bc:24:11:00:00:01": "Proxmox Server Solutions",
		"da:a1:19:00:00:01": "", // randomized MAC
		"ff:ff:ff:ff:ff:ff": "",
		"bad":               "",
	}
	for mac, want := range cases {
		if got := Lookup(mac); got != want {
			t.Errorf("Lookup(%s) = %q, want %q", mac, got, want)
		}
	}
	if Count() < 30000 {
		t.Errorf("only %d prefixes loaded", Count())
	}
}
