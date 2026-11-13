package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func newProjectionStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:projections-"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return store
}

func mustExec(t *testing.T, store *Store, query string, args ...any) {
	t.Helper()
	if _, err := store.db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// projectionJSONKeys marshals a projection and returns its top-level keys.
func projectionJSONKeys(t *testing.T, value any) map[string]bool {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal projection: %v", err)
	}
	keys := make(map[string]bool, len(decoded))
	for key := range decoded {
		keys[key] = true
	}
	return keys
}

var forbiddenProjectionKeys = []string{
	"payload", "content", "lineage", "result", "failure_class", "input",
	"envelope", "warnings", "native_event_id", "native_source", "native_checkpoint_id",
	"effect_key", "idempotency_key", "diagnostics", "completed_effect_keys",
}

func assertNoForbiddenKeys(t *testing.T, keys map[string]bool, label string) {
	t.Helper()
	for _, forbidden := range forbiddenProjectionKeys {
		if keys[forbidden] {
			t.Fatalf("%s projection leaked forbidden field %q: keys=%v", label, forbidden, keys)
		}
	}
}

func TestProjectionListsAndDetailsExposeOnlyDeclaredFields(t *testing.T) {
	store := newProjectionStore(t)
	ctx := context.Background()
	const stamp = "2026-01-01T00:00:00Z"

	mustExec(t, store, `INSERT INTO tasks(id,title,description,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`, "task-1", "title", "desc", "open", 1, stamp, stamp)
	mustExec(t, store, `INSERT INTO schedule_definitions(id,cron,timezone,action_type,instruction,payload,enabled,retry_limit,misfire_policy,backfill_limit,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "sched-1", "0 9 * * *", "Asia/Shanghai", "agent", "daily", "{}", 1, 3, "coalesce_latest", 2, 1, stamp, stamp)
	mustExec(t, store, `INSERT INTO webhook_deliveries(id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "del-1", "exec-1", "", "effect-1", "https://example.invalid/hook", `{"secret":1}`, "failed", 1, 2, stamp, "", "boom", stamp, stamp)
	mustExec(t, store, `INSERT INTO notifications(id,execution_id,channel,status,payload,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, "note-1", "exec-1", "web", "pending", `{"secret":2}`, 1, stamp, stamp)
	mustExec(t, store, `INSERT INTO knowledge_generations(id,status,commit_marker,created_at,committed_at) VALUES(?,?,?,?,?)`, 1, "committed", "marker-1", stamp, stamp)
	mustExec(t, store, `INSERT INTO artifacts(id,generation_id,kind,content_hash,content,lineage,created_at) VALUES(?,?,?,?,?,?,?)`, "art-1", 1, "summary", "hash-1", `{"secret":3}`, `{"lineage":true}`, stamp)
	mustExec(t, store, `INSERT INTO source_metadata(id,path,content_hash,content,format,parser_version,provider_mode,source_fingerprint,warnings,untrusted_source,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, "src-1", "docs/a.md", "hash-src", `{"secret":4}`, "md", "danny-native", "local", "fp", `{"warn":true}`, 1, stamp, stamp)
	mustExec(t, store, `INSERT INTO task_harness_executions(id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "exec-1", "task-1", 1, "succeeded", 1, "direct", "planner.v1", "pd", "cd", "prd", "", "corr-1", "idem-1", `{"secret":5}`, `{"secret":6}`, `{"secret":7}`, stamp, stamp)

	projections := store.Projections()
	tasks, err := projections.ListTasks(ctx, 10)
	if err != nil || len(tasks) != 1 || tasks[0].ID != "task-1" {
		t.Fatalf("list tasks: %v %#v", err, tasks)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, tasks[0]), "task")

	schedules, err := projections.ListSchedules(ctx, 10)
	if err != nil || len(schedules) != 1 || !schedules[0].Enabled {
		t.Fatalf("list schedules: %v %#v", err, schedules)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, schedules[0]), "schedule")

	deliveries, err := projections.ListDeliveries(ctx, 10)
	if err != nil || len(deliveries) != 1 || deliveries[0].Status != "failed" {
		t.Fatalf("list deliveries: %v %#v", err, deliveries)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, deliveries[0]), "delivery")

	notes, err := projections.ListNotifications(ctx, 10)
	if err != nil || len(notes) != 1 {
		t.Fatalf("list notifications: %v %#v", err, notes)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, notes[0]), "notification")

	generations, err := projections.ListGenerations(ctx, 10)
	if err != nil || len(generations) != 1 || generations[0].CommittedAt == "" {
		t.Fatalf("list generations: %v %#v", err, generations)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, generations[0]), "generation")

	artifacts, err := projections.ListArtifacts(ctx, 1, 10)
	if err != nil || len(artifacts) != 1 || artifacts[0].ContentHash != "hash-1" {
		t.Fatalf("list artifacts: %v %#v", err, artifacts)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, artifacts[0]), "artifact")

	sources, err := projections.ListSources(ctx, 10)
	if err != nil || len(sources) != 1 || !sources[0].UntrustedSource {
		t.Fatalf("list sources: %v %#v", err, sources)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, sources[0]), "source")

	executions, err := projections.ListExecutions(ctx, 10)
	if err != nil || len(executions) != 1 || executions[0].ID != "exec-1" {
		t.Fatalf("list executions: %v %#v", err, executions)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, executions[0]), "execution")

	status, err := projections.IndexStatus(ctx)
	if err != nil || status.GenerationID != 1 || status.ArtifactCount != 1 || status.SourceCount != 1 {
		t.Fatalf("index status: %v %#v", err, status)
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, status), "index status")
}

func TestProjectionListsAreBounded(t *testing.T) {
	store := newProjectionStore(t)
	ctx := context.Background()
	const stamp = "2026-01-01T00:00:00Z"
	for i := 0; i < ProjectionListLimit+12; i++ {
		mustExec(t, store, `INSERT INTO tasks(id,title,description,status,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?)`,
			fmt.Sprintf("task-%03d", i), "title", "", "open", 1, stamp, fmt.Sprintf("2026-01-01T00:00:%02dZ", i%60))
	}
	tasks, err := store.Projections().ListTasks(ctx, 0)
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != ProjectionListLimit {
		t.Fatalf("list must be capped at %d, got %d", ProjectionListLimit, len(tasks))
	}
	small, err := store.Projections().ListTasks(ctx, 5)
	if err != nil || len(small) != 5 {
		t.Fatalf("explicit limit: %v len=%d", err, len(small))
	}
	over, err := store.Projections().ListTasks(ctx, ProjectionListLimit*3)
	if err != nil || len(over) != ProjectionListLimit {
		t.Fatalf("over-limit must clamp: %v len=%d", err, len(over))
	}
}

func TestProjectionUnknownIDsAreTypedNotFound(t *testing.T) {
	store := newProjectionStore(t)
	ctx := context.Background()
	projections := store.Projections()
	checks := []struct {
		name string
		fn   func() error
	}{
		{"task", func() error { _, err := projections.GetTask(ctx, "missing"); return err }},
		{"schedule", func() error { _, err := projections.GetSchedule(ctx, "missing"); return err }},
		{"delivery", func() error { _, err := projections.GetDelivery(ctx, "missing"); return err }},
		{"execution", func() error { _, err := projections.GetExecution(ctx, "missing"); return err }},
		{"inspector", func() error { _, err := projections.RunInspector(ctx, "missing"); return err }},
	}
	for _, check := range checks {
		if err := check.fn(); !errors.Is(err, ErrNotFound) {
			t.Fatalf("%s: expected ErrNotFound, got %v", check.name, err)
		}
	}
}

func TestRunInspectorAssemblesDurableEvidenceOnly(t *testing.T) {
	store := newProjectionStore(t)
	ctx := context.Background()
	const stamp = "2026-01-01T00:00:00Z"

	mustExec(t, store, `INSERT INTO task_harness_executions(id,task_id,task_revision,status,revision,mode,plan_version,plan_digest,capability_digest,projection_digest,parent_run_id,correlation_id,idempotency_key,input,result,failure_class,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "exec-1", "task-1", 1, "paused", 1, "direct", "planner.v2", "pd", "cd", "prd", "", "corr-1", "idem-1", `{}`, `{}`, `{}`, stamp, stamp)
	mustExec(t, store, `INSERT INTO task_harness_plans(execution_id,version,parent_version,mode,profile_digest,plan_digest,capability_digest,projection_digest,rationale,replan_reason,budget,write_scope,authority_expanded,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "exec-1", "planner.v1", "", "direct", "prof", "pd", "cd", "prd", "simple", "", 0, 0, 0, stamp)
	mustExec(t, store, `INSERT INTO task_harness_plans(execution_id,version,parent_version,mode,profile_digest,plan_digest,capability_digest,projection_digest,rationale,replan_reason,budget,write_scope,authority_expanded,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "exec-1", "planner.v2", "planner.v1", "direct", "prof", "pd", "cd", "prd", "replan", "approval_required", 0, 0, 0, stamp)
	mustExec(t, store, `INSERT INTO task_harness_checkpoints(execution_id,run_id,plan_version,plan_digest,capability_digest,projection_digest,current_step,pause_reason,completed_effect_keys,diagnostics,revision,created_at,updated_at,native_checkpoint_id) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, "exec-1", "run-1", "planner.v2", "pd", "cd", "prd", "step-3", "approval_required", `[]`, `[]`, 1, stamp, stamp, "native-secret")
	mustExec(t, store, `INSERT INTO context_projection_snapshots(id,scope,session_id,turn_id,binding_id,run_id,contract_version,digest,source_digest,envelope,created_at,updated_at,continuity_reference) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, "cps-1", "scope", "session", "turn", "binding", "exec-1", "danny.context.v1", "digest-1", "source-1", `{"secret":1}`, stamp, stamp, "stele-ref-1")
	mustExec(t, store, `INSERT INTO task_harness_evidence(id,execution_id,effect_key,plan_version,capability_digest,projection_digest,status,created_at,native_event_id) VALUES(?,?,?,?,?,?,?,?,?)`, "ev-1", "exec-1", "effect-1", "planner.v2", "cd", "prd", `{}`, stamp, "native-ev-1")
	mustExec(t, store, `INSERT INTO agent_ui_events(execution_id,seq,kind,terminal,contract_version,payload,created_at,native_event_id) VALUES(?,?,?,?,?,?,?,?)`, "exec-1", 1, "text_message", 0, "danny.agent-ui.projection.v1", `{"text":"hi"}`, stamp, "native-ui-1")

	inspector, err := store.Projections().RunInspector(ctx, "exec-1")
	if err != nil {
		t.Fatalf("run inspector: %v", err)
	}
	if inspector.Execution.ID != "exec-1" || len(inspector.Plans) != 2 || inspector.Plans[1].Version != "planner.v2" {
		t.Fatalf("inspector plans: %#v", inspector)
	}
	if inspector.Checkpoint == nil || inspector.Checkpoint.PauseReason != "approval_required" || inspector.Checkpoint.CurrentStep != "step-3" {
		t.Fatalf("inspector checkpoint: %#v", inspector.Checkpoint)
	}
	if len(inspector.ContextProjections) != 1 || inspector.ContextProjections[0].Digest != "digest-1" {
		t.Fatalf("inspector context refs: %#v", inspector.ContextProjections)
	}
	if inspector.EvidenceCount != 1 || inspector.EventCount != 1 {
		t.Fatalf("inspector counts: evidence=%d events=%d", inspector.EvidenceCount, inspector.EventCount)
	}
	raw, _ := json.Marshal(inspector)
	joined := string(raw)
	for _, leaked := range []string{"native-secret", "native-ev-1", "native-ui-1", "stele-ref-1", `"secret"`, "effect-1"} {
		if strings.Contains(joined, leaked) {
			t.Fatalf("inspector leaked %q: %s", leaked, joined)
		}
	}
	assertNoForbiddenKeys(t, projectionJSONKeys(t, inspector.Execution), "inspector execution")
}

func TestProjectionNullExecutionIDIsSafe(t *testing.T) {
	store := newProjectionStore(t)
	ctx := context.Background()
	const stamp = "2026-01-01T00:00:00Z"
	mustExec(t, store, `INSERT INTO notifications(id,execution_id,channel,status,payload,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, "note-1", nil, "web", "pending", `{}`, 1, stamp, stamp)
	notes, err := store.Projections().ListNotifications(ctx, 10)
	if err != nil || len(notes) != 1 || notes[0].ExecutionID != "" {
		t.Fatalf("null execution id: %v %#v", err, notes)
	}
}

// Compile-time guard: the projection repository is read-only by contract.
var _ ProjectionRepository = (*projectionRepository)(nil)
