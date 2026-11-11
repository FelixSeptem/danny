package baymax

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/FelixSeptem/baymax/context/hostprojection"
	"github.com/FelixSeptem/danny/internal/context/assembly"
)

// projectionDescriptor translates Danny-owned disclosure facts into Baymax's
// reference-only host projection. It never copies history, memory, or source
// bodies into the descriptor.
func projectionDescriptor(projection assembly.Projection, req AuthorizedRequest) (hostprojection.Descriptor, error) {
	references := make([]hostprojection.SourceReference, 0, len(projection.References)+1)
	for _, reference := range projection.References {
		key := strings.TrimSpace(reference.ID)
		if key == "" {
			key = strings.TrimSpace(reference.Locator)
		}
		if key == "" {
			return hostprojection.Descriptor{}, fmt.Errorf("projection reference is missing identity")
		}
		digest := sha256.Sum256([]byte(reference.Kind + "|" + reference.Locator))
		references = append(references, hostprojection.SourceReference{Reference: key, Digest: "sha256:" + hex.EncodeToString(digest[:])})
	}
	if len(references) == 0 {
		digest := sha256.Sum256([]byte(projection.Digest))
		references = append(references, hostprojection.SourceReference{Reference: "projection:" + projection.Digest, Digest: "sha256:" + hex.EncodeToString(digest[:])})
	}
	descriptor := hostprojection.Descriptor{
		SchemaVersion:     hostprojection.SchemaVersionV1,
		ProjectionID:      "danny:" + req.RunID,
		ProjectionVersion: 1,
		CapabilityDigest:  projection.CapabilityDigest,
		Capabilities:      []hostprojection.Capability{hostprojection.CapabilityRead, hostprojection.CapabilityReference},
		SourceReferences:  references,
	}
	if strings.TrimSpace(req.PlanDigest) != "" {
		descriptor.Plan = &hostprojection.PlanCorrelation{PlanID: req.RunID, PlanVersion: 1, Digest: req.PlanDigest}
	}
	digest, err := hostprojection.CanonicalDigest(descriptor)
	if err != nil {
		return hostprojection.Descriptor{}, err
	}
	descriptor.Digest = digest
	return descriptor, nil
}

type referenceProjectionResolver struct{}

func (referenceProjectionResolver) ResolveHostContext(_ context.Context, reference hostprojection.SourceReference) (hostprojection.Material, error) {
	if strings.TrimSpace(reference.Reference) == "" {
		return hostprojection.Material{}, hostprojection.ErrSourceNotFound
	}
	// The resolver returns only an ephemeral opaque reference marker. Source
	// content remains owned by Stele/source services and is never retained here.
	return hostprojection.Material{Role: "reference", Content: "[source reference: " + reference.Reference + "]"}, nil
}
