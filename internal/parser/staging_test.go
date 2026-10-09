package parser

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestStagingStoreRestrictsObjectIDsAndPublishesAtomically(t *testing.T) {
	root := t.TempDir()
	store := StagingStore{Root: root}
	if _, err := store.Put("../escape", bytes.NewBufferString("bad")); err == nil {
		t.Fatal("path traversal was accepted")
	}
	if _, err := store.Put("object-1", bytes.NewBufferString("hello")); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, "object-1"))
	if err != nil || string(content) != "hello" {
		t.Fatalf("staged content = %q, err=%v", content, err)
	}
}
