// Package integration defines the contract every integration (core or plugin)
// implements, and the registry the rest of the core uses to find them.
package integration

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/riccardoalv/omini/internal/model"
)

// Kind tells where an integration comes from.
type Kind string

const (
	KindCore   Kind = "core"   // built into the Go binary
	KindPlugin Kind = "plugin" // external Python plugin
)

// Info describes an integration type and the form the UI renders to configure it.
type Info struct {
	Type        string            `json:"type"`
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Kind        Kind              `json:"kind"`
	Fields      []model.FormField `json:"fields"`
}

// Config is the user-provided configuration of one integration instance:
// values of the form fields, keyed by field key.
type Config map[string]any

// String returns the string value of key, or "" when missing.
func (c Config) String(key string) string {
	v, _ := c[key].(string)
	return v
}

// Bool returns the boolean value of key, or def when missing.
func (c Config) Bool(key string, def bool) bool {
	if v, ok := c[key].(bool); ok {
		return v
	}
	return def
}

// Int returns the integer value of key, or def when missing.
func (c Config) Int(key string, def int) int {
	switch v := c[key].(type) {
	case float64: // JSON numbers
		return int(v)
	case int:
		return v
	}
	return def
}

// Integration reads data from a device, controller or piece of software.
// Implementations must be read-only: they never change device configuration.
type Integration interface {
	Info() Info
	// Collect returns a snapshot of every device reachable through this
	// integration. Optional data that cannot be read must be skipped, not fail
	// the whole collection.
	Collect(ctx context.Context, cfg Config) ([]model.Device, error)
	// Test checks connectivity and credentials, returning a short message.
	Test(ctx context.Context, cfg Config) (string, error)
}

// Validator is implemented by integrations that check settings beyond the
// field types (e.g. port ranges); it runs before settings are saved.
type Validator interface {
	Validate(cfg Config) error
}

// Registry holds the available integration types.
type Registry struct {
	mu    sync.RWMutex
	items map[string]Integration
}

func NewRegistry() *Registry {
	return &Registry{items: map[string]Integration{}}
}

func (r *Registry) Register(i Integration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[i.Info().Type] = i
}

func (r *Registry) Get(typ string) (Integration, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	i, ok := r.items[typ]
	if !ok {
		return nil, fmt.Errorf("unknown integration type %q", typ)
	}
	return i, nil
}

func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Info, 0, len(r.items))
	for _, i := range r.items {
		out = append(out, i.Info())
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Name < out[b].Name })
	return out
}

// SecretKeys returns the keys of the fields of type secret for an integration type.
func (r *Registry) SecretKeys(typ string) []string {
	i, err := r.Get(typ)
	if err != nil {
		return nil
	}
	var keys []string
	for _, f := range i.Info().Fields {
		if f.Type == model.FormFieldTypeSecret {
			keys = append(keys, f.Key)
		}
	}
	return keys
}
