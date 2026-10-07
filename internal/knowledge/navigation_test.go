package knowledge

import (
	"context"
	"testing"
)

func TestNavigatorSearchReturnsOnlyCommittedGenerationReferences(t *testing.T) {
	store := NewMemoryKnowledgeStore()
	store.Put(2, ArtifactCandidate{ID: "concept-1", Kind: ArtifactConcept, Content: "Baymax context", Lineage: []string{"source-1"}, Citations: []Citation{{SourceID: "source-1"}}})
	navigator := NewNavigator(store)
	result, err := navigator.Search(context.Background(), 2, "baymax", ArtifactConcept)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.References) != 1 || result.References[0].GenerationID != 2 || result.References[0].Lineage[0] != "source-1" {
		t.Fatalf("result=%#v", result)
	}
	missing, err := navigator.ReadSource(context.Background(), 1, "concept-1", 100)
	if err == nil || missing.GenerationID != 0 {
		t.Fatalf("staged/missing artifact unexpectedly returned: %#v err=%v", missing, err)
	}
}
