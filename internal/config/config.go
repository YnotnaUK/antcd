package config

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/util/validation"
)

const (
	TargetTypeManifests = "manifests"
	TargetTypeHelm      = "helm"
)

type ServerConfig struct {
	Port          int    `yaml:"port"`
	WebhookSecret string `yaml:"webhookSecret"` // Secret required to trigger /api/v1/sync
}

// Target is a path within a repository that is deployed as a unit.
type Target struct {
	Name        string         `yaml:"name"`
	Type        string         `yaml:"type"`
	Path        string         `yaml:"path"`
	ReleaseName string         `yaml:"releaseName"` // helm only, defaults to Name
	ValuesFiles []string       `yaml:"valuesFiles"` // helm only
	Values      map[string]any `yaml:"values"`      // helm only
}

// Repo is a Git repository with its own polling settings and one or more targets.
type Repo struct {
	Name         string        `yaml:"name"`
	URL          string        `yaml:"url"`
	Branch       string        `yaml:"branch"`
	PollInterval time.Duration `yaml:"pollInterval"`
	TokenEnv     string        `yaml:"tokenEnv"` // Optional: name of env var holding the token for private repos
	Targets      []Target      `yaml:"targets"`

	Token string `yaml:"-"` // Resolved from TokenEnv
}

type Config struct {
	Server ServerConfig `yaml:"server"`
	Repos  []Repo       `yaml:"repos"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return Parse(data)
}

// Parse decodes, applies defaults to, and validates a config document.
func Parse(data []byte) (*Config, error) {
	cfg := &Config{Server: ServerConfig{Port: 8080}}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(cfg); err != nil {
		return nil, fmt.Errorf("parsing config yaml: %w", err)
	}

	// Read webhook secret from environment variable if present
	if envSecret := os.Getenv("ANTCD_WEBHOOK_SECRET"); envSecret != "" {
		cfg.Server.WebhookSecret = envSecret
	}

	for i := range cfg.Repos {
		r := &cfg.Repos[i]
		if r.Branch == "" {
			r.Branch = "main"
		}
		if r.PollInterval == 0 {
			r.PollInterval = 30 * time.Second
		}
		if r.TokenEnv != "" {
			r.Token = os.Getenv(r.TokenEnv)
		}
		for j := range r.Targets {
			t := &r.Targets[j]
			if t.Type == "" {
				t.Type = TargetTypeManifests
			}
			if t.Type == TargetTypeHelm && t.ReleaseName == "" {
				t.ReleaseName = t.Name
			}
		}
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}
	return cfg, nil
}

func (c *Config) validate() error {
	var errs []error
	if len(c.Repos) == 0 {
		return errors.New("at least one repo is required")
	}

	repoNames := map[string]bool{}
	for i, r := range c.Repos {
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("repos[%d]: name is required", i))
		} else if repoNames[r.Name] {
			errs = append(errs, fmt.Errorf("repos[%d]: duplicate name %q", i, r.Name))
		}
		repoNames[r.Name] = true
		errs = append(errs, labelValueErrors(fmt.Sprintf("repo %q name", r.Name), r.Name)...)

		if r.URL == "" {
			errs = append(errs, fmt.Errorf("repo %q: url is required", r.Name))
		}
		if r.PollInterval < 0 {
			errs = append(errs, fmt.Errorf("repo %q: pollInterval must be positive", r.Name))
		}
		if len(r.Targets) == 0 {
			errs = append(errs, fmt.Errorf("repo %q: at least one target is required", r.Name))
		}

		targetNames := map[string]bool{}
		for j, t := range r.Targets {
			if t.Name == "" {
				errs = append(errs, fmt.Errorf("repo %q targets[%d]: name is required", r.Name, j))
			} else if targetNames[t.Name] {
				errs = append(errs, fmt.Errorf("repo %q targets[%d]: duplicate name %q", r.Name, j, t.Name))
			}
			targetNames[t.Name] = true
			errs = append(errs, labelValueErrors(fmt.Sprintf("repo %q target %q name", r.Name, t.Name), t.Name)...)

			if t.Path == "" {
				errs = append(errs, fmt.Errorf("repo %q target %q: path is required", r.Name, t.Name))
			} else if !filepath.IsLocal(t.Path) {
				errs = append(errs, fmt.Errorf("repo %q target %q: path %q must be relative and stay within the repository", r.Name, t.Name, t.Path))
			}
			switch t.Type {
			case TargetTypeManifests:
				if t.ReleaseName != "" || len(t.ValuesFiles) > 0 || len(t.Values) > 0 {
					errs = append(errs, fmt.Errorf("repo %q target %q: releaseName, valuesFiles and values are only valid for type %q", r.Name, t.Name, TargetTypeHelm))
				}
			case TargetTypeHelm:
			default:
				errs = append(errs, fmt.Errorf("repo %q target %q: unknown type %q (want %q or %q)", r.Name, t.Name, t.Type, TargetTypeManifests, TargetTypeHelm))
			}
		}
	}
	return errors.Join(errs...)
}

// labelValueErrors checks that v can be used as a Kubernetes label value, as names are stamped onto resources.
func labelValueErrors(what, v string) []error {
	var errs []error
	for _, msg := range validation.IsValidLabelValue(v) {
		errs = append(errs, fmt.Errorf("%s %q is not a valid label value: %s", what, v, msg))
	}
	return errs
}
