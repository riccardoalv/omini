package api_test

import (
	"context"
	"testing"
	"time"

	"github.com/riccardoalv/omini/internal/store"
)

func TestInternetUsageAndDestinations(t *testing.T) {
	h := newHarness(t, nil)
	h.login()
	h.do("POST", "/api/integrations", map[string]any{"name": "demo", "type": "demo", "config": map[string]any{}}, nil)
	if err := h.coll.CollectNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	nas := "mac:00:11:32:aa:00:01"
	if err := h.store.SaveInternet(context.Background(), now, []store.Usage{
		{Node: nas, RX: 5000, TX: 100}, {Node: "ip:192.168.1.77", RX: 10},
	}, []store.Destination{
		{Node: nas, Dest: "youtube.com", Proto: 6, Port: 443, RX: 4000, TX: 50},
		{Node: nas, Dest: "1.1.1.1", Proto: 17, Port: 53, RX: 1000, TX: 50},
	}); err != nil {
		t.Fatal(err)
	}
	var usage struct {
		Devices []struct {
			Node, Label string
			RX, TX      uint64
			OnMap       bool `json:"on_map"`
		} `json:"devices"`
		Total struct{ RX, TX uint64 } `json:"total"`
	}
	if code := h.do("GET", "/api/internet?range=today", nil, &usage); code != 200 {
		t.Fatalf("internet: %d", code)
	}
	if len(usage.Devices) != 2 || usage.Devices[0].Node != nas || !usage.Devices[0].OnMap || usage.Devices[1].Label != "192.168.1.77" || usage.Total.RX != 5010 {
		t.Fatalf("usage = %+v", usage)
	}
	var overview struct {
		Series []struct{ RX uint64 } `json:"series"`
		StepS  int                   `json:"step_s"`
		Now    map[string]float64    `json:"now"`
	}
	h.do("GET", "/api/internet?range=1h", nil, &overview)
	if overview.StepS != 60 || len(overview.Series) < 60 || overview.Now["rx_bps"] == 0 {
		t.Fatalf("overview = step %d, %d points, now %v", overview.StepS, len(overview.Series), overview.Now)
	}
	if code := h.do("GET", "/api/internet?range=1y", nil, nil); code != 400 {
		t.Fatalf("bad range: %d", code)
	}
	var dests struct {
		Destinations []struct {
			Name string
			RX   uint64
		} `json:"destinations"`
	}
	if code := h.do("GET", "/api/nodes/"+nas+"/destinations?hours=1", nil, &dests); code != 200 {
		t.Fatalf("destinations: %d", code)
	}
	if len(dests.Destinations) != 2 || dests.Destinations[0].Name != "youtube.com" {
		t.Fatalf("destinations = %+v", dests)
	}
}
