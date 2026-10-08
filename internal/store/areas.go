package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Area is a named rectangle drawn on the map to group devices (e.g. "Rack",
// "Living room"). It remembers its member nodes and is drawn around them; the
// stored rectangle is used while no member is on the map.
type Area struct {
	ID        int64    `json:"id"`
	Name      string   `json:"name"`
	Color     string   `json:"color"`     // a preset (AreaColors) or "#rrggbb"
	Direction string   `json:"direction"` // map orientation the area belongs to: RIGHT | DOWN
	X         float64  `json:"x"`
	Y         float64  `json:"y"`
	Width     float64  `json:"width"`
	Height    float64  `json:"height"`
	Members   []string `json:"members"` // node ids; the area is drawn around them
	// Auto marks an area created automatically for a VLAN ("vlan:20") or a
	// subnet ("subnet:192.168.20.0/24"): the UI works out its members. One
	// per key; removing it only dismisses it, so it is not created again.
	Auto      string `json:"auto,omitempty"`
	Dismissed bool   `json:"dismissed,omitempty"`
}

// AreaColors are the preset colors offered by the UI; any "#rrggbb" is valid too.
var AreaColors = []string{"gray", "blue", "green", "yellow", "red", "purple"}

var (
	hexColor = regexp.MustCompile(`^#[0-9a-f]{6}$`)
	autoKey  = regexp.MustCompile(`^(vlan:[0-9]{1,4}|subnet:[0-9a-f.:]+/[0-9]{1,3})$`)
)

const (
	minAreaSize    = 60
	maxAreaNameLen = 60
	maxAreaMembers = 1000
)

// AreaUpdate changes the given fields of an area; nil fields are kept.
type AreaUpdate struct {
	Name    *string   `json:"name"`
	Color   *string   `json:"color"`
	X       *float64  `json:"x"`
	Y       *float64  `json:"y"`
	Width   *float64  `json:"width"`
	Height  *float64  `json:"height"`
	Members *[]string `json:"members"`
}

// ErrInvalidArea is returned for areas with an invalid name, color, orientation or size.
var ErrInvalidArea = errors.New("invalid area")

func (a *Area) validate() error {
	a.Name = strings.TrimSpace(a.Name)
	a.Color = strings.ToLower(strings.TrimSpace(a.Color))
	switch {
	case a.Name == "" || utf8.RuneCountInString(a.Name) > maxAreaNameLen:
		return fmt.Errorf("%w: the name must have 1 to %d characters", ErrInvalidArea, maxAreaNameLen)
	case !slices.Contains(AreaColors, a.Color) && !hexColor.MatchString(a.Color):
		return fmt.Errorf("%w: unknown color %q (a preset or #rrggbb)", ErrInvalidArea, a.Color)
	case a.Direction != "RIGHT" && a.Direction != "DOWN":
		return fmt.Errorf("%w: the direction must be RIGHT or DOWN", ErrInvalidArea)
	case a.Width < minAreaSize || a.Height < minAreaSize:
		return fmt.Errorf("%w: an area must be at least %dx%d", ErrInvalidArea, minAreaSize, minAreaSize)
	case len(a.Members) > maxAreaMembers:
		return fmt.Errorf("%w: at most %d devices per area", ErrInvalidArea, maxAreaMembers)
	case a.Auto != "" && !autoKey.MatchString(a.Auto):
		return fmt.Errorf("%w: unknown automatic area %q", ErrInvalidArea, a.Auto)
	}
	if a.Members == nil {
		a.Members = []string{}
	}
	return nil
}

const areaColumns = `id, name, color, direction, x, y, width, height, members, COALESCE(auto, ''), dismissed`

func scanArea(row interface{ Scan(...any) error }) (Area, error) {
	var (
		a       Area
		members string
	)
	err := row.Scan(&a.ID, &a.Name, &a.Color, &a.Direction, &a.X, &a.Y, &a.Width, &a.Height, &members, &a.Auto, &a.Dismissed)
	a.Members = decodeMembers(members)
	return a, err
}

// ListAreas returns every area, dismissed automatic ones included (the UI
// skips them, and needs them so it does not create them again).
func (s *Store) ListAreas(ctx context.Context) ([]Area, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+areaColumns+` FROM areas ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Area{}
	for rows.Next() {
		a, err := scanArea(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArea(ctx context.Context, id int64) (Area, error) {
	a, err := scanArea(s.db.QueryRowContext(ctx, `SELECT `+areaColumns+` FROM areas WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// CreateArea stores a new area. An automatic area that already exists (two
// browsers created it at once, or it was dismissed) is returned as it is.
func (s *Store) CreateArea(ctx context.Context, a Area) (Area, error) {
	a.Dismissed = false
	if err := a.validate(); err != nil {
		return a, err
	}
	var auto any
	if a.Auto != "" {
		auto = a.Auto
		existing, err := scanArea(s.db.QueryRowContext(ctx, `SELECT `+areaColumns+` FROM areas WHERE auto = ?`, a.Auto))
		if err == nil {
			return existing, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return a, err
		}
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO areas (name, color, direction, x, y, width, height, members, auto, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Color, a.Direction, a.X, a.Y, a.Width, a.Height, encodeMembers(a.Members), auto, time.Now().Unix())
	if err != nil {
		return a, err
	}
	a.ID, err = res.LastInsertId()
	return a, err
}

func (s *Store) UpdateArea(ctx context.Context, id int64, u AreaUpdate) (Area, error) {
	a, err := s.GetArea(ctx, id)
	if err != nil {
		return a, err
	}
	set := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setF := func(dst *float64, v *float64) {
		if v != nil {
			*dst = *v
		}
	}
	set(&a.Name, u.Name)
	set(&a.Color, u.Color)
	setF(&a.X, u.X)
	setF(&a.Y, u.Y)
	setF(&a.Width, u.Width)
	setF(&a.Height, u.Height)
	if u.Members != nil {
		a.Members = *u.Members
	}
	if err := a.validate(); err != nil {
		return a, err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE areas SET name = ?, color = ?, x = ?, y = ?, width = ?, height = ?, members = ? WHERE id = ?`,
		a.Name, a.Color, a.X, a.Y, a.Width, a.Height, encodeMembers(a.Members), id)
	return a, err
}

// DeleteArea removes an area. An automatic one is only dismissed: kept, so
// the UI does not create it again for the same VLAN or subnet.
func (s *Store) DeleteArea(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM areas WHERE id = ? AND auto IS NULL`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	res, err = s.db.ExecContext(ctx, `UPDATE areas SET dismissed = 1 WHERE id = ? AND dismissed = 0`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func encodeMembers(m []string) string {
	b, _ := json.Marshal(m)
	return string(b)
}

func decodeMembers(s string) []string {
	out := []string{}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}
