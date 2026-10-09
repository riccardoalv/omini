package store

import (
	"context"
	"time"

	"github.com/riccardoalv/omini/internal/model"
)

// DHCPFingerprints returns the last fingerprint of each MAC (see netscan).
func (s *Store) DHCPFingerprints(ctx context.Context) (map[model.MACAddress]model.DhcpFingerprint, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT mac, params, vendor_class, hostname FROM dhcp_fingerprints`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.MACAddress]model.DhcpFingerprint{}
	for rows.Next() {
		var mac, params, vendor, host string
		if err := rows.Scan(&mac, &params, &vendor, &host); err != nil {
			return nil, err
		}
		out[model.MACAddress(mac)] = model.DhcpFingerprint{
			Params: nonEmpty(params), VendorClass: nonEmpty(vendor), Hostname: nonEmpty(host),
		}
	}
	return out, rows.Err()
}

// SaveDHCPFingerprint records a MAC's fingerprint, replacing the one before.
func (s *Store) SaveDHCPFingerprint(ctx context.Context, mac model.MACAddress, fp model.DhcpFingerprint, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO dhcp_fingerprints (mac, params, vendor_class, hostname, seen_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (mac) DO UPDATE SET params = excluded.params, vendor_class = excluded.vendor_class,
			hostname = excluded.hostname, seen_at = excluded.seen_at`,
		string(mac), model.Deref(fp.Params), model.Deref(fp.VendorClass), model.Deref(fp.Hostname), unix(at))
	return err
}

func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
