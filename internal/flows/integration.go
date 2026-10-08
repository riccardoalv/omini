package flows

import (
	"context"
	"fmt"

	"github.com/riccardoalv/omini/internal/integration"
	"github.com/riccardoalv/omini/internal/model"
)

// Integration turns the flow collector on: while it is enabled, Omini listens
// for NetFlow/IPFIX and sFlow exports. It reports no devices: conversations
// are shown on their own screen and in the device panel.
type Integration struct {
	Service *Service
}

func New(s *Service) *Integration { return &Integration{Service: s} }

func (*Integration) Info() integration.Info {
	return integration.Info{
		Type: "flows",
		Name: "Traffic flows",
		Description: "Who talks to whom: receives NetFlow v5/v9, IPFIX or sFlow from your router, firewall or switch " +
			"(e.g. OPNsense: Reporting → NetFlow) and shows each device's conversations. Omini only listens.",
		Kind:   integration.KindCore,
		Single: true,
		Fields: []model.FormField{
			{
				Key: "netflow_port", Type: model.FormFieldTypeInt, Label: model.Ptr("NetFlow / IPFIX port (UDP)"), Default: 2055,
				Help: model.Ptr("Point the exporter at this server's address and this port. 0 turns it off."),
			},
			{
				Key: "sflow_port", Type: model.FormFieldTypeInt, Label: model.Ptr("sFlow port (UDP)"), Default: 6343,
				Help: model.Ptr("For switches that export sFlow. 0 turns it off."),
			},
		},
	}
}

func ports(cfg integration.Config) (int, int, error) {
	nf, sf := cfg.Int("netflow_port", 2055), cfg.Int("sflow_port", 6343)
	for _, p := range []int{nf, sf} {
		if p < 0 || p > 65535 {
			return 0, 0, fmt.Errorf("ports go from 1 to 65535 (0 turns one off)")
		}
	}
	if nf != 0 && nf == sf {
		return 0, 0, fmt.Errorf("NetFlow and sFlow need different ports")
	}
	if nf == 0 && sf == 0 {
		return 0, 0, fmt.Errorf("turn on at least one of the two ports")
	}
	return nf, sf, nil
}

// Validate checks the ports before they are saved.
func (*Integration) Validate(cfg integration.Config) error {
	_, _, err := ports(cfg)
	return err
}

func (i *Integration) Collect(ctx context.Context, cfg integration.Config) ([]model.Device, error) {
	nf, sf, err := ports(cfg)
	if err != nil {
		return nil, err
	}
	return []model.Device{}, i.Service.Configure(ctx, nf, sf)
}

func (i *Integration) Test(ctx context.Context, cfg integration.Config) (string, error) {
	nf, sf, err := ports(cfg)
	if err != nil {
		return "", err
	}
	if err := i.Service.Configure(ctx, nf, sf); err != nil {
		return "", err
	}
	if n := len(i.Service.Exporters()); n > 0 {
		return fmt.Sprintf("Listening; receiving from %d exporter(s)", n), nil
	}
	return "Listening; nothing received yet: point your exporter at this server", nil
}
