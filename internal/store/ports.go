package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

const maxPortLabelLen = 80

// ErrInvalidPortLabel is returned for an empty port or a too long description.
var ErrInvalidPortLabel = errors.New("invalid port description")

// PortLabels returns the user's port descriptions: node id → port → text.
func (s *Store) PortLabels(ctx context.Context) (map[string]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, port, label FROM port_labels`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]map[string]string{}
	for rows.Next() {
		var node, port, label string
		if err := rows.Scan(&node, &port, &label); err != nil {
			return nil, err
		}
		if out[node] == nil {
			out[node] = map[string]string{}
		}
		out[node][port] = label
	}
	return out, rows.Err()
}

// SetPortLabel saves the description of a port; an empty one removes it, so
// the description reported by the device shows again.
func (s *Store) SetPortLabel(ctx context.Context, node, port, label string) error {
	label = strings.TrimSpace(label)
	if node == "" || port == "" || utf8.RuneCountInString(label) > maxPortLabelLen {
		return fmt.Errorf("%w: at most %d characters", ErrInvalidPortLabel, maxPortLabelLen)
	}
	if label == "" {
		_, err := s.db.ExecContext(ctx, `DELETE FROM port_labels WHERE node_id = ? AND port = ?`, node, port)
		return err
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO port_labels (node_id, port, label) VALUES (?, ?, ?)
		ON CONFLICT (node_id, port) DO UPDATE SET label = excluded.label`, node, port, label)
	return err
}
