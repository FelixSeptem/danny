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

func TestHistoricalSteleEvidenceRecordsBlockedPublicRoutes(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-7ffd-live.evidence.json")
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
	if bundle.SteleVersion != "v0.0.0-20261019065620-7ffd30f9e635" {
		t.Fatalf("stele version=%q", bundle.SteleVersion)
	}
	turnBlocked, retrieveBlocked, replayPassed := false, false, false
	compilationParentBlocked, compilationMemoryBlocked, compilationTerminalBlocked := false, false, false
	for _, event := range bundle.Events {
		if event.Payload["path"] == "/v1/provider/turns" && event.Payload["error_code"] == "disclosure_denied" {
			turnBlocked = true
		}
		if event.Payload["retrieve_path"] == "/v1/provider/retrieve" && event.Payload["retrieve_error_code"] == "disclosure_denied" {
			retrieveBlocked = true
		}
		if event.Capability == "idempotent" && event.Payload["replayed"] == "true" && event.Status == runtimeprobe.StatusPassed {
			replayPassed = true
		}
		if event.Capability == "compilation" && event.Status == runtimeprobe.StatusFailedContract {
			switch event.Kind {
			case runtimeprobe.KindParent:
				compilationParentBlocked = true
			case runtimeprobe.KindMemory:
				if event.Payload["lookup_error"] == "stele /v1/provider/retrieve returned 403 Forbidden" &&
					event.Payload["reference_found"] == "false" &&
					event.Payload["replayed"] == "true" {
					compilationMemoryBlocked = true
				}
			case runtimeprobe.KindTerminal:
				compilationTerminalBlocked = true
			}
		}
	}
	if !turnBlocked || !retrieveBlocked || !replayPassed || !compilationParentBlocked || !compilationMemoryBlocked || !compilationTerminalBlocked {
		t.Fatalf("historical pin evidence missing route gate result: turns=%v retrieve=%v replay=%v compilation_parent=%v compilation_memory=%v compilation_terminal=%v", turnBlocked, retrieveBlocked, replayPassed, compilationParentBlocked, compilationMemoryBlocked, compilationTerminalBlocked)
	}
}

func TestCurrentPinnedSteleEvidenceRecordsTurnFailureAndPassingRoutes(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-b631-live.evidence.json")
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
	if bundle.SteleVersion != "v0.0.0-20261024024237-b63151c0464d" {
		t.Fatalf("stele version=%q", bundle.SteleVersion)
	}
	turnFailed, memoryPassed, restartPassed, replayPassed := false, false, false, false
	for _, event := range bundle.Events {
		if event.Capability == "context" && event.Payload["path"] == "/v1/provider/turns" && event.Payload["error"] == "stele /v1/provider/turns returned 400 Bad Request" {
			turnFailed = true
		}
		if event.Capability == "memory" && event.Status == runtimeprobe.StatusPassed && event.Payload["retrieve_path"] == "/v1/provider/retrieve" {
			memoryPassed = true
		}
		if event.Capability == "restart" && event.Status == runtimeprobe.StatusPassed {
			restartPassed = true
		}
		if event.Capability == "idempotent" && event.Status == runtimeprobe.StatusPassed && event.Payload["replayed"] == "true" {
			replayPassed = true
		}
	}
	if !turnFailed || !memoryPassed || !restartPassed || !replayPassed {
		t.Fatalf("current pin evidence missing observed results: turn_failed=%v memory_passed=%v restart_passed=%v replay_passed=%v", turnFailed, memoryPassed, restartPassed, replayPassed)
	}
}

func TestLatestSteleEvidenceRetainsTurnBlockAfterSessionBootstrapFix(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-8ae6028-live.evidence.json")
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
	if bundle.SteleVersion != "v0.0.0-20261024024237-b63151c0464d" {
		t.Fatalf("stele version=%q", bundle.SteleVersion)
	}
	turnBlocked, memoryPassed, restartPassed, replayPassed := false, false, false, false
	for _, event := range bundle.Events {
		if event.Capability == "context" && event.Payload["path"] == "/v1/provider/turns" && event.Payload["error"] == "stele /v1/provider/turns returned 400 Bad Request" {
			turnBlocked = true
		}
		if event.Capability == "memory" && event.Status == runtimeprobe.StatusPassed && event.Payload["retrieve_path"] == "/v1/provider/retrieve" {
			memoryPassed = true
		}
		if event.Capability == "restart" && event.Status == runtimeprobe.StatusPassed {
			restartPassed = true
		}
		if event.Capability == "idempotent" && event.Status == runtimeprobe.StatusPassed && event.Payload["replayed"] == "true" {
			replayPassed = true
		}
	}
	if !turnBlocked || !memoryPassed || !restartPassed || !replayPassed {
		t.Fatalf("latest pin evidence missing bootstrap regression result: turn_blocked=%v memory_passed=%v restart_passed=%v replay_passed=%v", turnBlocked, memoryPassed, restartPassed, replayPassed)
	}
}

func TestLatestSteleEvidenceRetainsTurnBlockAfterOptionalConversationFix(t *testing.T) {
	path := filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-09e51a7-live.evidence.json")
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
	if bundle.SteleVersion != "v0.0.0-20261024024237-b63151c0464d" {
		t.Fatalf("stele version=%q", bundle.SteleVersion)
	}
	turnBlocked, memoryPassed, restartPassed, replayPassed := false, false, false, false
	for _, event := range bundle.Events {
		if event.Capability == "context" && event.Payload["path"] == "/v1/provider/turns" && event.Payload["error"] == "stele /v1/provider/turns returned 400 Bad Request" {
			turnBlocked = true
		}
		if event.Capability == "memory" && event.Status == runtimeprobe.StatusPassed && event.Payload["retrieve_path"] == "/v1/provider/retrieve" {
			memoryPassed = true
		}
		if event.Capability == "restart" && event.Status == runtimeprobe.StatusPassed {
			restartPassed = true
		}
		if event.Capability == "idempotent" && event.Status == runtimeprobe.StatusPassed && event.Payload["replayed"] == "true" {
			replayPassed = true
		}
	}
	if !turnBlocked || !memoryPassed || !restartPassed || !replayPassed {
		t.Fatalf("latest pin evidence missing optional-conversation regression result: turn_blocked=%v memory_passed=%v restart_passed=%v replay_passed=%v", turnBlocked, memoryPassed, restartPassed, replayPassed)
	}
}
