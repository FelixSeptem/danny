package baymax

import (
	"context"
	"testing"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/danny/internal/knowledge"
)

func TestCompileExecutorUsesBaymaxTeamAndChildBoundaries(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{
		{Response: types.ModelResponse{FinalAnswer: "section one"}},
		{Response: types.ModelResponse{FinalAnswer: "section two"}},
		{Response: types.ModelResponse{FinalAnswer: "aggregate one"}},
		{Response: types.ModelResponse{FinalAnswer: "aggregate two"}},
		{Response: types.ModelResponse{FinalAnswer: "approve"}},
		{Response: types.ModelResponse{FinalAnswer: "approve"}},
	}))
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCompileExecutor(client)
	children, err := executor.Fanout(context.Background(), knowledge.CompileFanoutRequest{RunID: "run", CapabilityDigest: "cap", SectionIDs: []string{"s1", "s2"}})
	if err != nil || len(children) != 2 {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	for _, child := range children {
		if child.Status != "succeeded" || child.ParentRunID != "run" || child.RunID != "run:child:"+child.ID || len(child.SourceIDs) != 1 || child.SourceIDs[0] != child.ID || child.Result == "" {
			t.Fatalf("child lacks real execution finding/citation evidence: %#v", child)
		}
	}
	if err := executor.Aggregate(context.Background(), children); err != nil {
		t.Fatal(err)
	}
	vote, err := executor.Vote(context.Background(), children)
	if err != nil || !vote.Approved {
		t.Fatalf("vote=%#v err=%v", vote, err)
	}
}

func TestCompileExecutorRejectsMissingDecisionEvidence(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "unclear"}}}))
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCompileExecutor(client)
	_, err = executor.Vote(context.Background(), []knowledge.CompileChildResult{{ID: "section-1", RunID: "run:child:section-1", ParentRunID: "run", Status: "succeeded", Result: "section finding", PlanVersion: "compile.v1", CapabilityDigest: "cap", ProjectionDigest: "projection", SourceIDs: []string{"section-1"}}})
	if err == nil {
		t.Fatal("compiler accepted vote without member evidence")
	}
}
