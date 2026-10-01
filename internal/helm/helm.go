// Package helm renders Helm charts to plain manifests. Charts are rendered
// client-side only: no release is created, and hooks and cluster lookups are
// not supported.
package helm

import (
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/manifests"
	"gopkg.in/yaml.v3"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

const hookAnnotation = "helm.sh/hook"

// Options describes how to render a chart.
type Options struct {
	Root        string                 // Directory that ValuesFiles must stay within (the repository checkout)
	ChartPath   string                 // Directory containing Chart.yaml
	ReleaseName string                 // Exposed to templates as .Release.Name
	Namespace   string                 // Exposed to templates as .Release.Namespace; defaults to k8s.DefaultNamespace
	ValuesFiles []string               // Relative to ChartPath, applied in order over the chart defaults
	Values      map[string]interface{} // Inline values, applied last
}

// Render renders the chart and returns its manifests, including CRDs from the crds/ directory.
// Helm hooks are skipped, as hooks are not supported.
func Render(opts Options) ([]k8s.ManifestItem, error) {
	if opts.Namespace == "" {
		opts.Namespace = k8s.DefaultNamespace
	}

	chrt, err := loader.Load(opts.ChartPath)
	if err != nil {
		return nil, fmt.Errorf("loading chart: %w", err)
	}
	if chrt.Metadata.Type != "" && chrt.Metadata.Type != "application" {
		return nil, fmt.Errorf("chart type %q cannot be deployed", chrt.Metadata.Type)
	}
	if err := checkDependencies(chrt); err != nil {
		return nil, err
	}

	userValues, err := loadValues(opts)
	if err != nil {
		return nil, err
	}

	renderValues, err := chartutil.ToRenderValues(chrt, userValues, chartutil.ReleaseOptions{
		Name:      opts.ReleaseName,
		Namespace: opts.Namespace,
		Revision:  1,
		IsInstall: true,
	}, chartutil.DefaultCapabilities)
	if err != nil {
		return nil, fmt.Errorf("preparing values: %w", err)
	}

	rendered, err := engine.Render(chrt, renderValues)
	if err != nil {
		return nil, fmt.Errorf("rendering templates: %w", err)
	}

	var items []k8s.ManifestItem

	for _, crd := range chrt.CRDObjects() {
		docs, err := manifests.ParseDocuments(crd.File.Data, crd.Filename)
		if err != nil {
			return nil, err
		}
		items = append(items, docs...)
	}

	names := make([]string, 0, len(rendered))
	for name := range rendered {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		base := filepath.Base(name)
		if base == "NOTES.txt" || strings.HasPrefix(base, "_") {
			continue
		}
		docs, err := manifests.ParseDocuments([]byte(rendered[name]), name)
		if err != nil {
			return nil, err
		}
		for _, doc := range docs {
			if isHook(doc.Data) {
				log.Printf("[WARN] helm: skipping hook %s (%s): hooks are not supported", doc.FileName, doc.Kind)
				continue
			}
			items = append(items, doc)
		}
	}

	return items, nil
}

// checkDependencies fails if a declared dependency has not been vendored into charts/.
func checkDependencies(chrt *chart.Chart) error {
	present := map[string]bool{}
	for _, d := range chrt.Dependencies() {
		present[d.Name()] = true
	}

	var missing []string
	for _, dep := range chrt.Metadata.Dependencies {
		name := dep.Name
		if dep.Alias != "" {
			name = dep.Alias
		}
		if !present[name] && !present[dep.Name] {
			missing = append(missing, dep.Name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("chart dependencies not vendored: %s (run 'helm dependency build' and commit charts/)", strings.Join(missing, ", "))
	}
	return nil
}

func loadValues(opts Options) (map[string]interface{}, error) {
	merged := map[string]interface{}{}

	for _, f := range opts.ValuesFiles {
		path := filepath.Join(opts.ChartPath, f)
		rel, err := filepath.Rel(opts.Root, path)
		if err != nil || !filepath.IsLocal(rel) {
			return nil, fmt.Errorf("values file %q must stay within the repository", f)
		}
		vals, err := chartutil.ReadValuesFile(path)
		if err != nil {
			return nil, fmt.Errorf("reading values file %q: %w", f, err)
		}
		merged = mergeMaps(merged, vals)
	}

	return mergeMaps(merged, opts.Values), nil
}

// mergeMaps returns base with override merged in; nested maps merge recursively and other values are replaced.
func mergeMaps(base, override map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(base))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range override {
		if ov, ok := v.(map[string]interface{}); ok {
			if bv, ok := out[k].(map[string]interface{}); ok {
				out[k] = mergeMaps(bv, ov)
				continue
			}
		}
		out[k] = v
	}
	return out
}

func isHook(doc []byte) bool {
	var m struct {
		Metadata struct {
			Annotations map[string]string `yaml:"annotations"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(doc, &m); err != nil {
		return false
	}
	_, ok := m.Metadata.Annotations[hookAnnotation]
	return ok
}
