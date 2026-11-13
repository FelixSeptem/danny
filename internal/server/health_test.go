package server

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHealthEndpointsReportRealDependencies(t *testing.T) {
	svc := &Server{
		HealthCheck:   func(context.Context) error { return nil },
		StorageDriver: "sqlite",
		ArtifactDir:   t.TempDir(),
	}
	srv := startWorkbench(t, svc)

	// The Milestone 10 static boundary identifier is unchanged.
	status, body := doRequest(t, srv, http.MethodGet, "/api/health", "", nil)
	if status != http.StatusOK || !strings.Contains(body, `"boundary":"http-api-workbench"`) {
		t.Fatalf("/api/health changed: status=%d body=%s", status, body)
	}
	// Liveness does not depend on projections or storage at all.
	status, body = doRequest(t, srv, http.MethodGet, "/api/health/live", "", nil)
	if status != http.StatusOK || !strings.Contains(body, `"status":"live"`) {
		t.Fatalf("liveness: status=%d body=%s", status, body)
	}
	// Readiness reports the driver after a real dependency probe.
	status, body = doRequest(t, srv, http.MethodGet, "/api/health/ready", "", nil)
	if status != http.StatusOK || !strings.Contains(body, `"storage_driver":"sqlite"`) {
		t.Fatalf("readiness: status=%d body=%s", status, body)
	}

	svc.HealthCheck = func(context.Context) error { return errors.New("database unreachable") }
	status, body = doRequest(t, srv, http.MethodGet, "/api/health/ready", "", nil)
	if status != http.StatusServiceUnavailable || errorCode(t, body) != "dependency_unavailable" {
		t.Fatalf("failing probe: status=%d body=%s", status, body)
	}
	status, _ = doRequest(t, srv, http.MethodGet, "/api/health/live", "", nil)
	if status != http.StatusOK {
		t.Fatalf("liveness must stay up when a dependency fails, got %d", status)
	}

	svc.HealthCheck = nil
	status, body = doRequest(t, srv, http.MethodGet, "/api/health/ready", "", nil)
	if status != http.StatusServiceUnavailable || errorCode(t, body) != "unavailable" {
		t.Fatalf("missing probe: status=%d body=%s", status, body)
	}
	status, _ = doRequest(t, srv, http.MethodGet, "/api/health/bogus", "", nil)
	if status != http.StatusNotFound {
		t.Fatalf("unknown health path: status=%d", status)
	}
	status, _ = doRequest(t, srv, http.MethodPost, "/api/health/ready", "", sameOrigin)
	if status != http.StatusMethodNotAllowed {
		t.Fatalf("health endpoints must be read-only, got %d", status)
	}
}

func TestReadinessFailsClosedOnUnusableArtifactDirectory(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "artifacts")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	svc := &Server{
		HealthCheck: func(context.Context) error { return nil },
		ArtifactDir: blocker,
	}
	srv := startWorkbench(t, svc)
	status, body := doRequest(t, srv, http.MethodGet, "/api/health/ready", "", nil)
	if status != http.StatusServiceUnavailable || errorCode(t, body) != "dependency_unavailable" {
		t.Fatalf("unusable artifact dir: status=%d body=%s", status, body)
	}
}
