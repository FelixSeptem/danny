package main

import (
	"context"
	"fmt"
	"os"

	"github.com/FelixSeptem/danny/internal/config"
	"github.com/FelixSeptem/danny/internal/storage"
)

func main() {
	cfg, err := config.LoadFromEnv(os.LookupEnv)
	if err == nil {
		err = cfg.Validate()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	ctx := context.Background()
	store, err := storage.Open(ctx, cfg.StorageDriver, cfg.DatabaseURL)
	if err == nil {
		defer store.Close()
		err = store.Migrate(ctx)
	}
	if err == nil {
		err = store.Reconcile(ctx, cfg.ArtifactsDir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "storage startup failed:", err)
		os.Exit(1)
	}
	fmt.Printf("configuration valid: storage=%s listen=%s scheduler=%t\n", cfg.StorageDriver, cfg.ListenAddress, cfg.SchedulerEnabled)
}
