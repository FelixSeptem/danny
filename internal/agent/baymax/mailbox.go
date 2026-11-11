package baymax

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/FelixSeptem/baymax/orchestration/mailbox"
)

var ErrMailboxRequest = errors.New("baymax: invalid authorized mailbox request")

type MailboxRequest struct {
	Mailbox    *mailbox.Mailbox
	ConsumerID string
	Lease      time.Duration

	MessageID, IdempotencyKey, RunID, ParentRunID, CausationID, CorrelationID string
	AuthorityDigest, PlanDigest, WriteScope, EffectNamespace, Occurrence      string
	ResourceLimit                                                             int
	PlanVersion, CapabilityDigest, ProjectionDigest                           string
	Input                                                                     string
	ToolPolicy                                                                ToolPolicy
}

type MailboxExecutionResult struct {
	Execution ExecutionResult
	Mailbox   mailbox.HostPlanOutcome
}

// RunMailboxAuthorized binds one logical message to Baymax's native host-plan
// admission, then lets the native mailbox own lease, retry, DLQ and ack state.
// The model is executed once for the admitted message; a failed execution is
// requeued with the same idempotency and causation identity.
func (c *Client) RunMailboxAuthorized(ctx context.Context, req MailboxRequest) (MailboxExecutionResult, error) {
	if c == nil || req.Mailbox == nil || req.ConsumerID == "" || req.Lease <= 0 || req.MessageID == "" || req.IdempotencyKey == "" || req.RunID == "" || req.CorrelationID == "" || req.ParentRunID == "" || req.CausationID == "" || req.AuthorityDigest == "" || req.PlanDigest == "" || req.WriteScope == "" || req.EffectNamespace == "" || req.Occurrence == "" || req.ResourceLimit <= 0 || req.PlanVersion == "" || req.CapabilityDigest == "" || req.ProjectionDigest == "" || req.Input == "" {
		return MailboxExecutionResult{}, ErrMailboxRequest
	}
	binding := mailbox.HostPlanBinding{
		ContractVersion: mailbox.HostPlanBindingContractVersion,
		MessageID:       req.MessageID, IdempotencyKey: req.IdempotencyKey, RunID: req.RunID, ParentRunID: req.ParentRunID,
		CausationID: req.CausationID, AuthorityDigest: req.AuthorityDigest, PlanDigest: req.PlanDigest,
		WriteScope: req.WriteScope, EffectNamespace: req.EffectNamespace, Occurrence: req.Occurrence,
		HostCorrelation: req.CorrelationID, ResourceLimit: req.ResourceLimit,
	}
	if _, err := req.Mailbox.AdmitHostPlan(ctx, binding); err != nil {
		return MailboxExecutionResult{}, err
	}
	record, ok, err := req.Mailbox.ConsumeWithLease(ctx, req.ConsumerID, req.Lease, true)
	if err != nil || !ok {
		return MailboxExecutionResult{}, fmt.Errorf("mailbox consume: %w", err)
	}
	if record.Envelope.MessageID != req.MessageID {
		return MailboxExecutionResult{}, fmt.Errorf("%w: consumed message identity changed", ErrMailboxRequest)
	}
	planOutcome, err := mailbox.HostPlanOutcomeFromRecord(record)
	if err != nil {
		return MailboxExecutionResult{}, err
	}
	return c.executeMailboxRecord(ctx, req, record, planOutcome)
}

func (c *Client) executeMailboxRecord(ctx context.Context, req MailboxRequest, record mailbox.Record, planOutcome mailbox.HostPlanOutcome) (MailboxExecutionResult, error) {
	execution, runErr := c.RunAuthorized(ctx, AuthorizedRequest{
		RunID: req.RunID, ParentRunID: req.ParentRunID, CorrelationID: req.CorrelationID,
		PlanVersion: req.PlanVersion, PlanDigest: req.PlanDigest, CapabilityDigest: req.CapabilityDigest, ProjectionDigest: req.ProjectionDigest,
		Input: req.Input, ToolPolicy: req.ToolPolicy,
	})
	if runErr != nil || execution.Terminal.Status != "succeeded" {
		reason := "mailbox execution failed"
		if runErr != nil {
			reason = runErr.Error()
		} else if execution.Terminal.FailureClass != "" {
			reason = execution.Terminal.FailureClass
		}
		updatedRecord, requeueErr := req.Mailbox.Requeue(ctx, req.MessageID, req.ConsumerID, reason)
		if requeueErr != nil {
			return MailboxExecutionResult{Execution: execution, Mailbox: planOutcome}, fmt.Errorf("%s; mailbox requeue: %w", reason, requeueErr)
		}
		updated, outcomeErr := mailbox.HostPlanOutcomeFromRecord(updatedRecord)
		if outcomeErr == nil {
			planOutcome = updated
		}
		return MailboxExecutionResult{Execution: execution, Mailbox: planOutcome}, errors.New(reason)
	}
	acked, ackErr := req.Mailbox.Ack(ctx, req.MessageID, req.ConsumerID)
	if ackErr != nil {
		return MailboxExecutionResult{Execution: execution, Mailbox: planOutcome}, ackErr
	}
	planOutcome, err := mailbox.HostPlanOutcomeFromRecord(acked)
	if err != nil {
		return MailboxExecutionResult{Execution: execution}, err
	}
	return MailboxExecutionResult{Execution: execution, Mailbox: planOutcome}, nil
}
