package knowledge

import (
	"testing"

	"github.com/FelixSeptem/danny/internal/parser"
)

func TestNormalizeParsedSourcePreservesParserProvenance(t *testing.T) {
	metadata, err := NormalizeParsedSource("report.docx", parser.Result{ContentHash: "hash", SourceFingerprint: "fingerprint", DetectedFormat: "docx", ParserVersion: "parser-1", ProviderMode: parser.ProviderLocal, CanonicalMarkdown: "# Report\n\nbody\n", Warnings: []parser.Warning{{Code: "image_loss", Message: "image omitted"}}})
	if err != nil {
		t.Fatal(err)
	}
	if metadata.Format != "docx" || metadata.ParserVersion != "parser-1" || metadata.SourceFingerprint != "fingerprint" || !metadata.UntrustedSource {
		t.Fatalf("metadata provenance = %#v", metadata)
	}
	if len(metadata.Warnings) != 1 {
		t.Fatalf("warnings = %#v", metadata.Warnings)
	}
}
