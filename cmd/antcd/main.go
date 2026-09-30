package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/git"
	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/server"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	serializerYaml "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
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

	log.Printf("AntCD started. Polling %s every %v", cfg.Git.Repo, cfg.Git.PollInterval)
	watcher := git.NewWatcher(cfg.Git.Repo, cfg.Git.Branch, cfg.Git.Token)

	ticker := time.NewTicker(cfg.Git.PollInterval)
	defer ticker.Stop()

	// Initial reconcile
	reconcile(watcher, applier, cfg)

	for {
		select {
		case <-ticker.C:
			reconcile(watcher, applier, cfg)
		case <-syncTrigger:
			log.Println("[TRIGGER] Manual/Webhook sync triggered!")
			reconcile(watcher, applier, cfg)
		}
	}
}

func reconcile(w *git.Watcher, applier *k8s.Applier, cfg *config.Config) {
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

	manifestDir := filepath.Join(tempDir, cfg.Git.Path)
	if err := reconcileDirectory(manifestDir, applier, cfg.TargetNamespace); err != nil {
		log.Printf("[ERROR] Reconciliation failed: %v. Will retry.", err)
		return
	}

	w.RecordSuccess(commit)
	log.Printf("[SUCCESS] Successfully reconciled commit %s", commit)
}

func reconcileDirectory(dir string, applier *k8s.Applier, defaultNamespace string) error {
	var items []k8s.ManifestItem
	dec := serializerYaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)

	// 1. Collect all manifests
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}

		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}

		reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for {
			doc, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil || len(bytes.TrimSpace(doc)) == 0 {
				continue
			}

			obj := &unstructured.Unstructured{}
			_, gvk, err := dec.Decode(doc, nil, obj)
			if err != nil {
				return fmt.Errorf("decoding kind from %s: %w", filepath.Base(path), err)
			}

			items = append(items, k8s.ManifestItem{
				Kind:     gvk.Kind,
				Data:     doc,
				FileName: filepath.Base(path),
			})
		}
		return nil
	})

	if err != nil {
		return fmt.Errorf("reading manifests: %w", err)
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
