package knowledge

import (
	"encoding/json"
	"testing"
)

func TestNormalizeSourceAndSectionIdentityAreStable(t *testing.T) {
	one := SourceInput{Path: "docs/readme.md", Content: "# Title\r\n\r\n## Child\r\nbody\r\n"}
	two := SourceInput{Path: "docs/readme.md", Content: "# Title\n\n## Child\nbody\n"}
	a, err := NormalizeSource(one)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NormalizeSource(two)
	if err != nil {
		t.Fatal(err)
	}
	if a.ContentHash != b.ContentHash || a.SourceID != b.SourceID || string(a.Content) != string(b.Content) {
		t.Fatalf("normalization differs: %#v %#v", a, b)
	}
	if len(a.Sections) != 2 || a.Sections[1].ParentID != a.Sections[0].ID || a.Sections[1].Range.StartLine != 3 {
		t.Fatalf("sections=%#v", a.Sections)
	}
}

func TestScanDecisionsDistinguishUnchangedChangedAndRemoved(t *testing.T) {
	same, err := NormalizeSource(SourceInput{Path: "a.md", Content: "same"})
	if err != nil {
		t.Fatal(err)
	}
	previous := map[string]SourceMetadata{"a.md": same, "gone.md": {Path: "gone.md", ContentHash: "gone", SourceID: "source-gone"}}
	inputs := []SourceInput{{Path: "a.md", Content: "same"}, {Path: "changed.md", Content: "new"}}
	decisions, err := PlanScan(previous, inputs, true)
	if err != nil {
		t.Fatal(err)
	}
	if decisions[0].Kind != SourceUnchanged || decisions[1].Kind != SourceAdded || decisions[2].Kind != SourceRemoved {
		t.Fatalf("decisions=%#v", decisions)
	}
}

func TestCompilationPlanJSONPreservesLineageAndCitation(t *testing.T) {
	plan := CompilationPlan{PlanID: "plan-1", GenerationID: 4, EffectKey: "compile:4", Sources: []SourceDecision{{Kind: SourceAdded, Source: SourceMetadata{SourceID: "src-1", Path: "a.md", ContentHash: "hash"}}}, Artifacts: []ArtifactCandidate{{ID: "summary:src-1", Kind: ArtifactSummary, Content: "summary", Lineage: []string{"src-1"}, Citations: []Citation{{SourceID: "src-1", SectionID: "section-1", StartLine: 1, EndLine: 2}}}}}
	raw, err := json.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	var decoded CompilationPlan
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GenerationID != 4 || decoded.Artifacts[0].Citations[0].SourceID != "src-1" || decoded.Artifacts[0].Lineage[0] != "src-1" {
		t.Fatalf("decoded=%#v", decoded)
	}
}
