package topology

import "slices"

// carried are the device types a person carries: one of theirs online means
// they are home.
var carried = []string{"phone", "tablet", "wearable"}

// PeopleHome are the people (by id) one of whose phones, tablets or wearables
// is online on the map.
func PeopleHome(nodes []Node) map[int64]bool {
	out := map[int64]bool{}
	for _, n := range nodes {
		if n.OwnerID != 0 && n.Online && slices.Contains(carried, n.Type) {
			out[n.OwnerID] = true
		}
	}
	return out
}
