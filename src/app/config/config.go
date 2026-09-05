package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings is deploy-level only; everything the owner configures lives in the DB.
type Settings struct {
	Host     string `yaml:"host"`
	LogLevel string `yaml:"log_level"`
	// LogPath empty keeps the log on stdout, where a shop owner with no shell never sees it.
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
	// No default: a guessed public address reaches the buyer, in sitemap and canonical.
	if cfg.Settings.BaseURL == "" {
		return nil, fmt.Errorf("%s: base_url is required", path)
	}
	return &cfg, nil
}
