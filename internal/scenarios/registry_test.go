package scenarios

import (
	"strings"
	"testing"
)

// TestRegistryCoversEveryRequiredScenario is the registry invariant: the
// release gate reads this registry, so it must not be able to claim a
// scenario that has no entry, no reason, or no implemented proof.
func TestRegistryCoversEveryRequiredScenario(t *testing.T) {
	registry := Registry()
	seen := make(map[string]bool, len(registry))
	for _, scenario := range registry {
		if scenario.ID == "" {
			t.Fatal("a registry entry has an empty id")
		}
		if seen[scenario.ID] {
			t.Fatalf("scenario %s is registered twice", scenario.ID)
		}
		seen[scenario.ID] = true
		if scenario.Title == "" {
			t.Fatalf("scenario %s has no title", scenario.ID)
		}
		if scenario.Proof == "" {
			t.Fatalf("scenario %s has no proof reference", scenario.ID)
		}
		if scenario.Evidence == "" {
			t.Fatalf("scenario %s has no evidence location", scenario.ID)
		}
		if scenario.Reason == "" {
			t.Fatalf("scenario %s has no reason", scenario.ID)
		}
		switch scenario.Class {
		case ClassExecuted:
			proof, ok := Implemented[scenario.ID]
			if !ok {
				t.Fatalf("scenario %s is classified executed but has no implemented proof", scenario.ID)
			}
			if proof != scenario.Proof {
				t.Fatalf("scenario %s proof %q does not match the implemented proof %q", scenario.ID, scenario.Proof, proof)
			}
		case ClassLiveRequired:
			if !strings.HasPrefix(scenario.Proof, "cmd/") {
				t.Fatalf("live-required scenario %s must name the command that proves it, got %q", scenario.ID, scenario.Proof)
			}
		case ClassBlockedUpstream, ClassNotExecutable:
			if len(scenario.Reason) < 20 {
				t.Fatalf("scenario %s is classified %s and must explain why", scenario.ID, scenario.Class)
			}
		default:
			t.Fatalf("scenario %s has an unknown class %q", scenario.ID, scenario.Class)
		}
	}
	for _, id := range RequiredIDs {
		if !seen[id] {
			t.Fatalf("required scenario %s is not registered", id)
		}
	}
}

// TestImplementedProofsAreNotOrphaned keeps the implementation list from
// drifting: every proof must belong to a registered scenario.
func TestImplementedProofsAreNotOrphaned(t *testing.T) {
	for id := range Implemented {
		if _, ok := ByID(id); !ok {
			t.Fatalf("implemented proof %s has no registry entry", id)
		}
	}
}

// TestLiveRequiredScenariosAreNeverClaimedWithoutEvidence records the rule
// that a live-required scenario is never counted as a passing check.
func TestLiveRequiredScenariosAreNeverClaimedWithoutEvidence(t *testing.T) {
	for _, scenario := range Registry() {
		if scenario.Class != ClassLiveRequired {
			continue
		}
		if scenario.Proof == "" || scenario.Evidence == "" {
			t.Fatalf("live-required scenario %s must record its command and evidence location", scenario.ID)
		}
	}
}
