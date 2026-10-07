package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type scheduleRepository struct{ store *Store }
type webhookRepository struct{ store *Store }

func (r *webhookRepository) CommitTerminalWithDelivery(ctx context.Context, executionID string, expectedRevision int64, status ExecutionStatus, result, failure []byte, delivery WebhookDelivery) (TaskExecution, WebhookDelivery, error) {
	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	if delivery.EffectKey == "" || delivery.URL == "" {
		return TaskExecution{}, WebhookDelivery{}, fmt.Errorf("%w: effect key and url are required", ErrDeliveryConflict)
	}
	if delivery.Status == "" {
		delivery.Status = DeliveryPending
	}
	if delivery.NextAttemptAt.IsZero() {
		delivery.NextAttemptAt = time.Now().UTC()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = time.Now().UTC()
	}
	delivery.UpdatedAt = delivery.CreatedAt
	if delivery.Payload == nil {
		delivery.Payload = []byte{}
	}
	if delivery.Revision == 0 {
		delivery.Revision = 1
	}
	now := time.Now().UTC()
	err := r.store.WithTransaction(ctx, func(tx *Transaction) error {
		updated, err := tx.ExecContext(ctx, `UPDATE task_harness_executions SET status=?,result=?,failure_class=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`, status, result, failure, now.Format(time.RFC3339Nano), executionID, expectedRevision)
		if err != nil {
			return err
		}
		if n, _ := updated.RowsAffected(); n != 1 {
			return ErrRevisionConflict
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO webhook_deliveries(id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,lease_until,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, delivery.ID, delivery.ExecutionID, delivery.OccurrenceID, delivery.EffectKey, delivery.URL, delivery.Payload, delivery.Status, delivery.Revision, delivery.AttemptCount, delivery.NextAttemptAt.Format(time.RFC3339Nano), delivery.Owner, nullableTime(delivery.LeaseUntil), delivery.LastError, delivery.CreatedAt.Format(time.RFC3339Nano), delivery.UpdatedAt.Format(time.RFC3339Nano))
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrDeliveryConflict
			}
			return err
		}
		return nil
	})
	if err != nil {
		return TaskExecution{}, WebhookDelivery{}, err
	}
	execution, err := r.store.Executions().GetExecution(ctx, executionID)
	if err != nil {
		return TaskExecution{}, WebhookDelivery{}, err
	}
	return execution, delivery, nil
}

func validateSchedule(schedule Schedule) error {
	if strings.TrimSpace(schedule.ID) == "" {
		return fmt.Errorf("%w: id is required", ErrInvalidSchedule)
	}
	if !validCronExpression(schedule.Cron) {
		return fmt.Errorf("%w: cron expression is invalid", ErrInvalidSchedule)
	}
	if _, err := time.LoadLocation(schedule.Timezone); err != nil {
		return fmt.Errorf("%w: timezone: %v", ErrInvalidSchedule, err)
	}
	switch schedule.ActionType {
	case ScheduleActionAgent, ScheduleActionFixed, ScheduleActionCompilation:
	default:
		return fmt.Errorf("%w: action type %q", ErrInvalidSchedule, schedule.ActionType)
	}
	switch schedule.MisfirePolicy {
	case MisfireSkip, MisfireCoalesceLatest, MisfireBackfill:
	default:
		return fmt.Errorf("%w: misfire policy %q", ErrInvalidSchedule, schedule.MisfirePolicy)
	}
	if schedule.RetryLimit < 0 || schedule.BackfillLimit < 0 {
		return fmt.Errorf("%w: limits must be non-negative", ErrInvalidSchedule)
	}
	if schedule.MisfirePolicy == MisfireBackfill && schedule.BackfillLimit == 0 {
		return fmt.Errorf("%w: backfill limit must be positive", ErrInvalidSchedule)
	}
	return nil
}

func validCronExpression(expr string) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 7}}
	for i, field := range fields {
		if field == "" || !validCronField(field, limits[i][0], limits[i][1]) {
			return false
		}
	}
	return true
}

func validCronField(field string, min, max int) bool {
	for _, part := range strings.Split(field, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return false
		}
		base := part
		if strings.Contains(base, "/") {
			pieces := strings.Split(base, "/")
			if len(pieces) != 2 || pieces[1] == "" {
				return false
			}
			var step int
			if _, err := fmt.Sscanf(pieces[1], "%d", &step); err != nil || step <= 0 {
				return false
			}
			base = pieces[0]
		}
		if base == "*" {
			continue
		}
		if strings.Contains(base, "-") {
			pieces := strings.Split(base, "-")
			if len(pieces) != 2 {
				return false
			}
			var start, end int
			if _, err := fmt.Sscanf(pieces[0], "%d", &start); err != nil {
				return false
			}
			if _, err := fmt.Sscanf(pieces[1], "%d", &end); err != nil {
				return false
			}
			if start < min || end > max || start > end {
				return false
			}
			continue
		}
		var value int
		if _, err := fmt.Sscanf(base, "%d", &value); err != nil || value < min || value > max {
			return false
		}
	}
	return true
}

func (r *scheduleRepository) CreateSchedule(ctx context.Context, schedule Schedule) (Schedule, error) {
	if schedule.ID == "" {
		schedule.ID = uuid.NewString()
	}
	if schedule.MisfirePolicy == "" {
		schedule.MisfirePolicy = MisfireSkip
	}
	if schedule.ActionType == "" {
		schedule.ActionType = ScheduleActionAgent
	}
	if err := validateSchedule(schedule); err != nil {
		return Schedule{}, err
	}
	schedule.Revision = 1
	now := time.Now().UTC()
	schedule.CreatedAt, schedule.UpdatedAt = now, now
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO schedule_definitions(id,cron,timezone,action_type,instruction,payload,enabled,retry_limit,misfire_policy,backfill_limit,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`), schedule.ID, schedule.Cron, schedule.Timezone, schedule.ActionType, schedule.Instruction, schedule.Payload, boolInt(schedule.Enabled), schedule.RetryLimit, schedule.MisfirePolicy, schedule.BackfillLimit, schedule.Revision, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano))
	if err != nil {
		return Schedule{}, fmt.Errorf("create schedule: %w", err)
	}
	return schedule, nil
}

func scanSchedule(scanner interface{ Scan(...any) error }) (Schedule, error) {
	var schedule Schedule
	var enabled int
	var created, updated string
	err := scanner.Scan(&schedule.ID, &schedule.Cron, &schedule.Timezone, &schedule.ActionType, &schedule.Instruction, &schedule.Payload, &enabled, &schedule.RetryLimit, &schedule.MisfirePolicy, &schedule.BackfillLimit, &schedule.Revision, &created, &updated)
	if err != nil {
		return Schedule{}, err
	}
	schedule.Enabled = enabled != 0
	schedule.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	schedule.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return schedule, nil
}

func (r *scheduleRepository) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	schedule, err := scanSchedule(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,cron,timezone,action_type,instruction,payload,enabled,retry_limit,misfire_policy,backfill_limit,revision,created_at,updated_at FROM schedule_definitions WHERE id=?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return Schedule{}, ErrNotFound
	}
	if err != nil {
		return Schedule{}, fmt.Errorf("get schedule: %w", err)
	}
	return schedule, nil
}

func (r *scheduleRepository) UpdateSchedule(ctx context.Context, id string, expectedRevision int64, patch SchedulePatch) (Schedule, error) {
	current, err := r.GetSchedule(ctx, id)
	if err != nil {
		return Schedule{}, err
	}
	if current.Revision != expectedRevision {
		return Schedule{}, ErrRevisionConflict
	}
	if patch.Cron != nil {
		current.Cron = *patch.Cron
	}
	if patch.Timezone != nil {
		current.Timezone = *patch.Timezone
	}
	if patch.Instruction != nil {
		current.Instruction = *patch.Instruction
	}
	if patch.Payload != nil {
		current.Payload = *patch.Payload
	}
	if patch.ActionType != nil {
		current.ActionType = *patch.ActionType
	}
	if patch.Enabled != nil {
		current.Enabled = *patch.Enabled
	}
	if patch.RetryLimit != nil {
		current.RetryLimit = *patch.RetryLimit
	}
	if patch.BackfillLimit != nil {
		current.BackfillLimit = *patch.BackfillLimit
	}
	if patch.MisfirePolicy != nil {
		current.MisfirePolicy = *patch.MisfirePolicy
	}
	if err := validateSchedule(current); err != nil {
		return Schedule{}, err
	}
	now := time.Now().UTC()
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE schedule_definitions SET cron=?,timezone=?,action_type=?,instruction=?,payload=?,enabled=?,retry_limit=?,misfire_policy=?,backfill_limit=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), current.Cron, current.Timezone, current.ActionType, current.Instruction, current.Payload, boolInt(current.Enabled), current.RetryLimit, current.MisfirePolicy, current.BackfillLimit, now.Format(time.RFC3339Nano), id, expectedRevision)
	if err != nil {
		return Schedule{}, fmt.Errorf("update schedule: %w", err)
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return Schedule{}, ErrRevisionConflict
	}
	return r.GetSchedule(ctx, id)
}

func (r *scheduleRepository) AdmitOccurrence(ctx context.Context, occurrence ScheduleOccurrence, outbox OutboxMessage) (ScheduleOccurrence, error) {
	if occurrence.ID == "" {
		occurrence.ID = uuid.NewString()
	}
	if occurrence.ScheduleID == "" || occurrence.OccurrenceKey == "" || occurrence.IdempotencyKey == "" {
		return ScheduleOccurrence{}, fmt.Errorf("%w: occurrence identity is required", ErrInvalidSchedule)
	}
	if occurrence.Status == "" {
		occurrence.Status = OccurrenceAdmitted
	}
	if occurrence.Revision == 0 {
		occurrence.Revision = 1
	}
	if occurrence.CreatedAt.IsZero() {
		occurrence.CreatedAt = time.Now().UTC()
	}
	occurrence.UpdatedAt = occurrence.CreatedAt
	if occurrence.LocalTime.IsZero() {
		occurrence.LocalTime = occurrence.CreatedAt
	}
	if occurrence.ResolvedAt.IsZero() {
		occurrence.ResolvedAt = occurrence.LocalTime.UTC()
	}
	if outbox.Kind == "" {
		outbox.Kind = OutboxKindSchedule
	}
	if outbox.IdempotencyKey == "" {
		outbox.IdempotencyKey = occurrence.IdempotencyKey
	}
	if len(outbox.Payload) == 0 {
		outbox.Payload = []byte{}
	}
	err := r.store.WithTransaction(ctx, func(tx *Transaction) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO schedule_occurrences(id,schedule_id,schedule_revision,occurrence_key,local_time,resolved_at,timezone,tzdata_version,dst_decision,status,revision,attempt_count,execution_id,correlation_id,idempotency_key,policy_digest,capability_digest,plan_version,projection_digest,effect_namespace,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, occurrence.ID, occurrence.ScheduleID, occurrence.ScheduleRevision, occurrence.OccurrenceKey, occurrence.LocalTime.Format(time.RFC3339Nano), occurrence.ResolvedAt.UTC().Format(time.RFC3339Nano), occurrence.Timezone, occurrence.TzdataVersion, occurrence.DSTDecision, occurrence.Status, occurrence.Revision, occurrence.AttemptCount, occurrence.ExecutionID, occurrence.CorrelationID, occurrence.IdempotencyKey, occurrence.PolicyDigest, occurrence.CapabilityDigest, occurrence.PlanVersion, occurrence.ProjectionDigest, occurrence.EffectNamespace, occurrence.LastError, occurrence.CreatedAt.Format(time.RFC3339Nano), occurrence.UpdatedAt.Format(time.RFC3339Nano))
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrOccurrenceConflict
			}
			return fmt.Errorf("admit occurrence: %w", err)
		}
		if err := tx.EnqueueOutbox(ctx, outbox); err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "unique") {
				return ErrOccurrenceConflict
			}
			return fmt.Errorf("admit occurrence outbox: %w", err)
		}
		return nil
	})
	if errors.Is(err, ErrOccurrenceConflict) {
		existing, findErr := r.FindOccurrence(ctx, occurrence.ScheduleID, occurrence.OccurrenceKey)
		if findErr == nil && existing.IdempotencyKey == occurrence.IdempotencyKey {
			return existing, nil
		}
		return ScheduleOccurrence{}, ErrOccurrenceConflict
	}
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	return occurrence, nil
}

func scanOccurrence(scanner interface{ Scan(...any) error }) (ScheduleOccurrence, error) {
	var o ScheduleOccurrence
	var local, resolved, created, updated string
	err := scanner.Scan(&o.ID, &o.ScheduleID, &o.ScheduleRevision, &o.OccurrenceKey, &local, &resolved, &o.Timezone, &o.TzdataVersion, &o.DSTDecision, &o.Status, &o.Revision, &o.AttemptCount, &o.ExecutionID, &o.CorrelationID, &o.IdempotencyKey, &o.PolicyDigest, &o.CapabilityDigest, &o.PlanVersion, &o.ProjectionDigest, &o.EffectNamespace, &o.LastError, &created, &updated)
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	o.LocalTime, _ = time.Parse(time.RFC3339Nano, local)
	o.ResolvedAt, _ = time.Parse(time.RFC3339Nano, resolved)
	o.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	o.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return o, nil
}

const occurrenceColumns = `id,schedule_id,schedule_revision,occurrence_key,local_time,resolved_at,timezone,tzdata_version,dst_decision,status,revision,attempt_count,execution_id,correlation_id,idempotency_key,policy_digest,capability_digest,plan_version,projection_digest,effect_namespace,last_error,created_at,updated_at`

func (r *scheduleRepository) GetOccurrence(ctx context.Context, id string) (ScheduleOccurrence, error) {
	o, err := scanOccurrence(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT `+occurrenceColumns+` FROM schedule_occurrences WHERE id=?`), id))
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleOccurrence{}, ErrNotFound
	}
	return o, err
}

func (r *scheduleRepository) FindOccurrence(ctx context.Context, scheduleID, key string) (ScheduleOccurrence, error) {
	o, err := scanOccurrence(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT `+occurrenceColumns+` FROM schedule_occurrences WHERE schedule_id=? AND occurrence_key=?`), scheduleID, key))
	if errors.Is(err, sql.ErrNoRows) {
		return ScheduleOccurrence{}, ErrNotFound
	}
	return o, err
}

func (r *scheduleRepository) UpdateOccurrence(ctx context.Context, id string, expectedRevision int64, patch OccurrencePatch) (ScheduleOccurrence, error) {
	current, err := r.GetOccurrence(ctx, id)
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	if current.Revision != expectedRevision {
		return ScheduleOccurrence{}, ErrRevisionConflict
	}
	if !validOccurrenceTransition(current.Status, patch.Status) {
		return ScheduleOccurrence{}, ErrInvalidTransition
	}
	if patch.Status != "" {
		current.Status = patch.Status
	}
	if patch.ExecutionID != nil {
		current.ExecutionID = *patch.ExecutionID
	}
	if patch.CorrelationID != nil {
		current.CorrelationID = *patch.CorrelationID
	}
	if patch.AttemptCount != nil {
		current.AttemptCount = *patch.AttemptCount
	}
	if patch.LastError != nil {
		current.LastError = *patch.LastError
	}
	now := time.Now().UTC()
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE schedule_occurrences SET status=?,attempt_count=?,execution_id=?,correlation_id=?,last_error=?,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), current.Status, current.AttemptCount, current.ExecutionID, current.CorrelationID, current.LastError, now.Format(time.RFC3339Nano), id, expectedRevision)
	if err != nil {
		return ScheduleOccurrence{}, err
	}
	if n, _ := result.RowsAffected(); n != 1 {
		return ScheduleOccurrence{}, ErrRevisionConflict
	}
	return r.GetOccurrence(ctx, id)
}

func validOccurrenceTransition(from, to OccurrenceStatus) bool {
	if to == "" || from == to {
		return true
	}
	switch from {
	case OccurrenceAdmitted:
		return to == OccurrenceDispatching || to == OccurrenceSkipped || to == OccurrenceCoalesced || to == OccurrenceRecoveryRequired
	case OccurrenceDispatching:
		return to == OccurrenceDispatched || to == OccurrenceRetryable || to == OccurrenceDeadLettered || to == OccurrenceRecoveryRequired
	case OccurrenceDispatched:
		return to == OccurrenceRunning || to == OccurrenceRetryable || to == OccurrenceDeadLettered || to == OccurrenceRecoveryRequired
	case OccurrenceRunning:
		return to == OccurrenceSucceeded || to == OccurrenceFailed || to == OccurrenceRetryable || to == OccurrenceRecoveryRequired
	case OccurrenceRetryable:
		return to == OccurrenceDispatching || to == OccurrenceDeadLettered || to == OccurrenceRecoveryRequired
	default:
		return false
	}
}

func (r *scheduleRepository) ListOccurrences(ctx context.Context, scheduleID string) ([]ScheduleOccurrence, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT `+occurrenceColumns+` FROM schedule_occurrences WHERE schedule_id=? ORDER BY local_time, id`), scheduleID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var result []ScheduleOccurrence
	for rows.Next() {
		o, scanErr := scanOccurrence(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		result = append(result, o)
	}
	return result, rows.Err()
}

func (r *scheduleRepository) RecordDispatchAttempt(ctx context.Context, attempt DispatchAttempt) (DispatchAttempt, error) {
	if attempt.ID == "" {
		attempt.ID = uuid.NewString()
	}
	if attempt.CreatedAt.IsZero() {
		attempt.CreatedAt = time.Now().UTC()
	}
	attempt.UpdatedAt = attempt.CreatedAt
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO schedule_dispatch_attempts(id,occurrence_id,outbox_id,status,attempt,owner,fencing,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`), attempt.ID, attempt.OccurrenceID, attempt.OutboxID, attempt.Status, attempt.Attempt, attempt.Owner, attempt.Fencing, attempt.CreatedAt.Format(time.RFC3339Nano), attempt.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return DispatchAttempt{}, err
	}
	return attempt, nil
}

func (r *scheduleRepository) ListDispatchAttempts(ctx context.Context, occurrenceID string) ([]DispatchAttempt, error) {
	rows, err := r.store.db.QueryContext(ctx, r.store.bind(`SELECT id,occurrence_id,outbox_id,status,attempt,owner,fencing,created_at,updated_at FROM schedule_dispatch_attempts WHERE occurrence_id=? ORDER BY attempt,id`), occurrenceID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var attempts []DispatchAttempt
	for rows.Next() {
		var attempt DispatchAttempt
		var created, updated string
		if err := rows.Scan(&attempt.ID, &attempt.OccurrenceID, &attempt.OutboxID, &attempt.Status, &attempt.Attempt, &attempt.Owner, &attempt.Fencing, &created, &updated); err != nil {
			return nil, err
		}
		attempt.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		attempt.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		attempts = append(attempts, attempt)
	}
	return attempts, rows.Err()
}

func (r *webhookRepository) CreateDelivery(ctx context.Context, delivery WebhookDelivery) (WebhookDelivery, error) {
	if delivery.ID == "" {
		delivery.ID = uuid.NewString()
	}
	if strings.TrimSpace(delivery.EffectKey) == "" || strings.TrimSpace(delivery.URL) == "" {
		return WebhookDelivery{}, fmt.Errorf("%w: effect key and url are required", ErrDeliveryConflict)
	}
	if delivery.Status == "" {
		delivery.Status = DeliveryPending
	}
	if delivery.NextAttemptAt.IsZero() {
		delivery.NextAttemptAt = time.Now().UTC()
	}
	if delivery.CreatedAt.IsZero() {
		delivery.CreatedAt = time.Now().UTC()
	}
	delivery.UpdatedAt = delivery.CreatedAt
	if delivery.Payload == nil {
		delivery.Payload = []byte{}
	}
	if delivery.Revision == 0 {
		delivery.Revision = 1
	}
	_, err := r.store.db.ExecContext(ctx, r.store.bind(`INSERT INTO webhook_deliveries(id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,lease_until,last_error,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`), delivery.ID, delivery.ExecutionID, delivery.OccurrenceID, delivery.EffectKey, delivery.URL, delivery.Payload, delivery.Status, delivery.Revision, delivery.AttemptCount, delivery.NextAttemptAt.Format(time.RFC3339Nano), delivery.Owner, nullableTime(delivery.LeaseUntil), delivery.LastError, delivery.CreatedAt.Format(time.RFC3339Nano), delivery.UpdatedAt.Format(time.RFC3339Nano))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return r.getByEffectKey(ctx, delivery.EffectKey)
		}
		return WebhookDelivery{}, err
	}
	return delivery, nil
}

func (r *webhookRepository) getByEffectKey(ctx context.Context, key string) (WebhookDelivery, error) {
	return r.scanDelivery(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,lease_until,last_error,created_at,updated_at FROM webhook_deliveries WHERE effect_key=?`), key))
}

func (r *webhookRepository) scanDelivery(scanner interface{ Scan(...any) error }) (WebhookDelivery, error) {
	var d WebhookDelivery
	var next, created, updated string
	var lease sql.NullString
	err := scanner.Scan(&d.ID, &d.ExecutionID, &d.OccurrenceID, &d.EffectKey, &d.URL, &d.Payload, &d.Status, &d.Revision, &d.AttemptCount, &next, &d.Owner, &lease, &d.LastError, &created, &updated)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return WebhookDelivery{}, ErrNotFound
		}
		return WebhookDelivery{}, err
	}
	d.NextAttemptAt, _ = time.Parse(time.RFC3339Nano, next)
	d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	d.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if lease.Valid {
		value, _ := time.Parse(time.RFC3339Nano, lease.String)
		d.LeaseUntil = &value
	}
	return d, nil
}

func (r *webhookRepository) GetDelivery(ctx context.Context, id string) (WebhookDelivery, error) {
	return r.scanDelivery(r.store.db.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,lease_until,last_error,created_at,updated_at FROM webhook_deliveries WHERE id=?`), id))
}

func (r *webhookRepository) ClaimDelivery(ctx context.Context, id string, lease time.Duration) (WebhookDelivery, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return WebhookDelivery{}, err
	}
	defer func() { _ = tx.Rollback() }()
	delivery, err := r.scanDelivery(tx.QueryRowContext(ctx, r.store.bind(`SELECT id,execution_id,occurrence_id,effect_key,url,payload,status,revision,attempt_count,next_attempt_at,owner,lease_until,last_error,created_at,updated_at FROM webhook_deliveries WHERE id=?`), id))
	if err != nil {
		return WebhookDelivery{}, err
	}
	now := time.Now().UTC()
	if delivery.Status == DeliveryDelivered || delivery.Status == DeliveryFailed {
		return WebhookDelivery{}, ErrDeliveryConflict
	}
	if delivery.NextAttemptAt.After(now) {
		return WebhookDelivery{}, ErrNotFound
	}
	if delivery.LeaseUntil != nil && delivery.LeaseUntil.After(now) {
		return WebhookDelivery{}, ErrLeaseLost
	}
	owner := uuid.NewString()
	until := now.Add(lease)
	res, err := tx.ExecContext(ctx, r.store.bind(`UPDATE webhook_deliveries SET status=?,owner=?,lease_until=?,attempt_count=attempt_count+1,revision=revision+1,updated_at=? WHERE id=? AND revision=?`), DeliveryLeased, owner, until.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), id, delivery.Revision)
	if err != nil {
		return WebhookDelivery{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return WebhookDelivery{}, ErrLeaseLost
	}
	if err := tx.Commit(); err != nil {
		return WebhookDelivery{}, err
	}
	delivery.Status, delivery.Owner, delivery.LeaseUntil = DeliveryLeased, owner, &until
	delivery.AttemptCount++
	delivery.Revision++
	delivery.UpdatedAt = now
	return delivery, nil
}

func (r *webhookRepository) transitionDelivery(ctx context.Context, id, owner string, revision int64, status WebhookDeliveryStatus, next time.Time, lastError string) error {
	res, err := r.store.db.ExecContext(ctx, r.store.bind(`UPDATE webhook_deliveries SET status=?,owner='',lease_until=NULL,next_attempt_at=?,last_error=?,revision=revision+1,updated_at=? WHERE id=? AND status=? AND owner=? AND revision=?`), status, next.UTC().Format(time.RFC3339Nano), lastError, time.Now().UTC().Format(time.RFC3339Nano), id, DeliveryLeased, owner, revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrLeaseLost
	}
	return nil
}

func (r *webhookRepository) MarkDelivered(ctx context.Context, id, owner string, revision int64) error {
	return r.transitionDelivery(ctx, id, owner, revision, DeliveryDelivered, time.Now().UTC(), "")
}
func (r *webhookRepository) RetryDelivery(ctx context.Context, id, owner string, revision int64, next time.Time, lastError string) error {
	return r.transitionDelivery(ctx, id, owner, revision, DeliveryRetryable, next, lastError)
}
func (r *webhookRepository) FailDelivery(ctx context.Context, id, owner string, revision int64, lastError string) error {
	return r.transitionDelivery(ctx, id, owner, revision, DeliveryFailed, time.Now().UTC(), lastError)
}
