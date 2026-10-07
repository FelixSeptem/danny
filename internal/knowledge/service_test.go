package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestCompilationServiceCommitsOnlyValidatedPlan(t *testing.T) {
	committer := &fakePlanCommitter{}
	service := NewCompilationService(NewCompiler(), committer)
	result, err := service.Compile(context.Background(), CompileRequest{PlanID: "plan", GenerationID: 1, EffectKey: "effect", Inputs: []SourceInput{{Path: "a.md", Content: "# A\nbody"}}})
	if err != nil || !result.Committed || committer.plan.PlanID != "plan" {
		t.Fatalf("result=%#v err=%v committer=%#v", result, err, committer)
	}
	err = service.Commit(context.Background(), CompilationPlan{PlanID: "injected", GenerationID: 2, EffectKey: "effect-2", Sources: []SourceDecision{{Kind: SourceAdded, Source: SourceMetadata{SourceID: "source", Path: "a.md", ContentHash: "hash"}}}, Artifacts: []ArtifactCandidate{{ID: "bad", Kind: ArtifactSummary, Content: ""}}})
	if !errors.Is(err, ErrCompilationInvalid) || committer.calls != 1 {
		t.Fatalf("invalid commit err=%v calls=%d", err, committer.calls)
	}
}

type fakePlanCommitter struct {
	plan  CompilationPlan
	calls int
}

func (f *fakePlanCommitter) Commit(_ context.Context, plan CompilationPlan) error {
	f.calls++
	f.plan = plan
	return nil
}
