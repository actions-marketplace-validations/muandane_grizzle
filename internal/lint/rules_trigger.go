package lint

import (
	"cmp"
	"fmt"
	"strings"

	"github.com/muandane/grizzle/internal/schema"
)

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
			target := schema.ExtractTriggerFunction(trg.Definition)
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

// routineExists reports whether s.Routines contains any routine whose name
// matches target (case-insensitive, ignoring quotes and schema prefix).
func routineExists(s *schema.Schema, target string) bool {
	if s == nil || len(s.Routines) == 0 {
		return false
	}
	targetClean := schema.CleanFunctionName(target)
	for k, r := range s.Routines {
		var rName string
		if r != nil {
			rName = r.Name
		}
		before, _, _ := strings.Cut(k, "(")
		rName = cmp.Or(rName, before, k)
		if strings.EqualFold(schema.CleanFunctionName(rName), targetClean) {
			return true
		}
	}
	return false
}
