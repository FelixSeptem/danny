package runtimeprobe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const EvidenceSchemaVersion = "runtime-probe.v1"

type Status string

const (
	StatusPassed              Status = "passed"
	StatusFailedContract      Status = "failed-contract"
	StatusSkippedUnconfigured Status = "skipped-unconfigured"
	StatusBlockedUpstream     Status = "blocked-upstream"
)

type Kind string

const (
	KindParent    Kind = "parent"
	KindChild     Kind = "child"
	KindTeam      Kind = "team"
	KindMailbox   Kind = "mailbox"
	KindScheduler Kind = "scheduler"
	KindContext   Kind = "context"
	KindMemory    Kind = "memory"
	KindTerminal  Kind = "terminal"
)

type Event struct {
	Sequence      int64             `json:"sequence"`
	ID            string            `json:"id"`
	Kind          Kind              `json:"kind"`
	ParentID      string            `json:"parent_id,omitempty"`
	CorrelationID string            `json:"correlation_id"`
	Capability    string            `json:"capability,omitempty"`
	Status        Status            `json:"status"`
	At            time.Time         `json:"at"`
	Payload       map[string]string `json:"payload,omitempty"`
}

type Bundle struct {
	SchemaVersion string  `json:"schema_version"`
	BaymaxVersion string  `json:"baymax_version"`
	SteleVersion  string  `json:"stele_version"`
	Events        []Event `json:"events"`
	// ProviderCompat records the deviation the harness had to absorb to reach
	// the provider, so a passing bundle never hides that the provider was not
	// spoken to on its own terms. Empty means the provider was called as-is.
	ProviderCompat string `json:"provider_compat,omitempty"`
}

func (b Bundle) Validate() error {
	if b.SchemaVersion != EvidenceSchemaVersion {
		return fmt.Errorf("schema_version must be %q", EvidenceSchemaVersion)
	}
	if strings.TrimSpace(b.BaymaxVersion) == "" || strings.TrimSpace(b.SteleVersion) == "" {
		return fmt.Errorf("dependency versions are required")
	}
	if len(b.Events) == 0 {
		return fmt.Errorf("events must not be empty")
	}
	seen := make(map[string]Event, len(b.Events))
	terminalByCorrelation := make(map[string]bool)
	for index, event := range b.Events {
		wantSequence := int64(index + 1)
		if event.Sequence != wantSequence {
			return fmt.Errorf("event %q has sequence %d, want %d", event.ID, event.Sequence, wantSequence)
		}
		if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.CorrelationID) == "" {
			return fmt.Errorf("event %d requires id and correlation_id", event.Sequence)
		}
		if _, exists := seen[event.ID]; exists {
			return fmt.Errorf("event id %q is duplicated", event.ID)
		}
		if !validKind(event.Kind) {
			return fmt.Errorf("event %q has invalid kind %q", event.ID, event.Kind)
		}
		if !validStatus(event.Status) {
			return fmt.Errorf("event %q has invalid status %q", event.ID, event.Status)
		}
		if event.At.IsZero() {
			return fmt.Errorf("event %q requires timestamp", event.ID)
		}
		if event.Kind != KindParent {
			if strings.TrimSpace(event.ParentID) == "" {
				return fmt.Errorf("event %q requires parent_id", event.ID)
			}
			if _, exists := seen[event.ParentID]; !exists {
				return fmt.Errorf("event %q references unknown parent %q", event.ID, event.ParentID)
			}
			if event.ParentID == event.ID {
				return fmt.Errorf("event %q cannot reference itself", event.ID)
			}
			if !hasAncestor(seen, event.ParentID, event.CorrelationID) {
				return fmt.Errorf("event %q does not link to an ancestor with correlation %q", event.ID, event.CorrelationID)
			}
		}
		if event.Kind == KindTerminal {
			if terminalByCorrelation[event.CorrelationID] {
				return fmt.Errorf("correlation %q has duplicate terminal events", event.CorrelationID)
			}
			terminalByCorrelation[event.CorrelationID] = true
		}
		seen[event.ID] = event
	}
	return nil
}

func hasAncestor(seen map[string]Event, id, correlation string) bool {
	visited := map[string]bool{}
	for id != "" && !visited[id] {
		visited[id] = true
		event, ok := seen[id]
		if !ok {
			return false
		}
		if event.CorrelationID == correlation {
			return true
		}
		id = event.ParentID
	}
	return false
}

func validKind(kind Kind) bool {
	switch kind {
	case KindParent, KindChild, KindTeam, KindMailbox, KindScheduler, KindContext, KindMemory, KindTerminal:
		return true
	default:
		return false
	}
}

func validStatus(status Status) bool {
	switch status {
	case StatusPassed, StatusFailedContract, StatusSkippedUnconfigured, StatusBlockedUpstream:
		return true
	default:
		return false
	}
}

// Redact returns a copy safe for evidence persistence. Secret values are removed,
// and fields that can contain prompts or provider payloads are represented by a digest.
func Redact(bundle Bundle, secrets []string, maxValueBytes int) (Bundle, error) {
	if maxValueBytes <= 0 {
		return Bundle{}, fmt.Errorf("maxValueBytes must be positive")
	}
	out := bundle
	out.Events = make([]Event, len(bundle.Events))
	for index, event := range bundle.Events {
		out.Events[index] = event
		if event.Payload == nil {
			continue
		}
		out.Events[index].Payload = make(map[string]string, len(event.Payload))
		for key, value := range event.Payload {
			clean := value
			for _, secret := range secrets {
				if secret != "" {
					clean = strings.ReplaceAll(clean, secret, "[REDACTED]")
				}
			}
			lowerKey := strings.ToLower(key)
			if isSensitiveKey(lowerKey) || len(clean) > maxValueBytes || strings.Contains(clean, "[REDACTED]") {
				clean = digest(clean)
			}
			out.Events[index].Payload[key] = clean
		}
	}
	if err := out.Validate(); err != nil {
		return Bundle{}, err
	}
	return out, nil
}

func isSensitiveKey(key string) bool {
	for _, fragment := range []string{"authorization", "token", "secret", "password", "prompt", "payload", "body", "header"} {
		if strings.Contains(key, fragment) {
			return true
		}
	}
	return false
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (b Bundle) MarshalJSON() ([]byte, error) {
	type alias Bundle
	return json.Marshal(alias(b))
}
