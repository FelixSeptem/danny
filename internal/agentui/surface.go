package agentui

import (
	"encoding/json"
	"fmt"
)

// CatalogVersion is the restricted Danny dynamic-surface catalog version.
const CatalogVersion = "danny.catalog.v1"

// SurfaceContractV1 is the version-independent surface model contract.
const SurfaceContractV1 = "danny.surface.v1"

// Component is one server-registered catalog component. Model-generated
// component names, functions, and props are never executable by default:
// only entries registered here can appear on a surface.
type Component struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	PropsSchema json.RawMessage `json:"props_schema,omitempty"`
}

// Action is one server-registered surface action. An accepted submission
// enters Danny's existing authorization and commit gates; the action never
// expands Baymax tool capabilities by itself.
type Action struct {
	ID          string          `json:"id"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	PolicyRef   string          `json:"policy_ref"`
}

// Catalog is the server-registered component and action allowlist.
type Catalog struct {
	Version    string      `json:"version"`
	Components []Component `json:"components"`
	Actions    []Action    `json:"actions"`
}

// RegisteredComponent reports whether the name is in the catalog.
func (c *Catalog) RegisteredComponent(name string) bool {
	for _, comp := range c.Components {
		if comp.Name == name {
			return true
		}
	}
	return false
}

// RegisteredAction returns the catalog action with the given ID.
func (c *Catalog) RegisteredAction(id string) (Action, bool) {
	for _, act := range c.Actions {
		if act.ID == id {
			return act, true
		}
	}
	return Action{}, false
}

// SurfaceNode is one node of a surface component tree.
type SurfaceNode struct {
	Component string          `json:"component"`
	Props     json.RawMessage `json:"props,omitempty"`
	Children  []SurfaceNode   `json:"children,omitempty"`
}

// DynamicSurface is a version-independent application model: a server-approved
// component tree over the restricted catalog, bound to one execution.
type DynamicSurface struct {
	ContractVersion string        `json:"contract_version"`
	ID              string        `json:"id"`
	ExecutionID     string        `json:"execution_id"`
	Revision        int64         `json:"revision"`
	CatalogVersion  string        `json:"catalog_version"`
	Tree            []SurfaceNode `json:"tree"`
	Actions         []string      `json:"actions"`
	CreatedAt       string        `json:"created_at,omitempty"`
	UpdatedAt       string        `json:"updated_at,omitempty"`
}

// SurfaceAction pairs the action the caller wants with the authority claims
// the submission makes. Claims beyond the registered action are untrusted.
type SurfaceAction struct {
	ActionID       string          `json:"action_id"`
	UserActivation bool            `json:"user_activation"`
	Payload        json.RawMessage `json:"payload"`
}

// SurfaceSubmission is one validated action submission.
type SurfaceSubmission struct {
	ContractVersion string          `json:"contract_version"`
	ExecutionID     string          `json:"execution_id"`
	SurfaceID       string          `json:"surface_id"`
	SurfaceRevision int64           `json:"surface_revision"`
	TaskRevision    int64           `json:"task_revision"`
	IdempotencyKey  string          `json:"idempotency_key"`
	Actions         []SurfaceAction `json:"actions"`
}

// ValidateSurface enforces the restricted-catalog rule: every node references
// a registered component and every surface action is a registered action.
// Untrusted content can never register new entries through this path.
func ValidateSurface(catalog *Catalog, surface DynamicSurface) error {
	if surface.ContractVersion != SurfaceContractV1 {
		return fmt.Errorf("agentui: unsupported surface contract %q", surface.ContractVersion)
	}
	if surface.ID == "" || surface.ExecutionID == "" || surface.CatalogVersion != catalog.Version {
		return fmt.Errorf("agentui: surface requires identity and the current catalog version")
	}
	if surface.Revision <= 0 {
		return fmt.Errorf("agentui: surface revision must be positive")
	}
	seenActions := map[string]struct{}{}
	for _, id := range surface.Actions {
		if _, ok := catalog.RegisteredAction(id); !ok {
			return fmt.Errorf("agentui: surface action %q is not registered", id)
		}
		if _, dup := seenActions[id]; dup {
			return fmt.Errorf("agentui: surface action %q is duplicated", id)
		}
		seenActions[id] = struct{}{}
	}
	for i := range surface.Tree {
		if err := validateNode(catalog, &surface.Tree[i], fmt.Sprintf("tree[%d]", i)); err != nil {
			return err
		}
	}
	return nil
}

// NewTree parses a surface component tree from JSON. Callers bind untrusted
// data here; ValidateSurface enforces the restricted catalog afterwards.
func NewTree(raw []byte) ([]SurfaceNode, error) {
	var tree []SurfaceNode
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("agentui: surface tree is not valid JSON: %w", err)
	}
	return tree, nil
}

func validateNode(catalog *Catalog, node *SurfaceNode, path string) error {
	if !catalog.RegisteredComponent(node.Component) {
		return fmt.Errorf("agentui: component %q at %s is not registered", node.Component, path)
	}
	if len(node.Props) > maxPayloadBytes {
		return fmt.Errorf("agentui: props at %s exceed %d bytes", path, maxPayloadBytes)
	}
	var props map[string]json.RawMessage
	if len(node.Props) > 0 {
		if err := json.Unmarshal(node.Props, &props); err != nil {
			return fmt.Errorf("agentui: props at %s are not a JSON object", path)
		}
		for key := range props {
			if isReservedKey(key) {
				return fmt.Errorf("agentui: reserved prop key %q at %s is not allowed", key, path)
			}
		}
	}
	for i := range node.Children {
		if err := validateNode(catalog, &node.Children[i], path+".children["+fmt.Sprint(i)+"]"); err != nil {
			return err
		}
	}
	return nil
}

// Typed validation errors.
var (
	// ErrStaleSurface marks a submission against an outdated surface revision.
	ErrStaleSurface = fmt.Errorf("agentui: stale surface revision")
	// ErrStaleTask marks a submission against an outdated task revision.
	ErrStaleTask = fmt.Errorf("agentui: stale task revision")
	// ErrUnknownAction marks a submission whose action is not on the surface.
	ErrUnknownAction = fmt.Errorf("agentui: action is not allowed on this surface")
	// ErrSchemaViolation marks a payload that violates the action input schema.
	ErrSchemaViolation = fmt.Errorf("agentui: payload violates the action input schema")
	// ErrMissingActivation marks a submission without user activation.
	ErrMissingActivation = fmt.Errorf("agentui: submission lacks user activation")
	// ErrUnauthorized marks a submission whose policy reference is unauthorized.
	ErrUnauthorized = fmt.Errorf("agentui: submission is not authorized")
	// ErrMalformedSubmission marks an invalid submission envelope.
	ErrMalformedSubmission = fmt.Errorf("agentui: malformed submission")
)

// ValidateSubmission checks one submission against the current surface,
// catalog, and task revision. Payloads claiming different scope, policy, or
// capability than the registered action are rejected as untrusted data.
func ValidateSubmission(catalog *Catalog, surface DynamicSurface, currentTaskRevision int64, sub SurfaceSubmission) error {
	if sub.ContractVersion != SurfaceContractV1 || sub.ExecutionID == "" || sub.SurfaceID == "" || sub.IdempotencyKey == "" {
		return fmt.Errorf("%w: identity, surface and idempotency key are required", ErrMalformedSubmission)
	}
	if sub.SurfaceID != surface.ID || sub.ExecutionID != surface.ExecutionID {
		return fmt.Errorf("%w: submission is bound to a different execution or surface", ErrMalformedSubmission)
	}
	if sub.SurfaceRevision != surface.Revision {
		return fmt.Errorf("%w: submitted %d, current %d", ErrStaleSurface, sub.SurfaceRevision, surface.Revision)
	}
	if sub.TaskRevision > 0 && sub.TaskRevision != currentTaskRevision {
		return fmt.Errorf("%w: submitted %d, current %d", ErrStaleTask, sub.TaskRevision, currentTaskRevision)
	}
	if len(sub.Actions) == 0 {
		return fmt.Errorf("%w: no actions submitted", ErrMalformedSubmission)
	}
	for _, act := range sub.Actions {
		registered, ok := catalog.RegisteredAction(act.ActionID)
		if !ok {
			return fmt.Errorf("%w: %q is not registered", ErrUnknownAction, act.ActionID)
		}
		allowed := false
		for _, id := range surface.Actions {
			if id == act.ActionID {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("%w: %q is not on surface %q", ErrUnknownAction, act.ActionID, surface.ID)
		}
		if !act.UserActivation {
			return fmt.Errorf("%w: action %q", ErrMissingActivation, act.ActionID)
		}
		if registered.PolicyRef == "" {
			return fmt.Errorf("%w: action %q has no policy reference", ErrUnauthorized, act.ActionID)
		}
		if len(act.Payload) > maxPayloadBytes {
			return fmt.Errorf("%w: payload exceeds %d bytes", ErrSchemaViolation, maxPayloadBytes)
		}
		if err := validateAgainstSchema(registered.InputSchema, act.Payload); err != nil {
			return fmt.Errorf("%w: action %q: %v", ErrSchemaViolation, act.ActionID, err)
		}
	}
	return nil
}

// validateAgainstSchema enforces the minimal contract the catalog registers:
// an input schema is a JSON object of required keys with primitive types.
// Rich JSON Schema is deliberately deferred until the Milestone 10 workbench
// needs it; this keeps server validation honest instead of decorative.
func validateAgainstSchema(schema json.RawMessage, payload json.RawMessage) error {
	if len(schema) == 0 {
		return nil
	}
	var spec struct {
		Required []string `json:"required"`
		Props    map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(schema, &spec); err != nil {
		return fmt.Errorf("registered schema is invalid: %v", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(payload, &payloadMap); err != nil {
		return fmt.Errorf("payload is not a JSON object")
	}
	for _, key := range spec.Required {
		if _, ok := payloadMap[key]; !ok {
			return fmt.Errorf("missing required key %q", key)
		}
	}
	for key, want := range spec.Props {
		value, ok := payloadMap[key]
		if !ok {
			continue
		}
		switch want.Type {
		case "string":
			if _, ok := value.(string); !ok {
				return fmt.Errorf("key %q must be a string", key)
			}
		case "number":
			if _, ok := value.(float64); !ok {
				return fmt.Errorf("key %q must be a number", key)
			}
		case "boolean":
			if _, ok := value.(bool); !ok {
				return fmt.Errorf("key %q must be a boolean", key)
			}
		}
	}
	return nil
}
