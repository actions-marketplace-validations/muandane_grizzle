//go:build integration

package exec_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/muandane/grizzle/internal/exec"
	"github.com/muandane/grizzle/internal/testutil"
)

func TestPostgres_CompositePK_TwoAndThreeColumnBackfill(t *testing.T) {
	db := testutil.TestDatabase(t)
	ctx := t.Context()

	const testSchema = "backfill_comp_test"
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		DROP SCHEMA IF EXISTS %s CASCADE;
		CREATE SCHEMA %s;
	`, testSchema, testSchema))
	if err != nil {
		t.Fatalf("setup test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, testSchema))
	})

	// 1. Two-column key: (tenant_id, user_id)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.pg_comp2 (
			tenant_id INT NOT NULL,
			user_id INT NOT NULL,
			old_col TEXT NOT NULL,
			new_col TEXT,
			PRIMARY KEY (tenant_id, user_id)
		);
	`, testSchema))
	if err != nil {
		t.Fatalf("setup pg_comp2: %v", err)
	}

	for tid := 1; tid <= 3; tid++ {
		for uid := 1; uid <= 8; uid++ {
			val := fmt.Sprintf("val_%d_%d", tid, uid)
			if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.pg_comp2 VALUES ($1, $2, $3, NULL)`, testSchema), tid, uid, val); err != nil {
				t.Fatalf("insert pg_comp2: %v", err)
			}
		}
	}

	hook2 := exec.NewCopyBackfillHook(exec.DialectPostgres, 5)
	err = exec.RunBackfill(ctx, db, testSchema, map[string]string{testSchema + ".pg_comp2.old_col": "new_col"}, nil, hook2, nil)
	if err != nil {
		t.Fatalf("RunBackfill 2-col failed: %v", err)
	}

	var nulls2 int
	_ = db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.pg_comp2 WHERE new_col IS NULL`, testSchema)).Scan(&nulls2)
	if nulls2 != 0 {
		t.Fatalf("expected 0 NULL rows in pg_comp2, got %d", nulls2)
	}

	// 2. Three-column key: (org_id, dept_id, item_id)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.pg_comp3 (
			org_id INT NOT NULL,
			dept_id INT NOT NULL,
			item_id INT NOT NULL,
			old_col TEXT NOT NULL,
			new_col TEXT,
			PRIMARY KEY (org_id, dept_id, item_id)
		);
	`, testSchema))
	if err != nil {
		t.Fatalf("setup pg_comp3: %v", err)
	}

	for o := 1; o <= 2; o++ {
		for d := 1; d <= 3; d++ {
			for i := 1; i <= 5; i++ {
				val := fmt.Sprintf("item_%d_%d_%d", o, d, i)
				if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.pg_comp3 VALUES ($1, $2, $3, $4, NULL)`, testSchema), o, d, i, val); err != nil {
					t.Fatalf("insert pg_comp3: %v", err)
				}
			}
		}
	}

	hook3 := exec.NewCopyBackfillHook(exec.DialectPostgres, 7)
	err = exec.RunBackfill(ctx, db, testSchema, map[string]string{testSchema + ".pg_comp3.old_col": "new_col"}, nil, hook3, nil)
	if err != nil {
		t.Fatalf("RunBackfill 3-col failed: %v", err)
	}

	var nulls3 int
	_ = db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.pg_comp3 WHERE new_col IS NULL`, testSchema)).Scan(&nulls3)
	if nulls3 != 0 {
		t.Fatalf("expected 0 NULL rows in pg_comp3, got %d", nulls3)
	}

	// 3. Resume mid-run test: (app_id, log_id)
	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.pg_resume (
			app_id INT NOT NULL,
			log_id INT NOT NULL,
			raw_val TEXT NOT NULL,
			san_val TEXT,
			PRIMARY KEY (app_id, log_id)
		);
	`, testSchema))
	if err != nil {
		t.Fatalf("setup pg_resume: %v", err)
	}

	for i := 1; i <= 20; i++ {
		msg := fmt.Sprintf("raw_%d", i)
		var sanitized any
		if i <= 8 {
			sanitized = msg // pre-populated
		}
		if _, err := db.ExecContext(ctx, fmt.Sprintf(`INSERT INTO %s.pg_resume VALUES (10, $1, $2, $3)`, testSchema), i, msg, sanitized); err != nil {
			t.Fatalf("insert pg_resume: %v", err)
		}
	}

	hookResume := exec.NewCopyBackfillHook(exec.DialectPostgres, 4)
	err = exec.RunBackfill(ctx, db, testSchema, map[string]string{testSchema + ".pg_resume.raw_val": "san_val"}, nil, hookResume, nil)
	if err != nil {
		t.Fatalf("RunBackfill resume failed: %v", err)
	}

	var nullsResume int
	_ = db.QueryRowContext(ctx, fmt.Sprintf(`SELECT COUNT(*) FROM %s.pg_resume WHERE san_val IS NULL`, testSchema)).Scan(&nullsResume)
	if nullsResume != 0 {
		t.Fatalf("expected 0 NULL rows in pg_resume after resume backfill, got %d", nullsResume)
	}
}

func TestPostgres_CompositePK_IndexVerification(t *testing.T) {
	db := testutil.TestDatabase(t)
	ctx := t.Context()

	const testSchema = "backfill_comp_idx_test"
	_, err := db.ExecContext(ctx, fmt.Sprintf(`
		DROP SCHEMA IF EXISTS %s CASCADE;
		CREATE SCHEMA %s;
	`, testSchema, testSchema))
	if err != nil {
		t.Fatalf("setup test schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), fmt.Sprintf(`DROP SCHEMA IF EXISTS %s CASCADE`, testSchema))
	})

	_, err = db.ExecContext(ctx, fmt.Sprintf(`
		CREATE TABLE %s.pg_no_pk (val TEXT);
		CREATE TABLE %s.pg_good_pk (a INT, b TEXT, PRIMARY KEY (a, b));
	`, testSchema, testSchema))
	if err != nil {
		t.Fatalf("setup tables: %v", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback()

	// 1. Table with no PK -> fails
	_, err = exec.LookupTablePK(ctx, tx, exec.DialectPostgres, testSchema, "pg_no_pk")
	if err == nil {
		t.Fatal("expected error for table without PK, got nil")
	}
	if !strings.Contains(err.Error(), "requires a primary key") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. Table with composite PK -> succeeds and returns columns in order
	pkCols, err := exec.LookupTablePK(ctx, tx, exec.DialectPostgres, testSchema, "pg_good_pk")
	if err != nil {
		t.Fatalf("expected success for pg_good_pk, got: %v", err)
	}
	if len(pkCols) != 2 || pkCols[0] != "a" || pkCols[1] != "b" {
		t.Fatalf("unexpected PK columns: %v", pkCols)
	}
}
