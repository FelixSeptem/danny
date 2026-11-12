// Package runtimeboundary contains Danny-owned contracts at the Baymax and
// Stele integration boundary. It deliberately imports neither upstream
// implementation nor internal packages so domain code can remain stable when
// dependency versions change.
package runtimeboundary

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ExecutionContractV1  = "danny.execution.v1"
	ContextContractV1    = "danny.context.v1"
	CapabilityManifestV1 = "danny.capabilities.v1"
)

var (
	ErrUnsupportedVersion = errors.New("runtime boundary: unsupported contract version")
	ErrMissingAuthority   = errors.New("runtime boundary: required authority is missing")
	ErrCapabilityBlocked  = errors.New("runtime boundary: capability is blocked")
	ErrCapabilityMissing  = errors.New("runtime boundary: capability is unavailable")
)

type ExecutionForm string

const (
	FormDirect       ExecutionForm = "direct"
	FormSubagent     ExecutionForm = "subagent_fanout"
	FormParallelTeam ExecutionForm = "team_parallel"
	FormVoteTeam     ExecutionForm = "team_vote"
	FormWorkflow     ExecutionForm = "workflow"
	FormMailbox      ExecutionForm = "mailbox_async"
	FormScheduled    ExecutionForm = "scheduled"
)

type CapabilityStatus string

const (
	CapabilityVerified    CapabilityStatus = "verified"
	CapabilityUnsupported CapabilityStatus = "unsupported"
	CapabilityBlocked     CapabilityStatus = "blocked-upstream"
)

// Capability records the exact dependency pin and evidence used to make a
// mode available. Evidence is descriptive and must point to a reproducible
// fixture, not merely a package import.
type Capability struct {
	Form       ExecutionForm    `json:"form"`
	Status     CapabilityStatus `json:"status"`
	Dependency string           `json:"dependency"`
	Pin        string           `json:"pin"`
	PublicAPI  string           `json:"public_api"`
	Evidence   string           `json:"evidence"`
	Reason     string           `json:"reason,omitempty"`
}

type CapabilityManifest struct {
	Version      string       `json:"version"`
	Capabilities []Capability `json:"capabilities"`
	Digest       string       `json:"digest"`
}

func DefaultCapabilityManifest() CapabilityManifest {
	return NewCapabilityManifest([]Capability{
		{Form: FormDirect, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "core/runner.Engine.Run", Evidence: "internal/agent/baymax/adapter_test.go"},
		{Form: FormSubagent, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/composer.DispatchChild", Evidence: "testdata/runtimeprobe/baymax-live-pause-restore.evidence.json", Reason: "per-pause-reason fresh-process pause/restore reproduced in internal/agent/baymax/continuation_restore_test.go and exercised live for input_required/approval_required/budget_exhausted: the pause checkpoint persisted through the production child owner and a fresh OS process restored it through the authorized boundary with a real provider call"},
		{Form: FormParallelTeam, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/teams", Evidence: "testdata/runtimeprobe/baymax-live-pause-restore.evidence.json", Reason: "the team continuation owner is wired and the batch-level pause boundary persisted a checkpoint that a fresh OS process restored with real member executions; the duplicate resume converged without re-running members"},
		{Form: FormVoteTeam, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/teams", Evidence: "testdata/runtimeprobe/baymax-live-pause-restore.evidence.json", Reason: "vote teams share the wired team continuation owner; live pause/restore passed for all three pause reasons with the winner vote resolved after restore"},
		{Form: FormWorkflow, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/workflow.Engine", Evidence: "internal/agent/baymax/adapter_test.go", Reason: "verified for explicit file-backed workflow continuation on same-host single-writer deployment"},
		{Form: FormMailbox, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/mailbox.AdmitHostPlan", Evidence: "testdata/runtimeprobe/baymax-live-pause-restore.evidence.json", Reason: "verified for one admitted logical message with native lease/retry/DLQ; upstream 198 handler pause restore is wired through a configurable handler and reproduced in continuation_restore_test.go; the 2026-11-12 live run exercised handler pause and fresh-process resume for all three pause reasons"},
		{Form: FormScheduled, Status: CapabilityVerified, Dependency: "baymax", Pin: "v0.0.0-20261110150429-cca95f31f71b", PublicAPI: "orchestration/scheduler", Evidence: "internal/agent/baymax/scheduled_test.go"},
	})
}

func NewCapabilityManifest(capabilities []Capability) CapabilityManifest {
	copyCaps := append([]Capability(nil), capabilities...)
	sort.Slice(copyCaps, func(i, j int) bool { return copyCaps[i].Form < copyCaps[j].Form })
	m := CapabilityManifest{Version: CapabilityManifestV1, Capabilities: copyCaps}
	m.Digest = digestWithoutField(m)
	return m
}

func (m CapabilityManifest) Validate() error {
	if m.Version != CapabilityManifestV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, m.Version)
	}
	seen := map[ExecutionForm]bool{}
	for _, c := range m.Capabilities {
		if c.Form == "" || seen[c.Form] || c.Dependency == "" || c.Pin == "" || c.PublicAPI == "" || c.Evidence == "" {
			return fmt.Errorf("runtime boundary: invalid capability %q", c.Form)
		}
		seen[c.Form] = true
		if c.Status != CapabilityVerified && c.Status != CapabilityUnsupported && c.Status != CapabilityBlocked {
			return fmt.Errorf("runtime boundary: invalid capability status %q", c.Status)
		}
	}
	return nil
}

func (m CapabilityManifest) DigestMatches() bool {
	return m.Digest != "" && m.Digest == digestWithoutField(m)
}

func (m CapabilityManifest) Lookup(form ExecutionForm) (Capability, error) {
	if err := m.Validate(); err != nil {
		return Capability{}, err
	}
	if !m.DigestMatches() {
		return Capability{}, ErrCapabilityBlocked
	}
	for _, c := range m.Capabilities {
		if c.Form == form {
			return c, nil
		}
	}
	return Capability{}, fmt.Errorf("%w: %s", ErrCapabilityMissing, form)
}

func (m CapabilityManifest) Allows(form ExecutionForm) error {
	c, err := m.Lookup(form)
	if err != nil {
		return err
	}
	switch c.Status {
	case CapabilityVerified:
		return nil
	case CapabilityBlocked:
		return fmt.Errorf("%w: %s", ErrCapabilityBlocked, c.Reason)
	default:
		return fmt.Errorf("%w: %s", ErrCapabilityMissing, c.Reason)
	}
}

type Authority struct {
	TaskRevision     int64  `json:"task_revision"`
	PlanVersion      string `json:"plan_version"`
	PlanDigest       string `json:"plan_digest"`
	CapabilityDigest string `json:"capability_digest"`
	ProjectionDigest string `json:"projection_digest"`
	WriteScope       string `json:"write_scope"`
	EffectNamespace  string `json:"effect_namespace"`
	OccurrenceKey    string `json:"occurrence_key,omitempty"`
}

type Assignment struct {
	ID        string `json:"id"`
	Role      string `json:"role"`
	Objective string `json:"objective"`
	Budget    int    `json:"budget"`
}

type ExecutionRequest struct {
	Version       string        `json:"version"`
	ExecutionID   string        `json:"execution_id"`
	ParentRunID   string        `json:"parent_run_id,omitempty"`
	CorrelationID string        `json:"correlation_id"`
	Form          ExecutionForm `json:"form"`
	Authority     Authority     `json:"authority"`
	Assignments   []Assignment  `json:"assignments,omitempty"`
	Input         string        `json:"input"`
}

func (r ExecutionRequest) Validate(manifest CapabilityManifest) error {
	if r.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if strings.TrimSpace(r.ExecutionID) == "" || strings.TrimSpace(r.CorrelationID) == "" || r.Form == "" {
		return ErrMissingAuthority
	}
	if r.Authority.TaskRevision <= 0 || r.Authority.PlanVersion == "" || r.Authority.PlanDigest == "" || r.Authority.CapabilityDigest == "" || r.Authority.ProjectionDigest == "" || r.Authority.WriteScope == "" || r.Authority.EffectNamespace == "" {
		return ErrMissingAuthority
	}
	return manifest.Allows(r.Form)
}

type Event struct {
	Version        string          `json:"version"`
	ExecutionID    string          `json:"execution_id"`
	NativeID       string          `json:"native_id"`
	NativeParent   string          `json:"native_parent,omitempty"`
	NativeSequence *int64          `json:"native_sequence,omitempty"`
	Kind           string          `json:"kind"`
	Status         string          `json:"status,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	CorrelationID  string          `json:"correlation_id"`
}

func (e Event) Validate() error {
	if e.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, e.Version)
	}
	if strings.TrimSpace(e.ExecutionID) == "" || strings.TrimSpace(e.NativeID) == "" || strings.TrimSpace(e.Kind) == "" || strings.TrimSpace(e.CorrelationID) == "" {
		return ErrMissingAuthority
	}
	if e.NativeSequence != nil && *e.NativeSequence <= 0 {
		return fmt.Errorf("runtime boundary: native sequence must be positive")
	}
	return nil
}

type Outcome struct {
	Version       string `json:"version"`
	ExecutionID   string `json:"execution_id"`
	NativeRunID   string `json:"native_run_id"`
	Status        string `json:"status"`
	FailureClass  string `json:"failure_class,omitempty"`
	Result        string `json:"result,omitempty"`
	TerminalEvent string `json:"terminal_event"`
}

func (o Outcome) Validate() error {
	if o.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, o.Version)
	}
	if strings.TrimSpace(o.ExecutionID) == "" || strings.TrimSpace(o.NativeRunID) == "" || strings.TrimSpace(o.Status) == "" || strings.TrimSpace(o.TerminalEvent) == "" {
		return ErrMissingAuthority
	}
	return nil
}

// ExecutionAdapter is the only contract consumed by product orchestration.
// Implementations translate this DTO to Baymax public types and return native
// identity and terminal evidence without exposing upstream structs.
type ExecutionAdapter interface {
	Execute(context.Context, ExecutionRequest) (Outcome, []Event, error)
	Control(context.Context, ControlRequest) error
}

type ContextAdapter interface {
	Assemble(context.Context, ContextRequest) (ContextEnvelope, error)
}

type ControlRequest struct {
	Version        string `json:"version"`
	ExecutionID    string `json:"execution_id"`
	NativeRunID    string `json:"native_run_id"`
	SessionID      string `json:"session_id,omitempty"`
	Action         string `json:"action"`
	CausationID    string `json:"causation_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (r ControlRequest) Validate() error {
	if r.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if strings.TrimSpace(r.ExecutionID) == "" || strings.TrimSpace(r.NativeRunID) == "" || strings.TrimSpace(r.Action) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrMissingAuthority
	}
	return nil
}

type CheckpointBinding struct {
	Version        string    `json:"version"`
	ExecutionID    string    `json:"execution_id"`
	NativeID       string    `json:"native_id"`
	NativeVersion  string    `json:"native_version"`
	NativeDigest   string    `json:"native_digest"`
	SourceCursor   string    `json:"source_cursor,omitempty"`
	Authority      Authority `json:"authority"`
	PauseReason    string    `json:"pause_reason"`
	DecisionToken  string    `json:"decision_token,omitempty"`
	OwnerNamespace string    `json:"owner_namespace,omitempty"`
}

func (c CheckpointBinding) Validate() error {
	if c.Version != ExecutionContractV1 || c.ExecutionID == "" || c.NativeID == "" || c.NativeVersion == "" || c.NativeDigest == "" || c.PauseReason == "" {
		return ErrMissingAuthority
	}
	if c.Authority.TaskRevision <= 0 || c.Authority.PlanVersion == "" || c.Authority.PlanDigest == "" || c.Authority.CapabilityDigest == "" || c.Authority.ProjectionDigest == "" || c.Authority.WriteScope == "" || c.Authority.EffectNamespace == "" {
		return ErrMissingAuthority
	}
	return nil
}

// NativeBudget is a product-neutral projection of a source-owned budget.
// Units are defined by the native dependency contract.
type NativeBudget struct {
	Revision     string    `json:"revision,omitempty"`
	Limit        int64     `json:"limit,omitempty"`
	Consumed     int64     `json:"consumed,omitempty"`
	ToolLimit    int64     `json:"tool_limit,omitempty"`
	ToolConsumed int64     `json:"tool_consumed,omitempty"`
	Deadline     time.Time `json:"deadline,omitempty"`
}

type CheckpointReference struct {
	Version            string       `json:"version"`
	CheckpointID       string       `json:"checkpoint_id"`
	Digest             string       `json:"digest"`
	SourceComponent    string       `json:"source_component"`
	OwnerNamespace     string       `json:"owner_namespace"`
	RunID              string       `json:"run_id"`
	SessionID          string       `json:"session_id"`
	ParentRunID        string       `json:"parent_run_id,omitempty"`
	PauseReason        string       `json:"pause_reason"`
	SourcePosition     string       `json:"source_position"`
	AuthorityDigest    string       `json:"authority_digest,omitempty"`
	PlanVersion        string       `json:"plan_version,omitempty"`
	PlanDigest         string       `json:"plan_digest,omitempty"`
	CapabilityDigest   string       `json:"capability_digest,omitempty"`
	ProjectionDigest   string       `json:"projection_digest,omitempty"`
	WriteScopeDigest   string       `json:"write_scope_digest,omitempty"`
	EffectNamespace    string       `json:"effect_namespace,omitempty"`
	OccurrenceKey      string       `json:"occurrence_key,omitempty"`
	PendingActionToken string       `json:"pending_action_token,omitempty"`
	CreatedAt          time.Time    `json:"created_at"`
	ExpiresAt          time.Time    `json:"expires_at"`
	Budget             NativeBudget `json:"budget,omitempty"`
}

type AuthorityBinding struct {
	TaskRevision     int64  `json:"task_revision,omitempty"`
	PlanVersion      string `json:"plan_version,omitempty"`
	PlanDigest       string `json:"plan_digest"`
	CapabilityDigest string `json:"capability_digest"`
	ProjectionDigest string `json:"projection_digest"`
	WriteScopeDigest string `json:"write_scope_digest"`
	EffectNamespace  string `json:"effect_namespace"`
	OccurrenceKey    string `json:"occurrence_key,omitempty"`
}

type NativeResumeRequest struct {
	Checkpoint     CheckpointReference `json:"checkpoint"`
	DecisionToken  string              `json:"decision_token"`
	IdempotencyKey string              `json:"idempotency_key"`
	Authority      AuthorityBinding    `json:"authority"`
	ProposedBudget *NativeBudget       `json:"proposed_budget,omitempty"`
}

type NativeResumeOutcome struct {
	ExecutionID        string               `json:"execution_id"`
	NativeRunID        string               `json:"native_run_id"`
	Status             string               `json:"status"`
	Result             string               `json:"result,omitempty"`
	FailureClass       string               `json:"failure_class,omitempty"`
	TerminalEvent      string               `json:"terminal_event,omitempty"`
	Checkpoint         *CheckpointReference `json:"checkpoint,omitempty"`
	PendingActionToken string               `json:"pending_action_token,omitempty"`
}

type NativeContinuationAdapter interface {
	ResumeNative(context.Context, NativeResumeRequest) (NativeResumeOutcome, error)
}

type ControlOutcome struct {
	Status           string `json:"status"`
	TerminalObserved bool   `json:"terminal_observed"`
}

type NativeControlAdapter interface {
	Control(context.Context, ControlRequest) (ControlOutcome, error)
}

type RetryRequest struct {
	Version        string    `json:"version"`
	ExecutionID    string    `json:"execution_id"`
	NativeRunID    string    `json:"native_run_id"`
	SessionID      string    `json:"session_id,omitempty"`
	CausationID    string    `json:"causation_id"`
	IdempotencyKey string    `json:"idempotency_key"`
	Input          string    `json:"input"`
	Authority      Authority `json:"authority"`
}

func (r RetryRequest) Validate() error {
	if r.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if strings.TrimSpace(r.ExecutionID) == "" || strings.TrimSpace(r.NativeRunID) == "" || strings.TrimSpace(r.CausationID) == "" || strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrMissingAuthority
	}
	if r.Authority.TaskRevision <= 0 || r.Authority.PlanVersion == "" || r.Authority.PlanDigest == "" || r.Authority.CapabilityDigest == "" || r.Authority.ProjectionDigest == "" || r.Authority.WriteScope == "" || r.Authority.EffectNamespace == "" {
		return ErrMissingAuthority
	}
	return nil
}

type RetryOutcome struct {
	ExecutionID  string `json:"execution_id"`
	NativeRunID  string `json:"native_run_id"`
	CausationID  string `json:"causation_id"`
	Status       string `json:"status"`
	Result       string `json:"result,omitempty"`
	FailureClass string `json:"failure_class,omitempty"`
}

type NativeRetryAdapter interface {
	Retry(context.Context, RetryRequest) (RetryOutcome, error)
}

const (
	RecoveryTerminal   = "terminal"
	RecoveryCheckpoint = "checkpoint"
	RecoveryGap        = "gap"
	RecoveryMissing    = "missing"
)

// RecoveryRequest asks the source owner to inspect an uncertain operation.
// It is deliberately read-only: implementations must find retained source
// facts or report that the history cannot be reconciled.
type RecoveryRequest struct {
	Version         string    `json:"version"`
	ExecutionID     string    `json:"execution_id"`
	NativeRunID     string    `json:"native_run_id"`
	SourceKind      string    `json:"source_kind"`
	SourceNamespace string    `json:"source_namespace"`
	SourceID        string    `json:"source_id"`
	SourceCursor    string    `json:"source_cursor,omitempty"`
	Authority       Authority `json:"authority"`
}

func (r RecoveryRequest) Validate() error {
	if r.Version != ExecutionContractV1 {
		return fmt.Errorf("%w: %s", ErrUnsupportedVersion, r.Version)
	}
	if strings.TrimSpace(r.ExecutionID) == "" || strings.TrimSpace(r.NativeRunID) == "" || strings.TrimSpace(r.SourceKind) == "" || strings.TrimSpace(r.SourceNamespace) == "" || strings.TrimSpace(r.SourceID) == "" {
		return ErrMissingAuthority
	}
	if r.Authority.TaskRevision <= 0 || r.Authority.PlanVersion == "" || r.Authority.PlanDigest == "" || r.Authority.CapabilityDigest == "" || r.Authority.ProjectionDigest == "" || r.Authority.WriteScope == "" || r.Authority.EffectNamespace == "" {
		return ErrMissingAuthority
	}
	return nil
}

type RecoveryOutcome struct {
	Status           string               `json:"status"`
	NativeRunID      string               `json:"native_run_id,omitempty"`
	NativeEventID    string               `json:"native_event_id,omitempty"`
	NativeVersion    string               `json:"native_version,omitempty"`
	NativeSequence   *int64               `json:"native_sequence,omitempty"`
	SourceSequence   int64                `json:"source_sequence,omitempty"`
	SourceCursor     string               `json:"source_cursor,omitempty"`
	ObservationScope string               `json:"observation_scope,omitempty"`
	TerminalStatus   string               `json:"terminal_status,omitempty"`
	Result           string               `json:"result,omitempty"`
	FailureClass     string               `json:"failure_class,omitempty"`
	Checkpoint       *CheckpointReference `json:"checkpoint,omitempty"`
}

type NativeRecoveryAdapter interface {
	Reconcile(context.Context, RecoveryRequest) (RecoveryOutcome, error)
}

func digestWithoutField(value any) string {
	clone := value
	if m, ok := value.(CapabilityManifest); ok {
		clone = struct {
			Version      string       `json:"version"`
			Capabilities []Capability `json:"capabilities"`
		}{m.Version, m.Capabilities}
	}
	raw, _ := json.Marshal(clone)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
