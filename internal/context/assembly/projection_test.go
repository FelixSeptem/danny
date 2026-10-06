package assembly

import (
	"errors"
	"testing"
)

func TestProjectionCompactsHistoryButPreservesReferences(t *testing.T) {
	builder := NewBuilder(ProjectionLimits{MaxBytes: 450})
	projection, err := builder.Build(ProjectionRequest{Role: RoleSectionAnalyst, Prefix: "policy-prefix", CapabilityDigest: "cap", Task: "analyze section", History: []string{"very long history entry one", "very long history entry two", "very long history entry three"}, References: []Reference{{ID: "source-1", Kind: "source", Locator: "doc.md#intro"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(projection.Serialized) > 450 {
		t.Fatalf("projection bytes = %d", len(projection.Serialized))
	}
	if len(projection.References) != 1 || projection.References[0].Locator != "doc.md#intro" {
		t.Fatalf("references lost: %#v", projection.References)
	}
	if projection.Handoff.Version != "handoff.v1" || projection.Budget.Compacted == false {
		t.Fatalf("handoff/budget missing: %#v", projection)
	}
}

func TestProjectionRejectsMissingCapabilityDigest(t *testing.T) {
	_, err := NewBuilder(ProjectionLimits{MaxBytes: 512}).Build(ProjectionRequest{Role: RoleCoordinator, Prefix: "prefix"})
	if !errors.Is(err, ErrProjectionInvalid) {
		t.Fatalf("error = %v", err)
	}
}

func TestRoleProjectionDoesNotExposeUnauthorizedFields(t *testing.T) {
	p, err := NewBuilder(ProjectionLimits{MaxBytes: 512}).Build(ProjectionRequest{Role: RoleCitationValidator, Prefix: "prefix", CapabilityDigest: "cap", Task: "validate", PrivateMemory: "secret memory", References: []Reference{{ID: "r", Kind: "source", Locator: "doc#1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p.PrivateMemory != "" {
		t.Fatalf("private memory leaked to citation validator: %#v", p)
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
		if (len(p.History) > 0) != tc.history || (p.PrivateMemory != "") != tc.private {
			t.Fatalf("%s fields history=%v private=%v", tc.role, len(p.History) > 0, p.PrivateMemory != "")
		}
	}
}
