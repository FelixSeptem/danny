package knowledge

import (
	"context"
	"errors"
)

var ErrCompileBlocked = errors.New("knowledge: compilation blocked")

type CompileFanoutRequest struct {
	RunID, CapabilityDigest string
	SectionIDs              []string
}
type CompileChildResult struct {
	ID, Status, Error string
	SourceIDs         []string
}
type CompileVoteResult struct {
	Approved    bool
	EvidenceIDs []string
	Reason      string
}
type CompileRunRequest struct {
	RunID, ProjectionDigest string
	Plan                    CompilationPlan
	SectionIDs              []string
}
type CompileRunResult struct {
	Committed bool
	Children  []CompileChildResult
	Vote      CompileVoteResult
}

type CompileExecutor interface {
	Fanout(context.Context, CompileFanoutRequest) ([]CompileChildResult, error)
	Aggregate(context.Context, []CompileChildResult) error
	Vote(context.Context, []CompileChildResult) (CompileVoteResult, error)
	Commit(context.Context, CompilationPlan) error
}

type CompileCoordinator struct{ executor CompileExecutor }

func NewCompileCoordinator(executor CompileExecutor) *CompileCoordinator {
	return &CompileCoordinator{executor: executor}
}

func (c *CompileCoordinator) Run(ctx context.Context, request CompileRunRequest) (CompileRunResult, error) {
	if c == nil || c.executor == nil || request.Plan.ValidateError() != nil || request.Plan.CapabilityDigest == "" || request.ProjectionDigest == "" {
		return CompileRunResult{}, ErrCompileBlocked
	}
	children, err := c.executor.Fanout(ctx, CompileFanoutRequest{RunID: request.RunID, CapabilityDigest: request.Plan.CapabilityDigest, SectionIDs: request.SectionIDs})
	result := CompileRunResult{Children: children}
	if err != nil {
		return result, err
	}
	for _, child := range children {
		if child.Status != "succeeded" {
			return result, ErrCompileBlocked
		}
	}
	if err := c.executor.Aggregate(ctx, children); err != nil {
		return result, err
	}
	vote, err := c.executor.Vote(ctx, children)
	result.Vote = vote
	if err != nil || !vote.Approved {
		return result, ErrCompileBlocked
	}
	if err := c.executor.Commit(ctx, request.Plan); err != nil {
		return result, err
	}
	result.Committed = true
	return result, nil
}
