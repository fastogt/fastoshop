package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings - the single source of truth at deploy level. Everything the owner
// configures (SMTP, shop) lives in the DB (the settings table), not here.
type Settings struct {
	Host     string `yaml:"host"`
	LogLevel string `yaml:"log_level"`
	// LogPath - where the log is written so the owner can read it from the admin.
	// Empty keeps the log on stdout, which is where the operator's journald picks
	// it up; a shop owner has no shell and would never see it there.
	LogPath  string `yaml:"log_path"`
	Database string `yaml:"database"`
	// BaseURL - the public storefront address (for sitemap, canonical, emails).
	BaseURL string `yaml:"base_url"`
}

type Config struct {
	Settings Settings `yaml:"settings"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if cfg.Settings.Host == "" {
		cfg.Settings.Host = "127.0.0.1:9097"
	}
	if cfg.Settings.LogLevel == "" {
		cfg.Settings.LogLevel = "INFO"
	}
	if cfg.Settings.Database == "" {
		cfg.Settings.Database = "~/.fastoshop/fastoshop.db"
	}
	if cfg.Settings.BaseURL == "" {
		cfg.Settings.BaseURL = "http://localhost:9097"
	}
	return &cfg, nil
}
