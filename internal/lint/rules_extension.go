package lint

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/muandane/grizzle/internal/schema"
)

var (
	// createExtensionStmtRe matches CREATE EXTENSION [IF NOT EXISTS] name ...
	// Captures: (1) quoted name, (2) unquoted name, (3) remainder of statement.
	createExtensionStmtRe = regexp.MustCompile(
		`(?is)^\s*CREATE\s+EXTENSION\s+(?:IF\s+NOT\s+EXISTS\s+)?(?:"([^"]+)"|([a-zA-Z0-9_$-]+))(.*)$`)

	// versionPinRe matches an explicit VERSION clause within a CREATE EXTENSION statement.
	versionPinRe = regexp.MustCompile(`(?is)\bVERSION\s+(?:'([^']*)'|"([^"]*)"|([^\s;]+))`)
)

// UnversionedExtension (L011) reports CREATE EXTENSION statements in SchemaSQL
// (s.SourceSQL) or s.Extensions that lack an explicit VERSION pin.
type UnversionedExtension struct{}

// ID returns the rule identifier.
func (UnversionedExtension) ID() string { return "L011" }

// Description returns a human-readable rule summary.
func (UnversionedExtension) Description() string {
	return "CREATE EXTENSION statements should pin an explicit VERSION for reproducible environments"
}

// Check returns an info diagnostic for each extension lacking an explicit VERSION pin.
func (UnversionedExtension) Check(s *schema.Schema) []Diagnostic {
	if s == nil {
		return nil
	}
	var diags []Diagnostic
	evaluated := make(map[string]bool)

	// 1. Inspect SourceSQL if present.
	if s.SourceSQL != "" {
		for _, rawStmt := range schema.SplitStatements(s.SourceSQL) {
			stmt := stripLeadingSQLComments(rawStmt)
			m := createExtensionStmtRe.FindStringSubmatch(stmt)
			if m == nil {
				continue
			}
			name := m[1]
			if name == "" {
				name = m[2]
			}
			remainder := m[3]
			key := strings.ToLower(name)
			if evaluated[key] {
				continue
			}
			evaluated[key] = true

			vm := versionPinRe.FindStringSubmatch(remainder)
			hasVersion := false
			if vm != nil {
				ver := vm[1]
				if ver == "" {
					ver = vm[2]
				}
				if ver == "" {
					ver = vm[3]
				}
				if strings.TrimSpace(ver) != "" {
					hasVersion = true
				}
			}

			if !hasVersion {
				diags = append(diags, Diagnostic{
					RuleID:   "L011",
					Severity: SeverityInfo,
					Message:  fmt.Sprintf("extension %q has no VERSION pin; consider pinning with VERSION '...' for reproducible environments", name),
				})
			}
		}
	}

	// 2. Inspect s.Extensions for any extensions not already evaluated from SourceSQL.
	if len(s.Extensions) > 0 {
		for _, key := range slices.Sorted(maps.Keys(s.Extensions)) {
			ext := s.Extensions[key]
			if ext == nil {
				continue
			}
			extName := ext.Name
			if extName == "" {
				extName = key
			}
			lowerKey := strings.ToLower(extName)
			if evaluated[lowerKey] {
				continue
			}
			evaluated[lowerKey] = true

			if strings.TrimSpace(ext.Version) == "" {
				diags = append(diags, Diagnostic{
					RuleID:   "L011",
					Severity: SeverityInfo,
					Message:  fmt.Sprintf("extension %q has no VERSION pin; consider pinning with VERSION '...' for reproducible environments", extName),
				})
			}
		}
	}

	return diags
}

// stripLeadingSQLComments removes leading whitespace, single-line (--), and
// multi-line (/* ... */) comments before the first SQL statement token.
func stripLeadingSQLComments(s string) string {
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "--"):
			if newline := strings.IndexByte(s, '\n'); newline >= 0 {
				s = s[newline+1:]
				continue
			}
			return ""
		case strings.HasPrefix(s, "/*"):
			if end := strings.Index(s[2:], "*/"); end >= 0 {
				s = s[end+4:]
				continue
			}
			return s
		default:
			return s
		}
	}
}
