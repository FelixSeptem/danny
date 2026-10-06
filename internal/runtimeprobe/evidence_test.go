package runtimeprobe

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestEvidenceBundleRejectsSequenceGapsAndDuplicateTerminals(t *testing.T) {
	bundle := Bundle{
		SchemaVersion: "runtime-probe.v1",
		BaymaxVersion: "baymax-test",
		SteleVersion:  "stele-test",
		Events: []Event{
			{Sequence: 1, ID: "parent", Kind: KindParent, CorrelationID: "corr", Status: StatusPassed, At: time.Unix(1, 0)},
			{Sequence: 3, ID: "terminal-a", Kind: KindTerminal, ParentID: "parent", CorrelationID: "corr", Status: StatusPassed, At: time.Unix(2, 0)},
			{Sequence: 4, ID: "terminal-b", Kind: KindTerminal, ParentID: "parent", CorrelationID: "corr", Status: StatusPassed, At: time.Unix(3, 0)},
		},
	}
	if err := bundle.Validate(); err == nil || !strings.Contains(err.Error(), "sequence") {
		t.Fatalf("Validate() error = %v, want sequence error", err)
	}
	bundle.Events[1].Sequence = 2
	if err := bundle.Validate(); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("Validate() error = %v, want duplicate terminal error", err)
	}
}

func TestRedactedEvidenceDoesNotSerializeSecretsOrUnboundedPayload(t *testing.T) {
	bundle := Bundle{
		SchemaVersion: "runtime-probe.v1",
		BaymaxVersion: "baymax-test",
		SteleVersion:  "stele-test",
		Events: []Event{{
			Sequence: 1, ID: "parent", Kind: KindParent, CorrelationID: "corr", Status: StatusPassed, At: time.Unix(1, 0),
			Payload: map[string]string{
				"authorization": "Bearer super-secret-token",
				"prompt":        strings.Repeat("private prompt ", 100),
				"result":        "visible result",
			},
		}},
	}
	redacted, err := Redact(bundle, []string{"super-secret-token"}, 32)
	if err != nil {
		t.Fatalf("Redact() error = %v", err)
	}
	raw, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	serialized := string(raw)
	if strings.Contains(serialized, "super-secret-token") || strings.Contains(serialized, "private prompt") {
		t.Fatalf("redacted evidence contains secret or unbounded content: %s", serialized)
	}
	if !strings.Contains(serialized, "visible result") || !strings.Contains(serialized, "sha256:") {
		t.Fatalf("redacted evidence lost safe content or digest: %s", serialized)
	}
}

func TestEvidenceBundleRejectsBrokenAncestorCorrelation(t *testing.T) {
	bundle := Bundle{
		SchemaVersion: EvidenceSchemaVersion,
		BaymaxVersion: "baymax-test",
		SteleVersion:  "stele-test",
		Events: []Event{
			{Sequence: 1, ID: "parent", Kind: KindParent, CorrelationID: "corr-parent", Status: StatusPassed, At: time.Unix(1, 0)},
			{Sequence: 2, ID: "child", Kind: KindChild, ParentID: "parent", CorrelationID: "corr-other", Status: StatusPassed, At: time.Unix(2, 0)},
		},
	}
	if err := bundle.Validate(); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("Validate() error = %v, want ancestor correlation error", err)
	}
}
