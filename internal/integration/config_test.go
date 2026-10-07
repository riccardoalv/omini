package integration

import (
	"testing"

	"github.com/riccardoalv/omini/internal/model"
	"github.com/riccardoalv/omini/internal/secret"
)

var fields = []model.FormField{
	{Key: "host", Type: model.FormFieldTypeHost, Required: true},
	{Key: "community", Type: model.FormFieldTypeSecret, Required: true, Default: "public"},
	{Key: "port", Type: model.FormFieldTypeInt, Default: 161},
}

func TestNormalize(t *testing.T) {
	cfg, err := Normalize(fields, Config{"host": " 192.168.1.2 ", "unknown": "x"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg["host"] != "192.168.1.2" || cfg["community"] != "public" || cfg["port"] != 161 {
		t.Fatalf("unexpected config: %v", cfg)
	}
	if _, ok := cfg["unknown"]; ok {
		t.Fatal("unknown keys must be dropped")
	}
	if _, err := Normalize(fields, Config{}); err == nil {
		t.Fatal("expected missing host error")
	}
	if _, err := Normalize(fields, Config{"host": "h", "port": "abc"}); err == nil {
		t.Fatal("expected type error for port")
	}
}

func TestSecretsLifecycle(t *testing.T) {
	key := make([]byte, 32)
	box, _ := secret.New(key)
	cfg := Config{"host": "h", "community": "s3cret", "port": float64(161)}

	sealed, err := SealSecrets(box, fields, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sealed["community"] == "s3cret" || sealed["host"] != "h" {
		t.Fatalf("only secret fields must be sealed: %v", sealed)
	}

	masked := MaskSecrets(fields, sealed)
	if masked["community"] != Masked {
		t.Fatalf("secret not masked: %v", masked)
	}

	// The UI sends the masked value back: the stored secret must be kept.
	kept := KeepMaskedSecrets(fields, masked, sealed)
	opened, err := OpenSecrets(box, fields, kept)
	if err != nil {
		t.Fatal(err)
	}
	if opened["community"] != "s3cret" {
		t.Fatalf("secret lost after a masked round trip: %v", opened)
	}
}

func TestURLFieldsAcceptBareAddresses(t *testing.T) {
	fields := []model.FormField{{Key: "url", Type: model.FormFieldTypeURL, Required: true}}
	for in, want := range map[string]string{
		"192.168.1.1":           "https://192.168.1.1",
		" fw.lan:8443/ ":        "https://fw.lan:8443",
		"http://192.168.1.1":    "http://192.168.1.1",
		"https://opnsense.lan/": "https://opnsense.lan",
	} {
		out, err := Normalize(fields, Config{"url": in})
		if err != nil || out["url"] != want {
			t.Errorf("%q: got %v, %v; want %q", in, out["url"], err, want)
		}
	}
	for _, bad := range []string{"ftp://fw", "https://", "://x"} {
		if _, err := Normalize(fields, Config{"url": bad}); err == nil {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
