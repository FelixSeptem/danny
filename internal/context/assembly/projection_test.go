package assembly

import (
	"bytes"
	"errors"
	"testing"
)

func TestProjectionRetainsOnlyRoleTrustAndReferenceFacts(t *testing.T) {
	builder := NewBuilder(ProjectionLimits{MaxBytes: 450})
	projection, err := builder.Build(ProjectionRequest{
		Role:             RoleCoordinator,
		Prefix:           "policy-prefix",
		CapabilityDigest: "cap",
		Task:             "analyze section",
		History:          []string{"source history must not enter the Danny projection"},
		PrivateMemory:    "private memory must not enter the Danny projection",
		References:       []Reference{{ID: "source-1", Kind: "source", Locator: "doc.md#intro"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.References) != 1 || projection.References[0].Locator != "doc.md#intro" || projection.References[0].Trust != "untrusted" {
		t.Fatalf("references lost: %#v", projection.References)
	}
	for _, forbidden := range [][]byte{
		[]byte("source history must not enter the Danny projection"),
		[]byte("private memory must not enter the Danny projection"),
		[]byte("handoff.v1"),
		[]byte("max_bytes"),
		[]byte("compacted"),
	} {
		if bytes.Contains(projection.Serialized, forbidden) {
			t.Fatalf("Danny projection retained Baymax-owned material %q: %s", forbidden, projection.Serialized)
		}
	}
}

func TestProjectionRejectsMissingCapabilityDigest(t *testing.T) {
	_, err := NewBuilder(ProjectionLimits{MaxBytes: 512}).Build(ProjectionRequest{Role: RoleCoordinator, Prefix: "prefix"})
	if !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestProjectionRejectsUndeclaredRole(t *testing.T) {
	_, err := NewBuilder(ProjectionLimits{MaxBytes: 512}).Build(ProjectionRequest{Role: Role("untrusted_role"), Prefix: "prefix", CapabilityDigest: "cap"})
	if !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("error = %v, want invalid projection role", err)
	}
}

func TestProjectionMarksSourceReferencesAsUntrusted(t *testing.T) {
	p, err := NewBuilder(ProjectionLimits{MaxBytes: 4096}).Build(ProjectionRequest{Role: RoleSectionAnalyst, Prefix: "prefix", CapabilityDigest: "cap", References: []Reference{{ID: "source-1", Kind: "source", Locator: "note.md"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.References) != 1 || !p.References[0].Untrusted || p.References[0].Trust != "untrusted" {
		t.Fatalf("untrusted references = %#v", p.References)
	}
}

func TestRoleProjectionDoesNotExposeUnauthorizedFields(t *testing.T) {
	p, err := NewBuilder(ProjectionLimits{MaxBytes: 512}).Build(ProjectionRequest{Role: RoleCitationValidator, Prefix: "prefix", CapabilityDigest: "cap", Task: "validate", PrivateMemory: "secret memory", References: []Reference{{ID: "r", Kind: "source", Locator: "doc#1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(p.Serialized, []byte("secret memory")) {
		t.Fatalf("private memory leaked: %s", p.Serialized)
	}
}

func TestEachRoleReceivesOnlyAuthorizedProjectionFields(t *testing.T) {
	cases := []struct {
		role             Role
		history, private bool
	}{
		{RoleCoordinator, true, true},
		{RoleContextReader, true, false},
		{RoleKnowledgeNavigator, false, false},
		{RoleSectionAnalyst, true, true},
		{RoleCitationValidator, false, false},
		{RoleTaskMemoryInterpreter, true, true},
	}
	for _, tc := range cases {
		p, err := NewBuilder(ProjectionLimits{MaxBytes: 4096}).Build(ProjectionRequest{Role: tc.role, Prefix: "prefix", CapabilityDigest: "cap", Task: "task", History: []string{"history"}, PrivateMemory: "private", References: []Reference{{ID: "ref", Kind: "source", Locator: "doc#1"}}})
		if err != nil {
			t.Fatalf("%s: %v", tc.role, err)
		}
		if bytes.Contains(p.Serialized, []byte("history")) || bytes.Contains(p.Serialized, []byte("private")) {
			t.Fatalf("%s retained Baymax-owned fields: %s", tc.role, p.Serialized)
		}
	}
}
