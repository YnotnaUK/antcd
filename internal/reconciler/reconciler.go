package reconciler

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/helm"
	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/manifests"
)

// Applier applies manifests to a cluster and prunes stale resources.
type Applier interface {
	ApplyManifest(ctx context.Context, yamlData []byte, scope k8s.Scope) (*k8s.ResourceID, error)
	Prune(ctx context.Context, scope k8s.Scope, applied []k8s.ResourceID) error
}

// Source tracks a Git branch and checks it out on demand.
type Source interface {
	CheckForUpdates() (changed bool, commit string, err error)
	CloneToDir(dir string) error
	RecordSuccess(commit string)
}

// Repo reconciles every target of a single repository.
type Repo struct {
	cfg     config.Repo
	source  Source
	applier Applier // nil when no cluster is connected
}

func NewRepo(cfg config.Repo, source Source, applier Applier) *Repo {
	return &Repo{cfg: cfg, source: source, applier: applier}
}

// Run reconciles immediately, then on every poll tick or trigger until ctx is cancelled.
func (r *Repo) Run(ctx context.Context, trigger <-chan struct{}) {
	log.Printf("[%s] Polling %s every %v", r.cfg.Name, r.cfg.URL, r.cfg.PollInterval)

	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	r.Reconcile(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.Reconcile(ctx)
		case <-trigger:
			log.Printf("[%s] [TRIGGER] Manual/Webhook sync triggered!", r.cfg.Name)
			r.Reconcile(ctx)
		}
	}
}

// Reconcile syncs all targets if the remote has a new commit. The commit is only
// recorded when every target succeeds, so failures are retried on the next check.
func (r *Repo) Reconcile(ctx context.Context) {
	name := r.cfg.Name

	changed, commit, err := r.source.CheckForUpdates()
	if err != nil {
		log.Printf("[%s] [ERROR] Git check failed: %v", name, err)
		return
	}
	if !changed {
		log.Printf("[%s] [UP TO DATE] Commit %s unchanged", name, commit)
		return
	}

	log.Printf("[%s] [SYNC] New commit %s detected. Syncing targets...", name, commit)

	if r.applier == nil {
		log.Printf("[%s] [WARN] Skipped apply: No cluster connected. Will retry on next check.", name)
		return
	}

	tempDir, err := os.MkdirTemp("", "antcd-*")
	if err != nil {
		log.Printf("[%s] [ERROR] Creating temp dir: %v", name, err)
		return
	}
	defer os.RemoveAll(tempDir)

	if err := r.source.CloneToDir(tempDir); err != nil {
		log.Printf("[%s] [ERROR] Clone failed: %v", name, err)
		return
	}

	failed := false
	for _, t := range r.cfg.Targets {
		if err := r.reconcileTarget(ctx, tempDir, t); err != nil {
			log.Printf("[%s] [ERROR] Target %s failed: %v. Will retry.", name, t.Name, err)
			failed = true
		}
	}
	if failed {
		return
	}

	r.source.RecordSuccess(commit)
	log.Printf("[%s] [SUCCESS] Successfully reconciled commit %s", name, commit)
}

func (r *Repo) reconcileTarget(ctx context.Context, root string, t config.Target) error {
	scope := k8s.Scope{Repo: r.cfg.Name, Target: t.Name}

	var items []k8s.ManifestItem
	var err error
	switch t.Type {
	case config.TargetTypeManifests:
		items, err = manifests.Collect(filepath.Join(root, t.Path))
	case config.TargetTypeHelm:
		items, err = helm.Render(helm.Options{
			Root:        root,
			ChartPath:   filepath.Join(root, t.Path),
			ReleaseName: t.ReleaseName,
			ValuesFiles: t.ValuesFiles,
			Values:      t.Values,
		})
	default:
		err = fmt.Errorf("target type %q is not supported yet", t.Type)
	}
	if err != nil {
		return err
	}

	k8s.SortManifests(items)

	applied := make([]k8s.ResourceID, 0, len(items))
	for _, item := range items {
		resID, err := r.applier.ApplyManifest(ctx, item.Data, scope)
		if err != nil {
			return fmt.Errorf("applying %s (%s): %w", item.FileName, item.Kind, err)
		}
		applied = append(applied, *resID)
		log.Printf("[%s/%s] [APPLIED] %s (%s)", scope.Repo, scope.Target, item.FileName, item.Kind)
	}

	if err := r.applier.Prune(ctx, scope, applied); err != nil {
		log.Printf("[%s/%s] [WARN] Pruning completed with warnings: %v", scope.Repo, scope.Target, err)
	}
	return nil
}

// FanOut forwards each signal received on in to every out channel without blocking.
// A channel that already has a pending signal is skipped.
func FanOut(ctx context.Context, in <-chan struct{}, out []chan struct{}) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-in:
			for _, c := range out {
				select {
				case c <- struct{}{}:
				default:
				}
			}
		}
	}
}
