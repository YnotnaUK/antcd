package reconciler

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/k8s"
)

type fakeSource struct {
	changed  bool
	commit   string
	err      error
	clones   int
	recorded []string
	files    map[string]string
}

func (f *fakeSource) CheckForUpdates() (bool, string, error) { return f.changed, f.commit, f.err }
func (f *fakeSource) RecordSuccess(c string)                 { f.recorded = append(f.recorded, c) }
func (f *fakeSource) CloneToDir(dir string) error {
	f.clones++
	for name, body := range f.files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

type fakeApplier struct {
	applied map[k8s.Scope][]string
	pruned  []k8s.Scope
}

func (f *fakeApplier) ApplyManifest(_ context.Context, data []byte, scope k8s.Scope) (*k8s.ResourceID, error) {
	name := string(data)
	if f.applied == nil {
		f.applied = map[k8s.Scope][]string{}
	}
	f.applied[scope] = append(f.applied[scope], name)
	return &k8s.ResourceID{Name: name}, nil
}

func (f *fakeApplier) Prune(_ context.Context, scope k8s.Scope, _ []k8s.ResourceID) error {
	f.pruned = append(f.pruned, scope)
	return nil
}

const cmA = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: a\n"
const cmB = "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: b\n"

func testRepo(targets ...config.Target) config.Repo {
	return config.Repo{Name: "r", URL: "u", Targets: targets}
}

func manifestsTarget(name, path string) config.Target {
	return config.Target{Name: name, Type: config.TargetTypeManifests, Path: path}
}

func TestReconcileAllTargetsRecordsCommit(t *testing.T) {
	src := &fakeSource{changed: true, commit: "c1", files: map[string]string{"a/a.yaml": cmA, "b/b.yaml": cmB}}
	app := &fakeApplier{}
	NewRepo(testRepo(manifestsTarget("one", "a"), manifestsTarget("two", "b")), src, app).Reconcile(context.Background())

	if len(src.recorded) != 1 || src.recorded[0] != "c1" {
		t.Errorf("recorded = %v", src.recorded)
	}
	if len(app.applied[k8s.Scope{Repo: "r", Target: "one"}]) != 1 || len(app.applied[k8s.Scope{Repo: "r", Target: "two"}]) != 1 {
		t.Errorf("applied = %v", app.applied)
	}
	if len(app.pruned) != 2 {
		t.Errorf("pruned = %v", app.pruned)
	}
}

func TestReconcileFailingTargetDoesNotBlockOthersOrRecord(t *testing.T) {
	src := &fakeSource{changed: true, commit: "c1", files: map[string]string{"a/a.yaml": cmA, "b/b.yaml": cmB}}
	app := &fakeApplier{}
	repo := testRepo(manifestsTarget("bad", "missing"), manifestsTarget("good", "b"))
	NewRepo(repo, src, app).Reconcile(context.Background())

	if len(src.recorded) != 0 {
		t.Errorf("commit must not be recorded, got %v", src.recorded)
	}
	if len(app.applied[k8s.Scope{Repo: "r", Target: "good"}]) != 1 {
		t.Errorf("good target should still apply, got %v", app.applied)
	}
}

func TestReconcileApplyFailureSkipsPrune(t *testing.T) {
	src := &fakeSource{changed: true, commit: "c1", files: map[string]string{"a/a.yaml": cmA}}
	app := &fakeApplier{}
	failing := &alwaysFailApplier{fakeApplier: app}
	NewRepo(testRepo(manifestsTarget("one", "a")), src, failing).Reconcile(context.Background())

	if len(app.pruned) != 0 || len(src.recorded) != 0 {
		t.Errorf("pruned=%v recorded=%v", app.pruned, src.recorded)
	}
}

type alwaysFailApplier struct{ *fakeApplier }

func (alwaysFailApplier) ApplyManifest(context.Context, []byte, k8s.Scope) (*k8s.ResourceID, error) {
	return nil, errors.New("boom")
}

func TestReconcileUnchangedDoesNothing(t *testing.T) {
	src := &fakeSource{changed: false, commit: "c1"}
	NewRepo(testRepo(manifestsTarget("one", "a")), src, &fakeApplier{}).Reconcile(context.Background())
	if src.clones != 0 || len(src.recorded) != 0 {
		t.Errorf("clones=%d recorded=%v", src.clones, src.recorded)
	}
}

func TestReconcileNoClusterSkips(t *testing.T) {
	src := &fakeSource{changed: true, commit: "c1"}
	NewRepo(testRepo(manifestsTarget("one", "a")), src, nil).Reconcile(context.Background())
	if src.clones != 0 || len(src.recorded) != 0 {
		t.Errorf("clones=%d recorded=%v", src.clones, src.recorded)
	}
}

func TestReconcileGitErrorDoesNothing(t *testing.T) {
	src := &fakeSource{err: errors.New("down")}
	NewRepo(testRepo(manifestsTarget("one", "a")), src, &fakeApplier{}).Reconcile(context.Background())
	if src.clones != 0 || len(src.recorded) != 0 {
		t.Errorf("clones=%d recorded=%v", src.clones, src.recorded)
	}
}

func TestFanOut(t *testing.T) {
	in := make(chan struct{}, 1)
	a, b := make(chan struct{}, 1), make(chan struct{}, 1)
	b <- struct{}{} // already pending: must not block

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { FanOut(ctx, in, []chan struct{}{a, b}); close(done) }()

	in <- struct{}{}
	<-a
	cancel()
	<-done
	if len(b) != 1 {
		t.Errorf("pending signal should be kept, len=%d", len(b))
	}
}
