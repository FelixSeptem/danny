package agentui

import "encoding/json"

// DefaultCatalog returns the server-registered restricted catalog. It is the
// only production registration point: component and action names are fixed
// here, so model output, imported documents, and browser-provided data can
// never mint a new executable entry. Surface creation and rendering validate
// against this allowlist (ValidateSurface / RenderA2UI).
func DefaultCatalog() *Catalog {
	return &Catalog{
		Version: CatalogVersion,
		Components: []Component{
			{Name: "Card", PropsSchema: json.RawMessage(`{"properties":{"title":{"type":"string"}}}`)},
			{Name: "ChoiceChips", PropsSchema: json.RawMessage(`{"properties":{"options":{"type":"string"}}}`)},
			{Name: "Text", PropsSchema: json.RawMessage(`{"properties":{"value":{"type":"string"}}}`)},
		},
		Actions: []Action{
			{ID: "confirm", InputSchema: json.RawMessage(`{"required":["choice"],"properties":{"choice":{"type":"string"}}}`), PolicyRef: "danny.policy.task.commit"},
			{ID: "dismiss", InputSchema: json.RawMessage(`{}`), PolicyRef: "danny.policy.read"},
		},
	}
}
