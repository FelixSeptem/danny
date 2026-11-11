package assembly

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
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
	Trust     string `json:"trust,omitempty"`
}

// ProjectionLimits is retained for compatibility; Baymax owns final pressure.
type ProjectionLimits struct{ MaxBytes int }
type ProjectionRequest struct {
	Role                           Role
	Prefix, CapabilityDigest, Task string
	History                        []string
	References                     []Reference
	PrivateMemory                  string
}
type Projection struct {
	Role             Role        `json:"role"`
	Prefix           string      `json:"prefix"`
	CapabilityDigest string      `json:"capability_digest"`
	Task             string      `json:"task,omitempty"`
	References       []Reference `json:"references"`
	Digest           string      `json:"digest"`
	Serialized       []byte      `json:"-"`
}
type Builder struct{}

func NewBuilder(_ ProjectionLimits) Builder { return Builder{} }
func (b Builder) Build(req ProjectionRequest) (Projection, error) {
	if req.Role == "" || strings.TrimSpace(req.Prefix) == "" || strings.TrimSpace(req.CapabilityDigest) == "" {
		return Projection{}, ErrProjectionInvalid
	}
	switch req.Role {
	case RoleCoordinator, RoleContextReader, RoleKnowledgeNavigator, RoleSectionAnalyst, RoleCitationValidator, RoleTaskMemoryInterpreter:
	default:
		return Projection{}, ErrProjectionInvalid
	}
	references := append([]Reference(nil), req.References...)
	for index := range references {
		if references[index].Kind == "source" || references[index].Kind == "source_section" || references[index].Kind == "memory" || references[index].Kind == "generated" {
			references[index].Untrusted = true
			references[index].Trust = "untrusted"
		}
	}
	// History and private memory remain owned by Baymax/Stele and are not
	// serialized into Danny's projection.
	p := Projection{Role: req.Role, Prefix: req.Prefix, CapabilityDigest: req.CapabilityDigest, Task: req.Task, References: references}
	p.Serialized = b.serialize(p)
	sum := sha256.Sum256(p.Serialized)
	p.Digest = hex.EncodeToString(sum[:])
	return p, nil
}
func (b Builder) serialize(p Projection) []byte { raw, _ := json.Marshal(p); return raw }
