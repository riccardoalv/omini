package insights

import (
	"github.com/riccardoalv/omini/internal/model"
)

func init() {
	Rules["service_stopped"] = serviceStopped
}

// serviceStopped: a service the device is configured to run is not running.
func serviceStopped(in Input, _ index) []Insight {
	var out []Insight
	for _, n := range devices(in) {
		if !n.Online {
			continue // its last data is old: device_offline tells it
		}
		for _, s := range n.Device.Services {
			if !model.Deref(s.Enabled) || s.Running == nil || *s.Running {
				continue
			}
			p := map[string]any{"service": s.Name}
			if d := model.Deref(s.Description); d != "" {
				p["description"] = d
			}
			out = append(out, node("service_stopped", Warning, n, s.Name, p))
		}
	}
	return out
}
