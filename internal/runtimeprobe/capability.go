package runtimeprobe

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// CapabilityManifest is the versioned evidence inventory for dependency
// contracts. It intentionally records evidence pointers and availability
// without treating an unavailable live service as a passing capability.
type CapabilityManifest struct {
	SchemaVersion  string               `json:"schema_version"`
	DependencyPins map[string]string    `json:"dependency_pins"`
	Capabilities   []CapabilityEvidence `json:"capabilities"`
}

type CapabilityEvidence struct {
	ID             string   `json:"id"`
	Dependency     string   `json:"dependency"`
	Status         string   `json:"status"`
	PublicContract string   `json:"public_contract"`
	Evidence       []string `json:"evidence"`
	Notes          string   `json:"notes"`
}

const CapabilityManifestV1 = "runtime-capabilities.v1"

func ReadCapabilityManifest(path string) (CapabilityManifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return CapabilityManifest{}, err
	}
	var manifest CapabilityManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return CapabilityManifest{}, fmt.Errorf("decode capability manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return CapabilityManifest{}, err
	}
	return manifest, nil
}

func (m CapabilityManifest) Validate() error {
	if m.SchemaVersion != CapabilityManifestV1 {
		return fmt.Errorf("capability manifest schema_version must be %q", CapabilityManifestV1)
	}
	if len(m.DependencyPins) == 0 {
		return fmt.Errorf("capability manifest dependency_pins must not be empty")
	}
	if len(m.Capabilities) == 0 {
		return fmt.Errorf("capability manifest capabilities must not be empty")
	}
	seen := make(map[string]struct{}, len(m.Capabilities))
	for i, capability := range m.Capabilities {
		if strings.TrimSpace(capability.ID) == "" || strings.TrimSpace(capability.Dependency) == "" || strings.TrimSpace(capability.PublicContract) == "" {
			return fmt.Errorf("capability manifest capabilities[%d] has missing identity or contract", i)
		}
		if _, exists := seen[capability.ID]; exists {
			return fmt.Errorf("capability manifest duplicates %q", capability.ID)
		}
		seen[capability.ID] = struct{}{}
		switch capability.Status {
		case "verified", "unsupported", "blocked", "incomplete":
		default:
			return fmt.Errorf("capability %q has invalid status %q", capability.ID, capability.Status)
		}
		if len(capability.Evidence) == 0 {
			return fmt.Errorf("capability %q has no evidence pointer", capability.ID)
		}
		for _, evidence := range capability.Evidence {
			if strings.TrimSpace(evidence) == "" {
				return fmt.Errorf("capability %q has an empty evidence pointer", capability.ID)
			}
		}
	}
	return nil
}
