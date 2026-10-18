package model

import (
	"encoding/json"
	"testing"
)

// A radio's width decodes (an integer enum never matched the decoded int64).
func TestRadioWidth(t *testing.T) {
	var r Radio
	if err := json.Unmarshal([]byte(`{"name":"wifi0","band":"2.4ghz","channel":6,"width_mhz":20}`), &r); err != nil {
		t.Fatal(err)
	}
	if r.WidthMhz == nil || *r.WidthMhz != 20 {
		t.Fatalf("width: %+v", r.WidthMhz)
	}
	if err := json.Unmarshal([]byte(`{"name":"wifi0","band":"2.4ghz","width_mhz":10}`), &r); err == nil {
		t.Fatal("10 MHz is not a width")
	}
}
