package fixtures

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ReadManifest decodes and validates a JSON manifest from disk.
func ReadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	var manifest Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode fixture manifest: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

// Manifest records the dependency versions and expected deterministic fixture outcomes.
type Manifest struct {
	DependencyVersions map[string]string `json:"dependency_versions"`
	Fixtures           []Entry           `json:"fixtures"`
}

// Entry identifies one checked-in fixture and its expected result.
type Entry struct {
	ID             string `json:"id"`
	Path           string `json:"path"`
	SHA256         string `json:"sha256"`
	ExpectedResult string `json:"expected_result"`
}

// Validate checks the manifest shape without reading fixture files.
func (m Manifest) Validate() error {
	if len(m.DependencyVersions) == 0 {
		return fmt.Errorf("dependency_versions must contain at least one dependency")
	}
	if len(m.Fixtures) == 0 {
		return fmt.Errorf("fixtures must contain at least one entry")
	}
	seen := make(map[string]struct{}, len(m.Fixtures))
	for index, entry := range m.Fixtures {
		if strings.TrimSpace(entry.ID) == "" {
			return fmt.Errorf("fixtures[%d].id must not be empty", index)
		}
		if _, ok := seen[entry.ID]; ok {
			return fmt.Errorf("fixtures[%d].id %q is duplicated", index, entry.ID)
		}
		seen[entry.ID] = struct{}{}
		if strings.TrimSpace(entry.Path) == "" {
			return fmt.Errorf("fixtures[%d].path must not be empty", index)
		}
		if len(entry.SHA256) != 64 {
			return fmt.Errorf("fixtures[%d].sha256 must be a 64-character hex digest", index)
		}
		if _, err := hex.DecodeString(entry.SHA256); err != nil {
			return fmt.Errorf("fixtures[%d].sha256 must be hexadecimal: %w", index, err)
		}
		if strings.TrimSpace(entry.ExpectedResult) == "" {
			return fmt.Errorf("fixtures[%d].expected_result must not be empty", index)
		}
	}
	return nil
}
