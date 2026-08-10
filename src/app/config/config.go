package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings — единственный source of truth деплой-уровня. Всё, что настраивает
// владелец (SMTP, магазин), живёт в БД (таблица settings), не здесь.
type Settings struct {
	Host     string `yaml:"host"`
	LogPath  string `yaml:"log_path"`
	LogLevel string `yaml:"log_level"`
	Database string `yaml:"database"`
	// BaseURL — публичный адрес витрины (для sitemap, canonical, писем).
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
