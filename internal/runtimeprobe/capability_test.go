package runtimeprobe

import (
	"path/filepath"
	"testing"
)

func TestSteleCapabilityManifestIsVersionedAndExplicit(t *testing.T) {
	manifest, err := ReadCapabilityManifest(filepath.Join("..", "..", "testdata", "runtimeprobe", "stele-capabilities.json"))
	if err != nil {
		t.Fatal(err)
	}
	if manifest.DependencyPins["stele"] != "v0.0.0-20261111135338-19fc90134492" {
		t.Fatalf("stele pin=%q", manifest.DependencyPins["stele"])
	}
	statuses := map[string]string{}
	for _, capability := range manifest.Capabilities {
		statuses[capability.ID] = capability.Status
	}
	for id, want := range map[string]string{
		"stele.context.schema":          "verified",
		"stele.context.continuity":      "verified",
		"stele.context.live-service":    "verified",
		"stele.context.snapshot-replay": "unsupported",
	} {
		if statuses[id] != want {
			t.Fatalf("capability %s=%q, want %q", id, statuses[id], want)
		}
	}
}
