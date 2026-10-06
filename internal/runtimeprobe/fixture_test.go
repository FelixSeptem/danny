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
	if got := hex.EncodeToString(sum[:]); got != "5d667cb6dc4570393b802bad32564fcab07e50b6a3741baf8f6289a1664a747c" {
		t.Fatalf("fixture hash changed: %s", got)
	}
}
