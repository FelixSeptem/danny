package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type PolicyKind string

const (
	PolicyRead      PolicyKind = "read"
	PolicyWrite     PolicyKind = "write"
	PolicyScheduled PolicyKind = "scheduled"
)

type WriteScope string

const (
	WriteScopeNone       WriteScope = "none"
	WriteScopeUser       WriteScope = "user"
	WriteScopeOccurrence WriteScope = "occurrence"
)

var (
	ErrCapabilityDenied   = errors.New("knowledge agent: capability denied")
	ErrPolicyViolation    = errors.New("knowledge agent: policy violation")
	ErrApprovalRequired   = errors.New("knowledge agent: approval required")
	ErrUnsupportedMode    = errors.New("knowledge agent: unsupported execution mode")
	ErrPlanMismatch       = errors.New("knowledge agent: plan version mismatch")
	ErrCapabilityMismatch = errors.New("knowledge agent: capability digest mismatch")
	ErrSensitiveEvidence  = errors.New("knowledge agent: sensitive evidence")
)

type Policy struct {
	Kind                 PolicyKind
	Tools                []string
	WriteScope           WriteScope
	ConfirmationRequired bool
	OccurrenceRequired   bool
}

func DefaultPolicy(kind PolicyKind) Policy {
	switch kind {
	case PolicyWrite:
		return Policy{Kind: kind, Tools: []string{"search_knowledge", "read_source", "save_memory", "create_task"}, WriteScope: WriteScopeUser}
	case PolicyScheduled:
		return Policy{Kind: kind, Tools: []string{"search_knowledge", "read_source", "save_memory", "create_task"}, WriteScope: WriteScopeOccurrence, OccurrenceRequired: true}
	default:
		return Policy{Kind: PolicyRead, Tools: []string{"search_knowledge", "read_source"}, WriteScope: WriteScopeNone}
	}
}

func (p Policy) Allows(name string) bool {
	for _, allowed := range p.Tools {
		if allowed == name {
			return true
		}
	}
	return false
}
func (p Policy) CapabilityDigest() string {
	tools := append([]string(nil), p.Tools...)
	sort.Strings(tools)
	raw, _ := json.Marshal(struct {
		Kind                PolicyKind
		Tools               []string
		Scope               WriteScope
		Confirm, Occurrence bool
	}{p.Kind, tools, p.WriteScope, p.ConfirmationRequired, p.OccurrenceRequired})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type ToolCall struct {
	Name          string
	Input         map[string]any
	Confirmed     bool
	OccurrenceKey string
	RunID         string
	CorrelationID string
}

type EventKind string

const (
	EventApprovalRequired EventKind = "approval_required"
	EventPolicyViolation  EventKind = "policy_violation"
	EventToolCompleted    EventKind = "tool_completed"
	EventToolFailed       EventKind = "tool_failed"
)

type PolicyEvent struct {
	Kind             EventKind
	RunID            string
	CorrelationID    string
	Tool             string
	Reason           string
	CapabilityDigest string
}

type InvocationOutcome struct {
	Event PolicyEvent
	Err   error
}

type ToolDefinition struct {
	Name                 string
	Policy               PolicyKind
	WriteScope           WriteScope
	ConfirmationRequired bool
	Invoke               func(context.Context, map[string]any) error
}

type TrustLevel string

const (
	TrustUntrusted TrustLevel = "untrusted"
)

type DataSource string

const (
	SourceDocument          DataSource = "document"
	SourceKnowledgeArtifact DataSource = "knowledge_artifact"
	SourceSummary           DataSource = "summary"
	SourceModelOutput       DataSource = "model_output"
)

type UntrustedText struct {
	Source  DataSource
	Content string
	Trust   TrustLevel
}

func NewUntrustedText(source DataSource, content string) UntrustedText {
	return UntrustedText{Source: source, Content: content, Trust: TrustUntrusted}
}
func (u UntrustedText) IsUntrusted() bool { return u.Trust == TrustUntrusted }

type ToolRegistry struct{ tools map[string]ToolDefinition }

func NewToolRegistry() *ToolRegistry { return &ToolRegistry{tools: map[string]ToolDefinition{}} }
func (r *ToolRegistry) Register(def ToolDefinition) error {
	if def.Name == "" || def.Invoke == nil {
		return ErrCapabilityDenied
	}
	r.tools[def.Name] = def
	return nil
}
func (r *ToolRegistry) Invoke(ctx context.Context, policy Policy, call ToolCall) error {
	return r.InvokeOutcome(ctx, policy, call).Err
}

// InvokeOutcome applies server-owned policy and returns a typed event for
// durable correlation. The tool callback is never called for a policy
// violation or an approval-required write.
func (r *ToolRegistry) InvokeOutcome(ctx context.Context, policy Policy, call ToolCall) InvocationOutcome {
	def, ok := r.tools[call.Name]
	if !ok {
		return InvocationOutcome{Event: PolicyEvent{Kind: EventPolicyViolation, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, Reason: "tool is not registered", CapabilityDigest: policy.CapabilityDigest()}, Err: fmt.Errorf("%w: %w: %s", ErrPolicyViolation, ErrCapabilityDenied, call.Name)}
	}
	if def.Policy != policy.Kind || def.WriteScope != WriteScopeNone && policy.WriteScope == WriteScopeNone {
		return InvocationOutcome{Event: PolicyEvent{Kind: EventPolicyViolation, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, Reason: "tool is outside active policy", CapabilityDigest: policy.CapabilityDigest()}, Err: fmt.Errorf("%w: %w: %s", ErrPolicyViolation, ErrCapabilityDenied, call.Name)}
	}
	if def.ConfirmationRequired && !call.Confirmed {
		return InvocationOutcome{Event: PolicyEvent{Kind: EventApprovalRequired, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, Reason: "confirmation is required", CapabilityDigest: policy.CapabilityDigest()}, Err: ErrApprovalRequired}
	}
	if err := NewCapabilityGate().Authorize(policy, call); err != nil {
		return InvocationOutcome{Event: PolicyEvent{Kind: EventPolicyViolation, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, Reason: err.Error(), CapabilityDigest: policy.CapabilityDigest()}, Err: fmt.Errorf("%w: %w", ErrPolicyViolation, err)}
	}
	if err := def.Invoke(ctx, call.Input); err != nil {
		return InvocationOutcome{Event: PolicyEvent{Kind: EventToolFailed, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, Reason: err.Error(), CapabilityDigest: policy.CapabilityDigest()}, Err: err}
	}
	return InvocationOutcome{Event: PolicyEvent{Kind: EventToolCompleted, RunID: call.RunID, CorrelationID: call.CorrelationID, Tool: call.Name, CapabilityDigest: policy.CapabilityDigest()}}
}

type CapabilityGate struct{}

func NewCapabilityGate() *CapabilityGate { return &CapabilityGate{} }
func (g *CapabilityGate) Authorize(policy Policy, call ToolCall) error {
	if !policy.Allows(call.Name) {
		return fmt.Errorf("%w: %s", ErrCapabilityDenied, call.Name)
	}
	if policy.WriteScope == WriteScopeNone && isMutation(call.Name) {
		return ErrCapabilityDenied
	}
	if policy.ConfirmationRequired && !call.Confirmed {
		return ErrCapabilityDenied
	}
	if policy.OccurrenceRequired && call.OccurrenceKey == "" {
		return ErrCapabilityDenied
	}
	return nil
}
func isMutation(name string) bool {
	return name == "save_memory" || name == "create_task" || name == "create_schedule" || name == "send_webhook" || name == "commit_compilation"
}

type ExecutionMode string

const (
	ModeDirect         ExecutionMode = "direct"
	ModeSubagentFanout ExecutionMode = "subagent_fanout"
	ModeTeamParallel   ExecutionMode = "team_parallel"
	ModeTeamVote       ExecutionMode = "team_vote"
	ModeWorkflow       ExecutionMode = "workflow"
	ModeMailboxAsync   ExecutionMode = "mailbox_async"
	ModeScheduled      ExecutionMode = "scheduled"
)

type TaskProfile struct {
	Objective                                                                   string
	Complexity, Parallelism, EvidenceBreadth, Latency, Risk, WriteScope, Budget int
	Dependencies                                                                []string
}
type ExecutionPlan struct {
	Version, Digest, Rationale, CapabilityDigest string
	Mode                                         ExecutionMode
	ChildLimit, PerChildBudget, AggregationLimit int
	ProfileDigest                                string
}
type Planner struct{ Version string }

func NewPlanner(version string) Planner { return Planner{Version: version} }
func (p Planner) Plan(ctx context.Context, profile TaskProfile) (ExecutionPlan, error) {
	return p.PlanMode(ctx, profile, "")
}
func (p Planner) PlanMode(_ context.Context, profile TaskProfile, requested ExecutionMode) (ExecutionPlan, error) {
	profile.Dependencies = append([]string(nil), profile.Dependencies...)
	sort.Strings(profile.Dependencies)
	if profile.Parallelism < 1 {
		profile.Parallelism = 1
	}
	mode := requested
	if mode == "" {
		if profile.Parallelism > 1 && profile.Complexity >= 3 && profile.Budget >= 1000 {
			mode = ModeSubagentFanout
		} else {
			mode = ModeDirect
		}
	}
	if mode != ModeDirect && mode != ModeSubagentFanout {
		return ExecutionPlan{}, fmt.Errorf("%w: %s", ErrUnsupportedMode, mode)
	}
	profileRaw, _ := json.Marshal(profile)
	profileSum := sha256.Sum256(profileRaw)
	profileDigest := hex.EncodeToString(profileSum[:])
	plan := ExecutionPlan{Version: p.Version, Mode: mode, ProfileDigest: profileDigest, CapabilityDigest: fmt.Sprintf("profile:%d:%d:%d", profile.Complexity, profile.WriteScope, profile.Budget)}
	if mode == ModeDirect {
		plan.Rationale = "low complexity or ordered execution"
	} else {
		plan.ChildLimit = profile.Parallelism
		if plan.ChildLimit > 8 {
			plan.ChildLimit = 8
		}
		plan.PerChildBudget = profile.Budget / plan.ChildLimit
		if plan.PerChildBudget < 1 {
			plan.PerChildBudget = 1
		}
		plan.AggregationLimit = plan.ChildLimit
		plan.Rationale = "independent bounded work"
	}
	raw, _ := json.Marshal(plan)
	sum := sha256.Sum256(raw)
	plan.Digest = hex.EncodeToString(sum[:])
	return plan, nil
}

type PauseReason string

const (
	PauseApprovalRequired PauseReason = "approval_required"
	PauseInputRequired    PauseReason = "input_required"
	PauseBudgetExhausted  PauseReason = "budget_exhausted"
)

type Checkpoint struct {
	RunID, PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest string
	PauseReason                                                        PauseReason
	PreviousPlanVersion, ReplanReason                                  string
}
type ResumeRequest struct{ PlanVersion, CapabilityDigest string }

func (c Checkpoint) ValidateResume(req ResumeRequest) error {
	if c.PlanVersion != req.PlanVersion {
		return ErrPlanMismatch
	}
	if c.CapabilityDigest != req.CapabilityDigest {
		return ErrCapabilityMismatch
	}
	return nil
}

func (c Checkpoint) Replan(version, reason string) Checkpoint {
	c.PreviousPlanVersion = c.PlanVersion
	c.PlanVersion = version
	c.ReplanReason = reason
	return c
}

var ErrCheckpointNotFound = errors.New("knowledge agent: checkpoint not found")

type CheckpointStore interface {
	Save(context.Context, Checkpoint) error
	Get(context.Context, string) (Checkpoint, error)
}

type MemoryCheckpointStore struct {
	mu    sync.RWMutex
	items map[string]Checkpoint
}

func NewMemoryCheckpointStore() *MemoryCheckpointStore {
	return &MemoryCheckpointStore{items: map[string]Checkpoint{}}
}
func (s *MemoryCheckpointStore) Save(_ context.Context, checkpoint Checkpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[checkpoint.RunID] = checkpoint
	return nil
}
func (s *MemoryCheckpointStore) Get(_ context.Context, runID string) (Checkpoint, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	checkpoint, ok := s.items[runID]
	if !ok {
		return Checkpoint{}, ErrCheckpointNotFound
	}
	return checkpoint, nil
}

type ExecutionEvidence struct{ RunID, ParentRunID, ChildRunID, PlanVersion, ProjectionDigest, CapabilityDigest, Diagnostic, TerminalStatus, FailureClass, ProviderPayload string }

type ExecutionRequest struct {
	RunID, ParentRunID, CorrelationID                           string
	PlanVersion, PlanDigest, CapabilityDigest, ProjectionDigest string
	Input                                                       string
	Policy                                                      Policy
}

type ExecutionOutcome struct{ Status, Result, FailureClass string }

type Executor interface {
	Run(context.Context, ExecutionRequest) (ExecutionOutcome, error)
	RunChild(context.Context, ExecutionRequest) (ExecutionOutcome, error)
}

type EvidenceStore interface {
	Save(context.Context, ExecutionEvidence) error
}

type MemoryEvidenceStore struct{ Evidence []ExecutionEvidence }

func NewMemoryEvidenceStore() *MemoryEvidenceStore { return &MemoryEvidenceStore{} }
func (s *MemoryEvidenceStore) Save(_ context.Context, evidence ExecutionEvidence) error {
	s.Evidence = append(s.Evidence, evidence)
	return nil
}

type AgentRequest struct {
	RunID, ParentRunID, CorrelationID string
	Policy                            Policy
	Profile                           TaskProfile
	ProjectionDigest                  string
}

type AgentResult struct {
	Plan     ExecutionPlan
	Evidence ExecutionEvidence
}

type Agent struct {
	planner  Planner
	executor Executor
	evidence EvidenceStore
}

func NewAgent(planner Planner, executor Executor, evidence EvidenceStore) *Agent {
	return &Agent{planner: planner, executor: executor, evidence: evidence}
}

func (a *Agent) Run(ctx context.Context, req AgentRequest) (AgentResult, error) {
	if a == nil || a.executor == nil || a.evidence == nil || req.RunID == "" || req.CorrelationID == "" {
		return AgentResult{}, ErrCapabilityDenied
	}
	plan, err := a.planner.Plan(ctx, req.Profile)
	if err != nil {
		return AgentResult{}, err
	}
	plan.CapabilityDigest = req.Policy.CapabilityDigest()
	plan.Digest = digestPlan(plan)
	base := ExecutionRequest{RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID, PlanVersion: plan.Version, PlanDigest: plan.Digest, CapabilityDigest: plan.CapabilityDigest, ProjectionDigest: req.ProjectionDigest, Input: req.Profile.Objective, Policy: req.Policy}
	outcome, err := a.executor.Run(ctx, base)
	if err != nil {
		return AgentResult{}, err
	}
	if plan.Mode == ModeSubagentFanout {
		for i := 0; i < plan.ChildLimit; i++ {
			child := base
			child.RunID = fmt.Sprintf("%s:child:%d", req.RunID, i+1)
			child.ParentRunID = req.RunID
			if _, childErr := a.executor.RunChild(ctx, child); childErr != nil && err == nil {
				err = childErr
			}
		}
	}
	evidence := ExecutionEvidence{RunID: req.RunID, ParentRunID: req.ParentRunID, PlanVersion: plan.Version, ProjectionDigest: req.ProjectionDigest, CapabilityDigest: plan.CapabilityDigest, TerminalStatus: outcome.Status, FailureClass: outcome.FailureClass}
	if err := evidence.Validate(); err != nil {
		return AgentResult{}, err
	}
	if err := a.evidence.Save(ctx, evidence); err != nil {
		return AgentResult{}, err
	}
	return AgentResult{Plan: plan, Evidence: evidence}, nil
}

func digestPlan(plan ExecutionPlan) string {
	copy := plan
	copy.Digest = ""
	raw, _ := json.Marshal(copy)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func (e ExecutionEvidence) Validate() error {
	if e.ProviderPayload != "" {
		return ErrSensitiveEvidence
	}
	for _, value := range []string{e.Diagnostic, e.FailureClass} {
		if strings.Contains(strings.ToLower(value), "authorization:") || strings.Contains(strings.ToLower(value), "api_key") || strings.Contains(strings.ToLower(value), "bearer ") {
			return ErrSensitiveEvidence
		}
	}
	return nil
}
