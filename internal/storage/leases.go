package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type leaseRepository struct{ store *Store }

func (r *leaseRepository) Acquire(ctx context.Context, resourceType, resourceID, owner string, duration time.Duration) (Lease, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return Lease{}, err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	var currentOwner, untilText string
	var fencing int64
	err = tx.QueryRowContext(ctx, r.store.bind(`SELECT owner,fencing,lease_until FROM leases WHERE resource_type=? AND resource_id=?`), resourceType, resourceID).Scan(&currentOwner, &fencing, &untilText)
	if errors.Is(err, sql.ErrNoRows) {
		fencing = 1
	} else if err != nil {
		return Lease{}, err
	} else {
		until, parseErr := time.Parse(time.RFC3339Nano, untilText)
		if parseErr != nil {
			return Lease{}, parseErr
		}
		if until.After(now) && currentOwner != owner {
			return Lease{}, ErrLeaseLost
		}
		fencing++
	}
	lease := Lease{ResourceType: resourceType, ResourceID: resourceID, Owner: owner, Fencing: fencing, LeaseUntil: now.Add(duration), HeartbeatAt: now}
	if _, err := tx.ExecContext(ctx, r.store.bind(`DELETE FROM leases WHERE resource_type=? AND resource_id=?`), resourceType, resourceID); err != nil {
		return Lease{}, err
	}
	if _, err := tx.ExecContext(ctx, r.store.bind(`INSERT INTO leases(resource_type,resource_id,owner,fencing,lease_until,heartbeat_at) VALUES(?,?,?,?,?,?)`), resourceType, resourceID, owner, fencing, lease.LeaseUntil.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano)); err != nil {
		return Lease{}, err
	}
	if err := r.store.fail("lease_claim"); err != nil {
		return Lease{}, err
	}
	if err := tx.Commit(); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

func (r *leaseRepository) Release(ctx context.Context, resourceType, resourceID, owner string, fencing int64) error {
	result, err := r.store.db.ExecContext(ctx, r.store.bind(`DELETE FROM leases WHERE resource_type=? AND resource_id=? AND owner=? AND fencing=?`), resourceType, resourceID, owner, fencing)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return ErrLeaseLost
	}
	return nil
}
