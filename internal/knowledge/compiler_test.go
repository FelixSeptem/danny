package knowledge

import (
	"context"
	"testing"
)

func TestCompilerBuildsIncrementalPlanWithSectionTreeAndSummary(t *testing.T) {
	compiler := NewCompiler()
	plan, err := compiler.Plan(context.Background(), CompileRequest{PlanID: "plan-1", GenerationID: 1, EffectKey: "compile:1", Inputs: []SourceInput{{Path: "a.md", Content: "# Alpha\nbody"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.Validate().Valid() || len(plan.Sources) != 1 || len(plan.Artifacts) < 2 {
		t.Fatalf("plan=%#v validation=%#v", plan, plan.Validate())
	}
	var foundTree, foundSummary bool
	for _, artifact := range plan.Artifacts {
		foundTree = foundTree || artifact.Kind == ArtifactSectionTree
		foundSummary = foundSummary || artifact.Kind == ArtifactSummary
	}
	if !foundTree || !foundSummary {
		t.Fatalf("artifacts=%#v", plan.Artifacts)
	}
}
