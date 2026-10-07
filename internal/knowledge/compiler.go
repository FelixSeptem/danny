package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
)

type CompileRequest struct {
	PlanID           string
	GenerationID     int64
	EffectKey        string
	Previous         map[string]SourceMetadata
	Inputs           []SourceInput
	SavedExploration bool
}

type Compiler struct{}

func NewCompiler() Compiler { return Compiler{} }

func (Compiler) Plan(_ context.Context, request CompileRequest) (CompilationPlan, error) {
	decisions, err := PlanScan(request.Previous, request.Inputs, true)
	if err != nil {
		return CompilationPlan{}, err
	}
	plan := CompilationPlan{PlanID: request.PlanID, Version: "compiler.v1", GenerationID: request.GenerationID, EffectKey: request.EffectKey, Sources: decisions, Artifacts: []ArtifactCandidate{}}
	for _, decision := range decisions {
		if decision.Kind == SourceRemoved || decision.Kind == SourceUnchanged {
			continue
		}
		source := decision.Source
		citations := make([]Citation, 0, len(source.Sections))
		for _, section := range source.Sections {
			citations = append(citations, Citation{SourceID: source.SourceID, SectionID: section.ID, StartLine: section.Range.StartLine, EndLine: section.Range.EndLine})
		}
		treeContent, _ := json.Marshal(source.Sections)
		plan.Artifacts = append(plan.Artifacts,
			ArtifactCandidate{ID: string(ArtifactSectionTree) + ":" + source.SourceID, Kind: ArtifactSectionTree, Content: string(treeContent), ContentHash: hashContent(string(treeContent)), Lineage: []string{source.SourceID}, Citations: citations},
			ArtifactCandidate{ID: string(ArtifactSummary) + ":" + source.SourceID, Kind: ArtifactSummary, Content: summarize(source), ContentHash: hashContent(summarize(source)), Lineage: []string{source.SourceID}, Citations: citations},
			ArtifactCandidate{ID: string(ArtifactConcept) + ":" + source.SourceID, Kind: ArtifactConcept, Content: conceptContent(source), ContentHash: hashContent(conceptContent(source)), Lineage: []string{source.SourceID}, Citations: citations},
			ArtifactCandidate{ID: string(ArtifactEntity) + ":" + source.SourceID, Kind: ArtifactEntity, Content: entityContent(source), ContentHash: hashContent(entityContent(source)), Lineage: []string{source.SourceID}, Citations: citations},
		)
	}
	if len(plan.Artifacts) > 0 {
		index := ArtifactCandidate{ID: string(ArtifactIndex) + ":" + fmt.Sprint(request.GenerationID), Kind: ArtifactIndex, Content: indexContent(plan.Artifacts), ContentHash: "", Links: make([]string, 0, len(plan.Artifacts))}
		for _, artifact := range plan.Artifacts {
			index.Links = append(index.Links, artifact.ID)
		}
		index.ContentHash = hashContent(index.Content)
		plan.Artifacts = append(plan.Artifacts, index)
		if request.SavedExploration {
			plan.Artifacts = append(plan.Artifacts, ArtifactCandidate{ID: string(ArtifactExploration) + ":" + fmt.Sprint(request.GenerationID), Kind: ArtifactExploration, Content: index.Content, ContentHash: hashContent(index.Content), Lineage: []string{index.ID}, Links: []string{index.ID}})
		}
	}
	return plan, plan.ValidateError()
}

func summarize(source SourceMetadata) string {
	if len(source.Sections) == 0 {
		return string(source.Content)
	}
	return source.Sections[0].Title
}

func conceptContent(source SourceMetadata) string {
	if len(source.Sections) == 0 {
		return source.Path
	}
	return "concept: " + source.Sections[0].Title
}
func entityContent(source SourceMetadata) string { return "entity: " + source.Path }
func indexContent(artifacts []ArtifactCandidate) string {
	raw, _ := json.Marshal(artifacts)
	return string(raw)
}
