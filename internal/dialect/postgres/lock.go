package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"slices"
	"time"

	"github.com/muandane/grizzle/internal/backoff"
	"github.com/muandane/grizzle/internal/dialect"
	"github.com/muandane/grizzle/internal/plan"
)

// lockPollMaxBackoff caps the advisory-lock polling backoff well below the retry
// campaign ceiling: migrators must grab the lock promptly once it is released,
// while still cutting poll QPS ~10x versus the fixed 20ms ticker.
const lockPollMaxBackoff = 250 * time.Millisecond

// GenerateLockID produces a deterministic 64-bit integer hash from a schema identifier.
func GenerateLockID(schema string) int64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte("grizzle:schema_lock:" + schema))
	return int64(h.Sum64())
}

// AcquireAdvisoryLock acquires a PostgreSQL transaction-level exclusive advisory lock.
// This lock is automatically released by PostgreSQL when the transaction commits or aborts.
func AcquireAdvisoryLock(ctx context.Context, dbtx dialect.DBTX, lockID int64) error {
	var dummy int
	err := dbtx.QueryRowContext(ctx, "SELECT 1 FROM pg_advisory_xact_lock($1);", lockID).Scan(&dummy)
	if err != nil && err != sql.ErrNoRows {
		return fmt.Errorf("failed to acquire pg_advisory_xact_lock: %w", err)
	}
	return nil
}

// VerifySessionDedicated probes whether the connection preserves session state
// across queries. Connection poolers operating in transaction pooling mode (such
// as PgBouncer with pool_mode = transaction) reassign or reset (DISCARD ALL) the
// underlying server connection between statements, which breaks session-level
// advisory locks and leads to orphaned locks or hangs.
func VerifySessionDedicated(ctx context.Context, dbtx dialect.DBTX) error {
	const probeVal = "grizzle_session_active"
	// Set custom GUC in session scope (is_local = false)
	var setDummy string
	err := dbtx.QueryRowContext(ctx, "SELECT set_config('grizzle.session_probe', $1, false);", probeVal).Scan(&setDummy)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}

	var gotVal string
	err = dbtx.QueryRowContext(ctx, "SELECT current_setting('grizzle.session_probe', true);").Scan(&gotVal)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return nil
	}

	// Clean up probe setting on best effort
	var clearDummy string
	_ = dbtx.QueryRowContext(ctx, "SELECT set_config('grizzle.session_probe', '', false);").Scan(&clearDummy)

	if gotVal != probeVal {
		return fmt.Errorf("%w: session state was not preserved across queries (expected %q, got %q); connect directly to PostgreSQL or configure PgBouncer in session pooling mode", plan.ErrTransactionPooling, probeVal, gotVal)
	}
	return nil
}

// AcquireSessionAdvisoryLock acquires a PostgreSQL session-level exclusive advisory lock on a dedicated connection.
// It uses pg_try_advisory_lock in a non-blocking loop to avoid holding open server-side lock wait queues
// that would cause PostgreSQL deadlock detection against concurrent non-transactional DDL such as CREATE INDEX CONCURRENTLY.
func AcquireSessionAdvisoryLock(ctx context.Context, dbtx dialect.DBTX, lockID int64) error {
	if err := VerifySessionDedicated(ctx, dbtx); err != nil {
		return err
	}

	attempt := 0
	for {
		var acquired bool
		err := dbtx.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1);", lockID).Scan(&acquired)
		if err != nil {
			// Context expiry while polling must surface as ErrLockTimeout so
			// Sync/Apply retry logic (IsRetryable) can back off and retry.
			if ctx.Err() != nil {
				return fmt.Errorf("%w: %w", plan.ErrLockTimeout, err)
			}
			return fmt.Errorf("failed checking pg_try_advisory_lock: %w", err)
		}
		if acquired {
			return nil
		}

		timer := time.NewTimer(backoff.ComputeWithMax(attempt, nil, lockPollMaxBackoff))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %v", plan.ErrLockTimeout, ctx.Err())
		case <-timer.C:
		}
		attempt++
	}
}

// ReleaseSessionAdvisoryLock releases a PostgreSQL session-level exclusive advisory lock on a dedicated connection.
func ReleaseSessionAdvisoryLock(ctx context.Context, dbtx dialect.DBTX, lockID int64) error {
	var released bool
	err := dbtx.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1);", lockID).Scan(&released)
	if err != nil {
		return fmt.Errorf("failed to release pg_advisory_unlock: %w", err)
	}
	if !released {
		return fmt.Errorf("%w: advisory lock was not held by this connection (possible transaction-mode pooling or connection reuse)", plan.ErrTransactionPooling)
	}
	return nil
}

// Hash32 computes a deterministic 32-bit signed integer hash from a string.
func Hash32(s string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return int32(h.Sum32())
}

// AcquireSessionAdvisoryLock2 acquires a PostgreSQL session-level advisory lock using two 32-bit keys (namespace, key).
func AcquireSessionAdvisoryLock2(ctx context.Context, dbtx dialect.DBTX, key1, key2 int32) error {
	if err := VerifySessionDedicated(ctx, dbtx); err != nil {
		return err
	}

	attempt := 0
	for {
		var acquired bool
		err := dbtx.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1, $2);", key1, key2).Scan(&acquired)
		if err != nil {
			// Context expiry while polling must surface as ErrLockTimeout so
			// Sync/Apply retry logic (IsRetryable) can back off and retry.
			if ctx.Err() != nil {
				return fmt.Errorf("%w: %w", plan.ErrLockTimeout, err)
			}
			return fmt.Errorf("failed checking pg_try_advisory_lock($1, $2): %w", err)
		}
		if acquired {
			return nil
		}

		timer := time.NewTimer(backoff.ComputeWithMax(attempt, nil, lockPollMaxBackoff))
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %v", plan.ErrLockTimeout, ctx.Err())
		case <-timer.C:
		}
		attempt++
	}
}

// ReleaseSessionAdvisoryLock2 releases a PostgreSQL session-level advisory lock using two 32-bit keys (namespace, key).
func ReleaseSessionAdvisoryLock2(ctx context.Context, dbtx dialect.DBTX, key1, key2 int32) error {
	var released bool
	err := dbtx.QueryRowContext(ctx, "SELECT pg_advisory_unlock($1, $2);", key1, key2).Scan(&released)
	if err != nil {
		return fmt.Errorf("failed to release pg_advisory_unlock($1, $2): %w", err)
	}
	if !released {
		return fmt.Errorf("%w: advisory lock was not held by this connection (possible transaction-mode pooling or connection reuse)", plan.ErrTransactionPooling)
	}
	return nil
}

// AcquireSchemaLocks acquires session-level advisory locks for the given schemas in sorted, deduped order
// using the two-int form (hash32(namespace), hash32(schema)).
// If any lock acquisition fails, all locks acquired so far are released in reverse order before returning the error.
func AcquireSchemaLocks(ctx context.Context, dbtx dialect.DBTX, namespace string, schemas []string) ([]string, error) {
	if namespace == "" {
		namespace = "grizzle"
	}
	nsKey := Hash32(namespace)

	sorted := make([]string, len(schemas))
	copy(sorted, schemas)
	slices.Sort(sorted)
	sorted = slices.Compact(sorted)

	acquired := make([]string, 0, len(sorted))
	for _, s := range sorted {
		schemaKey := Hash32(s)
		if err := AcquireSessionAdvisoryLock2(ctx, dbtx, nsKey, schemaKey); err != nil {
			// Roll back all acquired locks in reverse order
			for _, a := range slices.Backward(acquired) {
				_ = ReleaseSessionAdvisoryLock2(context.Background(), dbtx, nsKey, Hash32(a))
			}
			return nil, err
		}
		acquired = append(acquired, s)
	}
	return acquired, nil
}

// ReleaseSchemaLocks releases session-level advisory locks for the given schemas in reverse order.
func ReleaseSchemaLocks(ctx context.Context, dbtx dialect.DBTX, namespace string, schemas []string) error {
	if namespace == "" {
		namespace = "grizzle"
	}
	nsKey := Hash32(namespace)

	var firstErr error
	for _, schema := range slices.Backward(schemas) {
		schemaKey := Hash32(schema)
		if err := ReleaseSessionAdvisoryLock2(ctx, dbtx, nsKey, schemaKey); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
