package knowledge

import (
	"context"
	"errors"
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

func TestCompilerRejectsFailedParserWithoutGenerationInputs(t *testing.T) {
	compiler := NewCompiler()
	plan, err := compiler.Plan(context.Background(), CompileRequest{
		PlanID: "failed-parser-plan",
		Inputs: []SourceInput{{Path: "broken.docx", Content: "", ParserStatus: "failed", ParserFailure: "parser crashed"}},
	})
	if !errors.Is(err, ErrParserResultFailed) {
		t.Fatalf("Plan() error = %v, want ErrParserResultFailed", err)
	}
	if len(plan.Sources) != 0 || len(plan.Artifacts) != 0 {
		t.Fatalf("failed parser produced generation inputs: %#v", plan)
	}
}
