package runtimeprobe_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestOfflineFixtureIsValidAndStable(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "offline.expected.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var bundle runtimeprobe.Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatal(err)
	}
	if err := bundle.Validate(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != "8d5d4c1366b266436b58b0813bd62fa82af6a6141ac395b67d0f27527f007d0c" {
		t.Fatalf("fixture hash changed: %s", got)
	}
}
