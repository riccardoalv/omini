package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// Area is a named rectangle drawn on the map to group devices (e.g. "Rack",
// "Living room"). Membership is geometric: the nodes inside it move with it.
type Area struct {
	ID        int64   `json:"id"`
	Name      string  `json:"name"`
	Color     string  `json:"color"`
	Direction string  `json:"direction"` // map orientation the area belongs to: RIGHT | DOWN
	X         float64 `json:"x"`
	Y         float64 `json:"y"`
	Width     float64 `json:"width"`
	Height    float64 `json:"height"`
}

// AreaColors are the colors offered by the UI.
var AreaColors = []string{"gray", "blue", "green", "yellow", "red", "purple"}

const (
	minAreaSize    = 60
	maxAreaNameLen = 60
)

// AreaUpdate changes the given fields of an area; nil fields are kept.
type AreaUpdate struct {
	Name   *string  `json:"name"`
	Color  *string  `json:"color"`
	X      *float64 `json:"x"`
	Y      *float64 `json:"y"`
	Width  *float64 `json:"width"`
	Height *float64 `json:"height"`
}

// ErrInvalidArea is returned for areas with an invalid name, color, orientation or size.
var ErrInvalidArea = errors.New("invalid area")

func (a *Area) validate() error {
	a.Name = strings.TrimSpace(a.Name)
	switch {
	case a.Name == "" || utf8.RuneCountInString(a.Name) > maxAreaNameLen:
		return fmt.Errorf("%w: the name must have 1 to %d characters", ErrInvalidArea, maxAreaNameLen)
	case !slices.Contains(AreaColors, a.Color):
		return fmt.Errorf("%w: unknown color %q", ErrInvalidArea, a.Color)
	case a.Direction != "RIGHT" && a.Direction != "DOWN":
		return fmt.Errorf("%w: the direction must be RIGHT or DOWN", ErrInvalidArea)
	case a.Width < minAreaSize || a.Height < minAreaSize:
		return fmt.Errorf("%w: an area must be at least %dx%d", ErrInvalidArea, minAreaSize, minAreaSize)
	}
	return nil
}

func (s *Store) ListAreas(ctx context.Context) ([]Area, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, color, direction, x, y, width, height FROM areas ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Area{}
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.Name, &a.Color, &a.Direction, &a.X, &a.Y, &a.Width, &a.Height); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) GetArea(ctx context.Context, id int64) (Area, error) {
	var a Area
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, color, direction, x, y, width, height FROM areas WHERE id = ?`, id).
		Scan(&a.ID, &a.Name, &a.Color, &a.Direction, &a.X, &a.Y, &a.Width, &a.Height)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CreateArea(ctx context.Context, a Area) (Area, error) {
	if err := a.validate(); err != nil {
		return a, err
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO areas (name, color, direction, x, y, width, height, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		a.Name, a.Color, a.Direction, a.X, a.Y, a.Width, a.Height, time.Now().Unix())
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
	if err := a.validate(); err != nil {
		return a, err
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE areas SET name = ?, color = ?, x = ?, y = ?, width = ?, height = ? WHERE id = ?`,
		a.Name, a.Color, a.X, a.Y, a.Width, a.Height, id)
	return a, err
}

func (s *Store) DeleteArea(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM areas WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
