package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Person is someone who owns devices on the network (a device has at most
// one owner, InventoryEntry.OwnerID).
type Person struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Color     string    `json:"color,omitempty"` // #rrggbb, optional
	CreatedAt time.Time `json:"created_at"`
}

// PersonNameMax is the longest name a person can have, in characters.
const PersonNameMax = 60

var (
	// ErrPersonName: an empty name, or one over PersonNameMax characters.
	ErrPersonName = errors.New("a person needs a name of up to 60 characters")
	// ErrPersonExists: another person has that name (ignoring case).
	ErrPersonExists = errors.New("there is already a person with that name")
	// ErrPersonColor: a color that is not #rrggbb.
	ErrPersonColor = errors.New("the color must be #rrggbb")
	// ErrNoSuchPerson: an owner that is not one of the people.
	ErrNoSuchPerson = errors.New("no such person")
)

// OptionalID is a JSON field that may be absent (Set false), null (Set,
// ID 0) or a number.
type OptionalID struct {
	Set bool
	ID  int64
}

func (o *OptionalID) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.ID = 0
		return nil
	}
	var id int64
	if err := json.Unmarshal(b, &id); err != nil {
		return err
	}
	o.ID = id
	return nil
}

func cleanPerson(name, color string) (string, string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" || utf8.RuneCountInString(name) > PersonNameMax {
		return "", "", ErrPersonName
	}
	color = strings.ToLower(strings.TrimSpace(color))
	if color != "" && !hexColor.MatchString(color) {
		return "", "", ErrPersonColor
	}
	return name, color, nil
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// ListPeople are the people, by name.
func (s *Store) ListPeople(ctx context.Context) ([]Person, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, color, created_at FROM people ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Person{}
	for rows.Next() {
		var (
			p  Person
			at int64
		)
		if err := rows.Scan(&p.ID, &p.Name, &p.Color, &at); err != nil {
			return nil, err
		}
		p.CreatedAt = fromUnix(at)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPerson is one person.
func (s *Store) GetPerson(ctx context.Context, id int64) (Person, error) {
	var (
		p  Person
		at int64
	)
	err := s.db.QueryRowContext(ctx, `SELECT id, name, color, created_at FROM people WHERE id = ?`, id).
		Scan(&p.ID, &p.Name, &p.Color, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Person{}, ErrNotFound
	}
	p.CreatedAt = fromUnix(at)
	return p, err
}

// CreatePerson adds a person.
func (s *Store) CreatePerson(ctx context.Context, name, color string, at time.Time) (Person, error) {
	name, color, err := cleanPerson(name, color)
	if err != nil {
		return Person{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO people (name, color, created_at) VALUES (?, ?, ?)`, name, color, unix(at))
	if isUnique(err) {
		return Person{}, ErrPersonExists
	} else if err != nil {
		return Person{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Person{}, err
	}
	return Person{ID: id, Name: name, Color: color, CreatedAt: fromUnix(unix(at))}, nil
}

// UpdatePerson renames a person or changes their color (nil: unchanged).
func (s *Store) UpdatePerson(ctx context.Context, id int64, name, color *string) (Person, error) {
	p, err := s.GetPerson(ctx, id)
	if err != nil {
		return Person{}, err
	}
	n, c := p.Name, p.Color
	if name != nil {
		n = *name
	}
	if color != nil {
		c = *color
	}
	if n, c, err = cleanPerson(n, c); err != nil {
		return Person{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE people SET name = ?, color = ? WHERE id = ?`, n, c, id)
	if isUnique(err) {
		return Person{}, ErrPersonExists
	} else if err != nil {
		return Person{}, err
	}
	p.Name, p.Color = n, c
	return p, nil
}

// DeletePerson removes a person; their devices are left without an owner.
func (s *Store) DeletePerson(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after Commit
	if _, err := tx.ExecContext(ctx, `UPDATE inventory SET owner_id = NULL WHERE owner_id = ?`, id); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM people WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return tx.Commit()
}

// setOwner gives the device an owner (0: nobody).
func (s *Store) setOwner(ctx context.Context, deviceID string, personID int64) error {
	if personID != 0 {
		if _, err := s.GetPerson(ctx, personID); errors.Is(err, ErrNotFound) {
			return ErrNoSuchPerson
		} else if err != nil {
			return err
		}
	}
	var owner any
	if personID != 0 {
		owner = personID
	}
	_, err := s.db.ExecContext(ctx, `UPDATE inventory SET owner_id = ? WHERE id = ?`, owner, deviceID)
	return err
}
