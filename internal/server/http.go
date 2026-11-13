package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/FelixSeptem/danny/internal/agentui"
	"github.com/FelixSeptem/danny/internal/storage"
)

// TaskUpdater is the narrow slice of TaskRepository the workbench may write
// through. It exists so the assembled service does not depend on a concrete
// *Store and so a missing updater fails closed in tests.
type TaskUpdater interface {
	Update(ctx context.Context, id string, expectedRevision int64, patch storage.TaskPatch) (storage.Task, error)
}

// SurfaceSource renders a stored, server-approved surface into the A2UI v0.9.1
// wire format. It is read-only: the client never supplies a component tree, and
// unregistered components are rejected by the Milestone 9 adapter.
type SurfaceSource interface {
	RenderSurface(ctx context.Context, surfaceID string) ([]byte, error)
}

// StorageSurfaceSource is the production SurfaceSource backed by the durable
// surface repository and the restricted server-registered catalog.
type StorageSurfaceSource struct {
	Store   *storage.Store
	Catalog *agentui.Catalog
}

var _ SurfaceSource = (*StorageSurfaceSource)(nil)

func (s *StorageSurfaceSource) RenderSurface(ctx context.Context, surfaceID string) ([]byte, error) {
	surface, _, err := s.Store.AgentUI().GetSurface(ctx, surfaceID)
	if err != nil {
		return nil, err
	}
	return agentui.RenderA2UI(s.Catalog, surface)
}

// Server assembles the browser boundary. It mounts the Milestone 9 /agent/
// subtree unchanged, adds read-only /api/ projections, and serves workbench
// assets. Zero-value fields fail closed: a missing dependency disables only
// its own endpoints, never fabricating a response.
type Server struct {
	AgentUI     *AgentUIService
	Projections storage.ProjectionRepository
	Tasks       TaskUpdater
	Surfaces    SurfaceSource
	StaticDir   string

	// HealthCheck is the storage dependency probe used by /api/health/ready.
	// A nil probe fails closed. StorageDriver and ArtifactDir are reported as
	// metadata; they never widen the read-only projection boundary.
	HealthCheck   func(ctx context.Context) error
	StorageDriver string
	ArtifactDir   string
}

// Handler returns the assembled, same-origin-protected HTTP handler. Routing
// parses path segments manually so behavior is independent of the serving
// mux's pattern-matching semantics (the Milestone 9 decision).
func (s *Server) Handler() http.Handler {
	return sameOriginGuard(http.HandlerFunc(s.serve))
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	segments := splitPath(r.URL.Path)
	if len(segments) == 0 {
		s.serveStatic(w, r, "")
		return
	}
	switch segments[0] {
	case "agent":
		if s.AgentUI == nil {
			writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "agent UI boundary is not configured")
			return
		}
		s.AgentUI.Handler().ServeHTTP(w, r)
	case "api":
		s.serveAPI(w, r, segments)
	default:
		s.serveStatic(w, r, strings.Join(segments, "/"))
	}
}

func splitPath(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "/")
}

// sameOriginGuard is the single fail-closed gate for state-changing requests.
// It wraps the whole handler, including the Milestone 9 cancel and
// surface-action endpoints, so no endpoint can be reached through a cross-site
// POST on a loopback-bound service.
func sameOriginGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isReadMethod(r.Method) && !requestIsSameOrigin(r) {
			writeJSONError(w, http.StatusForbidden, "same_origin_required", "state-changing requests require a matching Origin or Sec-Fetch-Site: same-origin")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isReadMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func requestIsSameOrigin(r *http.Request) bool {
	if strings.EqualFold(strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")), "same-origin") {
		return true
	}
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" || strings.EqualFold(origin, "null") {
		return false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	if !strings.EqualFold(parsed.Host, r.Host) {
		return false
	}
	scheme := parsed.Scheme
	if scheme == "" {
		return false
	}
	expected := "http"
	if r.TLS != nil {
		expected = "https"
	}
	return strings.EqualFold(scheme, expected)
}

// serveAPI dispatches the read-only projection endpoints and the single
// revision-aware task mutation.
func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) < 2 {
		http.NotFound(w, r)
		return
	}
	resource := segments[1]
	if resource == "health" {
		s.serveHealth(w, r, segments)
		return
	}
	if resource == "surfaces" {
		s.serveSurface(w, r, segments)
		return
	}
	if s.Projections == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "projection repository is not configured")
		return
	}
	ctx := r.Context()
	switch resource {
	case "tasks":
		s.serveTasks(w, r, ctx, segments)
	case "schedules":
		s.serveSchedules(w, r, ctx, segments)
	case "deliveries":
		s.serveDeliveries(w, r, ctx, segments)
	case "notifications":
		s.serveList(w, r, "notifications", func() (any, error) { return s.Projections.ListNotifications(ctx, 0) })
	case "knowledge":
		s.serveKnowledge(w, r, ctx, segments)
	case "runs":
		s.serveRuns(w, r, ctx, segments)
	default:
		http.NotFound(w, r)
	}
}

// serveHealth is dispatched before the projection guard: liveness and
// readiness must not depend on a configured projection repository.
func (s *Server) serveHealth(w http.ResponseWriter, r *http.Request, segments []string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "health endpoints are read-only")
		return
	}
	if len(segments) == 2 {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "boundary": "http-api-workbench"})
		return
	}
	if len(segments) != 3 {
		http.NotFound(w, r)
		return
	}
	switch segments[2] {
	case "live":
		writeJSON(w, http.StatusOK, map[string]any{"status": "live", "boundary": "docker-operations"})
	case "ready":
		s.serveReadiness(w, r)
	default:
		http.NotFound(w, r)
	}
}

// serveReadiness performs a real dependency probe. A missing probe, a failing
// storage ping, or an unusable artifact directory produces a typed 503 rather
// than an empty success, so Compose health checks cannot gate on a lie.
func (s *Server) serveReadiness(w http.ResponseWriter, r *http.Request) {
	if s.HealthCheck == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "health source is not configured")
		return
	}
	if err := s.HealthCheck(r.Context()); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "dependency_unavailable", err.Error())
		return
	}
	if err := ensureArtifactDir(s.ArtifactDir); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, "dependency_unavailable", "artifact directory is unusable: "+err.Error())
		return
	}
	driver := s.StorageDriver
	if driver == "" {
		driver = "unknown"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":          "ready",
		"boundary":        "docker-operations",
		"storage_driver":  driver,
		"artifacts_ready": true,
	})
}

func ensureArtifactDir(dir string) error {
	if dir == "" {
		return errors.New("artifact directory is not configured")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("path is not a directory")
	}
	return nil
}

// serveSurface serves the A2UI v0.9.1 wire rendering of a stored,
// server-approved surface. The rendering is produced by the Milestone 9
// adapter over the restricted catalog; the client only consumes it.
func (s *Server) serveSurface(w http.ResponseWriter, r *http.Request, segments []string) {
	if len(segments) != 3 || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "surfaces are read-only")
		return
	}
	if s.Surfaces == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "surface source is not configured")
		return
	}
	wire, err := s.Surfaces.RenderSurface(r.Context(), segments[2])
	if err != nil {
		writeStorageError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(wire)
}

// serveList is the shared read-only list/dispatch helper for the simple views.
func (s *Server) serveList(w http.ResponseWriter, r *http.Request, label string, load func() (any, error)) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", label+" is read-only")
		return
	}
	value, err := load()
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, value)
}

func (s *Server) serveTasks(w http.ResponseWriter, r *http.Request, ctx context.Context, segments []string) {
	switch {
	case len(segments) == 2 && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.serveList(w, r, "tasks", func() (any, error) { return s.Projections.ListTasks(ctx, 0) })
	case len(segments) == 3 && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		task, err := s.Projections.GetTask(ctx, segments[2])
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, task)
	case len(segments) == 3 && r.Method == http.MethodPatch:
		s.handleTaskPatch(w, r, ctx, segments[2])
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported task operation")
	}
}

func (s *Server) handleTaskPatch(w http.ResponseWriter, r *http.Request, ctx context.Context, id string) {
	if s.Tasks == nil {
		writeJSONError(w, http.StatusServiceUnavailable, "unavailable", "task updater is not configured")
		return
	}
	var req struct {
		ExpectedRevision *int64  `json:"expected_revision"`
		Title            *string `json:"title"`
		Description      *string `json:"description"`
		Status           *string `json:"status"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil || req.ExpectedRevision == nil || *req.ExpectedRevision <= 0 {
		writeJSONError(w, http.StatusBadRequest, "malformed", "expected_revision is required and must be positive")
		return
	}
	patch := storage.TaskPatch{Title: req.Title, Description: req.Description}
	if req.Status != nil {
		patch.Status = *req.Status
	}
	updated, err := s.Tasks.Update(ctx, id, *req.ExpectedRevision, patch)
	if err != nil {
		writeStorageError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, storage.TaskProjection{
		ID:          updated.ID,
		Title:       updated.Title,
		Description: updated.Description,
		Status:      updated.Status,
		Revision:    updated.Revision,
		CreatedAt:   updated.CreatedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:   updated.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
}

func (s *Server) serveSchedules(w http.ResponseWriter, r *http.Request, ctx context.Context, segments []string) {
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case len(segments) == 2 && read:
		s.serveList(w, r, "schedules", func() (any, error) { return s.Projections.ListSchedules(ctx, 0) })
	case len(segments) == 3 && read:
		schedule, err := s.Projections.GetSchedule(ctx, segments[2])
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, schedule)
	case len(segments) == 4 && segments[3] == "occurrences" && read:
		s.serveList(w, r, "occurrences", func() (any, error) { return s.Projections.ListOccurrences(ctx, segments[2], 0) })
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported schedule operation")
	}
}

func (s *Server) serveDeliveries(w http.ResponseWriter, r *http.Request, ctx context.Context, segments []string) {
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case len(segments) == 2 && read:
		s.serveList(w, r, "deliveries", func() (any, error) { return s.Projections.ListDeliveries(ctx, 0) })
	case len(segments) == 3 && read:
		delivery, err := s.Projections.GetDelivery(ctx, segments[2])
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, delivery)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported delivery operation")
	}
}

func (s *Server) serveKnowledge(w http.ResponseWriter, r *http.Request, ctx context.Context, segments []string) {
	if len(segments) != 3 || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported knowledge operation")
		return
	}
	switch segments[2] {
	case "generations":
		s.serveList(w, r, "generations", func() (any, error) { return s.Projections.ListGenerations(ctx, 0) })
	case "artifacts":
		generationID, err := parseGenerationID(r)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "malformed", err.Error())
			return
		}
		s.serveList(w, r, "artifacts", func() (any, error) { return s.Projections.ListArtifacts(ctx, generationID, 0) })
	case "sources":
		s.serveList(w, r, "sources", func() (any, error) { return s.Projections.ListSources(ctx, 0) })
	case "status":
		status, err := s.Projections.IndexStatus(ctx)
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, status)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) serveRuns(w http.ResponseWriter, r *http.Request, ctx context.Context, segments []string) {
	read := r.Method == http.MethodGet || r.Method == http.MethodHead
	switch {
	case len(segments) == 2 && read:
		s.serveList(w, r, "runs", func() (any, error) { return s.Projections.ListExecutions(ctx, 0) })
	case len(segments) == 3 && read:
		inspector, err := s.Projections.RunInspector(ctx, segments[2])
		if err != nil {
			writeStorageError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, inspector)
	default:
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "unsupported run operation")
	}
}

func parseGenerationID(r *http.Request) (int64, error) {
	raw := r.URL.Query().Get("generation_id")
	if raw == "" {
		return 0, errors.New("generation_id query parameter is required")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, errors.New("generation_id must be a positive integer")
	}
	return id, nil
}

// serveStatic serves built workbench assets with an SPA fallback. When no
// asset directory is configured, a small built-in placeholder keeps the Go
// service usable without a Node toolchain.
func (s *Server) serveStatic(w http.ResponseWriter, r *http.Request, _ string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		writeJSONError(w, http.StatusMethodNotAllowed, "method_not_allowed", "static assets are read-only")
		return
	}
	if s.StaticDir == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, placeholderHTML)
		return
	}
	clean := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/"))
	rel := strings.TrimPrefix(clean, "/")
	if rel == "" {
		rel = "index.html"
	}
	candidate := filepath.Join(s.StaticDir, filepath.FromSlash(rel))
	if info, err := os.Stat(candidate); err != nil || info.IsDir() {
		candidate = filepath.Join(s.StaticDir, "index.html")
	}
	if _, err := os.Stat(candidate); err != nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, placeholderHTML)
		return
	}
	http.ServeFile(w, r, candidate)
}

const placeholderHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Danny workbench</title></head><body><main><h1>Danny workbench</h1><p>The HTTP API boundary is running. Build the workbench assets to serve the full client.</p></main></body></html>`

func writeStorageError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, storage.ErrNotFound):
		writeJSONError(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, storage.ErrRevisionConflict):
		writeJSONError(w, http.StatusConflict, "task_revision_conflict", "the task has a newer revision; reload before updating")
	default:
		writeJSONError(w, http.StatusInternalServerError, "projection_failed", "projection query failed")
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
