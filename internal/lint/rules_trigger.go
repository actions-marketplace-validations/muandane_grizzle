package lint

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/muandane/grizzle/internal/schema"
)

// triggerExecRe matches the EXECUTE FUNCTION or EXECUTE PROCEDURE clause of a
// PostgreSQL trigger definition, capturing the function/procedure identifier
// before the opening argument parenthesis or statement termination.
var triggerExecRe = regexp.MustCompile(`(?is)\bEXECUTE\s+(?:FUNCTION|PROCEDURE)\s+([^(;]+?)(?:\s*\(|$)`)

// TriggerWithoutFunction (L010) reports triggers declared on a table whose
// executed function/procedure is not defined in the desired schema's routines.
type TriggerWithoutFunction struct{}

// ID returns the rule identifier.
func (TriggerWithoutFunction) ID() string { return "L010" }

// Description returns a human-readable rule summary.
func (TriggerWithoutFunction) Description() string {
	return "Trigger executed function/procedure should be defined in the desired schema routines"
}

// Check returns a warning for each trigger whose target function/procedure is
// missing from s.Routines.
func (TriggerWithoutFunction) Check(s *schema.Schema) []Diagnostic {
	var diags []Diagnostic
	for _, name := range sortedTableNames(s) {
		t := s.Tables[name]
		if t == nil {
			continue
		}
		for _, trgName := range sortedTriggerNames(t) {
			trg := t.Triggers[trgName]
			if trg == nil {
				continue
			}
			target := extractTriggerFunction(trg.Definition)
			if target == "" {
				continue
			}
			if !routineExists(s, target) {
				diags = append(diags, Diagnostic{
					RuleID:   "L010",
					Severity: SeverityWarning,
					Table:    name,
					Column:   trgName,
					Message:  fmt.Sprintf("trigger executes function %q which is not defined in the desired schema", target),
				})
			}
		}
	}
	return diags
}

// extractTriggerFunction parses the target function name from a canonical
// pg_get_triggerdef or CREATE TRIGGER definition, stripping any schema qualification
// and surrounding quotes.
func extractTriggerFunction(def string) string {
	m := triggerExecRe.FindStringSubmatch(def)
	if m == nil {
		return ""
	}
	return cleanFunctionName(m[1])
}

// cleanFunctionName extracts the bare function name from a potentially qualified
// or quoted SQL identifier (e.g. public.my_func -> my_func, "my_func" -> my_func,
// "public"."my_func" -> my_func).
func cleanFunctionName(raw string) string {
	raw = strings.TrimSpace(raw)
	parts := schema.ParseQualifiedIdentifier(raw)
	if len(parts) > 0 {
		raw = parts[len(parts)-1]
	}
	return strings.Trim(strings.TrimSpace(raw), `"`)
}

// routineExists reports whether s.Routines contains any routine whose name
// matches target (case-insensitive, ignoring quotes and schema prefix).
func routineExists(s *schema.Schema, target string) bool {
	if s == nil || len(s.Routines) == 0 {
		return false
	}
	targetClean := strings.ToLower(cleanFunctionName(target))
	for k, r := range s.Routines {
		rName := ""
		if r != nil && r.Name != "" {
			rName = r.Name
		} else {
			if before, _, ok := strings.Cut(k, "("); ok {
				rName = before
			} else {
				rName = k
			}
		}
		if strings.ToLower(cleanFunctionName(rName)) == targetClean {
			return true
		}
	}
	return false
}
