package lint_test

import (
	"strings"
	"testing"

	"github.com/muandane/grizzle/internal/lint"
	"github.com/muandane/grizzle/internal/schema"
)

func TestNonIdempotentSeed_Metadata(t *testing.T) {
	rule := lint.NonIdempotentSeed{}
	if rule.ID() != "L012" {
		t.Fatalf("expected ID L012, got %s", rule.ID())
	}
	if !strings.Contains(rule.Description(), "idempotent") {
		t.Fatalf("expected description to mention idempotent, got %s", rule.Description())
	}
}

func TestNonIdempotentSeed_NilOrEmpty(t *testing.T) {
	rule := lint.NonIdempotentSeed{}

	if diags := rule.Check(nil); len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for nil schema, got %d", len(diags))
	}

	s := &schema.Schema{Name: "public", SeedSQL: ""}
	if diags := rule.Check(s); len(diags) != 0 {
		t.Fatalf("expected 0 diagnostics for empty SeedSQL, got %d", len(diags))
	}
}

func TestNonIdempotentSeed_L012(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantDiags   int
		wantTable   string
		wantMessage string
	}{
		// Bare INSERTs (unsafe)
		{
			name:        "bare INSERT into table",
			sql:         "INSERT INTO users (id, name) VALUES (1, 'alice');",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},
		{
			name:        "bare INSERT lowercase",
			sql:         "insert into users values (1, 'alice');",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},
		{
			name:        "bare INSERT quoted identifier",
			sql:         `INSERT INTO "users" (id) VALUES (1);`,
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},
		{
			name:        "bare INSERT schema qualified",
			sql:         `INSERT INTO "public"."users" (id) VALUES (1);`,
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},
		{
			name:        "bare INSERT without INTO",
			sql:         "INSERT users (id) VALUES (1);",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},

		// Safe INSERT patterns
		{
			name:      "INSERT with ON CONFLICT DO NOTHING",
			sql:       "INSERT INTO users (id, name) VALUES (1, 'alice') ON CONFLICT DO NOTHING;",
			wantDiags: 0,
		},
		{
			name:      "INSERT with ON CONFLICT targeting column DO NOTHING",
			sql:       "INSERT INTO users (id, name) VALUES (1, 'alice') ON CONFLICT (id) DO NOTHING;",
			wantDiags: 0,
		},
		{
			name:      "INSERT with ON CONFLICT DO UPDATE",
			sql:       "INSERT INTO users (id, name) VALUES (1, 'alice') ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name;",
			wantDiags: 0,
		},
		{
			name:      "INSERT with multiline ON CONFLICT",
			sql:       "INSERT INTO users (id, name) VALUES (1, 'alice')\n  ON CONFLICT (id)\n  DO NOTHING;",
			wantDiags: 0,
		},
		{
			name:      "SQLite INSERT OR IGNORE",
			sql:       "INSERT OR IGNORE INTO users (id, name) VALUES (1, 'alice');",
			wantDiags: 0,
		},
		{
			name:      "SQLite INSERT OR REPLACE",
			sql:       "INSERT OR REPLACE INTO users (id, name) VALUES (1, 'alice');",
			wantDiags: 0,
		},
		{
			name:      "INSERT with WHERE NOT EXISTS",
			sql:       "INSERT INTO users (id, name) SELECT 1, 'alice' WHERE NOT EXISTS (SELECT 1 FROM users WHERE id = 1);",
			wantDiags: 0,
		},

		// TRUNCATE statements (destructive)
		{
			name:        "TRUNCATE bare table",
			sql:         "TRUNCATE users;",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "TRUNCATE in SeedSQL wipes existing data",
		},
		{
			name:        "TRUNCATE TABLE statement",
			sql:         "TRUNCATE TABLE users;",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "TRUNCATE in SeedSQL wipes existing data",
		},
		{
			name:        "TRUNCATE TABLE quoted",
			sql:         `TRUNCATE TABLE "users";`,
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "TRUNCATE in SeedSQL wipes existing data",
		},
		{
			name:        "TRUNCATE TABLE ONLY qualified",
			sql:         `TRUNCATE TABLE ONLY public."users" CASCADE;`,
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "TRUNCATE in SeedSQL wipes existing data",
		},

		// DELETE statements
		{
			name:        "unconditional DELETE FROM table",
			sql:         "DELETE FROM users;",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "unconditional DELETE in SeedSQL wipes table data",
		},
		{
			name:        "unconditional DELETE FROM quoted table",
			sql:         `DELETE FROM "users";`,
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "unconditional DELETE in SeedSQL wipes table data",
		},
		{
			name:        "unconditional DELETE without FROM",
			sql:         "DELETE users;",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "unconditional DELETE in SeedSQL wipes table data",
		},
		{
			name:      "conditional DELETE with WHERE",
			sql:       "DELETE FROM users WHERE id = 1;",
			wantDiags: 0,
		},
		{
			name:      "conditional DELETE with complex WHERE clause",
			sql:       "DELETE FROM users WHERE created_at < NOW() - INTERVAL '30 days';",
			wantDiags: 0,
		},

		// Comments & non-DML statements
		{
			name:      "commented out bare INSERT is ignored",
			sql:       "-- INSERT INTO users (id) VALUES (1);",
			wantDiags: 0,
		},
		{
			name:      "commented out TRUNCATE is ignored",
			sql:       "/* TRUNCATE TABLE users; */",
			wantDiags: 0,
		},
		{
			name:        "leading comment before bare INSERT is stripped and flagged",
			sql:         "-- Seed initial administrator\nINSERT INTO users (id) VALUES (1);",
			wantDiags:   1,
			wantTable:   "users",
			wantMessage: "INSERT statement has no conflict or existence guard",
		},
		{
			name:      "non-DML statements are not flagged",
			sql:       "SELECT 1; SET search_path TO public;",
			wantDiags: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := lint.LintSeedSQL(tt.sql)
			if len(diags) != tt.wantDiags {
				t.Fatalf("expected %d diagnostics, got %d: %+v", tt.wantDiags, len(diags), diags)
			}
			if tt.wantDiags > 0 {
				d := diags[0]
				if d.RuleID != "L012" {
					t.Errorf("expected RuleID L012, got %s", d.RuleID)
				}
				if d.Severity != lint.SeverityWarning {
					t.Errorf("expected Severity WARNING, got %s", d.Severity)
				}
				if d.Table != tt.wantTable {
					t.Errorf("expected Table %q, got %q", tt.wantTable, d.Table)
				}
				if !strings.Contains(d.Message, tt.wantMessage) {
					t.Errorf("expected message containing %q, got %q", tt.wantMessage, d.Message)
				}
			}
		})
	}
}

func TestNonIdempotentSeed_MultipleStatements(t *testing.T) {
	sql := `
		-- Idempotent insert
		INSERT INTO roles (id, name) VALUES (1, 'admin') ON CONFLICT DO NOTHING;

		-- Dangerous truncate
		TRUNCATE TABLE audit_log;

		-- Bare insert
		INSERT INTO permissions (id, code) VALUES (10, 'read:all');

		-- Unconditional delete
		DELETE FROM sessions;

		-- Safe conditional delete
		DELETE FROM temp_tokens WHERE expires_at < NOW();
	`
	diags := lint.LintSeedSQL(sql)
	if len(diags) != 3 {
		t.Fatalf("expected 3 diagnostics, got %d: %+v", len(diags), diags)
	}

	for _, d := range diags {
		if d.RuleID != "L012" {
			t.Errorf("expected L012, got %s", d.RuleID)
		}
	}
}

func TestNonIdempotentSeed_InDefaultRules(t *testing.T) {
	// Schema with non-idempotent seed SQL
	s := &schema.Schema{
		Name:    "public",
		SeedSQL: "INSERT INTO users (id) VALUES (1);",
	}

	diags := lint.Lint(s, lint.DefaultRules()...)
	var l012Diags []lint.Diagnostic
	for _, d := range diags {
		if d.RuleID == "L012" {
			l012Diags = append(l012Diags, d)
		}
	}
	if len(l012Diags) != 1 {
		t.Fatalf("expected 1 L012 diagnostic from DefaultRules, got %d", len(l012Diags))
	}
	if l012Diags[0].Table != "users" {
		t.Errorf("expected Table 'users', got %q", l012Diags[0].Table)
	}

	// Schema with clean seed SQL
	cleanSchema := &schema.Schema{
		Name:    "public",
		SeedSQL: "INSERT INTO users (id) VALUES (1) ON CONFLICT DO NOTHING;",
	}
	cleanDiags := lint.Lint(cleanSchema, lint.DefaultRules()...)
	for _, d := range cleanDiags {
		if d.RuleID == "L012" {
			t.Errorf("unexpected L012 diagnostic for clean seed: %+v", d)
		}
	}

	// Schema with empty SeedSQL
	emptySchema := &schema.Schema{Name: "public"}
	emptyDiags := lint.Lint(emptySchema, lint.DefaultRules()...)
	for _, d := range emptyDiags {
		if d.RuleID == "L012" {
			t.Errorf("unexpected L012 diagnostic for empty seed: %+v", d)
		}
	}
}
