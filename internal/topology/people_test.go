package topology

import "testing"

func TestPeopleHome(t *testing.T) {
	nodes := []Node{
		{ID: "ana-phone", OwnerID: 1, Type: "phone", Online: true},
		{ID: "bob-laptop", OwnerID: 2, Type: "laptop", Online: true}, // a laptop stays home
		{ID: "bob-watch", OwnerID: 2, Type: "wearable", Online: false},
		{ID: "cid-tablet", OwnerID: 3, Type: "tablet", Online: true},
		{ID: "nobody", Type: "phone", Online: true},
	}
	home := PeopleHome(nodes)
	if !home[1] || home[2] || !home[3] || len(home) != 2 {
		t.Fatalf("home: %v", home)
	}
}
