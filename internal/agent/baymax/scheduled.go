package baymax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
)

var ErrInvalidScheduledCommand = errors.New("baymax: invalid scheduled command")

// ScheduledCommand is Danny's narrow, versioned handoff payload. Baymax owns
// all scheduler and mailbox state transitions after this payload is enqueued.
type ScheduledCommand struct {
	OccurrenceID, OccurrenceKey, ScheduleID, CorrelationID, IdempotencyKey string
	ScheduleRevision                                                       int64
	PolicyDigest, PlanVersion, CapabilityDigest, ProjectionDigest          string
	EffectNamespace                                                        string
	Instruction                                                            string
	RetryLimit                                                             int
	ParentRunID, CausationID, WriteScope                                   string
	ResourceLimit                                                          int
}

func (c ScheduledCommand) Validate() error {
	if strings.TrimSpace(c.OccurrenceID) == "" || strings.TrimSpace(c.OccurrenceKey) == "" || strings.TrimSpace(c.ScheduleID) == "" || strings.TrimSpace(c.CorrelationID) == "" || strings.TrimSpace(c.IdempotencyKey) == "" {
		return ErrInvalidScheduledCommand
	}
	if c.ScheduleRevision <= 0 || strings.TrimSpace(c.PolicyDigest) == "" || strings.TrimSpace(c.PlanVersion) == "" || strings.TrimSpace(c.CapabilityDigest) == "" || strings.TrimSpace(c.ProjectionDigest) == "" || strings.TrimSpace(c.EffectNamespace) == "" {
		return ErrInvalidScheduledCommand
	}
	return nil
}

type SchedulerMailboxAdapter struct {
	Scheduler *scheduler.Scheduler
	Mailbox   *mailbox.Mailbox
}

type ScheduledHandler func(context.Context, ScheduledCommand) error

type MailboxWorker struct {
	Adapter SchedulerMailboxAdapter
	Handle  ScheduledHandler
}

// RunOnce lets Baymax own mailbox lease, retry, requeue, and dead-letter
// transitions. The handler is responsible for using the occurrence
// idempotency key when admitting the Danny task harness execution.
func (w MailboxWorker) RunOnce(ctx context.Context, consumerID string, lease time.Duration) (bool, error) {
	if w.Handle == nil {
		return false, ErrInvalidScheduledCommand
	}
	command, record, ok, err := w.Adapter.Consume(ctx, consumerID, lease)
	if err != nil || !ok {
		return ok, err
	}
	if err := w.Handle(ctx, command); err != nil {
		if requeueErr := w.Adapter.Requeue(ctx, record.Envelope.MessageID, consumerID, err.Error()); requeueErr != nil {
			return true, fmt.Errorf("scheduled handler: %w; requeue: %v", err, requeueErr)
		}
		return true, err
	}
	if err := w.Adapter.Ack(ctx, record.Envelope.MessageID, consumerID); err != nil {
		return true, err
	}
	return true, nil
}

func (a SchedulerMailboxAdapter) Enqueue(ctx context.Context, command ScheduledCommand, notBefore time.Time) (scheduler.TaskRecord, error) {
	if a.Scheduler == nil {
		return scheduler.TaskRecord{}, fmt.Errorf("%w: scheduler is required", ErrInvalidScheduledCommand)
	}
	if err := command.Validate(); err != nil {
		return scheduler.TaskRecord{}, err
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return scheduler.TaskRecord{}, err
	}
	return a.Scheduler.Enqueue(ctx, scheduler.Task{TaskID: command.OccurrenceID, RunID: command.CorrelationID, Priority: scheduler.TaskPriorityNormal, MaxAttempts: command.RetryLimit + 1, NotBefore: notBefore.UTC(), Payload: map[string]any{"scheduled_command": string(payload)}})
}

// DispatchDue performs exactly one native scheduler claim and turns it into a
// native mailbox command. It acknowledges the scheduler only after Mailbox
// PublishCommand has returned, so a scheduler retry retains the same payload.
func (a SchedulerMailboxAdapter) DispatchDue(ctx context.Context, workerID string) (ScheduledCommand, bool, error) {
	if a.Scheduler == nil || a.Mailbox == nil {
		return ScheduledCommand{}, false, fmt.Errorf("%w: scheduler and mailbox are required", ErrInvalidScheduledCommand)
	}
	claimed, ok, err := a.Scheduler.Claim(ctx, workerID)
	if err != nil || !ok {
		return ScheduledCommand{}, ok, err
	}
	command, err := commandFromTask(claimed.Record.Task)
	if err != nil {
		_, _ = a.Scheduler.Fail(ctx, scheduler.TerminalCommit{TaskID: claimed.Record.Task.TaskID, AttemptID: claimed.Attempt.AttemptID, Status: scheduler.TaskStateFailed, ErrorMessage: err.Error(), OutcomeKey: "invalid-command", CommittedAt: time.Now().UTC()})
		return ScheduledCommand{}, true, err
	}
	envelope := mailbox.Envelope{MessageID: command.OccurrenceID, IdempotencyKey: command.IdempotencyKey, CorrelationID: command.CorrelationID, Kind: mailbox.KindCommand, TaskID: command.OccurrenceID, RunID: command.CorrelationID, Payload: commandPayload(command)}
	if command.ParentRunID != "" || command.CausationID != "" || command.WriteScope != "" || command.ResourceLimit > 0 {
		if command.ParentRunID == "" || command.CausationID == "" || command.WriteScope == "" || command.ResourceLimit <= 0 {
			return ScheduledCommand{}, true, ErrInvalidScheduledCommand
		}
		envelope.HostPlanBinding = &mailbox.HostPlanBinding{ContractVersion: mailbox.HostPlanBindingContractVersion, MessageID: command.OccurrenceID, IdempotencyKey: command.IdempotencyKey, RunID: command.CorrelationID, ParentRunID: command.ParentRunID, CausationID: command.CausationID, AuthorityDigest: command.CapabilityDigest, PlanDigest: command.PlanVersion, WriteScope: command.WriteScope, EffectNamespace: command.EffectNamespace, Occurrence: command.OccurrenceKey, HostCorrelation: command.CorrelationID, ResourceLimit: command.ResourceLimit}
	}
	_, err = a.Mailbox.PublishCommand(ctx, envelope)
	if err != nil {
		_, requeueErr := a.Scheduler.Requeue(ctx, claimed.Record.Task.TaskID, err.Error())
		if requeueErr != nil {
			return ScheduledCommand{}, true, fmt.Errorf("publish mailbox: %w; scheduler requeue: %v", err, requeueErr)
		}
		return ScheduledCommand{}, true, err
	}
	if _, err := a.Scheduler.Complete(ctx, scheduler.TerminalCommit{TaskID: claimed.Record.Task.TaskID, AttemptID: claimed.Attempt.AttemptID, Status: scheduler.TaskStateSucceeded, Result: map[string]any{"mailbox_message_id": command.OccurrenceID}, OutcomeKey: command.IdempotencyKey, CommittedAt: time.Now().UTC()}); err != nil {
		return ScheduledCommand{}, true, err
	}
	return command, true, nil
}

func (a SchedulerMailboxAdapter) Consume(ctx context.Context, consumerID string, lease time.Duration) (ScheduledCommand, mailbox.Record, bool, error) {
	if a.Mailbox == nil {
		return ScheduledCommand{}, mailbox.Record{}, false, fmt.Errorf("%w: mailbox is required", ErrInvalidScheduledCommand)
	}
	record, ok, err := a.Mailbox.ConsumeWithLease(ctx, consumerID, lease, true)
	if err != nil || !ok {
		return ScheduledCommand{}, record, ok, err
	}
	command, err := commandFromEnvelope(record.Envelope)
	if err != nil {
		return ScheduledCommand{}, record, true, err
	}
	return command, record, true, nil
}

func (a SchedulerMailboxAdapter) Ack(ctx context.Context, messageID, consumerID string) error {
	if a.Mailbox == nil {
		return fmt.Errorf("%w: mailbox is required", ErrInvalidScheduledCommand)
	}
	_, err := a.Mailbox.Ack(ctx, messageID, consumerID)
	return err
}

func (a SchedulerMailboxAdapter) Requeue(ctx context.Context, messageID, consumerID, reason string) error {
	if a.Mailbox == nil {
		return fmt.Errorf("%w: mailbox is required", ErrInvalidScheduledCommand)
	}
	_, err := a.Mailbox.Requeue(ctx, messageID, consumerID, reason)
	return err
}

func (a SchedulerMailboxAdapter) Nack(ctx context.Context, messageID, consumerID, reason string) error {
	if a.Mailbox == nil {
		return fmt.Errorf("%w: mailbox is required", ErrInvalidScheduledCommand)
	}
	_, err := a.Mailbox.Nack(ctx, messageID, consumerID, reason)
	return err
}

func commandPayload(command ScheduledCommand) map[string]any {
	payload, _ := json.Marshal(command)
	return map[string]any{"scheduled_command": string(payload)}
}

func commandFromTask(task scheduler.Task) (ScheduledCommand, error) {
	return commandFromPayload(task.Payload)
}
func commandFromEnvelope(envelope mailbox.Envelope) (ScheduledCommand, error) {
	return commandFromPayload(envelope.Payload)
}

func commandFromPayload(payload map[string]any) (ScheduledCommand, error) {
	raw, ok := payload["scheduled_command"].(string)
	if !ok || raw == "" {
		return ScheduledCommand{}, ErrInvalidScheduledCommand
	}
	var command ScheduledCommand
	if err := json.Unmarshal([]byte(raw), &command); err != nil {
		return ScheduledCommand{}, err
	}
	if err := command.Validate(); err != nil {
		return ScheduledCommand{}, err
	}
	return command, nil
}
