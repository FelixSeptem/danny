package storage

import (
	"context"
	"errors"
	"testing"
)

func TestParserJobAndSourceCommitTogether(t *testing.T) {
	ctx := context.Background()
	store, err := OpenSQLite(ctx, "file:parser-transaction?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	store.SetFailureInjector(func(point string) error {
		if point == "state_outbox_commit" {
			return errors.New("injected crash")
		}
		return nil
	})
	err = store.WithTransaction(ctx, func(tx *Transaction) error {
		return tx.CreateParserJobWithSource(ctx, ParserJob{ID: "job-tx", ContractVersion: "v1", IdempotencyKey: "key-tx", StagingObjectID: "object-tx", SourceFingerprint: "source-tx", ContentHash: "hash-tx", Filename: "note.md"}, ParserSourceRecord{ID: "source-tx", Path: "note.md", ContentHash: "hash-tx", Content: []byte("# note\n"), Format: "md", ParserVersion: "native", ProviderMode: "local"})
	})
	if err == nil {
		t.Fatal("transaction unexpectedly committed")
	}
	if _, err := store.ParserJobs().Get(ctx, "job-tx"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("parser job after rollback = %v", err)
	}
	var count int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM source_metadata WHERE id='source-tx'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("source rows after rollback = %d", count)
	}
}
