package exec

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestBuildKeysetSQL_SingleAndComposite(t *testing.T) {
	t.Parallel()

	// 1-column Postgres
	s1pg := BuildKeysetSelectSQL(DialectPostgres, `"users"`, []string{"id"}, "new_col", false)
	if s1pg != `SELECT "id" FROM "users" WHERE "new_col" IS NULL ORDER BY "id" ASC LIMIT $1` {
		t.Fatalf("unexpected 1-col pg initial select: %s", s1pg)
	}
	s1pgCursor := BuildKeysetSelectSQL(DialectPostgres, `"users"`, []string{"id"}, "new_col", true)
	if s1pgCursor != `SELECT "id" FROM "users" WHERE "new_col" IS NULL AND "id" > $1 ORDER BY "id" ASC LIMIT $2` {
		t.Fatalf("unexpected 1-col pg cursor select: %s", s1pgCursor)
	}

	// 2-column Postgres (row-value compare)
	s2pg := BuildKeysetSelectSQL(DialectPostgres, `"users"`, []string{"tenant_id", "user_id"}, "new_col", true)
	if !strings.Contains(s2pg, `("tenant_id", "user_id") > ($1, $2)`) {
		t.Fatalf("missing row-value compare in 2-col pg select: %s", s2pg)
	}
	if !strings.Contains(s2pg, `ORDER BY "tenant_id" ASC, "user_id" ASC`) {
		t.Fatalf("missing order by in 2-col pg select: %s", s2pg)
	}
	if !strings.Contains(s2pg, `LIMIT $3`) {
		t.Fatalf("missing limit $3 in 2-col pg select: %s", s2pg)
	}

	// 3-column Postgres (row-value compare)
	s3pg := BuildKeysetSelectSQL(DialectPostgres, `"items"`, []string{"org_id", "dept_id", "item_id"}, "new_col", true)
	if !strings.Contains(s3pg, `("org_id", "dept_id", "item_id") > ($1, $2, $3)`) {
		t.Fatalf("missing row-value compare in 3-col pg select: %s", s3pg)
	}
	if !strings.Contains(s3pg, `LIMIT $4`) {
		t.Fatalf("missing limit $4 in 3-col pg select: %s", s3pg)
	}

	// 2-column SQLite
	s2sq := BuildKeysetSelectSQL(DialectSQLite, `"users"`, []string{"tenant_id", "user_id"}, "new_col", true)
	if !strings.Contains(s2sq, `("tenant_id", "user_id") > (?, ?)`) {
		t.Fatalf("missing row-value compare in 2-col sqlite select: %s", s2sq)
	}
	if !strings.Contains(s2sq, `LIMIT ?`) {
		t.Fatalf("missing limit ? in 2-col sqlite select: %s", s2sq)
	}

	// Update statements
	u1pg := BuildKeysetUpdateSQL(DialectPostgres, `"users"`, []string{"id"}, "old_col", "new_col", 3)
	if u1pg != `UPDATE "users" SET "new_col" = "old_col" WHERE "id" IN ($1, $2, $3)` {
		t.Fatalf("unexpected 1-col pg update: %s", u1pg)
	}

	u2pg := BuildKeysetUpdateSQL(DialectPostgres, `"users"`, []string{"tenant_id", "user_id"}, "old_col", "new_col", 2)
	if u2pg != `UPDATE "users" SET "new_col" = "old_col" WHERE ("tenant_id", "user_id") IN (($1, $2), ($3, $4))` {
		t.Fatalf("unexpected 2-col pg update: %s", u2pg)
	}

	u3sq := BuildKeysetUpdateSQL(DialectSQLite, `"items"`, []string{"org_id", "dept_id", "item_id"}, "old_col", "new_col", 2)
	if u3sq != `UPDATE "items" SET "new_col" = "old_col" WHERE ("org_id", "dept_id", "item_id") IN ((?, ?, ?), (?, ?, ?))` {
		t.Fatalf("unexpected 3-col sqlite update: %s", u3sq)
	}
}

func TestCompositePK_TwoColumnKey_KeysetBackfill(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", "file:comp2_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := t.Context()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE tenant_users (
			tenant_id INT NOT NULL,
			user_id INT NOT NULL,
			old_email TEXT NOT NULL,
			new_email TEXT,
			PRIMARY KEY (tenant_id, user_id)
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert 25 rows across 3 tenants
	for tid := 1; tid <= 3; tid++ {
		for uid := 1; uid <= 8; uid++ {
			email := fmt.Sprintf("user_%d_%d@example.com", tid, uid)
			_, err := db.ExecContext(ctx, `INSERT INTO tenant_users VALUES (?, ?, ?, NULL)`, tid, uid, email)
			if err != nil {
				t.Fatalf("insert: %v", err)
			}
		}
	}
	// Extra row for total 25
	_, _ = db.ExecContext(ctx, `INSERT INTO tenant_users VALUES (4, 1, 'user_4_1@example.com', NULL)`)

	hook := NewCopyBackfillHook(DialectSQLite, 5)

	err = RunBackfill(ctx, db, "", map[string]string{"tenant_users.old_email": "new_email"}, nil, hook, nil)
	if err != nil {
		t.Fatalf("RunBackfill failed: %v", err)
	}

	// Verify all 25 rows backfilled
	var nullCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_users WHERE new_email IS NULL`).Scan(&nullCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if nullCount != 0 {
		t.Fatalf("expected 0 NULL rows, got %d", nullCount)
	}

	var mismatchCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tenant_users WHERE old_email != new_email`).Scan(&mismatchCount); err != nil {
		t.Fatalf("count mismatches: %v", err)
	}
	if mismatchCount != 0 {
		t.Fatalf("expected 0 mismatched rows, got %d", mismatchCount)
	}
}

func TestCompositePK_ThreeColumnKey_KeysetBackfill(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", "file:comp3_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := t.Context()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE inventory (
			org_id INT NOT NULL,
			warehouse_id INT NOT NULL,
			sku_id INT NOT NULL,
			legacy_code TEXT NOT NULL,
			new_code TEXT,
			PRIMARY KEY (org_id, warehouse_id, sku_id)
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert 30 rows
	var totalRows int
	for o := 1; o <= 2; o++ {
		for w := 1; w <= 3; w++ {
			for s := 1; s <= 5; s++ {
				code := fmt.Sprintf("CODE-%d-%d-%d", o, w, s)
				_, err := db.ExecContext(ctx, `INSERT INTO inventory VALUES (?, ?, ?, ?, NULL)`, o, w, s, code)
				if err != nil {
					t.Fatalf("insert: %v", err)
				}
				totalRows++
			}
		}
	}
	if totalRows != 30 {
		t.Fatalf("expected 30 test rows, got %d", totalRows)
	}

	// Batch size 7 (not evenly divisible by 30)
	hook := NewCopyBackfillHook(DialectSQLite, 7)

	err = RunBackfill(ctx, db, "", map[string]string{"inventory.legacy_code": "new_code"}, nil, hook, nil)
	if err != nil {
		t.Fatalf("RunBackfill failed: %v", err)
	}

	var nullCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM inventory WHERE new_code IS NULL`).Scan(&nullCount); err != nil {
		t.Fatalf("count: %v", err)
	}
	if nullCount != 0 {
		t.Fatalf("expected 0 NULL rows, got %d", nullCount)
	}
}

func TestCompositePK_ResumeMidRun(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", "file:resume_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := t.Context()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE logs (
			app_id INT NOT NULL,
			log_id INT NOT NULL,
			raw_msg TEXT NOT NULL,
			sanitized_msg TEXT,
			PRIMARY KEY (app_id, log_id)
		);
	`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	// Insert 20 rows
	for i := 1; i <= 20; i++ {
		msg := fmt.Sprintf("log_%d", i)
		var sanitized any
		if i <= 8 {
			// Pre-populate first 8 rows (mid-run resume scenario)
			sanitized = msg
		}
		_, err := db.ExecContext(ctx, `INSERT INTO logs VALUES (1, ?, ?, ?)`, i, msg, sanitized)
		if err != nil {
			t.Fatalf("insert: %v", err)
		}
	}

	// Verify 12 rows are initially NULL
	var initialNulls int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM logs WHERE sanitized_msg IS NULL`).Scan(&initialNulls)
	if initialNulls != 12 {
		t.Fatalf("expected 12 NULL rows initially, got %d", initialNulls)
	}

	// Run backfill with batch size 5
	hook := NewCopyBackfillHook(DialectSQLite, 5)
	err = RunBackfill(ctx, db, "", map[string]string{"logs.raw_msg": "sanitized_msg"}, nil, hook, nil)
	if err != nil {
		t.Fatalf("RunBackfill failed: %v", err)
	}

	// Verify all rows now backfilled
	var finalNulls int
	_ = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM logs WHERE sanitized_msg IS NULL`).Scan(&finalNulls)
	if finalNulls != 0 {
		t.Fatalf("expected 0 NULL rows after resume backfill, got %d", finalNulls)
	}
}

func TestCompositePK_IndexVerification_Failures(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", "file:idx_verify_test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer func() { _ = db.Close() }()

	ctx := t.Context()
	_, _ = db.ExecContext(ctx, `
		CREATE TABLE no_pk_tbl (val TEXT);
		CREATE TABLE good_pk_tbl (a INT, b TEXT, PRIMARY KEY (a, b));
	`)

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	// 1. Table with no PK -> fails
	_, err = LookupTablePK(ctx, tx, DialectSQLite, "", "no_pk_tbl")
	if err == nil {
		t.Fatal("expected error for table without PK, got nil")
	}
	if !strings.Contains(err.Error(), "requires a primary key") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 2. Table with composite PK -> succeeds and returns columns in order
	pkCols, err := LookupTablePK(ctx, tx, DialectSQLite, "", "good_pk_tbl")
	if err != nil {
		t.Fatalf("expected success for good_pk_tbl, got: %v", err)
	}
	if len(pkCols) != 2 || pkCols[0] != "a" || pkCols[1] != "b" {
		t.Fatalf("unexpected PK columns: %v", pkCols)
	}
}
