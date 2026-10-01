package helm

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/ynotnauk/antcd/internal/k8s"
)

func opts(chart string) Options {
	return Options{
		Root:        "testdata",
		ChartPath:   filepath.Join("testdata", chart),
		ReleaseName: "rel",
	}
}

func kinds(items []k8s.ManifestItem) []string {
	var out []string
	for _, it := range items {
		out = append(out, it.Kind)
	}
	sort.Strings(out)
	return out
}

func find(t *testing.T, items []k8s.ManifestItem, kind string) string {
	t.Helper()
	for _, it := range items {
		if it.Kind == kind {
			return string(it.Data)
		}
	}
	t.Fatalf("no %s in %v", kind, kinds(items))
	return ""
}

func TestRenderBasic(t *testing.T) {
	items, err := Render(opts("basic"))
	if err != nil {
		t.Fatal(err)
	}

	// CRD, ConfigMap, Deployment and subchart Service; NOTES, partials, empty docs and hooks are dropped.
	got := strings.Join(kinds(items), ",")
	if want := "ConfigMap,CustomResourceDefinition,Deployment,Service"; got != want {
		t.Fatalf("kinds = %s, want %s", got, want)
	}

	dep := find(t, items, "Deployment")
	for _, want := range []string{"name: rel-basic", "namespace: default", "replicas: 1", "nginx:1.25"} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment missing %q:\n%s", want, dep)
		}
	}
}

func TestRenderNamespace(t *testing.T) {
	o := opts("basic")
	o.Namespace = "apps"
	items, err := Render(o)
	if err != nil {
		t.Fatal(err)
	}
	if dep := find(t, items, "Deployment"); !strings.Contains(dep, "namespace: apps") {
		t.Errorf("namespace not applied:\n%s", dep)
	}
}

func TestRenderValuesPrecedence(t *testing.T) {
	o := opts("basic")
	o.ValuesFiles = []string{"values-prod.yaml"}
	o.Values = map[string]interface{}{"image": map[string]interface{}{"repository": "custom"}}

	items, err := Render(o)
	if err != nil {
		t.Fatal(err)
	}
	dep := find(t, items, "Deployment")
	// replicas and tag come from the values file, repository from inline values (nested maps merge).
	for _, want := range []string{"replicas: 3", "custom:1.27"} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment missing %q:\n%s", want, dep)
		}
	}
	if cm := find(t, items, "ConfigMap"); !strings.Contains(cm, `tier: "web"`) {
		t.Errorf("chart default lost:\n%s", cm)
	}
}

func TestRenderValuesFileOutsideRoot(t *testing.T) {
	o := opts("basic")
	o.ValuesFiles = []string{"../../../go.mod"}
	if _, err := Render(o); err == nil || !strings.Contains(err.Error(), "within the repository") {
		t.Errorf("got %v", err)
	}
}

func TestRenderMissingValuesFile(t *testing.T) {
	o := opts("basic")
	o.ValuesFiles = []string{"nope.yaml"}
	if _, err := Render(o); err == nil {
		t.Error("expected error")
	}
}

func TestRenderErrors(t *testing.T) {
	tests := []struct{ name, chart, want string }{
		{"template error", "broken", "rendering templates"},
		{"unvendored dependency", "nodeps", "not vendored"},
		{"not a chart", "does-not-exist", "loading chart"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Render(opts(tt.chart))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("got %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestMergeMaps(t *testing.T) {
	base := map[string]interface{}{"a": 1, "n": map[string]interface{}{"x": 1, "y": 2}}
	over := map[string]interface{}{"a": 2, "n": map[string]interface{}{"y": 3}}
	got := mergeMaps(base, over)

	n := got["n"].(map[string]interface{})
	if got["a"] != 2 || n["x"] != 1 || n["y"] != 3 {
		t.Errorf("got %v", got)
	}
	if base["a"] != 1 || base["n"].(map[string]interface{})["y"] != 2 {
		t.Errorf("base mutated: %v", base)
	}
}
