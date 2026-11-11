package storage

import (
	"context"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
)

func newHarnessTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := OpenSQLite(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(context.Background()); err != nil {
		_ = store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestExecutionCheckpointSurvivesProcessRestart(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "restart.db")
	ctx := context.Background()
	store, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "restart-1", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "restart", Mode: "direct", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}, Plan: ExecutionPlanRecord{ExecutionID: "restart-1", Version: "v1", Mode: "direct", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Executions().SaveCheckpoint(ctx, ExecutionCheckpoint{ExecutionID: "restart-1", RunID: "restart-1", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p", PauseReason: []byte("input_required")}); err != nil {
		t.Fatal(err)
	}
	_ = store.Close()
	reopened, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	recovered, err := reopened.Executions().GetCheckpoint(ctx, "restart-1")
	if err != nil || recovered.PlanVersion != "v1" || string(recovered.PauseReason) != "input_required" {
		t.Fatalf("recovered=%#v err=%v", recovered, err)
	}
}

func TestExecutionRevisionUpdateAllowsExactlyOneWinner(t *testing.T) {
	store := newHarnessTestStore(t)
	ctx := context.Background()
	if _, err := store.Executions().Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "race-1", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "race", Mode: "direct", PlanVersion: "v1", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}, Plan: ExecutionPlanRecord{ExecutionID: "race-1", Version: "v1", Mode: "direct", PlanDigest: "d", CapabilityDigest: "c", ProjectionDigest: "p"}}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, status := range []ExecutionStatus{ExecutionDispatching, ExecutionPaused} {
		wg.Add(1)
		go func(status ExecutionStatus) {
			defer wg.Done()
			_, err := store.Executions().UpdateExecution(ctx, "race-1", 1, status, nil, nil)
			results <- err
		}(status)
	}
	wg.Wait()
	close(results)
	var success, conflicts int
	var other error
	for err := range results {
		if err == nil {
			success++
		} else if errors.Is(err, ErrRevisionConflict) {
			conflicts++
		} else if err != nil {
			other = err
		}
	}
	if success != 1 || conflicts != 1 {
		t.Fatalf("success=%d conflicts=%d other=%v", success, conflicts, other)
	}
}

func TestExecutionAdmissionAndReplayAreDurable(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	admission := ExecutionAdmission{Execution: TaskExecution{ID: "run-1", TaskID: "task-1", TaskRevision: 2, CorrelationID: "corr-1", IdempotencyKey: "once", Mode: "direct", PlanVersion: "planner.v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", Input: "summarize"}, Plan: ExecutionPlanRecord{ExecutionID: "run-1", Version: "planner.v1", Mode: "direct", ProfileDigest: "profile", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}
	one, err := repo.Admit(context.Background(), admission)
	if err != nil || one.Status != ExecutionAdmitted {
		t.Fatalf("admit=%#v err=%v", one, err)
	}
	replay, err := repo.Admit(context.Background(), admission)
	if err != nil || replay.ID != one.ID || replay.Revision != one.Revision {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	plans, err := repo.Plans(context.Background(), "run-1")
	if err != nil || len(plans) != 1 {
		t.Fatalf("plans=%#v err=%v", plans, err)
	}
	if _, err := repo.UpdateExecution(context.Background(), "run-1", 99, ExecutionSucceeded, []byte("ok"), nil); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale update=%v", err)
	}
}

func TestExecutionTaskRevisionBindingIsConditional(t *testing.T) {
	store := newHarnessTestStore(t)
	ctx := context.Background()
	task, err := store.Tasks().Create(ctx, Task{ID: "bind-task", Title: "original"})
	if err != nil {
		t.Fatal(err)
	}
	run, err := store.Executions().AdmitIfTaskRevisionCurrent(ctx, ExecutionAdmission{
		Execution: TaskExecution{ID: "bind-run", TaskID: task.ID, TaskRevision: task.Revision, CorrelationID: "bind-correlation", IdempotencyKey: "bind-once", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"},
		Plan:      ExecutionPlanRecord{ExecutionID: "bind-run", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"},
	})
	if err != nil {
		t.Fatal(err)
	}
	description := "replanned"
	updatedTask, err := store.Tasks().Update(ctx, task.ID, task.Revision, TaskPatch{Description: &description})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := store.Executions().BindExecutionTaskRevision(ctx, run.ID, run.Revision, updatedTask.Revision)
	if err != nil || bound.TaskRevision != updatedTask.Revision || bound.Revision != run.Revision+1 {
		t.Fatalf("bound=%#v err=%v", bound, err)
	}
	latestTitle := "advanced again"
	if _, err := store.Tasks().Update(ctx, task.ID, updatedTask.Revision, TaskPatch{Title: &latestTitle}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Executions().BindExecutionTaskRevision(ctx, bound.ID, bound.Revision, updatedTask.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale task binding error=%v", err)
	}
	unchanged, err := store.Executions().GetExecution(ctx, bound.ID)
	if err != nil || unchanged.TaskRevision != updatedTask.Revision || unchanged.Revision != bound.Revision {
		t.Fatalf("execution after rejected binding=%#v err=%v", unchanged, err)
	}
}

func TestCheckpointEvidenceAndReplanRetainHistory(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	_, err := repo.Admit(context.Background(), ExecutionAdmission{Execution: TaskExecution{ID: "run-2", TaskID: "task-2", CorrelationID: "corr-2", IdempotencyKey: "once-2", Mode: "direct", PlanVersion: "v1", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1"}, Plan: ExecutionPlanRecord{ExecutionID: "run-2", Version: "v1", Mode: "direct", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.SaveCheckpoint(context.Background(), ExecutionCheckpoint{ExecutionID: "run-2", RunID: "run-2", PlanVersion: "v1", PlanDigest: "d1", CapabilityDigest: "c1", ProjectionDigest: "p1", PauseReason: []byte("approval_required")}); err != nil {
		t.Fatal(err)
	}
	checkpoint, err := repo.GetCheckpoint(context.Background(), "run-2")
	if err != nil || checkpoint.PlanDigest != "d1" {
		t.Fatalf("checkpoint=%#v err=%v", checkpoint, err)
	}
	if err := repo.RecordReplan(context.Background(), ExecutionReplan{ExecutionID: "run-2", PreviousPlanVersion: "v1", NewPlanVersion: "v2", Reason: "task changed", PreviousProfileDigest: "p1", NewProfileDigest: "p2"}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordEvidence(context.Background(), ExecutionEvidenceRecord{ExecutionID: "run-2", EffectKey: "run-2:parent", PlanVersion: "v1", Status: []byte("succeeded")}); err != nil {
		t.Fatal(err)
	}
	replay, err := repo.RecordEvidence(context.Background(), ExecutionEvidenceRecord{ExecutionID: "run-2", EffectKey: "run-2:parent", PlanVersion: "v1", Status: []byte("failed")})
	if err != nil || string(replay.Status) != "succeeded" {
		t.Fatalf("evidence replay=%#v err=%v", replay, err)
	}
}

func TestNativeEvidenceProjectionDeduplicatesAndAdvancesCursorAtomically(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-run", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-run", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-run", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	evidence := ExecutionEvidenceRecord{ExecutionID: "native-run", ObservationScope: "scope", PlanVersion: "v1", CapabilityDigest: "cap", ProjectionDigest: "projection", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: "event-1", NativeVersion: "event.v1", NativeSequence: ptrInt64(10), Status: []byte(`{"kind":"tool_completed"}`), Result: []byte(`{"ok":true}`)}
	cursor := ExecutionSourceCursor{ExecutionID: "native-run", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-10", SourceSequence: 10, ContractVersion: NativeEvidenceContractV1}
	first, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
	if err != nil {
		t.Fatalf("ProjectNativeEvidence() error = %v", err)
	}
	replay, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
	if err != nil || replay.ObservationPosition != first.ObservationPosition {
		t.Fatalf("duplicate projection = %#v, err=%v; first=%#v", replay, err, first)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id='native-run'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("native evidence count=%d err=%v", count, err)
	}
	var persistedCursor string
	if err := store.DB().QueryRowContext(ctx, `SELECT cursor_value FROM execution_source_cursors WHERE execution_id='native-run' AND source_id='run-1'`).Scan(&persistedCursor); err != nil || persistedCursor != "cursor-10" {
		t.Fatalf("source cursor=%q err=%v", persistedCursor, err)
	}
	conflict := evidence
	conflict.Status = []byte(`{"kind":"run_failed"}`)
	conflictErr := ErrEvidenceConflict
	if _, err := repo.ProjectNativeEvidence(ctx, conflict, cursor); !errors.Is(err, conflictErr) {
		t.Fatalf("conflicting native identity error=%v, want %v", err, conflictErr)
	}
}

func TestNativeEvidenceProjectionRollsBackCursorOnCommitFailure(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-rollback", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-rollback", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-rollback", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "native_evidence_before_commit" {
			return errors.New("injected projection failure")
		}
		return nil
	})
	_, err := repo.ProjectNativeEvidence(ctx, ExecutionEvidenceRecord{ExecutionID: "native-rollback", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: "event-1", NativeVersion: "event.v1", Status: []byte(`{"kind":"terminal"}`)}, ExecutionSourceCursor{ExecutionID: "native-rollback", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-1", ContractVersion: NativeEvidenceContractV1})
	if err == nil {
		t.Fatal("injected projection failure unexpectedly committed")
	}
	store.SetFailureInjector(nil)
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id='native-rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("evidence count=%d err=%v", count, err)
	}
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM execution_source_cursors WHERE execution_id='native-rollback'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cursor count=%d err=%v", count, err)
	}
}

func TestNativeEvidenceProjectionAfterCommitFailureReplaysCommittedObservation(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-after-commit", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-after-commit", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-after-commit", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	evidence := ExecutionEvidenceRecord{ExecutionID: "native-after-commit", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: "event-1", NativeVersion: "event.v1", Status: []byte(`{"kind":"terminal"}`)}
	cursor := ExecutionSourceCursor{ExecutionID: "native-after-commit", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-1", ContractVersion: NativeEvidenceContractV1}
	store.SetFailureInjector(func(point string) error {
		if point == "native_evidence_after_commit" {
			return errors.New("caller lost commit acknowledgement")
		}
		return nil
	})
	if _, err := repo.ProjectNativeEvidence(ctx, evidence, cursor); err == nil {
		t.Fatal("post-commit failure injection did not fire")
	}
	store.SetFailureInjector(nil)
	replayed, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
	if err != nil || replayed.ObservationPosition != 1 {
		t.Fatalf("replayed=%#v err=%v", replayed, err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM task_harness_evidence WHERE execution_id='native-after-commit'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("evidence count=%d err=%v", count, err)
	}
}

func TestNativeEvidenceProjectionRejectsSourceSequenceRegression(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-order", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-order", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-order", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	base := ExecutionEvidenceRecord{ExecutionID: "native-order", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeVersion: "event.v1", Status: []byte(`{"kind":"event"}`)}
	for _, seq := range []int64{10, 9} {
		event := base
		event.NativeEventID = "event-" + string(rune('0'+seq/10))
		event.NativeSequence = ptrInt64(seq)
		cursor := ExecutionSourceCursor{ExecutionID: "native-order", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-" + string(rune('0'+seq/10)), SourceSequence: seq, ContractVersion: NativeEvidenceContractV1}
		_, err := repo.ProjectNativeEvidence(ctx, event, cursor)
		if seq == 10 && err != nil {
			t.Fatal(err)
		}
		if seq == 9 && !errors.Is(err, ErrSourceOrderConflict) {
			t.Fatalf("sequence regression error=%v", err)
		}
	}
}

func TestNativeEvidenceProjectionRejectsCursorRegression(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-cursor-order", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-cursor-order", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-cursor-order", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	baseEvidence := ExecutionEvidenceRecord{ExecutionID: "native-cursor-order", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeVersion: "event.v1", Status: []byte(`{"kind":"event"}`)}
	baseCursor := ExecutionSourceCursor{ExecutionID: "native-cursor-order", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", ContractVersion: NativeEvidenceContractV1}
	for _, sequence := range []int64{10, 9} {
		evidence := baseEvidence
		evidence.NativeEventID = "event-" + strconv.FormatInt(sequence, 10)
		cursor := baseCursor
		cursor.CursorValue = "cursor-" + strconv.FormatInt(sequence, 10)
		cursor.SourceSequence = sequence
		_, err := repo.ProjectNativeEvidence(ctx, evidence, cursor)
		if sequence == 10 && err != nil {
			t.Fatal(err)
		}
		if sequence == 9 && !errors.Is(err, ErrSourceOrderConflict) {
			t.Fatalf("cursor regression error=%v", err)
		}
	}
}

func TestNativeEvidenceProjectionConcurrentEventsHaveDistinctObservationPositions(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-concurrent", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-concurrent", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-concurrent", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	results := make(chan ExecutionEvidenceRecord, 2)
	errorsCh := make(chan error, 2)
	for _, eventID := range []string{"event-a", "event-b"} {
		go func(eventID string) {
			evidence, err := repo.ProjectNativeEvidence(ctx, ExecutionEvidenceRecord{ExecutionID: "native-concurrent", ObservationScope: "scope", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: eventID, NativeVersion: "event.v1", Status: []byte(eventID)}, ExecutionSourceCursor{ExecutionID: "native-concurrent", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: eventID, ContractVersion: NativeEvidenceContractV1})
			results <- evidence
			errorsCh <- err
		}(eventID)
	}
	positions := map[int64]bool{}
	for range 2 {
		evidence := <-results
		err := <-errorsCh
		if err != nil {
			t.Fatal(err)
		}
		if evidence.ObservationPosition == 0 || positions[evidence.ObservationPosition] {
			t.Fatalf("duplicate observation position %d", evidence.ObservationPosition)
		}
		positions[evidence.ObservationPosition] = true
	}
	if len(positions) != 2 {
		t.Fatalf("positions=%v", positions)
	}
}

func TestNativeEvidenceReplayPagesAreScopeBoundAndStableAcrossRestart(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "native-replay.db")
	store, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.Executions()
	if _, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-replay", TaskID: "task", CorrelationID: "corr", IdempotencyKey: "native-replay", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-replay", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}}); err != nil {
		t.Fatal(err)
	}
	for index := int64(1); index <= 3; index++ {
		_, err := repo.ProjectNativeEvidence(ctx, ExecutionEvidenceRecord{ExecutionID: "native-replay", ObservationScope: "tenant/project/ns", PlanVersion: "v1", ContractVersion: NativeEvidenceContractV1, NativeSourceKind: "runner", NativeSourceNamespace: "history-v1", NativeSourceID: "run-1", NativeEventID: "event-" + strconv.FormatInt(index, 10), NativeVersion: "event.v1", NativeSequence: ptrInt64(index), Status: []byte(`{"kind":"event"}`)}, ExecutionSourceCursor{ExecutionID: "native-replay", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "run-1", CursorVersion: "cursor.v1", CursorValue: "cursor-" + strconv.FormatInt(index, 10), SourceSequence: index, ContractVersion: NativeEvidenceContractV1})
		if err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: ExecutionEvidenceReplayCursor{ContractVersion: ExecutionEvidenceReplayContractV1, ExecutionID: "native-replay", Scope: "tenant/project/ns", Position: 0}, Limit: 2})
	if err != nil || len(first.Evidence) != 2 || first.NextCursor == nil || first.NextCursor.Position != 2 || first.RetentionFloor != 1 {
		t.Fatalf("first page=%#v err=%v", first, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	second, err := store.Executions().ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: *first.NextCursor, Limit: 2})
	if err != nil || len(second.Evidence) != 1 || second.Evidence[0].ObservationPosition != 3 || second.NextCursor != nil {
		t.Fatalf("second page=%#v err=%v", second, err)
	}
	if _, err := store.Executions().ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: ExecutionEvidenceReplayCursor{ContractVersion: ExecutionEvidenceReplayContractV1, ExecutionID: "native-replay", Scope: "foreign/scope", Position: 0}, Limit: 1}); !errors.Is(err, ErrReplayScope) {
		t.Fatalf("foreign scope error=%v", err)
	}
	if _, err := store.Executions().ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: ExecutionEvidenceReplayCursor{ContractVersion: ExecutionEvidenceReplayContractV1, ExecutionID: "native-replay", Scope: "tenant/project/ns", Position: 4}, Limit: 1}); !errors.Is(err, ErrReplayGap) {
		t.Fatalf("future cursor error=%v", err)
	}
	if _, err := store.Executions().ReplayEvidence(ctx, ExecutionEvidenceReplayRequest{Cursor: ExecutionEvidenceReplayCursor{ContractVersion: "wrong", ExecutionID: "native-replay", Scope: "tenant/project/ns"}, Limit: 1}); !errors.Is(err, ErrInvalidReplayCursor) {
		t.Fatalf("malformed cursor error=%v", err)
	}
}

func TestNativeCheckpointAndPauseCommitAtomicallyRejectsStaleWorkers(t *testing.T) {
	store := newHarnessTestStore(t)
	repo := store.Executions()
	ctx := context.Background()
	admitted, err := repo.Admit(ctx, ExecutionAdmission{Execution: TaskExecution{ID: "native-pause", TaskID: "task", TaskRevision: 4, CorrelationID: "corr", IdempotencyKey: "native-pause", Mode: "direct", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}, Plan: ExecutionPlanRecord{ExecutionID: "native-pause", Version: "v1", Mode: "direct", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection"}})
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := ExecutionCheckpoint{ExecutionID: "native-pause", RunID: "native-run", PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", PauseReason: []byte("approval_required"), ContractVersion: NativeCheckpointContract, NativeCheckpointID: "checkpoint-1", NativeCheckpointVersion: "checkpoint.v1", NativeCheckpointDigest: "checkpoint-digest", TaskRevision: 4, WriteScope: "task:4", EffectNamespace: "task:native-pause", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "native-run", SourceCursor: "cursor-10"}
	binding := ExecutionAuthorityBinding{ExecutionID: "native-pause", ContractVersion: NativeCheckpointContract, TaskRevision: 4, PlanVersion: "v1", PlanDigest: "plan", CapabilityDigest: "cap", ProjectionDigest: "projection", WriteScope: "task:4", EffectNamespace: "task:native-pause", CompletedEffectKeys: []byte(`[]`), NativeCheckpointID: "checkpoint-1", NativeCheckpointVersion: "checkpoint.v1", NativeCheckpointDigest: "checkpoint-digest", SourceKind: "runner", SourceNamespace: "history-v1", SourceID: "native-run", SourceCursor: "cursor-10"}
	paused, err := repo.SaveNativeCheckpointAndPause(ctx, checkpoint, binding, admitted.Revision)
	if err != nil || paused.Status != ExecutionPaused || paused.Revision != admitted.Revision+1 {
		t.Fatalf("paused=%#v err=%v", paused, err)
	}
	storedCheckpoint, err := repo.GetCheckpoint(ctx, "native-pause")
	if err != nil || storedCheckpoint.NativeCheckpointID != "checkpoint-1" || storedCheckpoint.ContractVersion != NativeCheckpointContract {
		t.Fatalf("checkpoint=%#v err=%v", storedCheckpoint, err)
	}
	var bindingID, bindingContract string
	if err := store.DB().QueryRowContext(ctx, `SELECT execution_id, contract_version FROM execution_authority_bindings WHERE execution_id='native-pause'`).Scan(&bindingID, &bindingContract); err != nil || bindingID != "native-pause" || bindingContract != NativeCheckpointContract {
		t.Fatalf("binding=%q contract=%q err=%v", bindingID, bindingContract, err)
	}
	if _, err := repo.SaveNativeCheckpointAndPause(ctx, checkpoint, binding, admitted.Revision); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale pause error=%v", err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "native_checkpoint_before_commit" {
			return errors.New("injected atomic pause failure")
		}
		return nil
	})
	secondCheckpoint := checkpoint
	secondCheckpoint.NativeCheckpointID = "checkpoint-2"
	secondCheckpoint.NativeCheckpointDigest = "checkpoint-digest-2"
	secondBinding := binding
	secondBinding.NativeCheckpointID = "checkpoint-2"
	secondBinding.NativeCheckpointDigest = "checkpoint-digest-2"
	if _, err := repo.SaveNativeCheckpointAndPause(ctx, secondCheckpoint, secondBinding, paused.Revision); err == nil {
		t.Fatal("atomic pause failure unexpectedly committed")
	}
	store.SetFailureInjector(nil)
	latest, err := repo.GetExecution(ctx, "native-pause")
	if err != nil || latest.Revision != paused.Revision || latest.Status != ExecutionPaused {
		t.Fatalf("execution after rollback=%#v err=%v", latest, err)
	}
	storedCheckpoint, err = repo.GetCheckpoint(ctx, "native-pause")
	if err != nil || storedCheckpoint.NativeCheckpointID != "checkpoint-1" {
		t.Fatalf("checkpoint after rollback=%#v err=%v", storedCheckpoint, err)
	}
}

func ptrInt64(value int64) *int64 { return &value }
