package integration

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
)

// Masked replaces secret values in API responses. Sending it back on update
// means "keep the stored value".
const Masked = "********"

// Normalize applies defaults, drops unknown keys and checks required fields.
func Normalize(fields []model.FormField, cfg Config) (Config, error) {
	out := Config{}
	var missing []string
	for _, f := range fields {
		v, ok := cfg[f.Key]
		if !ok || v == nil || v == "" {
			if f.Default != nil {
				out[f.Key] = f.Default
				continue
			}
			if f.Required {
				missing = append(missing, f.Key)
			}
			continue
		}
		switch f.Type {
		case model.FormFieldTypeInt:
			n, ok := v.(float64)
			if !ok {
				return nil, fmt.Errorf("field %q must be a number", f.Key)
			}
			v = n
		case model.FormFieldTypeBool:
			if _, ok := v.(bool); !ok {
				return nil, fmt.Errorf("field %q must be true or false", f.Key)
			}
		case model.FormFieldTypeURL:
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("field %q must be text", f.Key)
			}
			u, err := normalizeURL(s)
			if err != nil {
				return nil, fmt.Errorf("field %q: %w", f.Key, err)
			}
			v = u
		default:
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("field %q must be text", f.Key)
			}
			v = strings.TrimSpace(s)
		}
		out[f.Key] = v
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// KeepMaskedSecrets replaces secret values equal to Masked with the stored ones.
func KeepMaskedSecrets(fields []model.FormField, cfg, stored Config) Config {
	out := Config{}
	for k, v := range cfg {
		out[k] = v
	}
	for _, f := range fields {
		if f.Type == model.FormFieldTypeSecret && out[f.Key] == Masked {
			out[f.Key] = stored[f.Key]
		}
	}
	return out
}

// SealSecrets encrypts the values of secret fields, for storage.
func SealSecrets(box *secret.Box, fields []model.FormField, cfg Config) (Config, error) {
	return mapSecrets(fields, cfg, box.Seal)
}

// OpenSecrets decrypts the values of secret fields, for use by integrations.
func OpenSecrets(box *secret.Box, fields []model.FormField, cfg Config) (Config, error) {
	return mapSecrets(fields, cfg, box.Open)
}

// MaskSecrets hides the values of secret fields, for API responses.
func MaskSecrets(fields []model.FormField, cfg Config) Config {
	out, _ := mapSecrets(fields, cfg, func(v string) (string, error) {
		if v == "" {
			return "", nil
		}
		return Masked, nil
	})
	return out
}

func mapSecrets(fields []model.FormField, cfg Config, fn func(string) (string, error)) (Config, error) {
	out := Config{}
	for k, v := range cfg {
		out[k] = v
	}
	for _, f := range fields {
		if f.Type != model.FormFieldTypeSecret {
			continue
		}
		s, ok := out[f.Key].(string)
		if !ok {
			continue
		}
		v, err := fn(s)
		if err != nil {
			return nil, fmt.Errorf("field %q: %w", f.Key, err)
		}
		out[f.Key] = v
	}
	return out, nil
}

// normalizeURL accepts an address with or without scheme: "192.168.1.1" and
// "fw.lan:8443" become https://...; only http and https are allowed.
func normalizeURL(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return "", fmt.Errorf("enter an address like https://192.168.1.1 or 192.168.1.1")
	}
	return strings.TrimRight(u.String(), "/"), nil
}
