package assembly

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var ErrProjectionInvalid = errors.New("context projection: invalid request")

type Role string

const (
	RoleCoordinator           Role = "coordinator"
	RoleContextReader         Role = "context_reader"
	RoleKnowledgeNavigator    Role = "knowledge_navigator"
	RoleSectionAnalyst        Role = "section_analyst"
	RoleCitationValidator     Role = "citation_validator"
	RoleTaskMemoryInterpreter Role = "task_memory_interpreter"
)

type Reference struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Locator   string `json:"locator"`
	Untrusted bool   `json:"untrusted"`
}
type ProjectionLimits struct{ MaxBytes int }
type ProjectionRequest struct {
	Role                           Role
	Prefix, CapabilityDigest, Task string
	History                        []string
	References                     []Reference
	PrivateMemory                  string
}
type Handoff struct {
	Version    string      `json:"version"`
	Task       string      `json:"task"`
	References []Reference `json:"references"`
}
type BudgetView struct {
	MaxBytes  int  `json:"max_bytes"`
	UsedBytes int  `json:"used_bytes"`
	Compacted bool `json:"compacted"`
}
type Projection struct {
	Role             Role        `json:"role"`
	Prefix           string      `json:"prefix"`
	CapabilityDigest string      `json:"capability_digest"`
	Task             string      `json:"task,omitempty"`
	History          []string    `json:"history,omitempty"`
	References       []Reference `json:"references"`
	PrivateMemory    string      `json:"private_memory,omitempty"`
	Handoff          Handoff     `json:"handoff"`
	Budget           BudgetView  `json:"budget"`
	Digest           string      `json:"digest"`
	Serialized       []byte      `json:"-"`
}
type Builder struct{ limits ProjectionLimits }

func NewBuilder(limits ProjectionLimits) Builder {
	if limits.MaxBytes <= 0 {
		limits.MaxBytes = 4096
	}
	return Builder{limits: limits}
}
func (b Builder) Build(req ProjectionRequest) (Projection, error) {
	if req.Role == "" || strings.TrimSpace(req.Prefix) == "" || strings.TrimSpace(req.CapabilityDigest) == "" {
		return Projection{}, ErrProjectionInvalid
	}
	references := append([]Reference(nil), req.References...)
	for index := range references {
		if references[index].Kind == "source" || references[index].Kind == "source_section" {
			references[index].Untrusted = true
		}
	}
	p := Projection{Role: req.Role, Prefix: req.Prefix, CapabilityDigest: req.CapabilityDigest, Task: req.Task, History: append([]string(nil), req.History...), References: references, Handoff: Handoff{Version: "handoff.v1", Task: req.Task, References: append([]Reference(nil), references...)}, Budget: BudgetView{MaxBytes: b.limits.MaxBytes}}
	// Role projections are an allowlist. Omitted fields remain addressable by
	// reference and are never copied into a child prompt implicitly.
	switch req.Role {
	case RoleCoordinator:
		p.PrivateMemory = req.PrivateMemory
	case RoleContextReader:
		p.PrivateMemory = ""
	case RoleKnowledgeNavigator:
		p.History = nil
		p.PrivateMemory = ""
	case RoleSectionAnalyst:
		p.PrivateMemory = req.PrivateMemory
	case RoleCitationValidator:
		p.History = nil
		p.PrivateMemory = ""
	case RoleTaskMemoryInterpreter:
		p.PrivateMemory = req.PrivateMemory
	}
	p.Serialized = b.serialize(p)
	if len(p.Serialized) > b.limits.MaxBytes {
		p.History = nil
		p.PrivateMemory = ""
		p.Budget.Compacted = true
		p.Serialized = b.serialize(p)
	}
	if len(p.Serialized) > b.limits.MaxBytes {
		return Projection{}, fmt.Errorf("%w: projection exceeds limit", ErrProjectionInvalid)
	}
	p.Budget.UsedBytes = len(p.Serialized)
	sum := sha256.Sum256(p.Serialized)
	p.Digest = hex.EncodeToString(sum[:])
	return p, nil
}
func (b Builder) serialize(p Projection) []byte { raw, _ := json.Marshal(p); return raw }
