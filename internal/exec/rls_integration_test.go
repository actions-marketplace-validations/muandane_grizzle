//go:build integration

package exec_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/muandane/grizzle/internal/exec"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/scope"
	"github.com/muandane/grizzle/internal/testutil"
)

// TestRLS_PolicyLifecycle verifies greenfield RLS+policy sync, expression
// edit replace, and second-sync no-op against real PostgreSQL.
func TestRLS_PolicyLifecycle(t *testing.T) {
	db := testutil.TestDatabase(t)
	ctx := context.Background()

	schema := fmt.Sprintf("test_rls_%d", time.Now().UnixNano())
	_, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s;", schema))
	if err != nil {
		t.Fatalf("failed creating test schema: %v", err)
	}
	defer func() { _, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE;", schema)) }()

	policy := func(using string) string {
		return fmt.Sprintf(`
			CREATE TABLE docs (
				id BIGINT PRIMARY KEY,
				owner_id BIGINT NOT NULL
			);
			ALTER TABLE docs ENABLE ROW LEVEL SECURITY;
			CREATE POLICY docs_select_own ON docs FOR SELECT TO app_role USING (%s);
		`, using)
	}

	newCfg := func(sql string) exec.PostgresExecConfig {
		return exec.PostgresExecConfig{
			TargetSchema:     schema,
			SchemaSQL:        sql,
			Filters:          scope.Filters{},
			Policy:           plan.DropPolicy{},
			LockTimeout:      5 * time.Second,
			StatementTimeout: 30 * time.Second,
		}
	}

	// Create the role if missing (idempotent on the shared test DB).
	_, _ = db.Exec("DO $$ BEGIN IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'app_role') THEN CREATE ROLE app_role; END IF; END $$;")

	// 1. Greenfield sync: table + RLS + policy.
	if err := exec.SyncPostgres(ctx, db, newCfg(policy("owner_id = 1"))); err != nil {
		t.Fatalf("greenfield sync: %v", err)
	}
	var polCount int
	if err := db.QueryRow(fmt.Sprintf(
		`SELECT count(*) FROM pg_policy p JOIN pg_class c ON c.oid = p.polrelid JOIN pg_namespace n ON n.oid = c.relnamespace WHERE n.nspname = '%s' AND p.polname = 'docs_select_own';`, schema,
	)).Scan(&polCount); err != nil {
		t.Fatalf("query pg_policy: %v", err)
	}
	if polCount != 1 {
		t.Fatalf("expected policy docs_select_own, count = %d", polCount)
	}

	// 2. Second sync: no-op.
	p, err := exec.PlanDiffPostgres(ctx, db, newCfg(policy("owner_id = 1")))
	if err != nil {
		t.Fatalf("second plan: %v", err)
	}
	if len(p.Steps) != 0 {
		t.Fatalf("second sync must be a no-op, got %+v", p.Steps)
	}

	// 3. Edit policy expression: ALTER POLICY (non-destructive).
	p2, err := exec.PlanDiffPostgres(ctx, db, newCfg(policy("owner_id = 2")))
	if err != nil {
		t.Fatalf("edit plan: %v", err)
	}
	var sawAlter bool
	for _, s := range p2.Steps {
		if s.Type == plan.ChangeAlterPolicy {
			sawAlter = true
			if s.Destructive {
				t.Errorf("ALTER_POLICY step must not be destructive")
			}
		}
	}
	if !sawAlter {
		t.Fatalf("expected ALTER_POLICY, got %+v", p2.Steps)
	}

	// 4. Apply the edit: non-destructive ALTER_POLICY succeeds with default policy (AllowPolicy=false).
	if err := exec.SyncPostgres(ctx, db, newCfg(policy("owner_id = 2"))); err != nil {
		t.Fatalf("alter sync: %v", err)
	}
	p3, err := exec.PlanDiffPostgres(ctx, db, newCfg(policy("owner_id = 2")))
	if err != nil {
		t.Fatalf("post-alter plan: %v", err)
	}
	if len(p3.Steps) != 0 {
		t.Fatalf("post-alter sync must be a no-op, got %+v", p3.Steps)
	}

	// 5. Change policy command (SELECT -> ALL): requires DROP + CREATE and triggers destructive gate.
	cmdPolicy := func(cmd string) string {
		return fmt.Sprintf(`
			CREATE TABLE docs (
				id BIGINT PRIMARY KEY,
				owner_id BIGINT NOT NULL
			);
			ALTER TABLE docs ENABLE ROW LEVEL SECURITY;
			CREATE POLICY docs_select_own ON docs FOR %s TO app_role USING (owner_id = 2);
		`, cmd)
	}
	if err := exec.SyncPostgres(ctx, db, newCfg(cmdPolicy("ALL"))); err == nil {
		t.Fatalf("command change sync must fail with default policy (AllowPolicy=false)")
	} else {
		var dve *plan.DestructiveViolationError
		if !errors.As(err, &dve) {
			t.Fatalf("expected destructive violation, got: %v", err)
		}
	}
	replaceCfg := newCfg(cmdPolicy("ALL"))
	replaceCfg.Policy = plan.DropPolicy{AllowPolicy: true}
	replaceCfg.AcceptHazards = []plan.HazardCode{plan.HazardDropPolicy}
	if err := exec.SyncPostgres(ctx, db, replaceCfg); err != nil {
		t.Fatalf("replace sync: %v", err)
	}
	p4, err := exec.PlanDiffPostgres(ctx, db, newCfg(cmdPolicy("ALL")))
	if err != nil {
		t.Fatalf("post-replace plan: %v", err)
	}
	if len(p4.Steps) != 0 {
		t.Fatalf("post-replace sync must be a no-op, got %+v", p4.Steps)
	}
}

// TestRLS_DisableProducesStep verifies RLS disable when desired drops the flag.
func TestRLS_DisableProducesStep(t *testing.T) {
	db := testutil.TestDatabase(t)
	ctx := context.Background()

	schema := fmt.Sprintf("test_rls2_%d", time.Now().UnixNano())
	_, err := db.Exec(fmt.Sprintf("CREATE SCHEMA %s;", schema))
	if err != nil {
		t.Fatalf("failed creating test schema: %v", err)
	}
	defer func() { _, _ = db.Exec(fmt.Sprintf("DROP SCHEMA %s CASCADE;", schema)) }()

	base := exec.PostgresExecConfig{
		TargetSchema: schema,
		Filters:      scope.Filters{},
		Policy:       plan.DropPolicy{AllowPolicy: true},
	}
	withRLS := `
		CREATE TABLE docs (id BIGINT PRIMARY KEY);
		ALTER TABLE docs ENABLE ROW LEVEL SECURITY;
		CREATE POLICY docs_all ON docs USING (true);
	`
	if err := exec.SyncPostgres(ctx, db, func() exec.PostgresExecConfig {
		c := base
		c.SchemaSQL = withRLS
		return c
	}()); err != nil {
		t.Fatalf("sync with RLS: %v", err)
	}

	p, err := exec.PlanDiffPostgres(ctx, db, func() exec.PostgresExecConfig {
		c := base
		c.SchemaSQL = `CREATE TABLE docs (id BIGINT PRIMARY KEY);`
		return c
	}())
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var sawDisable, sawDrop bool
	for _, s := range p.Steps {
		switch s.Type {
		case plan.ChangeDisableRLS:
			sawDisable = true
		case plan.ChangeDropPolicy:
			sawDrop = true
		}
	}
	if !sawDisable || !sawDrop {
		t.Fatalf("expected DISABLE_RLS + DROP_POLICY, got %+v", p.Steps)
	}
}
