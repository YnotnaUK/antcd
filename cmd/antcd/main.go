package main

import (
	"flag"
	"log"
	"time"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/git"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("Starting AntCD... Polling %s every %v", cfg.Git.Repo, cfg.Git.PollInterval)

	watcher := git.NewWatcher(cfg.Git.Repo, cfg.Git.Branch, cfg.Git.Token)

	ticker := time.NewTicker(cfg.Git.PollInterval)
	defer ticker.Stop()

	// Initial check
	checkRepo(watcher)

	// Polling loop
	for range ticker.C {
		checkRepo(watcher)
	}
}

func checkRepo(w *git.Watcher) {
	changed, commit, err := w.CheckForUpdates()
	if err != nil {
		log.Printf("Error checking repo: %v", err)
		return
	}

	if changed {
		log.Printf("[SYNC NEEDED] New commit detected: %s", commit)
	} else {
		log.Printf("[UP TO DATE] Commit unchanged: %s", commit)
	}
}
