// Package deterministic provides scripted, network-free doubles for the
// execution boundaries the Milestone 12 scenario suite needs.
//
// The doubles are production-shaped code rather than test-only helpers, so
// the scenario suite and any later consumer share one implementation. Each
// double is scripted by the caller and records what it received, so a
// contract test asserts on the recorded calls (plan digest propagation,
// capability digest, citation payloads, aggregate ordering) instead of on
// model output.
//
// A double never reaches a provider. A scripted provider dial fails the
// check rather than falling back silently.
package deterministic

import (
	"context"
	"errors"
	"sync"

	agentknowledge "github.com/FelixSeptem/danny/internal/agent/knowledge"
	"github.com/FelixSeptem/danny/internal/knowledge"
)

// ErrProviderDial is returned when a double is asked to perform work that
// would require a model provider or an upstream service.
var ErrProviderDial = errors.New("deterministic: provider dial is not allowed")

// Call records one interaction with a double.
type Call struct {
	// Kind is the boundary that was called: run, run_child, fanout,
	// aggregate, vote, or commit.
	Kind string
	// Request is the execution request for run and run_child calls.
	Request agentknowledge.ExecutionRequest
	// Fanout is the compile fanout request.
	Fanout knowledge.CompileFanoutRequest
	// Plan is the compilation plan for commit calls.
	Plan knowledge.CompilationPlan
	// Children are the child results for aggregate and vote calls.
	Children []knowledge.CompileChildResult
}

// Executor is a scripted agentknowledge.Executor. It implements both Run and
// RunChild so a scenario can drive a coordinator run and a planned fanout.
type Executor struct {
	mu           sync.Mutex
	RunOutcome   agentknowledge.ExecutionOutcome
	ChildOutcome agentknowledge.ExecutionOutcome
	RunError     error
	ChildError   error
	calls        []Call
}

// NewExecutor returns an executor whose runs and children succeed.
func NewExecutor() *Executor {
	return &Executor{
		RunOutcome:   agentknowledge.ExecutionOutcome{Status: "succeeded"},
		ChildOutcome: agentknowledge.ExecutionOutcome{Status: "succeeded"},
	}
}

// Run records the request and returns the scripted outcome.
func (e *Executor) Run(_ context.Context, request agentknowledge.ExecutionRequest) (agentknowledge.ExecutionOutcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, Call{Kind: "run", Request: request})
	return e.RunOutcome, e.RunError
}

// RunChild records the request and returns the scripted child outcome.
func (e *Executor) RunChild(_ context.Context, request agentknowledge.ExecutionRequest) (agentknowledge.ExecutionOutcome, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.calls = append(e.calls, Call{Kind: "run_child", Request: request})
	return e.ChildOutcome, e.ChildError
}

// Calls returns a copy of the recorded calls.
func (e *Executor) Calls() []Call {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]Call(nil), e.calls...)
}

// CallsOfKind returns the recorded calls of one boundary kind.
func (e *Executor) CallsOfKind(kind string) []Call {
	calls := e.Calls()
	filtered := make([]Call, 0, len(calls))
	for _, call := range calls {
		if call.Kind == kind {
			filtered = append(filtered, call)
		}
	}
	return filtered
}

// CompileExecutor is a scripted knowledge.CompileExecutor covering the whole
// fanout, aggregate, vote, and commit sequence.
type CompileExecutor struct {
	mu             sync.Mutex
	Children       []knowledge.CompileChildResult
	VoteResult     knowledge.CompileVoteResult
	FanoutError    error
	AggregateError error
	VoteError      error
	CommitError    error
	calls          []Call
}

// NewCompileExecutor returns a compile executor that approves and commits.
func NewCompileExecutor(children []knowledge.CompileChildResult) *CompileExecutor {
	return &CompileExecutor{
		Children:   children,
		VoteResult: knowledge.CompileVoteResult{Approved: true},
	}
}

// Fanout records the request and returns the scripted children.
func (c *CompileExecutor) Fanout(_ context.Context, request knowledge.CompileFanoutRequest) ([]knowledge.CompileChildResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Kind: "fanout", Fanout: request})
	return c.Children, c.FanoutError
}

// Aggregate records the children it was given.
func (c *CompileExecutor) Aggregate(_ context.Context, children []knowledge.CompileChildResult) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Kind: "aggregate", Children: children})
	return c.AggregateError
}

// Vote records the children it was given and returns the scripted verdict.
func (c *CompileExecutor) Vote(_ context.Context, children []knowledge.CompileChildResult) (knowledge.CompileVoteResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Kind: "vote", Children: children})
	return c.VoteResult, c.VoteError
}

// Commit records the plan it was asked to commit.
func (c *CompileExecutor) Commit(_ context.Context, plan knowledge.CompilationPlan) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, Call{Kind: "commit", Plan: plan})
	return c.CommitError
}

// Calls returns a copy of the recorded calls.
func (c *CompileExecutor) Calls() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Call(nil), c.calls...)
}

// ProviderDial reports that a double cannot reach a provider. It exists so a
// contract test can assert the failure mode explicitly instead of relying on
// the absence of a network call.
func ProviderDial() error {
	return ErrProviderDial
}

// CallsOfKind returns the recorded calls of one boundary kind.
func (c *CompileExecutor) CallsOfKind(kind string) []Call {
	calls := c.Calls()
	filtered := make([]Call, 0, len(calls))
	for _, call := range calls {
		if call.Kind == kind {
			filtered = append(filtered, call)
		}
	}
	return filtered
}
