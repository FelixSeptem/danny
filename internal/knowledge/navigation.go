package knowledge

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
)

var ErrArtifactNotFound = errors.New("knowledge: artifact not found")

type KnowledgeStore interface {
	Put(int64, ArtifactCandidate)
	Get(context.Context, int64, string) (ArtifactCandidate, error)
	List(context.Context, int64, ArtifactKind) ([]ArtifactCandidate, error)
}

type MemoryKnowledgeStore struct {
	mu    sync.RWMutex
	items map[int64]map[string]ArtifactCandidate
}

func NewMemoryKnowledgeStore() *MemoryKnowledgeStore {
	return &MemoryKnowledgeStore{items: map[int64]map[string]ArtifactCandidate{}}
}
func (s *MemoryKnowledgeStore) Put(generation int64, artifact ArtifactCandidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.items[generation] == nil {
		s.items[generation] = map[string]ArtifactCandidate{}
	}
	s.items[generation][artifact.ID] = artifact
}
func (s *MemoryKnowledgeStore) Get(_ context.Context, generation int64, id string) (ArtifactCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.items[generation][id]
	if !ok {
		return ArtifactCandidate{}, ErrArtifactNotFound
	}
	return item, nil
}
func (s *MemoryKnowledgeStore) List(_ context.Context, generation int64, kind ArtifactKind) ([]ArtifactCandidate, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := []ArtifactCandidate{}
	for _, item := range s.items[generation] {
		if kind == "" || item.Kind == kind {
			result = append(result, item)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result, nil
}

type Navigator struct{ store KnowledgeStore }

func NewNavigator(store KnowledgeStore) *Navigator { return &Navigator{store: store} }

func (n *Navigator) ReadSource(ctx context.Context, generation int64, id string, maxBytes int) (NavigationResult, error) {
	artifact, err := n.store.Get(ctx, generation, id)
	if err != nil {
		return NavigationResult{}, err
	}
	content := artifact.Content
	truncated := false
	if maxBytes > 0 && len(content) > maxBytes {
		content, truncated = content[:maxBytes], true
	}
	return NavigationResult{GenerationID: generation, References: []NavigationReference{{ArtifactID: artifact.ID, Kind: artifact.Kind, GenerationID: generation, Lineage: artifact.Lineage, Citations: artifact.Citations}}, Content: content, Truncated: truncated}, nil
}

func (n *Navigator) Search(ctx context.Context, generation int64, query string, kind ArtifactKind) (NavigationResult, error) {
	items, err := n.store.List(ctx, generation, kind)
	if err != nil {
		return NavigationResult{}, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	result := NavigationResult{GenerationID: generation, References: []NavigationReference{}}
	for _, item := range items {
		if query == "" || strings.Contains(strings.ToLower(item.Content), query) || strings.Contains(strings.ToLower(item.ID), query) {
			result.References = append(result.References, NavigationReference{ArtifactID: item.ID, Kind: item.Kind, GenerationID: generation, Lineage: item.Lineage, Citations: item.Citations})
		}
	}
	return result, nil
}
