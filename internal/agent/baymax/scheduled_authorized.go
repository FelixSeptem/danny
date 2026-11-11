package baymax

import (
	"context"
	"fmt"
	"time"

	"github.com/FelixSeptem/baymax/orchestration/mailbox"
)

type ScheduledAuthorizedRequest struct {
	Adapter     SchedulerMailboxAdapter
	WorkerID    string
	ConsumerID  string
	Lease       time.Duration
	Command     ScheduledCommand
	ParentRunID string
	CausationID string
	WriteScope  string
	Input       string
}

type ScheduledAuthorizedResult struct {
	Execution ExecutionResult
	Mailbox   mailbox.HostPlanOutcome
}

func (c *Client) RunScheduledAuthorized(ctx context.Context, req ScheduledAuthorizedRequest) (ScheduledAuthorizedResult, error) {
	if c == nil || req.WorkerID == "" || req.ConsumerID == "" || req.Lease <= 0 || req.ParentRunID == "" || req.CausationID == "" || req.WriteScope == "" || req.Input == "" {
		return ScheduledAuthorizedResult{}, ErrMailboxRequest
	}
	if err := req.Command.Validate(); err != nil {
		return ScheduledAuthorizedResult{}, err
	}
	req.Command.ParentRunID = req.ParentRunID
	req.Command.CausationID = req.CausationID
	req.Command.WriteScope = req.WriteScope
	req.Command.ResourceLimit = 1
	if _, err := req.Adapter.Enqueue(ctx, req.Command, time.Now().UTC()); err != nil {
		return ScheduledAuthorizedResult{}, err
	}
	if _, ok, err := req.Adapter.DispatchDue(ctx, req.WorkerID); err != nil || !ok {
		if err != nil {
			return ScheduledAuthorizedResult{}, err
		}
		return ScheduledAuthorizedResult{}, fmt.Errorf("scheduled command was not dispatched")
	}
	mailboxReq := MailboxRequest{
		Mailbox: req.Adapter.Mailbox, ConsumerID: req.ConsumerID, Lease: req.Lease,
		MessageID: req.Command.OccurrenceID, IdempotencyKey: req.Command.IdempotencyKey, RunID: req.Command.CorrelationID,
		ParentRunID: req.ParentRunID, CausationID: req.CausationID, CorrelationID: req.Command.CorrelationID,
		AuthorityDigest: req.Command.CapabilityDigest, PlanDigest: req.Command.PlanVersion, WriteScope: req.WriteScope,
		EffectNamespace: req.Command.EffectNamespace, Occurrence: req.Command.OccurrenceKey, ResourceLimit: 1,
		PlanVersion: req.Command.PlanVersion, CapabilityDigest: req.Command.CapabilityDigest, ProjectionDigest: req.Command.ProjectionDigest,
		Input: req.Input,
	}
	record, ok, err := req.Adapter.Mailbox.ConsumeWithLease(ctx, req.ConsumerID, req.Lease, true)
	if err != nil || !ok {
		return ScheduledAuthorizedResult{}, fmt.Errorf("mailbox consume: %w", err)
	}
	planOutcome, outcomeErr := mailbox.HostPlanOutcomeFromRecord(record)
	if outcomeErr != nil {
		return ScheduledAuthorizedResult{}, outcomeErr
	}
	returnResult, err := c.executeMailboxRecord(ctx, mailboxReq, record, planOutcome)
	if err != nil {
		return ScheduledAuthorizedResult(returnResult), err
	}
	return ScheduledAuthorizedResult(returnResult), nil
}
