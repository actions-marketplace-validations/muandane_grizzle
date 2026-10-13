package postgres_test

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/muandane/grizzle/internal/dialect/postgres"
	"github.com/muandane/grizzle/internal/plan"
)

type poolMockDriver struct {
	mu           sync.Mutex
	mode         string // "pooled", "unheld_held_check", "unheld_release", "dedicated"
	sessionValue string
}

func (d *poolMockDriver) Open(name string) (driver.Conn, error) {
	return &poolMockConn{drv: d}, nil
}

type poolMockConn struct {
	drv *poolMockDriver
}

func (c *poolMockConn) Close() error { return nil }
func (c *poolMockConn) Begin() (driver.Tx, error) {
	return &poolMockTx{}, nil
}
func (c *poolMockConn) Prepare(query string) (driver.Stmt, error) {
	return &poolMockStmt{conn: c, query: query}, nil
}

type poolMockTx struct{}

func (t *poolMockTx) Commit() error   { return nil }
func (t *poolMockTx) Rollback() error { return nil }

type poolMockStmt struct {
	conn  *poolMockConn
	query string
}

func (s *poolMockStmt) Close() error  { return nil }
func (s *poolMockStmt) NumInput() int { return -1 }
func (s *poolMockStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(1), nil
}
func (s *poolMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	s.conn.drv.mu.Lock()
	defer s.conn.drv.mu.Unlock()

	q := s.query
	mode := s.conn.drv.mode

	switch {
	case strings.Contains(q, "set_config"):
		if len(args) >= 1 {
			if str, ok := args[0].(string); ok {
				s.conn.drv.sessionValue = str
			}
		}
		if mode == "pooled" {
			// Pooler resets session state immediately between queries
			s.conn.drv.sessionValue = ""
		}
		return &singleValueRows{colName: "set_config", val: "grizzle_session_active"}, nil

	case strings.Contains(q, "current_setting"):
		val := s.conn.drv.sessionValue
		if mode == "pooled" {
			val = ""
		}
		return &singleValueRows{colName: "current_setting", val: val}, nil

	case strings.Contains(q, "pg_try_advisory_lock"):
		return &singleValueRows{colName: "pg_try_advisory_lock", val: true}, nil

	case strings.Contains(q, "pg_locks"):
		held := true
		if mode == "unheld_held_check" {
			held = false
		}
		return &singleValueRows{colName: "exists", val: held}, nil

	case strings.Contains(q, "pg_advisory_unlock"):
		released := true
		if mode == "unheld_release" {
			released = false
		}
		return &singleValueRows{colName: "pg_advisory_unlock", val: released}, nil

	default:
		return &singleValueRows{colName: "dummy", val: 1}, nil
	}
}

type singleValueRows struct {
	colName string
	val     driver.Value
	read    bool
}

func (r *singleValueRows) Columns() []string { return []string{r.colName} }
func (r *singleValueRows) Close() error      { return nil }
func (r *singleValueRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	r.read = true
	dest[0] = r.val
	return nil
}

func setupMockDB(t *testing.T, mode string) *sql.DB {
	t.Helper()
	driverName := "mock_pool_" + t.Name() + "_" + strings.ReplaceAll(time.Now().String(), " ", "_")
	drv := &poolMockDriver{mode: mode}
	sql.Register(driverName, drv)

	db, err := sql.Open(driverName, "")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestAcquireSessionAdvisoryLock_TransactionPoolingDetected_FailsFast(t *testing.T) {
	t.Parallel()

	db := setupMockDB(t, "pooled")

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := postgres.AcquireSessionAdvisoryLock(ctx, db, 12345)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected AcquireSessionAdvisoryLock to fail on pooled connection, got nil")
	}
	if !errors.Is(err, plan.ErrTransactionPooling) {
		t.Fatalf("expected ErrTransactionPooling, got: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("advisory lock detection must fail fast without hanging, took %v", elapsed)
	}

	errMsg := err.Error()
	if !strings.Contains(errMsg, "transaction") || !strings.Contains(errMsg, "session") {
		t.Errorf("expected actionable error message referencing transaction/session pooling, got: %s", errMsg)
	}
}

func TestAcquireSessionAdvisoryLock2_TransactionPoolingDetected_FailsFast(t *testing.T) {
	t.Parallel()

	db := setupMockDB(t, "pooled")

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := postgres.AcquireSessionAdvisoryLock2(ctx, db, 100, 200)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected AcquireSessionAdvisoryLock2 to fail on pooled connection, got nil")
	}
	if !errors.Is(err, plan.ErrTransactionPooling) {
		t.Fatalf("expected ErrTransactionPooling, got: %v", err)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("advisory lock detection must fail fast without hanging, took %v", elapsed)
	}
}

func TestReleaseSessionAdvisoryLock_UnheldLock_Fails(t *testing.T) {
	t.Parallel()

	db := setupMockDB(t, "unheld_release")

	err := postgres.ReleaseSessionAdvisoryLock(context.Background(), db, 999)
	if err == nil {
		t.Fatal("expected ReleaseSessionAdvisoryLock to fail when lock not held, got nil")
	}
	if !errors.Is(err, plan.ErrTransactionPooling) {
		t.Fatalf("expected ErrTransactionPooling on unheld lock release, got: %v", err)
	}
}

func TestReleaseSessionAdvisoryLock2_UnheldLock_Fails(t *testing.T) {
	t.Parallel()

	db := setupMockDB(t, "unheld_release")

	err := postgres.ReleaseSessionAdvisoryLock2(context.Background(), db, 10, 20)
	if err == nil {
		t.Fatal("expected ReleaseSessionAdvisoryLock2 to fail when lock not held, got nil")
	}
	if !errors.Is(err, plan.ErrTransactionPooling) {
		t.Fatalf("expected ErrTransactionPooling on unheld lock release, got: %v", err)
	}
}

func TestAcquireSessionAdvisoryLock_DedicatedSession_Success(t *testing.T) {
	t.Parallel()

	db := setupMockDB(t, "dedicated")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := postgres.AcquireSessionAdvisoryLock(ctx, db, 12345); err != nil {
		t.Fatalf("expected successful lock acquisition on dedicated session, got: %v", err)
	}

	if err := postgres.ReleaseSessionAdvisoryLock(ctx, db, 12345); err != nil {
		t.Fatalf("expected successful release on dedicated session, got: %v", err)
	}
}
