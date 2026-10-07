package knowledge

import (
	"context"
	"encoding/json"

	"github.com/FelixSeptem/danny/internal/storage"
)

type StorageCommitter struct{ repository storage.CompilationRepository }

func NewStorageCommitter(repository storage.CompilationRepository) StorageCommitter {
	return StorageCommitter{repository: repository}
}

func (c StorageCommitter) Commit(ctx context.Context, plan CompilationPlan) error {
	sources := make([]storage.SourceRecord, 0, len(plan.Sources))
	for _, decision := range plan.Sources {
		if decision.Kind == SourceRemoved || decision.Kind == SourceUnchanged {
			continue
		}
		sources = append(sources, storage.SourceRecord{ID: decision.Source.SourceID, Path: decision.Source.Path, ContentHash: decision.Source.ContentHash, Content: decision.Source.Content})
	}
	artifacts := make([]storage.ArtifactRecord, 0, len(plan.Artifacts))
	for _, artifact := range plan.Artifacts {
		lineage, _ := json.Marshal(artifact.Lineage)
		contentHash := artifact.ContentHash
		if contentHash == "" {
			contentHash = hashContent(artifact.Content)
		}
		artifacts = append(artifacts, storage.ArtifactRecord{ID: artifact.ID, Kind: string(artifact.Kind), ContentHash: contentHash, Content: []byte(artifact.Content), Lineage: lineage})
	}
	manifest, _ := json.Marshal(plan)
	_, err := c.repository.Commit(ctx, storage.CompilationCommit{EffectKey: plan.EffectKey, Sources: sources, Artifacts: artifacts, Manifest: manifest, StelePayload: []byte(`{"event_type":"knowledge.compilation"}`)})
	return err
}
