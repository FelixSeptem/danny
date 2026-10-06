package fixtures

import "testing"

func TestValidateManifestRejectsMissingIdentity(t *testing.T) {
	manifest := Manifest{
		DependencyVersions: map[string]string{"baymax": "v1"},
		Fixtures:           []Entry{{ID: "", Path: "source.md", SHA256: "abc", ExpectedResult: "loaded"}},
	}
	if err := manifest.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want malformed manifest error")
	}
}

func TestValidateManifestAcceptsDeterministicEntry(t *testing.T) {
	manifest := Manifest{
		DependencyVersions: map[string]string{"baymax": "v0.0.0-20261003094000-553e07625efb", "stele": "v0.0.0-20261006031612-63d1f3dc4675"},
		Fixtures: []Entry{{
			ID:             "document-basic",
			Path:           "documents/basic.md",
			SHA256:         "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			ExpectedResult: "loaded",
		}},
	}
	if err := manifest.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
}
