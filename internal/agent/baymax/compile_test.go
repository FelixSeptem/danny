package baymax

import (
	"context"
	"testing"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/danny/internal/knowledge"
)

func TestCompileExecutorUsesBaymaxTeamAndChildBoundaries(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "ok"}}}))
	if err != nil {
		t.Fatal(err)
	}
	executor := NewCompileExecutor(client)
	children, err := executor.Fanout(context.Background(), knowledge.CompileFanoutRequest{RunID: "run", CapabilityDigest: "cap", SectionIDs: []string{"s1", "s2"}})
	if err != nil || len(children) != 2 {
		t.Fatalf("children=%#v err=%v", children, err)
	}
	if err := executor.Aggregate(context.Background(), children); err != nil {
		t.Fatal(err)
	}
	vote, err := executor.Vote(context.Background(), children)
	if err != nil || !vote.Approved {
		t.Fatalf("vote=%#v err=%v", vote, err)
	}
}
