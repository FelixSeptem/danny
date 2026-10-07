package knowledge

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type ArtifactKind string

const (
	ArtifactSource      ArtifactKind = "source"
	ArtifactSummary     ArtifactKind = "summary"
	ArtifactConcept     ArtifactKind = "concept"
	ArtifactEntity      ArtifactKind = "entity"
	ArtifactSectionTree ArtifactKind = "section_tree"
	ArtifactIndex       ArtifactKind = "index"
	ArtifactExploration ArtifactKind = "saved_exploration"
)

type Citation struct {
	SourceID  string `json:"source_id"`
	SectionID string `json:"section_id,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
}

type ArtifactCandidate struct {
	ID          string       `json:"id"`
	Kind        ArtifactKind `json:"kind"`
	Path        string       `json:"path,omitempty"`
	Content     string       `json:"content"`
	ContentHash string       `json:"content_hash,omitempty"`
	Lineage     []string     `json:"lineage,omitempty"`
	Links       []string     `json:"links,omitempty"`
	Citations   []Citation   `json:"citations,omitempty"`
}

type CompilationPlan struct {
	PlanID           string              `json:"plan_id"`
	Version          string              `json:"version,omitempty"`
	GenerationID     int64               `json:"generation_id"`
	EffectKey        string              `json:"effect_key"`
	CapabilityDigest string              `json:"capability_digest,omitempty"`
	Sources          []SourceDecision    `json:"sources"`
	Artifacts        []ArtifactCandidate `json:"artifacts"`
}

type ValidationDiagnostic struct{ Code, Message, ArtifactID, SourceID string }

type ValidationResult struct{ Diagnostics []ValidationDiagnostic }

func (v ValidationResult) Valid() bool { return len(v.Diagnostics) == 0 }

var (
	ErrCompilationInvalid = errors.New("knowledge: compilation plan invalid")
	ErrCitationInvalid    = errors.New("knowledge: citation invalid")
	ErrArtifactInvalid    = errors.New("knowledge: artifact invalid")
)

func (p CompilationPlan) Validate() ValidationResult {
	result := ValidationResult{}
	sources := map[string]SourceMetadata{}
	sections := map[string]Section{}
	for _, decision := range p.Sources {
		if decision.Kind == SourceRemoved {
			continue
		}
		sources[decision.Source.SourceID] = decision.Source
		for _, section := range decision.Source.Sections {
			sections[section.ID] = section
		}
	}
	artifacts := map[string]ArtifactCandidate{}
	for _, artifact := range p.Artifacts {
		if strings.TrimSpace(artifact.ID) == "" || artifact.Kind == "" || strings.TrimSpace(artifact.Content) == "" {
			result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "empty_artifact", Message: "artifact identity, kind, and content are required", ArtifactID: artifact.ID})
			continue
		}
		if _, exists := artifacts[artifact.ID]; exists {
			result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "duplicate_artifact", Message: "artifact identity is duplicated", ArtifactID: artifact.ID})
		}
		artifacts[artifact.ID] = artifact
		if artifact.Path != "" && (filepath.IsAbs(artifact.Path) || strings.HasPrefix(filepath.ToSlash(filepath.Clean(artifact.Path)), "../")) {
			result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "invalid_path", Message: "artifact path escapes the knowledge root", ArtifactID: artifact.ID})
		}
		for _, parent := range artifact.Lineage {
			if _, ok := sources[parent]; !ok {
				if _, artifactOK := artifacts[parent]; !artifactOK {
					result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "unknown_lineage", Message: "lineage identity is not present", ArtifactID: artifact.ID, SourceID: parent})
				}
			}
		}
		for _, link := range artifact.Links {
			if _, ok := artifacts[link]; !ok {
				if _, sourceOK := sources[link]; !sourceOK {
					result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "unknown_link", Message: "link identity is not present", ArtifactID: artifact.ID, SourceID: link})
				}
			}
		}
		for _, citation := range artifact.Citations {
			source, sourceOK := sources[citation.SourceID]
			if !sourceOK {
				result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "missing_source", Message: "citation source is not present", ArtifactID: artifact.ID, SourceID: citation.SourceID})
				continue
			}
			if citation.SectionID == "" {
				continue
			}
			section, sectionOK := sections[citation.SectionID]
			if !sectionOK || section.SourceID != citation.SourceID {
				result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "missing_section", Message: "citation section is not present for source", ArtifactID: artifact.ID, SourceID: citation.SourceID})
				continue
			}
			if citation.StartLine < section.Range.StartLine || citation.EndLine > section.Range.EndLine || citation.StartLine > citation.EndLine {
				result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "invalid_range", Message: "citation range is outside section range", ArtifactID: artifact.ID, SourceID: citation.SourceID})
			}
			_ = source
		}
	}
	if p.GenerationID < 0 || strings.TrimSpace(p.EffectKey) == "" {
		result.Diagnostics = append(result.Diagnostics, ValidationDiagnostic{Code: "missing_generation", Message: "generation and effect key are required"})
	}
	return result
}

func (p CompilationPlan) ValidateError() error {
	if result := p.Validate(); !result.Valid() {
		return fmt.Errorf("%w: %s", ErrCompilationInvalid, result.Diagnostics[0].Code)
	}
	return nil
}

func (p CompilationPlan) Digest() string {
	copy := p
	copy.EffectKey = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type NavigationReference struct {
	ArtifactID   string       `json:"artifact_id"`
	Kind         ArtifactKind `json:"kind"`
	GenerationID int64        `json:"generation_id"`
	Lineage      []string     `json:"lineage,omitempty"`
	Citations    []Citation   `json:"citations,omitempty"`
}
type NavigationResult struct {
	GenerationID int64                 `json:"generation_id"`
	References   []NavigationReference `json:"references"`
	Content      string                `json:"content,omitempty"`
	StartLine    int                   `json:"start_line,omitempty"`
	EndLine      int                   `json:"end_line,omitempty"`
	Truncated    bool                  `json:"truncated,omitempty"`
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
