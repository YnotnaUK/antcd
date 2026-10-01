package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/ynotnauk/antcd/internal/config"
	"github.com/ynotnauk/antcd/internal/git"
	"github.com/ynotnauk/antcd/internal/k8s"
	"github.com/ynotnauk/antcd/internal/reconciler"
	"github.com/ynotnauk/antcd/internal/server"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Keep the interface nil when there is no cluster; a nil *Applier would not be.
	var applier reconciler.Applier
	if a, err := k8s.NewApplier(); err != nil {
		log.Printf("[WARN] Running without active K8s cluster connection: %v", err)
	} else {
		applier = a
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	syncTrigger := make(chan struct{}, 1)
	srv := server.NewServer(cfg.Server.Port, cfg.Server.WebhookSecret, syncTrigger)
	go func() {
		if err := srv.Start(); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	}()

	var wg sync.WaitGroup
	triggers := make([]chan struct{}, len(cfg.Repos))
	for i, repo := range cfg.Repos {
		triggers[i] = make(chan struct{}, 1)
		watcher := git.NewWatcher(repo.URL, repo.Branch, repo.Token)
		r := reconciler.NewRepo(repo, watcher, applier)

		wg.Add(1)
		go func(trigger <-chan struct{}) {
			defer wg.Done()
			r.Run(ctx, trigger)
		}(triggers[i])
	}

	go reconciler.FanOut(ctx, syncTrigger, triggers)

	log.Printf("AntCD started with %d repo(s)", len(cfg.Repos))
	wg.Wait()
}
