package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/config"
	"github.com/FelixSeptem/danny/internal/server"
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
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	store, err := storage.Open(ctx, cfg.StorageDriver, cfg.DatabaseURL)
	if err == nil {
		defer func() { _ = store.Close() }()
		err = store.Migrate(ctx)
	}
	if err == nil {
		err = store.Reconcile(ctx, cfg.ArtifactsDir)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "storage startup failed:", err)
		os.Exit(1)
	}

	// The assembled service mounts the Milestone 9 /agent/ boundary unchanged.
	// Control (cancel) and the surface action executor stay unconfigured until
	// their product authorization/commit gates are wired; both fail closed
	// rather than executing without Danny authority. Rendering a stored surface
	// is read-only, so it is wired here through the restricted catalog.
	catalog := agentui.DefaultCatalog()
	agentUI := &server.AgentUIService{
		Source:   &server.StorageRunSource{Store: store},
		Surfaces: &server.SurfaceService{Store: store, Catalog: catalog},
	}
	service := &server.Server{
		AgentUI:     agentUI,
		Projections: store.Projections(),
		Tasks:       store.Tasks(),
		Surfaces:    &server.StorageSurfaceSource{Store: store, Catalog: catalog},
		StaticDir:   cfg.WorkbenchDir,
	}
	httpServer := &http.Server{
		Addr:              cfg.ListenAddress,
		Handler:           service.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		fmt.Printf("danny workbench listening on %s (loopback-only, same-origin enforced)\n", cfg.ListenAddress)
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		serveErr <- err
	}()

	select {
	case err := <-serveErr:
		if err != nil {
			fmt.Fprintln(os.Stderr, "http server failed:", err)
			os.Exit(1)
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			fmt.Fprintln(os.Stderr, "http shutdown failed:", err)
			os.Exit(1)
		}
		if err := <-serveErr; err != nil {
			fmt.Fprintln(os.Stderr, "http server failed:", err)
			os.Exit(1)
		}
	}
}
