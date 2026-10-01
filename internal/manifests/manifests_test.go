package manifests

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestCollect(t *testing.T) {
	items, err := Collect(filepath.Join("testdata", "good"))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.Kind+"@"+it.FileName)
	}
	sort.Strings(got)
	want := []string{"ConfigMap@multi.yaml", "Deployment@app.yml", "Namespace@multi.yaml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestCollectInvalidDocument(t *testing.T) {
	if _, err := Collect(filepath.Join("testdata", "bad")); err == nil {
		t.Error("expected error for document without kind")
	}
}

func TestCollectMissingDir(t *testing.T) {
	if _, err := Collect(filepath.Join("testdata", "nope")); err == nil {
		t.Error("expected error for missing directory")
	}
}
