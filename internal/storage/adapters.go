package storage

import "context"

// MailboxDispatcher is the application boundary for publishing a durable
// schedule intent to Baymax. Baymax owns mailbox lifecycle and execution.
type MailboxDispatcher interface {
	PublishMailbox(context.Context, string, []byte) error
}

// ContextProjector is the application boundary for projecting a durable intent
// into Stele. Stele owns context and memory persistence semantics.
type ContextProjector interface {
	ProjectContext(context.Context, string, []byte) error
}

// WebhookSender is the delivery boundary for notification intents.
type WebhookSender interface {
	DeliverWebhook(context.Context, string, []byte) error
}

func (s *Store) ValidateOutboxKind(kind OutboxKind) error {
	switch kind {
	case OutboxKindSchedule, OutboxKindStele, OutboxKindWebhook:
		return nil
	default:
		return ErrNotFound
	}
}
