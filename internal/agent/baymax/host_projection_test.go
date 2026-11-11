package baymax

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/FelixSeptem/baymax/context/hostprojection"
	"github.com/FelixSeptem/danny/internal/context/assembly"
)

func TestProjectionDescriptorContainsOnlyOpaqueReferenceFacts(t *testing.T) {
	projection, err := assembly.NewBuilder(assembly.ProjectionLimits{MaxBytes: 1}).Build(assembly.ProjectionRequest{
		Role: assembly.RoleSectionAnalyst, Prefix: "approved-prefix", CapabilityDigest: "cap-digest",
		Task: "analyze source", History: []string{"private history body"}, PrivateMemory: "private memory body",
		References: []assembly.Reference{{ID: "opaque://source/1", Kind: "source", Locator: "source body must not be copied"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	descriptor, err := projectionDescriptor(projection, AuthorizedRequest{RunID: "run-1", PlanDigest: "plan-digest"})
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := hostprojection.Admit(descriptor); err != nil {
		t.Fatalf("descriptor rejected: %v", err)
	}
	if !strings.Contains(string(encoded), "opaque://source/1") || !strings.Contains(string(encoded), "cap-digest") {
		t.Fatalf("descriptor lost reference/capability facts: %s", encoded)
	}
	for _, forbidden := range []string{"source body must not be copied", "private history body", "private memory body", "analyze source"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("descriptor leaked %q: %s", forbidden, encoded)
		}
	}
	if len(descriptor.Capabilities) != 2 || descriptor.Capabilities[0] != hostprojection.CapabilityRead || descriptor.Capabilities[1] != hostprojection.CapabilityReference {
		t.Fatalf("unexpected capabilities: %#v", descriptor.Capabilities)
	}
}

func TestReferenceProjectionResolverReturnsUntrustedOpaqueReference(t *testing.T) {
	material, err := (referenceProjectionResolver{}).ResolveHostContext(context.Background(), hostprojection.SourceReference{Reference: "opaque://memory/1"})
	if err != nil {
		t.Fatal(err)
	}
	if material.Role != "reference" || !strings.Contains(material.Content, "opaque://memory/1") {
		t.Fatalf("unexpected resolved reference material: %#v", material)
	}
	if strings.Contains(material.Content, "memory body") {
		t.Fatalf("resolver returned source content: %#v", material)
	}
	if _, err := (referenceProjectionResolver{}).ResolveHostContext(context.Background(), hostprojection.SourceReference{}); err == nil {
		t.Fatal("resolver accepted empty reference")
	}
}
