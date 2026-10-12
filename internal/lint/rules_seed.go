package lint

import (
	"regexp"
	"strings"

	"github.com/muandane/grizzle/internal/schema"
)

var (
	insertStmtRe     = regexp.MustCompile(`(?is)^\s*INSERT\b`)
	insertTableRe    = regexp.MustCompile(`(?is)^\s*INSERT\s+(?:OR\s+\w+\s+)?(?:INTO\s+)?([^\s(;,]+)`)
	onConflictRe     = regexp.MustCompile(`(?is)\bON\s+CONFLICT\b`)
	whereNotExistsRe = regexp.MustCompile(`(?is)\bWHERE\s+NOT\s+EXISTS\b`)
	orIgnoreRe       = regexp.MustCompile(`(?is)\bOR\s+IGNORE\b`)
	orReplaceRe      = regexp.MustCompile(`(?is)\bOR\s+REPLACE\b`)

	truncateStmtRe  = regexp.MustCompile(`(?is)^\s*TRUNCATE\b`)
	truncateTableRe = regexp.MustCompile(`(?is)^\s*TRUNCATE(?:\s+TABLE)?(?:\s+ONLY)?\s+([^\s,;(]+)`)

	deleteStmtRe  = regexp.MustCompile(`(?is)^\s*DELETE\b`)
	deleteTableRe = regexp.MustCompile(`(?is)^\s*DELETE(?:\s+FROM)?(?:\s+ONLY)?\s+([^\s,;(]+)`)
	whereRe       = regexp.MustCompile(`(?is)\bWHERE\b`)
)

// NonIdempotentSeed (L012) reports non-idempotent or destructive statements in SeedSQL.
// SeedSQL statements should be idempotent (use ON CONFLICT, WHERE NOT EXISTS, or avoid
// unconditional destructive DML).
type NonIdempotentSeed struct{}

// ID returns the rule identifier.
func (NonIdempotentSeed) ID() string { return "L012" }

// Description returns a human-readable rule summary.
func (NonIdempotentSeed) Description() string {
	return "SeedSQL statements should be idempotent (use ON CONFLICT, WHERE NOT EXISTS, or avoid unconditional destructive DML)"
}

// Check evaluates seed SQL idempotency against s.SeedSQL.
func (NonIdempotentSeed) Check(s *schema.Schema) []Diagnostic {
	if s == nil || s.SeedSQL == "" {
		return nil
	}
	return LintSeedSQL(s.SeedSQL)
}

// LintSeedSQL inspects SeedSQL statements and emits diagnostics for bare INSERTs,
// TRUNCATE statements, or unconditional DELETE statements.
//
//nolint:revive // LintSeedSQL is explicitly named per specification.
func LintSeedSQL(seedSQL string) []Diagnostic {
	var diags []Diagnostic
	for _, stmt := range schema.SplitStatements(seedSQL) {
		trimmed := schema.StripLeadingSQLComments(stmt)
		trimmed = strings.TrimSpace(trimmed)
		if trimmed == "" {
			continue
		}

		switch {
		case insertStmtRe.MatchString(trimmed):
			if !onConflictRe.MatchString(trimmed) &&
				!whereNotExistsRe.MatchString(trimmed) &&
				!orIgnoreRe.MatchString(trimmed) &&
				!orReplaceRe.MatchString(trimmed) {
				var tableName string
				if m := insertTableRe.FindStringSubmatch(trimmed); len(m) > 1 {
					tableName = schema.CleanIdentifier(m[1])
				}
				diags = append(diags, Diagnostic{
					RuleID:   "L012",
					Severity: SeverityWarning,
					Table:    tableName,
					Message:  "INSERT statement has no conflict or existence guard (ON CONFLICT / WHERE NOT EXISTS); re-executing SeedSQL may duplicate data or violate unique constraints",
				})
			}
		case truncateStmtRe.MatchString(trimmed):
			var tableName string
			if m := truncateTableRe.FindStringSubmatch(trimmed); len(m) > 1 {
				tableName = schema.CleanIdentifier(m[1])
			}
			diags = append(diags, Diagnostic{
				RuleID:   "L012",
				Severity: SeverityWarning,
				Table:    tableName,
				Message:  "TRUNCATE in SeedSQL wipes existing data; ensure this destructive operation is intended only for development environments",
			})
		case deleteStmtRe.MatchString(trimmed):
			if !whereRe.MatchString(trimmed) {
				var tableName string
				if m := deleteTableRe.FindStringSubmatch(trimmed); len(m) > 1 {
					tableName = schema.CleanIdentifier(m[1])
				}
				diags = append(diags, Diagnostic{
					RuleID:   "L012",
					Severity: SeverityWarning,
					Table:    tableName,
					Message:  "unconditional DELETE in SeedSQL wipes table data; consider adding a WHERE clause or guarding with ON CONFLICT",
				})
			}
		}
	}
	sortDiagnostics(diags)
	return diags
}
