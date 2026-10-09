package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FelixSeptem/danny/internal/parser"
)

func TestParserJobRepositoryIsIdempotentAndLeaseFenced(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:parser-jobs-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	want := ParserJob{ID: "job-1", IdempotencyKey: "parse-1", StagingObjectID: "object-1", SourceFingerprint: "source-1", ContentHash: "hash-1", Filename: "note.md", Policy: []byte(`{"max_bytes":100}`)}
	created, err := store.ParserJobs().Create(ctx, want)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := store.ParserJobs().Create(ctx, ParserJob{ID: "job-2", IdempotencyKey: want.IdempotencyKey, StagingObjectID: want.StagingObjectID, SourceFingerprint: want.SourceFingerprint, ContentHash: want.ContentHash, Filename: want.Filename})
	if err != nil || replay.ID != created.ID {
		t.Fatalf("replay = %#v, err=%v", replay, err)
	}
	claimed, err := store.ParserJobs().Claim(ctx, "worker-1", time.Minute)
	if err != nil || claimed.Status != string(parser.StatusRunning) || claimed.AttemptCount != 1 {
		t.Fatalf("claim = %#v, err=%v", claimed, err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, claimed.ID, "worker-2", []byte(`{}`), nil, string(parser.StatusSucceeded), "local"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale result error = %v, want ErrLeaseLost", err)
	}
	finished, err := store.ParserJobs().SaveResult(ctx, claimed.ID, "worker-1", []byte(`{"ok":true}`), nil, string(parser.StatusSucceeded), "local")
	if err != nil || finished.Status != string(parser.StatusSucceeded) {
		t.Fatalf("finished = %#v, err=%v", finished, err)
	}
}

func TestParserJobCancellationFencesInFlightResult(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:parser-cancel-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	created, err := store.ParserJobs().Create(ctx, ParserJob{
		ID: "cancel-job", IdempotencyKey: "cancel-key", StagingObjectID: "cancel-object",
		SourceFingerprint: "cancel-source", ContentHash: "cancel-hash", Filename: "note.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := store.ParserJobs().Claim(ctx, "worker-1", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ParserJobs().RequestCancel(ctx, created.ID); err != nil {
		t.Fatalf("RequestCancel() error = %v", err)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, claimed.ID, "worker-1", []byte(`{"ok":true}`), nil, string(parser.StatusSucceeded), "local"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale result after cancellation error = %v, want ErrLeaseLost", err)
	}
	job, err := store.ParserJobs().Get(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if job.Status != string(parser.StatusCancelled) {
		t.Fatalf("status after cancellation = %q, want %q", job.Status, parser.StatusCancelled)
	}
	if len(job.Result) != 0 {
		t.Fatalf("cancelled job has result %q", job.Result)
	}
}

func TestParserJobExpiredLeaseIsReclaimedOnce(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:parser-recovery-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	created, err := store.ParserJobs().Create(ctx, ParserJob{
		ID: "recovery-job", IdempotencyKey: "recovery-key", StagingObjectID: "recovery-object",
		SourceFingerprint: "recovery-source", ContentHash: "recovery-hash", Filename: "note.md",
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.ParserJobs().Claim(ctx, "worker-1", -time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.ParserJobs().Claim(ctx, "worker-2", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != created.ID || second.AttemptCount != first.AttemptCount+1 || second.Owner != "worker-2" {
		t.Fatalf("reclaimed job = %#v, want owner worker-2 and incremented attempt", second)
	}
	if _, err := store.ParserJobs().SaveResult(ctx, first.ID, "worker-1", []byte(`{"stale":true}`), nil, string(parser.StatusSucceeded), "local"); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("stale recovered result error = %v, want ErrLeaseLost", err)
	}
	finished, err := store.ParserJobs().SaveResult(ctx, second.ID, "worker-2", []byte(`{"ok":true}`), nil, string(parser.StatusSucceeded), "local")
	if err != nil {
		t.Fatal(err)
	}
	if finished.Status != string(parser.StatusSucceeded) || string(finished.Result) != `{"ok":true}` {
		t.Fatalf("finished recovery job = %#v", finished)
	}
}
