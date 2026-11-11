package baymax

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/FelixSeptem/baymax/core/types"
	"github.com/FelixSeptem/baymax/integration/fakes"
	"github.com/FelixSeptem/baymax/orchestration/mailbox"
	"github.com/FelixSeptem/baymax/orchestration/scheduler"
)

func testSchedulerMailboxAdapter(t *testing.T) SchedulerMailboxAdapter {
	t.Helper()
	s, err := scheduler.New(scheduler.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	m, err := mailbox.New(mailbox.NewMemoryStore(mailbox.Policy{MaxAttempts: 2, DLQEnabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	return SchedulerMailboxAdapter{Scheduler: s, Mailbox: m}
}

func scheduledCommand() ScheduledCommand {
	return ScheduledCommand{OccurrenceID: "occurrence-1", OccurrenceKey: "key-1", ScheduleID: "schedule-1", CorrelationID: "correlation-1", IdempotencyKey: "schedule-1:key-1", ScheduleRevision: 2, PolicyDigest: "policy", PlanVersion: "plan.v1", CapabilityDigest: "capability", ProjectionDigest: "projection", EffectNamespace: "schedule:schedule-1:key-1", Instruction: "run"}
}

func TestSchedulerMailboxAdapterPreservesCorrelation(t *testing.T) {
	adapter := testSchedulerMailboxAdapter(t)
	ctx := context.Background()
	command := scheduledCommand()
	if _, err := adapter.Enqueue(ctx, command, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	dispatched, ok, err := adapter.DispatchDue(ctx, "scheduler-worker")
	if err != nil || !ok || dispatched != command {
		t.Fatalf("dispatch=%#v ok=%v err=%v", dispatched, ok, err)
	}
	consumed, record, ok, err := adapter.Consume(ctx, "mailbox-worker", time.Minute)
	if err != nil || !ok || consumed != command || record.Envelope.CorrelationID != command.CorrelationID {
		t.Fatalf("consume=%#v record=%#v ok=%v err=%v", consumed, record, ok, err)
	}
	if err := adapter.Ack(ctx, record.Envelope.MessageID, "mailbox-worker"); err != nil {
		t.Fatal(err)
	}
}

func TestSchedulerMailboxAdapterUsesBaymaxLeaseRetryAndDeadLetter(t *testing.T) {
	adapter := testSchedulerMailboxAdapter(t)
	ctx := context.Background()
	command := scheduledCommand()
	if _, err := adapter.Enqueue(ctx, command, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.DispatchDue(ctx, "scheduler-worker"); err != nil {
		t.Fatal(err)
	}
	_, record, ok, err := adapter.Consume(ctx, "worker-a", time.Minute)
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v", ok, err)
	}
	if err := adapter.Ack(ctx, record.Envelope.MessageID, "worker-b"); !errors.Is(err, mailbox.ErrConsumerMismatch) {
		t.Fatalf("invalid ack error=%v", err)
	}
	if err := adapter.Requeue(ctx, record.Envelope.MessageID, "worker-a", "retry"); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(time.Second); ; {
		_, record, ok, err = adapter.Consume(ctx, "worker-a", time.Minute)
		if err != nil {
			t.Fatalf("retry consume: ok=%v err=%v", ok, err)
		}
		if ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("retry message did not become eligible")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := adapter.Nack(ctx, record.Envelope.MessageID, "worker-a", "failure"); err != nil {
		t.Fatal(err)
	}
	stats, err := adapter.Mailbox.Stats(ctx)
	if err != nil || stats.DeadLetterTotal != 1 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
}

func TestMailboxWorkerRequeuesHandlerFailureAndAcksSuccess(t *testing.T) {
	adapter := testSchedulerMailboxAdapter(t)
	ctx := context.Background()
	command := scheduledCommand()
	if _, err := adapter.Enqueue(ctx, command, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.DispatchDue(ctx, "scheduler-worker"); err != nil {
		t.Fatal(err)
	}
	called := 0
	worker := MailboxWorker{Adapter: adapter, Handle: func(context.Context, ScheduledCommand) error { called++; return nil }}
	ok, err := worker.RunOnce(ctx, "worker", time.Minute)
	if err != nil || !ok || called != 1 {
		t.Fatalf("worker success ok=%v called=%d err=%v", ok, called, err)
	}
	stats, err := adapter.Mailbox.Stats(ctx)
	if err != nil || stats.AckTotal != 1 {
		t.Fatalf("stats=%#v err=%v", stats, err)
	}
}

func TestAuthorizedMailboxContinuationUsesOneNativeAdmissionAndAcksSuccess(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "mailbox result"}}}))
	if err != nil {
		t.Fatal(err)
	}
	adapter := testSchedulerMailboxAdapter(t)
	result, err := client.RunMailboxAuthorized(context.Background(), MailboxRequest{
		Mailbox: adapter.Mailbox, ConsumerID: "mailbox-worker", Lease: time.Minute,
		MessageID: "mailbox-message", IdempotencyKey: "mailbox-effect", RunID: "mailbox-run", ParentRunID: "parent-run", CausationID: "cause-1", CorrelationID: "mailbox-correlation",
		AuthorityDigest: "cap-v1", PlanDigest: "plan-digest", WriteScope: "task", EffectNamespace: "effect:mailbox", Occurrence: "occurrence-1", ResourceLimit: 1,
		PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "execute mailbox command",
	})
	if err != nil || result.Execution.Terminal.Status != "succeeded" || result.Mailbox.LifecycleResult != "ack" {
		t.Fatalf("mailbox result=%#v err=%v", result, err)
	}
	stats, err := adapter.Mailbox.Stats(context.Background())
	if err != nil || stats.PublishedTotal != 1 || stats.AckTotal != 1 {
		t.Fatalf("mailbox stats=%#v err=%v", stats, err)
	}
}

func TestAuthorizedMailboxContinuationRequeuesFailedExecution(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Err: errors.New("mailbox model failed")}}))
	if err != nil {
		t.Fatal(err)
	}
	adapter := testSchedulerMailboxAdapter(t)
	result, err := client.RunMailboxAuthorized(context.Background(), MailboxRequest{
		Mailbox: adapter.Mailbox, ConsumerID: "mailbox-worker", Lease: time.Minute,
		MessageID: "mailbox-failure", IdempotencyKey: "mailbox-failure-effect", RunID: "mailbox-failure-run", ParentRunID: "parent-run", CausationID: "cause-1", CorrelationID: "mailbox-failure-correlation",
		AuthorityDigest: "cap-v1", PlanDigest: "plan-digest", WriteScope: "task", EffectNamespace: "effect:mailbox-failure", Occurrence: "occurrence-1", ResourceLimit: 1,
		PlanVersion: "plan-v1", CapabilityDigest: "cap-v1", ProjectionDigest: "projection-v1", Input: "execute failing command",
	})
	if err == nil || result.Execution.Terminal.Status != "failed" || result.Mailbox.LifecycleResult != "retry" {
		t.Fatalf("failed mailbox result=%#v err=%v", result, err)
	}
	stats, err := adapter.Mailbox.Stats(context.Background())
	if err != nil || stats.RequeueTotal != 1 || stats.AckTotal != 0 {
		t.Fatalf("mailbox failure stats=%#v err=%v", stats, err)
	}
}

func TestAuthorizedScheduledCallerUsesOccurrenceAndCommonMailboxBoundary(t *testing.T) {
	client, err := New(fakes.NewModel([]fakes.ModelStep{{Response: types.ModelResponse{FinalAnswer: "scheduled result"}}}))
	if err != nil {
		t.Fatal(err)
	}
	adapter := testSchedulerMailboxAdapter(t)
	command := scheduledCommand()
	result, err := client.RunScheduledAuthorized(context.Background(), ScheduledAuthorizedRequest{
		Adapter: adapter, WorkerID: "scheduler-worker", ConsumerID: "mailbox-worker", Lease: time.Minute,
		Command: command, ParentRunID: "scheduled-parent", CausationID: "scheduled-cause", WriteScope: "task", Input: "scheduled instruction",
	})
	if err != nil || result.Execution.Terminal.Status != "succeeded" || result.Mailbox.Occurrence != command.OccurrenceKey || result.Mailbox.LifecycleResult != "ack" {
		t.Fatalf("scheduled result=%#v err=%v", result, err)
	}
}
