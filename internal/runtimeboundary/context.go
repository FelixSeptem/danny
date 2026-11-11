package runtimeboundary

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type BudgetUnit string

const (
	BudgetItems      BudgetUnit = "items"
	BudgetCharacters BudgetUnit = "characters"
	BudgetBytes      BudgetUnit = "bytes"
	BudgetTokens     BudgetUnit = "tokens"
)

type ContextBudget struct {
	Unit  BudgetUnit `json:"unit"`
	Limit int        `json:"limit"`
}

type ContextRequest struct {
	Version          string        `json:"version"`
	RunID            string        `json:"run_id"`
	Scope            string        `json:"scope"`
	MemoryPath       string        `json:"memory_path,omitempty"`
	PathPrefix       string        `json:"path_prefix,omitempty"`
	SessionID        string        `json:"session_id,omitempty"`
	TurnID           string        `json:"turn_id,omitempty"`
	Query            string        `json:"query"`
	ProjectionIntent string        `json:"projection_intent,omitempty"`
	Budget           ContextBudget `json:"budget"`
	MaxBytes         int           `json:"max_bytes"`
	MaxReferences    int           `json:"max_references"`
	Role             string        `json:"role,omitempty"`
	PolicyDigest     string        `json:"policy_digest"`
}

type Provenance struct {
	SourceID string `json:"source_id"`
	Path     string `json:"path,omitempty"`
	Locator  string `json:"locator,omitempty"`
	Digest   string `json:"digest,omitempty"`
}

type ContextReference struct {
	ID         string     `json:"id"`
	Content    string     `json:"content,omitempty"`
	Citation   string     `json:"citation,omitempty"`
	Provenance Provenance `json:"provenance"`
}

type ContextEnvelope struct {
	Version          string             `json:"version"`
	RunID            string             `json:"run_id"`
	Scope            string             `json:"scope"`
	MemoryPath       string             `json:"memory_path,omitempty"`
	PathPrefix       string             `json:"path_prefix,omitempty"`
	SessionID        string             `json:"session_id,omitempty"`
	TurnID           string             `json:"turn_id,omitempty"`
	Role             string             `json:"role,omitempty"`
	ProjectionIntent string             `json:"projection_intent,omitempty"`
	Budget           ContextBudget      `json:"budget"`
	References       []ContextReference `json:"references"`
	Diagnostics      json.RawMessage    `json:"diagnostics,omitempty"`
	DiagnosticsState string             `json:"diagnostics_state"`
	SourceDigest     string             `json:"source_digest"`
	ProjectionDigest string             `json:"projection_digest"`
	PolicyDigest     string             `json:"policy_digest"`
}

func (r ContextRequest) Validate() error {
	if r.Version != ContextContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if strings.TrimSpace(r.RunID) == "" || strings.TrimSpace(r.Scope) == "" || strings.TrimSpace(r.Query) == "" || r.Budget.Limit <= 0 || r.PolicyDigest == "" {
		return ErrMissingAuthority
	}
	if r.Budget.Unit != BudgetItems && r.Budget.Unit != BudgetCharacters && r.Budget.Unit != BudgetBytes && r.Budget.Unit != BudgetTokens {
		return fmt.Errorf("runtime boundary: invalid budget unit %q", r.Budget.Unit)
	}
	if r.MaxBytes <= 0 || r.MaxReferences <= 0 {
		return fmt.Errorf("runtime boundary: response bounds are required")
	}
	if r.SessionID == "" {
		return ErrMissingAuthority
	}
	if r.MemoryPath != "" && r.PathPrefix != "" {
		return fmt.Errorf("runtime boundary: memory path and path prefix are mutually exclusive")
	}
	if len(r.MemoryPath) > 512 || len(r.PathPrefix) > 512 {
		return fmt.Errorf("runtime boundary: path selector exceeds 512 characters")
	}
	return nil
}

func (e ContextEnvelope) Validate(req ContextRequest) error {
	if e.Version != ContextContractV1 || e.RunID != req.RunID || e.Scope != req.Scope || e.MemoryPath != req.MemoryPath || e.PathPrefix != req.PathPrefix || e.SessionID != req.SessionID || e.TurnID != req.TurnID || e.Role != req.Role || e.ProjectionIntent != req.ProjectionIntent || e.Budget != req.Budget {
		return fmt.Errorf("runtime boundary: envelope authority mismatch")
	}
	if e.PolicyDigest != req.PolicyDigest || e.SourceDigest == "" || e.ProjectionDigest == "" {
		return ErrMissingAuthority
	}
	if e.DiagnosticsState != DiagnosticsOmitted && e.DiagnosticsState != DiagnosticsPresent {
		return fmt.Errorf("runtime boundary: invalid diagnostics state")
	}
	if e.DiagnosticsState == DiagnosticsPresent && len(e.Diagnostics) == 0 {
		return fmt.Errorf("runtime boundary: diagnostics marked present but omitted")
	}
	if e.DiagnosticsState == DiagnosticsOmitted && len(e.Diagnostics) != 0 {
		return fmt.Errorf("runtime boundary: diagnostics marked omitted but present")
	}
	if len(e.References) > req.MaxReferences {
		return fmt.Errorf("runtime boundary: reference bound exceeded")
	}
	for _, ref := range e.References {
		if ref.ID == "" || ref.Provenance.SourceID == "" {
			return fmt.Errorf("runtime boundary: reference provenance is required")
		}
	}
	return nil
}

const (
	DiagnosticsOmitted = "omitted"
	DiagnosticsPresent = "present"
)

func (e ContextEnvelope) ComputeProjectionDigest() string {
	copyEnvelope := e
	copyEnvelope.ProjectionDigest = ""
	raw, _ := json.Marshal(copyEnvelope)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
