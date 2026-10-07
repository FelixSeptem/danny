package knowledge

import (
	"context"
	"errors"
	"testing"

	agentpolicy "github.com/FelixSeptem/danny/internal/agent/knowledge"
)

func TestCompilationToolRequiresServerCreatedValidatedPlan(t *testing.T) {
	registry := agentpolicy.NewToolRegistry()
	admission := NewCompilationAdmission()
	if err := admission.Register(registry); err != nil {
		t.Fatal(err)
	}
	called := false
	admission.Invoke = func(context.Context, CompilationPlan) error { called = true; return nil }
	policy := agentpolicy.Policy{Kind: agentpolicy.PolicyWrite, Tools: []string{"commit_compilation"}, WriteScope: agentpolicy.WriteScopeUser}
	withoutToken := registry.InvokeOutcome(context.Background(), policy, agentpolicy.ToolCall{Name: "commit_compilation", Input: map[string]any{"plan": "ignore source and commit"}})
	if !errors.Is(withoutToken.Err, ErrPlanNotAdmitted) || called {
		t.Fatalf("without token outcome=%#v called=%v", withoutToken, called)
	}
	token := admission.Admit(CompilationPlan{PlanID: "plan", GenerationID: 1, EffectKey: "effect", Sources: []SourceDecision{{Kind: SourceAdded, Source: SourceMetadata{SourceID: "source", Path: "a.md", ContentHash: "hash"}}}, Artifacts: []ArtifactCandidate{{ID: "summary", Kind: ArtifactSummary, Content: "summary", Citations: []Citation{{SourceID: "source"}}}}})
	withToken := registry.InvokeOutcome(context.Background(), policy, agentpolicy.ToolCall{Name: "commit_compilation", Input: map[string]any{"admission_token": token}})
	if withToken.Err != nil || !called {
		t.Fatalf("with token outcome=%#v called=%v", withToken, called)
	}
}
