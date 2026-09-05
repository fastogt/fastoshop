package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	body := "settings:\n  host: \"\"\n  base_url: \"https://shop.example.com\"\n"
	if err := os.WriteFile(p, []byte(body), 0644); err != nil {
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
}

func TestLoadRequiresBaseURL(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.conf")
	if err := os.WriteFile(p, []byte("settings:\n  host: \"\"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(p); err == nil {
		t.Fatal("want an error without base_url, got none")
	}
}
