package netscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/snmp"
)

// agent is where an SNMP device was read: its address and the credential it
// answered to (a community, or snmp.V3Credential).
type agent struct {
	ip         netip.Addr
	credential string
}

// Apply changes an SNMP device with the write community, or with the v3 user
// when the device answers v3: a port enabled or disabled (IF-MIB
// ifAdminStatus), a port's VLANs, a VLAN created, renamed or deleted
// (Q-BRIDGE-MIB). It returns the device read again after the change.
func (s *Integration) Apply(ctx context.Context, cfg integration.Config, change model.Change) ([]model.Device, error) {
	write, a, opts, err := s.writer(cfg, change)
	if err != nil {
		return nil, err
	}
	switch change.Kind {
	case model.ChangeKindPortEnabled:
		enabled, ok := change.Value.(bool)
		if !ok || change.Port == nil {
			return nil, fmt.Errorf("port.enabled takes a port and true or false")
		}
		err = snmp.SetPortEnabled(ctx, write, *change.Port, enabled)
	case model.ChangeKindPortVlans:
		var v model.PortVlans
		if change.Port == nil || recast(change.Value, &v) != nil {
			return nil, fmt.Errorf("port.vlans takes a port and {untagged, tagged}")
		}
		err = snmp.SetPortVlans(ctx, write, *change.Port, v)
	case model.ChangeKindVlanSet, model.ChangeKindVlanDelete:
		var v struct {
			ID   uint16 `json:"id"`
			Name string `json:"name"`
		}
		if recast(change.Value, &v) != nil || v.ID < 1 || v.ID > 4094 {
			return nil, fmt.Errorf("%s takes {id, name}", change.Kind)
		}
		if change.Kind == model.ChangeKindVlanSet {
			err = snmp.SetVlan(ctx, write, v.ID, v.Name)
		} else {
			err = snmp.DeleteVlan(ctx, write, v.ID)
		}
	default:
		return nil, fmt.Errorf("the network scan cannot apply %s", change.Kind)
	}
	if err != nil {
		if errors.Is(err, snmp.ErrNoWrite) {
			return nil, fmt.Errorf("%w: check the write community in the network scan's settings, or the v3 user's write view", err)
		}
		return nil, err
	}
	d, err := snmp.Collect(ctx, s.snmpTarget(a.ip, a.credential, opts.v3))
	if err != nil {
		return nil, fmt.Errorf("the change was made, but reading the device again failed: %w", err)
	}
	return []model.Device{d}, nil
}

// recast reads a change's value (a Go value from the core) into v.
func recast(value any, v any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

// writer is the SNMP target that may write to the change's device.
func (s *Integration) writer(cfg integration.Config, change model.Change) (snmp.Target, agent, options, error) {
	opts, err := s.options(cfg)
	if err != nil {
		return snmp.Target{}, agent{}, opts, err
	}
	s.mu.Lock()
	a, known := s.agents[change.Device]
	s.mu.Unlock()
	if !known {
		return snmp.Target{}, a, opts, errors.New("this device has not been read by SNMP since Omini started: try again after the next round")
	}
	write := s.snmpTarget(a.ip, opts.writeCommunity, nil)
	switch {
	case a.credential == snmp.V3Credential && opts.v3 != nil:
		write = s.snmpTarget(a.ip, snmp.V3Credential, opts.v3)
	case opts.writeCommunity == "":
		return write, a, opts, errors.New("set the SNMP write community in the network scan's settings (or use an SNMP v3 user allowed to write)")
	}
	return write, a, opts, nil
}
