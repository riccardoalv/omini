package model

import "testing"

func TestNormMAC(t *testing.T) {
	cases := map[string]MACAddress{
		"AA-BB-CC-00-11-22": "aa:bb:cc:00:11:22",
		"aabb.cc00.1122":    "aa:bb:cc:00:11:22",
		"AABBCC001122":      "aa:bb:cc:00:11:22",
		"aa:bb:cc:00:11:22": "aa:bb:cc:00:11:22",
		"router":            "",
		"aa:bb:cc":          "",
		"":                  "",
	}
	for in, want := range cases {
		if got := NormMAC(in); got != want {
			t.Errorf("NormMAC(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMACFromBytes(t *testing.T) {
	if got := MACFromBytes([]byte{0xaa, 0xbb, 0xcc, 0, 0x11, 0x22}); got != "aa:bb:cc:00:11:22" {
		t.Errorf("got %q", got)
	}
	if got := MACFromBytes(make([]byte, 6)); got != "" {
		t.Errorf("all-zero MAC should be empty, got %q", got)
	}
}

func TestMACFlags(t *testing.T) {
	if !MACAddress("da:a1:19:00:00:01").IsRandomized() {
		t.Error("locally administered MAC should be randomized")
	}
	if MACAddress("00:1b:21:00:00:01").IsRandomized() {
		t.Error("vendor MAC should not be randomized")
	}
	if !MACAddress("ff:ff:ff:ff:ff:ff").IsGroup() || !MACAddress("01:00:5e:00:00:fb").IsGroup() {
		t.Error("broadcast/multicast should be group addresses")
	}
}
