package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	t.Setenv("MY_TOKEN", "abc")
	cfg, err := Parse([]byte(`
repos:
  - name: r1
    url: https://example.com/r1.git
    tokenEnv: MY_TOKEN
    targets:
      - {name: a, path: ./m}
      - {name: b, type: helm, path: ./c}
`))
	if err != nil {
		t.Fatal(err)
	}
	r := cfg.Repos[0]
	if cfg.Server.Port != 8080 || r.Branch != "main" || r.PollInterval != 30*time.Second || r.Token != "abc" {
		t.Errorf("unexpected defaults: %+v %+v", cfg.Server, r)
	}
	if r.Targets[0].Type != TargetTypeManifests {
		t.Errorf("type default = %q", r.Targets[0].Type)
	}
	if r.Targets[1].ReleaseName != "b" {
		t.Errorf("releaseName default = %q", r.Targets[1].ReleaseName)
	}
}

func TestParseMultiRepo(t *testing.T) {
	cfg, err := Parse([]byte(`
repos:
  - name: r1
    url: u1
    pollInterval: 10s
    targets: [{name: a, path: .}]
  - name: r2
    url: u2
    pollInterval: 5m
    targets: [{name: a, path: .}]
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[1].PollInterval != 5*time.Minute {
		t.Errorf("unexpected repos: %+v", cfg.Repos)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name, yaml, want string
	}{
		{"no repos", "server: {port: 1}", "at least one repo"},
		{"unknown field", "git: {repo: x}", "field git not found"},
		{"missing url", "repos: [{name: r, targets: [{name: a, path: .}]}]", "url is required"},
		{"missing name", "repos: [{url: u, targets: [{name: a, path: .}]}]", "name is required"},
		{"no targets", "repos: [{name: r, url: u}]", "at least one target"},
		{"dup repo", "repos: [{name: r, url: u, targets: [{name: a, path: .}]}, {name: r, url: u, targets: [{name: a, path: .}]}]", "duplicate name"},
		{"dup target", "repos: [{name: r, url: u, targets: [{name: a, path: .}, {name: a, path: .}]}]", "duplicate name"},
		{"missing path", "repos: [{name: r, url: u, targets: [{name: a}]}]", "path is required"},
		{"bad type", "repos: [{name: r, url: u, targets: [{name: a, type: kustomize, path: .}]}]", "unknown type"},
		{"helm field on manifests", "repos: [{name: r, url: u, targets: [{name: a, path: ., valuesFiles: [v.yaml]}]}]", "only valid for type"},
		{"bad repo label", "repos: [{name: 'my repo', url: u, targets: [{name: a, path: .}]}]", "valid label value"},
		{"bad target label", "repos: [{name: r, url: u, targets: [{name: 'a/b', path: .}]}]", "valid label value"},
		{"negative interval", "repos: [{name: r, url: u, pollInterval: -1s, targets: [{name: a, path: .}]}]", "pollInterval"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse([]byte(tt.yaml))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}
