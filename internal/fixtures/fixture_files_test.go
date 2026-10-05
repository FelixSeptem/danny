package fixtures

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckedInManifestMatchesFixtureHashes(t *testing.T) {
	root := filepath.Join("..", "..", "testdata", "fixtures")
	manifest, err := ReadManifest(filepath.Join(root, "manifest.json"))
	if err != nil {
		t.Fatalf("ReadManifest() error = %v", err)
	}
	for _, entry := range manifest.Fixtures {
		data, err := os.ReadFile(filepath.Join(root, entry.Path))
		if err != nil {
			t.Fatalf("read fixture %s: %v", entry.ID, err)
		}
		hash := sha256.Sum256(CanonicalBytes(data))
		if got := hex.EncodeToString(hash[:]); got != entry.SHA256 {
			t.Errorf("fixture %s hash = %s, want %s", entry.ID, got, entry.SHA256)
		}
	}
}
