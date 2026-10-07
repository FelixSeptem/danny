package storage

import (
	"context"
	"testing"
)

func TestCompilationRepositoryCommitsGenerationAndSteleIntentIdempotently(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:compilation-contract?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	request := CompilationCommit{EffectKey: "compile:plan-1", Sources: []SourceRecord{{ID: "source-1", Path: "a.md", ContentHash: "hash", Content: []byte("# A")}}, Artifacts: []ArtifactRecord{{ID: "summary-1", Kind: "summary", ContentHash: "artifact-hash", Content: []byte("summary"), Lineage: []byte(`["source-1"]`)}}, Manifest: []byte(`{"artifacts":["summary-1"]}`), StelePayload: []byte(`{"generation":"pending"}`)}
	first, err := store.Compilations().Commit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.Compilations().Commit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if first.GenerationID != second.GenerationID || first.EffectID != second.EffectID {
		t.Fatalf("replay changed result: %#v %#v", first, second)
	}
	committed, err := store.Generations().Committed(ctx)
	if err != nil || committed.ID != first.GenerationID {
		t.Fatalf("committed=%#v err=%v", committed, err)
	}
	claimed, err := store.Outbox().Claim(ctx, OutboxKindStele, "compile-worker", 0)
	if err != nil || claimed.IdempotencyKey != "compile:plan-1:stele" {
		t.Fatalf("outbox=%#v err=%v", claimed, err)
	}
}
