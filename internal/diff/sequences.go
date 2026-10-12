package diff

import (
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

// normalizeSequence fills standard PostgreSQL catalog defaults when desired schema omits them,
// preventing false-positive ALTER SEQUENCE diffs against live state.
func normalizeSequence(seq *schema.Sequence) *schema.Sequence {
	if seq == nil {
		return nil
	}
	norm := *seq
	if norm.DataType == "" {
		norm.DataType = "bigint"
	} else {
		norm.DataType = strings.ToLower(norm.DataType)
		switch norm.DataType {
		case "int8", "bigserial":
			norm.DataType = "bigint"
		case "int", "int4", "serial":
			norm.DataType = "integer"
		case "int2", "smallserial":
			norm.DataType = "smallint"
		}
	}
	if norm.Increment == 0 {
		norm.Increment = 1
	}
	if norm.Cache <= 0 {
		norm.Cache = 1
	}

	var defaultMin, defaultMax int64
	switch norm.DataType {
	case "smallint":
		if norm.Increment > 0 {
			defaultMin = 1
			defaultMax = math.MaxInt16
		} else {
			defaultMin = math.MinInt16
			defaultMax = -1
		}
	case "integer":
		if norm.Increment > 0 {
			defaultMin = 1
			defaultMax = math.MaxInt32
		} else {
			defaultMin = math.MinInt32
			defaultMax = -1
		}
	default: // bigint
		if norm.Increment > 0 {
			defaultMin = 1
			defaultMax = math.MaxInt64
		} else {
			defaultMin = math.MinInt64
			defaultMax = -1
		}
	}

	if norm.Start == 0 {
		if norm.Increment > 0 {
			norm.Start = defaultMin
		} else {
			norm.Start = defaultMax
		}
	}
	if norm.MinValue == nil {
		norm.MinValue = &defaultMin
	}
	if norm.MaxValue == nil {
		norm.MaxValue = &defaultMax
	}
	return &norm
}

// isImplicitSerialSequence checks if a sequence is an implicit sequence created
// automatically by PostgreSQL for a serial/bigserial/smallserial column.
func isImplicitSerialSequence(s *schema.Schema, seq *schema.Sequence) bool {
	if s == nil || seq == nil || seq.OwnedByTable == "" || seq.OwnedByColumn == "" {
		return false
	}
	tbl := s.Tables[seq.OwnedByTable]
	if tbl == nil {
		return false
	}
	col := tbl.Columns[seq.OwnedByColumn]
	if col == nil {
		return false
	}
	dt := strings.ToLower(col.DataType)
	if dt == "serial" || dt == "bigserial" || dt == "smallserial" {
		return true
	}
	if strings.HasPrefix(col.DefaultValue, "nextval(") &&
		(strings.Contains(col.DefaultValue, seq.Name) ||
			strings.Contains(col.DefaultValue, tbl.Name+"_"+col.Name+"_seq")) {
		return true
	}
	return false
}

// sequenceParamsDiffer checks if sequence parameters (increment, cache, cycle, min, max) differ.
func sequenceParamsDiffer(l, d *schema.Sequence) bool {
	if l.Increment != d.Increment {
		return true
	}
	if l.Cache != d.Cache {
		return true
	}
	if l.Cycle != d.Cycle {
		return true
	}
	if (l.MinValue == nil) != (d.MinValue == nil) {
		return true
	}
	if l.MinValue != nil && d.MinValue != nil && *l.MinValue != *d.MinValue {
		return true
	}
	if (l.MaxValue == nil) != (d.MaxValue == nil) {
		return true
	}
	if l.MaxValue != nil && d.MaxValue != nil && *l.MaxValue != *d.MaxValue {
		return true
	}
	return false
}

// sequenceOwnershipDiffers checks if sequence table/column ownership differs.
func sequenceOwnershipDiffers(l, d *schema.Sequence) bool {
	return l.OwnedByTable != d.OwnedByTable || l.OwnedByColumn != d.OwnedByColumn
}

// diffSequences compares live and desired sequences and produces pure changes.
func diffSequences(live, desired *schema.Schema, targetSchema string, _ scope.Filters) []Change {
	var changes []Change

	var dNames []string
	if desired != nil && len(desired.Sequences) > 0 {
		dNames = slices.Sorted(maps.Keys(desired.Sequences))
	}

	for _, name := range dNames {
		dSeq := desired.Sequences[name]
		var lSeq *schema.Sequence
		if live != nil && live.Sequences != nil {
			lSeq = live.Sequences[name]
		}

		if lSeq == nil {
			// New sequence declared in desired schema!
			if isImplicitSerialSequence(desired, dSeq) {
				// Implicit serial sequence: table creation (CREATE TABLE) creates it automatically.
				continue
			}

			changes = append(changes, Change{
				Type:        plan.ChangeCreateSequence,
				Schema:      targetSchema,
				Table:       name,
				Sequence:    dSeq,
				Destructive: false,
			})

			// If ownership is declared, plan ALTER SEQUENCE ... OWNED BY to run after tables exist.
			if dSeq.OwnedByTable != "" && dSeq.OwnedByColumn != "" {
				changes = append(changes, Change{
					Type:        plan.ChangeAlterSequence,
					Schema:      targetSchema,
					Table:       name,
					Sequence:    dSeq,
					Destructive: false,
				})
			}
			continue
		}

		// Sequence exists in both live and desired
		normL := normalizeSequence(lSeq)
		normD := normalizeSequence(dSeq)

		paramsDiffer := sequenceParamsDiffer(normL, normD)
		ownershipDiffers := sequenceOwnershipDiffers(lSeq, dSeq)

		if paramsDiffer || ownershipDiffers {
			changes = append(changes, Change{
				Type:        plan.ChangeAlterSequence,
				Schema:      targetSchema,
				Table:       name,
				Sequence:    dSeq,
				OldSequence: lSeq,
				Destructive: false,
			})
		}
	}

	var lNames []string
	if live != nil && len(live.Sequences) > 0 {
		lNames = slices.Sorted(maps.Keys(live.Sequences))
	}

	for _, name := range lNames {
		if desired != nil && desired.Sequences != nil && desired.Sequences[name] != nil {
			continue
		}

		lSeq := live.Sequences[name]

		// Safety invariant: unmanaged external sequences are never auto-dropped.
		if live.Unmanaged != nil && live.Unmanaged["sequence:"+name] != nil {
			continue
		}

		// Implicit serial sequence: table drop handles it.
		if isImplicitSerialSequence(live, lSeq) {
			continue
		}

		changes = append(changes, Change{
			Type:        plan.ChangeDropSequence,
			Schema:      targetSchema,
			Table:       name,
			Sequence:    lSeq,
			Destructive: true,
		})
	}

	return changes
}
