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
	if got := hex.EncodeToString(sum[:]); got != "be4703d95f02d01147d8d51af95f0c01140e03a1dde3445d048f72aaeedfb4fa" {
		t.Fatalf("fixture hash changed: %s", got)
	}
}
