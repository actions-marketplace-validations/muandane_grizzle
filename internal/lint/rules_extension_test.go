package lint_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/lint"
	"github.com/muandane/grizzle/internal/schema"
)

func TestUnversionedExtension_L011(t *testing.T) {
	rule := lint.UnversionedExtension{}
	if rule.ID() != "L011" {
		t.Fatalf("expected ID L011, got %s", rule.ID())
	}
	if rule.Description() == "" {
		t.Fatal("expected non-empty description")
	}

	tests := []struct {
		name         string
		schema       *schema.Schema
		wantDiags    int
		wantMessages []string
	}{
		{
			name: "unversioned extension in SourceSQL flags info",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext;",
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "unversioned extension with schema in SourceSQL flags info",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public;",
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "versioned extension in SourceSQL passes",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext VERSION '1.4';",
			},
			wantDiags: 0,
		},
		{
			name: "versioned extension with schema passes",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION IF NOT EXISTS citext WITH SCHEMA public VERSION '1.4';",
			},
			wantDiags: 0,
		},
		{
			name: "versioned extension with VERSION before SCHEMA passes",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext VERSION '1.4' WITH SCHEMA public;",
			},
			wantDiags: 0,
		},
		{
			name: "quoted extension name with hyphen flags info",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: `CREATE EXTENSION "uuid-ossp";`,
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "uuid-ossp" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "quoted extension name with version passes",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: `CREATE EXTENSION "uuid-ossp" VERSION '1.3';`,
			},
			wantDiags: 0,
		},
		{
			name: "empty version string flags info",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext VERSION '';",
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "commented out extension is ignored",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "-- CREATE EXTENSION citext;\n/* CREATE EXTENSION pgcrypto; */\nCREATE TABLE t (id bigint);",
			},
			wantDiags: 0,
		},
		{
			name: "leading comment before CREATE EXTENSION is preserved and flagged",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "-- enable extension\nCREATE EXTENSION citext;",
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "unversioned extension in s.Extensions flags info",
			schema: &schema.Schema{
				Name: "public",
				Extensions: map[string]*schema.Extension{
					"citext": {Name: "citext"},
				},
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "versioned extension in s.Extensions passes",
			schema: &schema.Schema{
				Name: "public",
				Extensions: map[string]*schema.Extension{
					"citext": {Name: "citext", Version: "1.4"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "SourceSQL and s.Extensions deduplicated without false positives",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext VERSION '1.4';",
				Extensions: map[string]*schema.Extension{
					"citext": {Name: "citext"}, // unpopulated Version in IR should not override SourceSQL pin
				},
			},
			wantDiags: 0,
		},
		{
			name: "unversioned in both SourceSQL and s.Extensions emits single diagnostic",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION citext;",
				Extensions: map[string]*schema.Extension{
					"citext": {Name: "citext"},
				},
			},
			wantDiags: 1,
			wantMessages: []string{
				`extension "citext" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
		{
			name: "multiple extensions in SourceSQL",
			schema: &schema.Schema{
				Name:      "public",
				SourceSQL: "CREATE EXTENSION pgcrypto; CREATE EXTENSION citext VERSION '1.4'; CREATE EXTENSION \"uuid-ossp\";",
			},
			wantDiags: 2,
			wantMessages: []string{
				`extension "pgcrypto" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
				`extension "uuid-ossp" has no VERSION pin; consider pinning with VERSION '...' for reproducible environments`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := lint.Lint(tt.schema, lint.UnversionedExtension{})
			if len(diags) != tt.wantDiags {
				t.Fatalf("expected %d diagnostics, got %d: %+v", tt.wantDiags, len(diags), diags)
			}
			for i, d := range diags {
				if d.RuleID != "L011" {
					t.Errorf("diagnostic[%d] expected RuleID L011, got %s", i, d.RuleID)
				}
				if d.Severity != lint.SeverityInfo {
					t.Errorf("diagnostic[%d] expected SeverityInfo, got %s", i, d.Severity)
				}
				if i < len(tt.wantMessages) && d.Message != tt.wantMessages[i] {
					t.Errorf("diagnostic[%d] expected Message %q, got %q", i, tt.wantMessages[i], d.Message)
				}
			}
		})
	}
}
