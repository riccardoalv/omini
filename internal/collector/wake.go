package collector

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"slices"
	"strconv"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/store"
)

// Wake-on-LAN: Omini sends the magic packet itself on its own networks; on
// another network, the router of that network sends it (wol.send).

// WakeVia says how a device can be woken: "omini", "router" or "" (it
// cannot), and the router.
type WakeVia struct {
	Via    string `json:"via,omitempty"`
	Router string `json:"router,omitempty"`
}

// MagicPacket is 6 × 0xff and the MAC 16 times.
func MagicPacket(mac net.HardwareAddr) []byte {
	out := bytes.Repeat([]byte{0xff}, 6)
	for range 16 {
		out = append(out, mac...)
	}
	return out
}

// localNets are the networks of the machine Omini runs on.
func localNets() []netip.Prefix {
	var out []netip.Prefix
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	for _, i := range ifaces {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if p, err := netip.ParsePrefix(n.String()); err == nil && p.Addr().Is4() {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

// sendMagic broadcasts the packet on the network of p (UDP port 9).
func sendMagic(p netip.Prefix, mac net.HardwareAddr) error {
	bcast := store.LastAddr(p.Masked())
	conn, err := net.Dial("udp4", net.JoinHostPort(bcast.String(), strconv.Itoa(9)))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_, err = conn.Write(MagicPacket(mac))
	return err
}

// wakeTarget is where a device's packet goes: Omini's own network holding
// its address, or the router whose network does.
func (c *Collector) wakeTarget(nodeID string) (model.MACAddress, netip.Addr, netip.Prefix, WakeVia, error) {
	n, ok := c.node(nodeID)
	mac := model.NormMAC(n.MAC)
	if !ok || mac == "" {
		return "", netip.Addr{}, netip.Prefix{}, WakeVia{}, fmt.Errorf("%w: waking a device needs its MAC", ErrNotChangeable)
	}
	ip, _ := netip.ParseAddr(n.IP)
	if fixed, ok := fixedIP(c.State().Topology, mac); ok {
		ip = fixed
	}
	if ip.IsValid() {
		for _, p := range c.opts.LocalNets() {
			if p.Contains(ip) {
				return mac, ip, p, WakeVia{Via: "omini"}, nil
			}
		}
		for _, r := range c.State().Topology.Nodes {
			if slices.Contains(r.Changes, model.ChangeKindWolSend) && networkHolding(r, ip) != nil {
				return mac, ip, netip.Prefix{}, WakeVia{Via: "router", Router: r.ID}, nil
			}
		}
	}
	return mac, ip, netip.Prefix{}, WakeVia{}, fmt.Errorf("%w: neither Omini nor a router it manages is on this device's network", ErrNotChangeable)
}

// CanWake says how a device would be woken.
func (c *Collector) CanWake(nodeID string) WakeVia {
	_, _, _, via, _ := c.wakeTarget(nodeID)
	return via
}

// Wake sends the device a Wake-on-LAN packet, and records it in its history.
func (c *Collector) Wake(ctx context.Context, nodeID string) (store.ConfigChange, error) {
	mac, ip, local, via, err := c.wakeTarget(nodeID)
	if err != nil {
		return store.ConfigChange{}, err
	}
	value := WolValue{MAC: mac, IP: ip.String()}
	if via.Via == "router" {
		return c.Apply(ctx, ChangeRequest{NodeID: via.Router, Kind: model.ChangeKindWolSend, Value: value})
	}
	hw, err := net.ParseMAC(string(mac))
	if err != nil {
		return store.ConfigChange{}, fmt.Errorf("%w: %w", ErrInvalidChange, err)
	}
	after, _ := json.Marshal(value)
	rec := store.ConfigChange{NodeID: nodeID, Kind: string(model.ChangeKindWolSend), After: after, At: c.opts.Now()}
	if err := c.opts.SendMagic(local, hw); err != nil {
		rec.Error = err.Error()
		slog.Warn("could not send the Wake-on-LAN packet", "node", nodeID, "err", err)
	} else {
		slog.Info("Wake-on-LAN packet sent", "node", nodeID, "network", local)
	}
	var addErr error
	if rec.ID, addErr = c.store.AddConfigChange(ctx, rec); addErr != nil {
		return rec, addErr
	}
	if rec.Error != "" {
		return rec, fmt.Errorf("could not send the Wake-on-LAN packet: %s", rec.Error)
	}
	return rec, nil
}
