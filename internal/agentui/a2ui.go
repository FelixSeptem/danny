package agentui

import (
	"encoding/json"
	"fmt"
)

// A2UIWireVersion is the only A2UI version Danny claims. v1.0 is a Candidate
// with an unverified React renderer; no feature may depend on it.
const A2UIWireVersion = "0.9.1"

// a2uiComponent is one wire component in the A2UI v0.9.1 declarative JSON
// shape: a component reference and its data props. It is never executable
// code; renderers draw it with host-owned implementations.
type a2uiComponent struct {
	Type     string          `json:"type"`
	Props    json.RawMessage `json:"props,omitempty"`
	Children []a2uiComponent `json:"children,omitempty"`
}

// a2uiAction is one wire action binding.
type a2uiAction struct {
	ID          string          `json:"id"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	PolicyRef   string          `json:"policy_ref"`
}

// a2uiSurface is the A2UI v0.9.1 wire surface.
type a2uiSurface struct {
	A2UIVersion string          `json:"a2ui_version"`
	SurfaceID   string          `json:"surface_id"`
	Revision    int64           `json:"revision"`
	Catalog     string          `json:"catalog_version"`
	Components  []a2uiComponent `json:"components"`
	Actions     []a2uiAction    `json:"actions"`
}

// RenderA2UI renders an approved surface into the A2UI v0.9.1 wire format.
// Every component and action must resolve to the registered catalog: model
// generated names never reach the wire.
func RenderA2UI(catalog *Catalog, surface DynamicSurface) ([]byte, error) {
	if surface.CatalogVersion != catalog.Version {
		return nil, fmt.Errorf("agentui: surface catalog %q does not match registered %q", surface.CatalogVersion, catalog.Version)
	}
	var walk func(nodes []SurfaceNode) ([]a2uiComponent, error)
	walk = func(nodes []SurfaceNode) ([]a2uiComponent, error) {
		out := make([]a2uiComponent, 0, len(nodes))
		for _, node := range nodes {
			if !catalog.RegisteredComponent(node.Component) {
				return nil, fmt.Errorf("agentui: component %q is not registered", node.Component)
			}
			children, err := walk(node.Children)
			if err != nil {
				return nil, err
			}
			out = append(out, a2uiComponent{Type: node.Component, Props: node.Props, Children: children})
		}
		return out, nil
	}
	components, err := walk(surface.Tree)
	if err != nil {
		return nil, err
	}
	actions := make([]a2uiAction, 0, len(surface.Actions))
	for _, id := range surface.Actions {
		registered, ok := catalog.RegisteredAction(id)
		if !ok {
			return nil, fmt.Errorf("agentui: action %q is not registered", id)
		}
		actions = append(actions, a2uiAction{ID: registered.ID, InputSchema: registered.InputSchema, PolicyRef: registered.PolicyRef})
	}
	wire := a2uiSurface{
		A2UIVersion: A2UIWireVersion,
		SurfaceID:   surface.ID,
		Revision:    surface.Revision,
		Catalog:     surface.CatalogVersion,
		Components:  components,
		Actions:     actions,
	}
	raw, err := json.MarshalIndent(wire, "", "  ")
	if err != nil {
		return nil, err
	}
	return raw, nil
}
