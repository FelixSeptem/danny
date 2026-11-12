package agentui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestA2UIFixtureIsCatalogBound generates the A2UI v0.9.1 wire fixture from
// the restricted catalog and verifies every referenced component and action
// resolves to a registered entry. The fixture is the renderer-consumable
// contract shipped for Milestone 10.
func TestA2UIFixtureIsCatalogBound(t *testing.T) {
	catalog := &Catalog{
		Version: CatalogVersion,
		Components: []Component{
			{Name: "Card", PropsSchema: json.RawMessage(`{"properties":{"title":{"type":"string"}}}`)},
			{Name: "ChoiceChips", PropsSchema: json.RawMessage(`{"properties":{"options":{"type":"string"}}}`)},
		},
		Actions: []Action{
			{ID: "confirm", InputSchema: json.RawMessage(`{"required":["choice"],"properties":{"choice":{"type":"string"}}}`), PolicyRef: "danny.policy.task.commit"},
			{ID: "dismiss", InputSchema: json.RawMessage(`{}`), PolicyRef: "danny.policy.read"},
		},
	}
	tree, err := NewTree([]byte(`[{"component":"Card","props":{"title":"Approve deployment"},"children":[{"component":"ChoiceChips","props":{"options":"approve,reject"}}]}]`))
	if err != nil {
		t.Fatalf("NewTree: %v", err)
	}
	surface := DynamicSurface{
		ContractVersion: SurfaceContractV1,
		ID:              "surface-deploy-confirm",
		ExecutionID:     "exec-fixture",
		Revision:        3,
		CatalogVersion:  CatalogVersion,
		Tree:            tree,
		Actions:         []string{"confirm", "dismiss"},
	}
	if err := ValidateSurface(catalog, surface); err != nil {
		t.Fatalf("fixture surface must validate: %v", err)
	}
	wire, err := RenderA2UI(catalog, surface)
	if err != nil {
		t.Fatalf("RenderA2UI: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(wire, &doc); err != nil {
		t.Fatalf("fixture is not JSON: %v", err)
	}
	if doc["a2ui_version"] != A2UIWireVersion {
		t.Fatalf("fixture version = %v, want %s", doc["a2ui_version"], A2UIWireVersion)
	}

	// Unregistered entries must fail the rendering path too.
	poisoned := surface
	poisonedTree, _ := NewTree([]byte(`[{"component":"EvilWidget"}]`))
	poisoned.Tree = poisonedTree
	if _, err := RenderA2UI(catalog, poisoned); err == nil {
		t.Fatal("unregistered component must fail rendering")
	}

	// Golden file: regenerate with -update.
	const golden = "testdata/surface-fixture.a2ui.json"
	if os.Getenv("UPDATE_FIXTURES") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, wire, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	stored, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden fixture missing (run with UPDATE_FIXTURES=1): %v", err)
	}
	if string(stored) != string(wire) {
		t.Fatal("rendered fixture drifted from the golden file")
	}
}

// TestAGUIWireFixture pins the AG-UI wire rendering for one full run so
// client contract tests have a stable artifact.
func TestAGUIWireFixture(t *testing.T) {
	events, err := DeriveExecutionEvents(successRecords())
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	lines := make([]map[string]any, 0, len(events))
	for _, event := range events {
		wire, err := RenderMaps(event)
		if err != nil {
			t.Fatalf("render %s: %v", event.Kind, err)
		}
		for _, wireEvent := range wire {
			raw, err := json.Marshal(wireEvent)
			if err != nil {
				t.Fatal(err)
			}
			var doc map[string]any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatal(err)
			}
			lines = append(lines, doc)
		}
	}
	blob, err := json.MarshalIndent(lines, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	const golden = "testdata/agui-run-fixture.json"
	if os.Getenv("UPDATE_FIXTURES") != "" {
		if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, blob, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	stored, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("golden fixture missing (run with UPDATE_FIXTURES=1): %v", err)
	}
	if string(stored) != string(blob) {
		t.Fatal("AG-UI wire fixture drifted from the golden file")
	}
}
