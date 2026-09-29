package main

import (
	"flag"
	"log"

	"github.com/ynotnauk/antcd/internal/config"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	log.Printf("AntCD configured successfully!")
	log.Printf("Target Repo: %s (branch: %s)", cfg.Git.Repo, cfg.Git.Branch)
	log.Printf("Poll Interval: %v", cfg.Git.PollInterval)
	log.Printf("Server Port: %d", cfg.Server.Port)
}
