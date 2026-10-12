package lint_test

import (
	"strings"
	"testing"

	"github.com/muandane/grizzle/internal/lint"
	"github.com/muandane/grizzle/internal/schema"
)

func TestUnsupportedSQLiteFeature_Metadata(t *testing.T) {
	rule := lint.UnsupportedSQLiteFeature{}
	if rule.ID() != "L013" {
		t.Fatalf("expected ID L013, got %s", rule.ID())
	}
	wantDesc := "SQLite does not support COMMENT ON or Row-Level Security (RLS)"
	if rule.Description() != wantDesc {
		t.Fatalf("expected description %q, got %q", wantDesc, rule.Description())
	}
}

func TestUnsupportedSQLiteFeature_L013(t *testing.T) {
	tests := []struct {
		name        string
		sql         string
		wantDiags   int
		wantTable   string
		wantColumn  string
		wantMessage string
	}{
		{
			name:        "comment on table",
			sql:         "COMMENT ON TABLE users IS 'user table';",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on table quoted",
			sql:         `COMMENT ON TABLE "users" IS 'user table';`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on table qualified",
			sql:         `COMMENT ON TABLE "public"."users" IS 'user table';`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on table is null",
			sql:         "COMMENT ON TABLE users IS NULL;",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on table no is clause",
			sql:         "COMMENT ON TABLE users;",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on column",
			sql:         "COMMENT ON COLUMN users.email IS 'user email';",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "email",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on column quoted",
			sql:         `COMMENT ON COLUMN "users"."email" IS 'user email';`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "email",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on column qualified",
			sql:         `COMMENT ON COLUMN "public"."users"."email" IS 'user email';`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "email",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "comment on column with quoted column name",
			sql:         `COMMENT ON COLUMN users."email" IS 'user email';`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "email",
			wantMessage: "SQLite does not support COMMENT ON",
		},
		{
			name:        "alter table enable row level security",
			sql:         "ALTER TABLE users ENABLE ROW LEVEL SECURITY;",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "alter table enable row level security quoted",
			sql:         `ALTER TABLE "users" ENABLE ROW LEVEL SECURITY;`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "alter table only enable row level security",
			sql:         "ALTER TABLE ONLY users ENABLE ROW LEVEL SECURITY;",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "alter table enable row level security qualified",
			sql:         `ALTER TABLE "public"."users" ENABLE ROW LEVEL SECURITY;`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "standalone enable row level security",
			sql:         "ENABLE ROW LEVEL SECURITY;",
			wantDiags:   1,
			wantTable:   "",
			wantColumn:  "",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "create policy",
			sql:         "CREATE POLICY user_policy ON users FOR SELECT USING (true);",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "user_policy",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "create policy quoted",
			sql:         `CREATE POLICY "user_policy" ON "users" FOR SELECT USING (true);`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "user_policy",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "create policy qualified table",
			sql:         `CREATE POLICY "user_policy" ON "public"."users" FOR SELECT USING (true);`,
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "user_policy",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:        "create policy without using clause",
			sql:         "CREATE POLICY p ON users;",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "p",
			wantMessage: "SQLite does not support row-level security",
		},
		{
			name:      "clean sqlite ddl passes",
			sql:       "CREATE TABLE users (id integer primary key, email text not null);",
			wantDiags: 0,
		},
		{
			name:      "commented out statements are ignored",
			sql:       "-- COMMENT ON TABLE users IS 'user table';\n/* ALTER TABLE users ENABLE ROW LEVEL SECURITY; */",
			wantDiags: 0,
		},
		{
			name:        "leading comment before statement is stripped",
			sql:         "-- Table note\nCOMMENT ON TABLE users IS 'user table';",
			wantDiags:   1,
			wantTable:   "users",
			wantColumn:  "",
			wantMessage: "SQLite does not support COMMENT ON",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &schema.Schema{
				Dialect:   "sqlite",
				SourceSQL: tt.sql,
			}
			diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{})
			if len(diags) != tt.wantDiags {
				t.Fatalf("expected %d diagnostics, got %d: %+v", tt.wantDiags, len(diags), diags)
			}
			if tt.wantDiags > 0 {
				d := diags[0]
				if d.RuleID != "L013" {
					t.Errorf("expected RuleID L013, got %s", d.RuleID)
				}
				if d.Severity != lint.SeverityWarning {
					t.Errorf("expected SeverityWarning, got %s", d.Severity)
				}
				if d.Table != tt.wantTable {
					t.Errorf("expected Table %q, got %q", tt.wantTable, d.Table)
				}
				if d.Column != tt.wantColumn {
					t.Errorf("expected Column %q, got %q", tt.wantColumn, d.Column)
				}
				if !strings.Contains(d.Message, tt.wantMessage) {
					t.Errorf("expected Message to contain %q, got %q", tt.wantMessage, d.Message)
				}
			}
		})
	}
}

func TestUnsupportedSQLiteFeature_DialectFiltering(t *testing.T) {
	unsupportedSQL := `
COMMENT ON TABLE users IS 'user table';
COMMENT ON COLUMN users.email IS 'user email';
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY users_policy ON users FOR SELECT USING (true);
`

	t.Run("postgres dialect produces 0 diagnostics", func(t *testing.T) {
		s := &schema.Schema{
			Dialect:   "postgres",
			SourceSQL: unsupportedSQL,
		}
		diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{})
		if len(diags) != 0 {
			t.Fatalf("expected 0 diagnostics for postgres dialect, got %d: %+v", len(diags), diags)
		}
	})

	t.Run("empty dialect produces 0 diagnostics", func(t *testing.T) {
		s := &schema.Schema{
			Dialect:   "",
			SourceSQL: unsupportedSQL,
		}
		diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{})
		if len(diags) != 0 {
			t.Fatalf("expected 0 diagnostics for empty dialect, got %d: %+v", len(diags), diags)
		}
	})

	t.Run("TargetDialect sqlite overrides non-sqlite schema dialect", func(t *testing.T) {
		s := &schema.Schema{
			Dialect:   "postgres",
			SourceSQL: unsupportedSQL,
		}
		diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{TargetDialect: "sqlite"})
		if len(diags) != 4 {
			t.Fatalf("expected 4 diagnostics when TargetDialect=sqlite, got %d: %+v", len(diags), diags)
		}
	})

	t.Run("TargetDialect postgres suppresses sqlite schema diagnostics", func(t *testing.T) {
		s := &schema.Schema{
			Dialect:   "sqlite",
			SourceSQL: unsupportedSQL,
		}
		diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{TargetDialect: "postgres"})
		if len(diags) != 0 {
			t.Fatalf("expected 0 diagnostics when TargetDialect=postgres, got %d: %+v", len(diags), diags)
		}
	})
}

func TestUnsupportedSQLiteFeature_MultipleStatements(t *testing.T) {
	sql := `
CREATE TABLE users (id integer primary key, email text);
COMMENT ON TABLE users IS 'user table';
COMMENT ON COLUMN users.email IS 'user email';
ALTER TABLE users ENABLE ROW LEVEL SECURITY;
CREATE POLICY user_policy ON users FOR SELECT USING (true);
`
	s := &schema.Schema{
		Dialect:   "sqlite",
		SourceSQL: sql,
	}

	diags := lint.Lint(s, lint.UnsupportedSQLiteFeature{})
	if len(diags) != 4 {
		t.Fatalf("expected 4 diagnostics, got %d: %+v", len(diags), diags)
	}

	for _, d := range diags {
		if d.RuleID != "L013" {
			t.Errorf("expected RuleID L013, got %s", d.RuleID)
		}
		if d.Severity != lint.SeverityWarning {
			t.Errorf("expected SeverityWarning, got %s", d.Severity)
		}
	}

	// Because Lint sorts by (RuleID, Table, Column, Message), check elements:
	foundCommentTable := false
	foundCommentCol := false
	foundAlterRLS := false
	foundCreatePolicy := false

	for _, d := range diags {
		switch {
		case d.Table == "users" && d.Column == "" && strings.Contains(d.Message, "COMMENT ON"):
			foundCommentTable = true
		case d.Table == "users" && d.Column == "email" && strings.Contains(d.Message, "COMMENT ON"):
			foundCommentCol = true
		case d.Table == "users" && d.Column == "" && strings.Contains(d.Message, "row-level security"):
			foundAlterRLS = true
		case d.Table == "users" && d.Column == "user_policy" && strings.Contains(d.Message, "row-level security"):
			foundCreatePolicy = true
		}
	}

	if !foundCommentTable {
		t.Errorf("missing COMMENT ON TABLE diagnostic")
	}
	if !foundCommentCol {
		t.Errorf("missing COMMENT ON COLUMN diagnostic")
	}
	if !foundAlterRLS {
		t.Errorf("missing ALTER TABLE ENABLE ROW LEVEL SECURITY diagnostic")
	}
	if !foundCreatePolicy {
		t.Errorf("missing CREATE POLICY diagnostic")
	}
}

func TestUnsupportedSQLiteFeature_InDefaultRules(t *testing.T) {
	sql := "COMMENT ON TABLE users IS 'test'; ALTER TABLE users ENABLE ROW LEVEL SECURITY;"

	sqliteSchema := &schema.Schema{
		Dialect:   "sqlite",
		SourceSQL: sql,
	}
	sqliteDiags := lint.Lint(sqliteSchema, lint.DefaultRules()...)
	sqliteL013Count := 0
	for _, d := range sqliteDiags {
		if d.RuleID == "L013" {
			sqliteL013Count++
		}
	}
	if sqliteL013Count != 2 {
		t.Errorf("expected 2 L013 diagnostics from DefaultRules on sqlite schema, got %d", sqliteL013Count)
	}

	pgSchema := &schema.Schema{
		Dialect:   "postgres",
		SourceSQL: sql,
	}
	pgDiags := lint.Lint(pgSchema, lint.DefaultRules()...)
	for _, d := range pgDiags {
		if d.RuleID == "L013" {
			t.Errorf("unexpected L013 diagnostic from DefaultRules on postgres schema: %+v", d)
		}
	}
}
