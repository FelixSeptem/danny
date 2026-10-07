package knowledge

import (
	"context"
	"testing"
)

func TestCompilationPlanValidationRejectsInvalidReferencesAndPaths(t *testing.T) {
	plan := CompilationPlan{GenerationID: 1, EffectKey: "effect", Sources: []SourceDecision{{Kind: SourceAdded, Source: SourceMetadata{SourceID: "source", Path: "a.md", ContentHash: "hash"}}}, Artifacts: []ArtifactCandidate{{ID: "bad", Kind: ArtifactSummary, Path: "../escape", Content: "claim", Citations: []Citation{{SourceID: "missing"}}, Links: []string{"unknown"}}}}
	result := plan.Validate()
	if result.Valid() || len(result.Diagnostics) < 3 {
		t.Fatalf("validation=%#v", result)
	}
}

func TestNavigationTruncatesBoundedSourceContent(t *testing.T) {
	store := NewMemoryKnowledgeStore()
	generation := int64(7)
	store.Put(generation, ArtifactCandidate{ID: "source-1", Kind: ArtifactSource, Content: "abcdefgh", Lineage: []string{"source-1"}, Citations: []Citation{{SourceID: "source-1", StartLine: 1, EndLine: 1}}})
	navigator := NewNavigator(store)
	result, err := navigator.ReadSource(context.Background(), generation, "source-1", 3)
	if err != nil {
		t.Fatal(err)
	}
	if result.Content != "abc" || !result.Truncated || result.GenerationID != generation {
		t.Fatalf("result=%#v", result)
	}
}
