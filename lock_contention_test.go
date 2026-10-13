package grizzle_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/internal/dialect/postgres"
)

func TestLockContention_QueryTableLockContention(t *testing.T) {
	db, _ := getPostgresDB(t)

	ctx := context.Background()
	schema := fmt.Sprintf("lc_query_schema_%d", time.Now().UnixNano())
	tbl := "users"

	if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %q;", schema)); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}
	defer func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE;", schema))
	}()

	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %q.%q (id INT PRIMARY KEY, title TEXT);", schema, tbl)); err != nil {
		t.Fatalf("creating test table: %v", err)
	}

	// Open a second connection to hold a lock
	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("getting conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	var conn2PID int
	if err := conn2.QueryRowContext(ctx, "SELECT pg_backend_pid();").Scan(&conn2PID); err != nil {
		t.Fatalf("getting conn2 pid: %v", err)
	}

	// Begin a transaction and read from the table to hold AccessShareLock
	tx, err := conn2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning tx on conn2: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SELECT * FROM %q.%q;", schema, tbl)); err != nil {
		t.Fatalf("reading table in tx: %v", err)
	}

	// Query lock contention from the primary connection
	conflicts, err := postgres.QueryTableLockContention(ctx, db, schema, tbl)
	if err != nil {
		t.Fatalf("QueryTableLockContention failed: %v", err)
	}

	if len(conflicts) != 1 {
		t.Fatalf("expected 1 lock conflict, got %d: %+v", len(conflicts), conflicts)
	}
	c := conflicts[0]
	if c.PID != conn2PID {
		t.Errorf("conflict PID = %d, want %d", c.PID, conn2PID)
	}
	if c.Schema != schema {
		t.Errorf("conflict Schema = %q, want %q", c.Schema, schema)
	}
	if c.Table != tbl {
		t.Errorf("conflict Table = %q, want %q", c.Table, tbl)
	}
	if c.LockMode != "AccessShareLock" {
		t.Errorf("conflict LockMode = %q, want AccessShareLock", c.LockMode)
	}

	// Release lock
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rolling back tx: %v", err)
	}

	// Query again: must have zero conflicts
	cleared, err := postgres.QueryTableLockContention(ctx, db, schema, tbl)
	if err != nil {
		t.Fatalf("QueryTableLockContention after rollback failed: %v", err)
	}
	if len(cleared) != 0 {
		t.Errorf("expected 0 conflicts after rollback, got %d", len(cleared))
	}
}

func TestLockContention_AdvisoryPreflightLogsWarningAndProceeds(t *testing.T) {
	db, _ := getPostgresDB(t)

	ctx := context.Background()
	schema := fmt.Sprintf("lc_advisory_schema_%d", time.Now().UnixNano())
	tbl := "items"

	if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %q;", schema)); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}
	defer func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE;", schema))
	}()

	if _, err := db.Exec(fmt.Sprintf("CREATE TABLE %q.%q (id INT PRIMARY KEY, title TEXT);", schema, tbl)); err != nil {
		t.Fatalf("creating test table: %v", err)
	}

	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("getting conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	tx, err := conn2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning tx on conn2: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SELECT * FROM %q.%q;", schema, tbl)); err != nil {
		t.Fatalf("reading table in tx: %v", err)
	}

	tables := []postgres.SchemaTable{{Schema: schema, Table: tbl}}
	// Advisory mode (failOnContention = false) must log warning and return nil without gating
	err = postgres.PreflightLockContention(ctx, db, tables, 100*time.Millisecond, false, nil)
	if err != nil {
		t.Fatalf("expected nil error in advisory mode, got: %v", err)
	}
}

func TestLockContention_FailOnContention_AbortsWhenLocked(t *testing.T) {
	db, _ := getPostgresDB(t)

	ctx := context.Background()
	schema := fmt.Sprintf("lc_gate_schema_%d", time.Now().UnixNano())
	tbl := "records"

	if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %q;", schema)); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}
	defer func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE;", schema))
	}()

	initialSQL := fmt.Sprintf("CREATE TABLE %q (id INT PRIMARY KEY, title TEXT);", tbl)
	if err := grizzle.Sync(ctx, db, grizzle.Options{
		Dialect:      grizzle.DialectPostgres,
		TargetSchema: schema,
		SchemaSQL:    initialSQL,
	}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("getting conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	tx, err := conn2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning tx on conn2: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SELECT * FROM %q.%q;", schema, tbl)); err != nil {
		t.Fatalf("reading table in tx: %v", err)
	}

	// Attempt migration that adds a column (requires strong lock on tbl) with FailOnContention = true
	newSchemaSQL := fmt.Sprintf("CREATE TABLE %q (id INT PRIMARY KEY, title TEXT, extra_col TEXT);", tbl)
	err = grizzle.Sync(ctx, db, grizzle.Options{
		Dialect:          grizzle.DialectPostgres,
		TargetSchema:     schema,
		SchemaSQL:        newSchemaSQL,
		FailOnContention: true,
		LockTimeout:      150 * time.Millisecond,
	})

	if err == nil {
		t.Fatalf("expected migration to abort with ErrLockContention, got nil")
	}

	if !errors.Is(err, grizzle.ErrLockContention) {
		t.Fatalf("expected errors.Is(err, ErrLockContention), got: %v", err)
	}

	var contentionErr *grizzle.LockContentionError
	if !errors.As(err, &contentionErr) {
		t.Fatalf("expected errors.As to match *LockContentionError, got: %T", err)
	}
	if len(contentionErr.Conflicts) == 0 {
		t.Fatalf("expected non-empty conflicts in LockContentionError")
	}

	// Verify the DDL was never executed: extra_col does not exist
	var colCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns 
		WHERE table_schema = $1 AND table_name = $2 AND column_name = 'extra_col';
	`, schema, tbl).Scan(&colCount)
	if err != nil {
		t.Fatalf("checking column count: %v", err)
	}
	if colCount != 0 {
		t.Errorf("extra_col was created despite contention abort")
	}
}

func TestLockContention_FailOnContention_BackoffRetriesAndSucceedsWhenCleared(t *testing.T) {
	db, _ := getPostgresDB(t)

	ctx := context.Background()
	schema := fmt.Sprintf("lc_clear_schema_%d", time.Now().UnixNano())
	tbl := "tasks"

	if _, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %q;", schema)); err != nil {
		t.Fatalf("creating test schema: %v", err)
	}
	defer func() {
		_, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %q CASCADE;", schema))
	}()

	initialSQL := fmt.Sprintf("CREATE TABLE %q (id INT PRIMARY KEY, title TEXT);", tbl)
	if err := grizzle.Sync(ctx, db, grizzle.Options{
		Dialect:      grizzle.DialectPostgres,
		TargetSchema: schema,
		SchemaSQL:    initialSQL,
	}); err != nil {
		t.Fatalf("initial sync failed: %v", err)
	}

	conn2, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("getting conn2: %v", err)
	}
	defer func() { _ = conn2.Close() }()

	tx, err := conn2.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("beginning tx on conn2: %v", err)
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("SELECT * FROM %q.%q;", schema, tbl)); err != nil {
		_ = tx.Rollback()
		t.Fatalf("reading table in tx: %v", err)
	}

	// Release lock in background after 60ms
	go func() {
		time.Sleep(60 * time.Millisecond)
		_ = tx.Rollback()
	}()

	// Migration with 2s LockTimeout should retry with backoff, see contention cleared, and succeed
	newSchemaSQL := fmt.Sprintf("CREATE TABLE %q (id INT PRIMARY KEY, title TEXT, resolved_col TEXT);", tbl)
	err = grizzle.Sync(ctx, db, grizzle.Options{
		Dialect:          grizzle.DialectPostgres,
		TargetSchema:     schema,
		SchemaSQL:        newSchemaSQL,
		FailOnContention: true,
		LockTimeout:      2 * time.Second,
	})

	if err != nil {
		t.Fatalf("expected Sync to succeed after lock cleared, got: %v", err)
	}

	// Verify column was successfully created
	var colCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.columns 
		WHERE table_schema = $1 AND table_name = $2 AND column_name = 'resolved_col';
	`, schema, tbl).Scan(&colCount)
	if err != nil {
		t.Fatalf("checking column count: %v", err)
	}
	if colCount != 1 {
		t.Errorf("expected resolved_col to be created, count=%d", colCount)
	}
}
