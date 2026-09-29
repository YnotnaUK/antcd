package main

import (
	"bufio"
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/git"
	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/server"
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
	srv := server.NewServer(cfg.Server.Port, syncTrigger)
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

	log.Printf("[SYNC] New commit %s. Syncing manifests...", commit)

	// Create temp directory for cloning
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

	if applier == nil {
		log.Println("[WARN] Skipped K8s apply: No cluster connected")
		return
	}

	manifestDir := filepath.Join(tempDir, cfg.Git.Path)
	applyDirectory(manifestDir, applier, cfg.TargetNamespace)
}

func applyDirectory(dir string, applier *k8s.Applier, defaultNamespace string) {
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
			log.Printf("[ERROR] Reading %s: %v", path, err)
			return nil
		}

		// Split multi-document YAMLs (separated by ---)
		decoder := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(data)))
		for {
			doc, err := decoder.Read()
			if err == io.EOF {
				break
			}
			if err != nil || len(bytes.TrimSpace(doc)) == 0 {
				continue
			}

			if err := applier.ApplyManifest(context.Background(), doc, defaultNamespace); err != nil {
				log.Printf("[ERROR] Applying %s: %v", filepath.Base(path), err)
			} else {
				log.Printf("[APPLIED] Successfully applied manifest from %s", filepath.Base(path))
			}
		}
		return nil
	})

	if err != nil {
		log.Printf("[ERROR] Walking directory %s: %v", dir, err)
	}
}
