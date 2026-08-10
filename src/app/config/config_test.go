package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(p, []byte("settings:\n  host: \"\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Settings.Host != "127.0.0.1:9097" {
		t.Errorf("host default: %q", cfg.Settings.Host)
	}
	if cfg.Settings.Database != "~/.fastoshop/fastoshop.db" {
		t.Errorf("db default: %q", cfg.Settings.Database)
	}
	if cfg.Settings.BaseURL != "http://localhost:9097" {
		t.Errorf("base_url default: %q", cfg.Settings.BaseURL)
	}
}
