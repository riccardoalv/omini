package model

import (
	"encoding/json"
	"testing"
)

// System health as a plugin sends it: load, swap, disks, sensors, updates.
func TestDeviceHealth(t *testing.T) {
	var d Device
	err := json.Unmarshal([]byte(`{
		"key": "fw", "name": "fw",
		"swap_pct": 0, "load_avg": [0.34, 0.35, 0.33],
		"temperatures": [{"sensor": "CPU 0", "kind": "cpu", "celsius": 47}],
		"storage": [{"mount": "/", "total_bytes": 6653407232, "used_bytes": 3820572672}],
		"firmware": {"current": "26.7.4", "latest": "26.7.5", "update_available": true, "updates": 3,
			"needs_reboot": true, "checked_at": "2026-10-07T02:54:18-04:00"}
	}`), &d)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.LoadAvg) != 3 || d.SwapPct == nil || d.Temperatures[0].Celsius != 47 ||
		*d.Storage[0].UsedBytes != 3820572672 || !*d.Firmware.UpdateAvailable || *d.Firmware.Updates != 3 {
		t.Fatalf("device: %+v", d)
	}

	for _, bad := range []string{
		`{"key": "fw", "name": "fw", "load_avg": [1, 2, 3, 4]}`,
		`{"key": "fw", "name": "fw", "swap_pct": 120}`,
		`{"key": "fw", "name": "fw", "temperatures": [{"sensor": "CPU 0"}]}`,
		`{"key": "fw", "name": "fw", "storage": [{"total_bytes": 1}]}`,
	} {
		if err := json.Unmarshal([]byte(bad), &Device{}); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}
