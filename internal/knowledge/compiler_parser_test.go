package knowledge

import (
	"context"
	"errors"
	"testing"

	"github.com/FelixSeptem/danny/internal/parser"
)

func TestCompilerRejectsFailedParserResult(t *testing.T) {
	_, err := NewCompiler().Plan(context.Background(), CompileRequest{PlanID: "plan", GenerationID: 1, EffectKey: "effect", Inputs: []SourceInput{{Path: "broken.pdf", Content: "not used", ParserStatus: string(parser.StatusResourceLimit), ParserFailure: "page limit"}}})
	if !errors.Is(err, ErrParserResultFailed) {
		t.Fatalf("error = %v", err)
	}
}
