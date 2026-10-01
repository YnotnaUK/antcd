package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/git"
	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/manifests"
	"github.com/ynotnauk/antcd/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	applier, err := k8s.NewApplier()
	if err != nil {
		log.Printf("[WARN] Running without active K8s cluster connection: %v", err)
	}

	syncTrigger := make(chan struct{}, 1)
	srv := server.NewServer(cfg.Server.Port, cfg.Server.WebhookSecret, syncTrigger)
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Interim: only the first repo's first target is reconciled until multi-repo support lands.
	repo := &cfg.Repos[0]
	log.Printf("AntCD started. Polling %s every %v", repo.URL, repo.PollInterval)
	watcher := git.NewWatcher(repo.URL, repo.Branch, repo.Token)

	ticker := time.NewTicker(repo.PollInterval)
	defer ticker.Stop()

	// Initial reconcile
	reconcile(watcher, applier, repo)

	for {
		select {
		case <-ticker.C:
			reconcile(watcher, applier, repo)
		case <-syncTrigger:
			log.Println("[TRIGGER] Manual/Webhook sync triggered!")
			reconcile(watcher, applier, repo)
		}
	}
}

func reconcile(w *git.Watcher, applier *k8s.Applier, repo *config.Repo) {
	changed, commit, err := w.CheckForUpdates()
	if err != nil {
		log.Printf("[ERROR] Git check failed: %v", err)
		return
	}

	if !changed {
		log.Printf("[UP TO DATE] Commit %s unchanged", commit)
		return
	}

	log.Printf("[SYNC] New commit %s detected. Syncing manifests...", commit)

	if applier == nil {
		log.Println("[WARN] Skipped apply: No cluster connected. Will retry on next check.")
		return
	}

	tempDir, err := os.MkdirTemp("", "antcd-*")
	if err != nil {
		log.Printf("[ERROR] Creating temp dir: %v", err)
		return
	}
	defer os.RemoveAll(tempDir)

	if err := w.CloneToDir(tempDir); err != nil {
		log.Printf("[ERROR] Clone failed: %v", err)
		return
	}

	manifestDir := filepath.Join(tempDir, repo.Targets[0].Path)
	if err := reconcileDirectory(manifestDir, applier, "default"); err != nil {
		log.Printf("[ERROR] Reconciliation failed: %v. Will retry.", err)
		return
	}

	w.RecordSuccess(commit)
	log.Printf("[SUCCESS] Successfully reconciled commit %s", commit)
}

func reconcileDirectory(dir string, applier *k8s.Applier, defaultNamespace string) error {
	// 1. Collect all manifests
	items, err := manifests.Collect(dir)
	if err != nil {
		return err
	}

	// 2. Sort manifests by dependency order
	k8s.SortManifests(items)

	// 3. Apply manifests in sorted order
	var applied []k8s.ResourceID
	ctx := context.Background()

	for _, item := range items {
		resID, err := applier.ApplyManifest(ctx, item.Data, defaultNamespace)
		if err != nil {
			return fmt.Errorf("applying %s (%s): %w", item.FileName, item.Kind, err)
		}
		applied = append(applied, *resID)
		log.Printf("[APPLIED] %s (%s)", item.FileName, item.Kind)
	}

	// 4. Prune resources deleted from Git
	if err := applier.Prune(ctx, applied); err != nil {
		log.Printf("[WARN] Pruning completed with warnings: %v", err)
	}

	return nil
}
