package lint

import (
	"regexp"
	"strings"

	"github.com/muandane/grizzle/internal/schema"
)

var (
	commentOnTableRe = regexp.MustCompile(
		`(?is)^\s*COMMENT\s+ON\s+TABLE\s+((?:[^\s;]+|"(?:[^"]|"")*")+)(?:[\s;]|$)`)

	commentOnColumnRe = regexp.MustCompile(
		`(?is)^\s*COMMENT\s+ON\s+COLUMN\s+((?:[^\s;]+|"(?:[^"]|"")*")+)(?:[\s;]|$)`)

	commentOnGeneralRe = regexp.MustCompile(
		`(?is)^\s*COMMENT\s+ON\s+(?:VIEW|INDEX|MATERIALIZED\s+VIEW|TYPE|DOMAIN|TRIGGER|FUNCTION|PROCEDURE|SEQUENCE)\s+((?:[^\s;]+|"(?:[^"]|"")*")+)(?:[\s;]|$)`)

	alterTableEnableRLSRe = regexp.MustCompile(
		`(?is)^\s*ALTER\s+TABLE\s+(?:ONLY\s+)?(?:IF\s+EXISTS\s+)?((?:[^\s;]+|"(?:[^"]|"")*")+)\s+ENABLE\s+ROW\s+LEVEL\s+SECURITY\b`)

	enableRLSRe = regexp.MustCompile(
		`(?is)^\s*ALTER\s+TABLE\s+.*?\bENABLE\s+ROW\s+LEVEL\s+SECURITY\b|^\s*ENABLE\s+ROW\s+LEVEL\s+SECURITY\b`)

	createPolicyRe = regexp.MustCompile(
		`(?is)^\s*CREATE\s+POLICY\s+(?:IF\s+NOT\s+EXISTS\s+)?((?:[^\s;]+|"(?:[^"]|"")*")+)\s+ON\s+((?:[^\s;]+|"(?:[^"]|"")*")+)(?:[\s;]|$)`)
)

// UnsupportedSQLiteFeature (L013) reports features declared in SchemaSQL that
// SQLite does not support: COMMENT ON and Row-Level Security (RLS). SQLite
// ignores comments (no catalog metadata) and lacks row-level security / policy catalogs.
type UnsupportedSQLiteFeature struct {
	TargetDialect string
}

// ID returns the rule identifier.
func (UnsupportedSQLiteFeature) ID() string { return "L013" }

// Description returns a human-readable rule summary.
func (UnsupportedSQLiteFeature) Description() string {
	return "SQLite does not support COMMENT ON or Row-Level Security (RLS)"
}

// Check inspects s.SourceSQL for COMMENT ON and RLS statements when target dialect is sqlite.
func (r UnsupportedSQLiteFeature) Check(s *schema.Schema) []Diagnostic {
	if s == nil || s.SourceSQL == "" {
		return nil
	}

	if r.TargetDialect != "" && r.TargetDialect != "sqlite" {
		return nil
	}
	if r.TargetDialect == "" && s.Dialect != "sqlite" {
		return nil
	}

	var diags []Diagnostic
	for _, rawStmt := range schema.SplitStatements(s.SourceSQL) {
		stmt := strings.TrimSpace(schema.StripComments(rawStmt))
		if stmt == "" {
			continue
		}

		if m := commentOnTableRe.FindStringSubmatch(stmt); m != nil {
			tbl := schema.CleanIdentifier(m[1])
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Table:    tbl,
				Message:  "SQLite does not support COMMENT ON; table comments in SchemaSQL are ignored (no catalog metadata)",
			})
			continue
		}

		if m := commentOnColumnRe.FindStringSubmatch(stmt); m != nil {
			tbl, col := parseTableAndColumn(m[1])
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Table:    tbl,
				Column:   col,
				Message:  "SQLite does not support COMMENT ON; column comments in SchemaSQL are ignored (no catalog metadata)",
			})
			continue
		}

		if m := commentOnGeneralRe.FindStringSubmatch(stmt); m != nil {
			target := schema.CleanIdentifier(m[1])
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Table:    target,
				Message:  "SQLite does not support COMMENT ON; comments in SchemaSQL are ignored (no catalog metadata)",
			})
			continue
		}

		if m := alterTableEnableRLSRe.FindStringSubmatch(stmt); m != nil {
			tbl := schema.CleanIdentifier(m[1])
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Table:    tbl,
				Message:  "SQLite does not support row-level security; ENABLE ROW LEVEL SECURITY is ignored",
			})
			continue
		}

		if enableRLSRe.MatchString(stmt) {
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Message:  "SQLite does not support row-level security; ENABLE ROW LEVEL SECURITY is ignored",
			})
			continue
		}

		if m := createPolicyRe.FindStringSubmatch(stmt); m != nil {
			policyName := schema.CleanIdentifier(m[1])
			tbl := schema.CleanIdentifier(m[2])
			diags = append(diags, Diagnostic{
				RuleID:   "L013",
				Severity: SeverityWarning,
				Table:    tbl,
				Column:   policyName,
				Message:  "SQLite does not support row-level security; CREATE POLICY is ignored",
			})
			continue
		}
	}

	return diags
}

func parseTableAndColumn(raw string) (table, column string) {
	parts := schema.ParseQualifiedIdentifier(raw)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", schema.CleanIdentifier(parts[0])
	default:
		return schema.CleanIdentifier(parts[len(parts)-2]), schema.CleanIdentifier(parts[len(parts)-1])
	}
}
