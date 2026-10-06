package runtimeprobe_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/FelixSeptem/danny/internal/runtimeprobe"
)

func TestSteleComposeEvidenceFixtureIsReviewable(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-compose.evidence.json")
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
	seenContext, seenRestart, seenReplay, seenMemory := false, false, false, false
	for _, event := range bundle.Events {
		if event.Capability == "context" && event.Status == runtimeprobe.StatusPassed {
			seenContext = true
		}
		if event.Capability == "restart" && event.Status == runtimeprobe.StatusPassed {
			seenRestart = true
		}
		if event.Capability == "idempotent" && event.Status == runtimeprobe.StatusPassed && event.Payload["replayed"] == "true" {
			seenReplay = true
		}
		if event.Capability == "memory" && event.Status == runtimeprobe.StatusPassed {
			seenMemory = true
		}
	}
	if !seenContext || !seenRestart || !seenReplay || !seenMemory {
		t.Fatalf("fixture missing expected capability evidence: context=%v restart=%v replay=%v memory=%v", seenContext, seenRestart, seenReplay, seenMemory)
	}
}
