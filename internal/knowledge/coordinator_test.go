package knowledge

import (
	"context"
	"errors"
	"testing"
)

func TestCompileCoordinatorBlocksCommitWhenChildFails(t *testing.T) {
	executor := &fakeCompileExecutor{children: []CompileChildResult{{ID: "child-1", Status: "failed", Error: "model failed"}}}
	coordinator := NewCompileCoordinator(executor)
	plan := CompilationPlan{PlanID: "plan", GenerationID: 1, EffectKey: "effect", CapabilityDigest: "cap", Sources: []SourceDecision{{Kind: SourceAdded, Source: SourceMetadata{SourceID: "source", Path: "a.md", ContentHash: "hash"}}}, Artifacts: []ArtifactCandidate{{ID: "summary", Kind: ArtifactSummary, Content: "summary", Citations: []Citation{{SourceID: "source"}}}}}
	result, err := coordinator.Run(context.Background(), CompileRunRequest{RunID: "run", ProjectionDigest: "projection", Plan: plan, SectionIDs: []string{"section-1"}})
	if !errors.Is(err, ErrCompileBlocked) || result.Committed {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

type fakeCompileExecutor struct {
	children []CompileChildResult
	commits  int
}

func (f *fakeCompileExecutor) Fanout(context.Context, CompileFanoutRequest) ([]CompileChildResult, error) {
	return f.children, nil
}
func (f *fakeCompileExecutor) Aggregate(context.Context, []CompileChildResult) error { return nil }
func (f *fakeCompileExecutor) Vote(context.Context, []CompileChildResult) (CompileVoteResult, error) {
	return CompileVoteResult{Approved: true}, nil
}
func (f *fakeCompileExecutor) Commit(context.Context, CompilationPlan) error { f.commits++; return nil }
