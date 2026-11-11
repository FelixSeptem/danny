package storage

import (
	"context"
	"errors"
	"testing"
)

func TestContextProjectionSnapshotsAreScopeBoundAndDurable(t *testing.T) {
	ctx := context.Background()
	dsn := t.TempDir() + "/context-projection.db"
	store, err := OpenSQLite(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	record := ContextProjectionSnapshot{ID: "projection-1", Scope: "tenant/project/ns", SessionID: "session-1", TurnID: "turn-1", BindingID: "binding-1", RunID: "run-1", Digest: "digest-1", SourceDigest: "source-1", ContractVersion: "danny.context.v1", Envelope: []byte(`{"projection":true}`)}
	if _, err := store.ContextProjections().Save(ctx, record); err != nil {
		t.Fatal(err)
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
	got, err := store.ContextProjections().Get(ctx, "tenant/project/ns", "projection-1")
	if err != nil || got.Digest != record.Digest || string(got.Envelope) != string(record.Envelope) || got.Scope != record.Scope || got.SessionID != record.SessionID || got.TurnID != record.TurnID || got.BindingID != record.BindingID || got.RunID != record.RunID {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	if _, err := store.ContextProjections().Get(ctx, "other/project/ns", "projection-1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign read error=%v", err)
	}
}

func TestContextProjectionSnapshotRejectsDigestReuseWithChangedEnvelope(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:context-projection-conflict?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	first := ContextProjectionSnapshot{ID: "p", Scope: "scope", SessionID: "s", TurnID: "t", BindingID: "b", RunID: "r", Digest: "d", SourceDigest: "source", ContractVersion: "danny.context.v1", Envelope: []byte(`{"a":1}`)}
	if _, err := store.ContextProjections().Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	first.Envelope = []byte(`{"a":2}`)
	if _, err := store.ContextProjections().Save(ctx, first); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("changed digest reuse error=%v", err)
	}
}

func TestContextProjectionSnapshotRefreshUsesNewDigestAndPreservesOldProjection(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:context-projection-refresh?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	repo := store.ContextProjections()
	first := ContextProjectionSnapshot{ID: "digest-1", Scope: "scope", SessionID: "session", TurnID: "turn-1", BindingID: "binding", RunID: "run", Digest: "digest-1", SourceDigest: "source-1", ContractVersion: "danny.context.v1", ContinuityContractVersion: "context-continuity-v1", ContinuityReference: "continuity-1", Envelope: []byte(`{"a":1}`)}
	second := first
	second.ID, second.Digest, second.SourceDigest, second.TurnID = "digest-2", "digest-2", "source-2", "turn-2"
	second.ContinuityReference = "continuity-2"
	second.Envelope = []byte(`{"a":2}`)
	if _, err := repo.Save(ctx, first); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Save(ctx, second); err != nil {
		t.Fatal(err)
	}
	gotFirst, err := repo.Get(ctx, first.Scope, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	gotSecond, err := repo.Get(ctx, second.Scope, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotFirst.Envelope) != `{"a":1}` || string(gotSecond.Envelope) != `{"a":2}` || gotFirst.ContinuityReference != "continuity-1" || gotSecond.ContinuityReference != "continuity-2" {
		t.Fatalf("old=%#v new=%#v", gotFirst, gotSecond)
	}
}
