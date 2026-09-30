package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type GitConfig struct {
	Repo         string        `yaml:"repo"`
	Branch       string        `yaml:"branch"`
	Path         string        `yaml:"path"`
	PollInterval time.Duration `yaml:"pollInterval"`
	Token        string        `yaml:"token"` // Optional: for private repos
}

type ServerConfig struct {
	Port          int    `yaml:"port"`
	WebhookSecret string `yaml:"webhookSecret"` // Secret required to trigger /api/v1/sync
}

type Config struct {
	Git             GitConfig    `yaml:"git"`
	Server          ServerConfig `yaml:"server"`
	TargetNamespace string       `yaml:"targetNamespace"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Defaults
	cfg := &Config{
		Git: GitConfig{
			Branch:       "main",
			Path:         ".",
			PollInterval: 30 * time.Second,
		},
		Server: ServerConfig{
			Port: 8080,
		},
		TargetNamespace: "default",
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config yaml: %w", err)
	}

	// Read git token from environment variable if present
	if envToken := os.Getenv("ANTCD_GIT_TOKEN"); envToken != "" {
		cfg.Git.Token = envToken
	}

	// Read webhook secret from environment variable if present
	if envSecret := os.Getenv("ANTCD_WEBHOOK_SECRET"); envSecret != "" {
		cfg.Server.WebhookSecret = envSecret
	}

	return cfg, nil
}
