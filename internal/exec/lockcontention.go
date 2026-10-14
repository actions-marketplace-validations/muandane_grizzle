package exec

import (
	"cmp"

	"github.com/muandane/grizzle/internal/dialect/postgres"
	"github.com/muandane/grizzle/internal/plan"
)

// StepNeedsStrongLock reports whether a plan step acquires a strong lock on an existing table
// (such as AccessExclusiveLock, ShareRowExclusiveLock, or ShareLock).
func StepNeedsStrongLock(s plan.Step) bool {
	if s.Table == "" {
		return false
	}
	switch s.Type {
	case plan.ChangeCreateTable:
		return false
	case plan.ChangeDropTable,
		plan.ChangeAddColumn,
		plan.ChangeDropColumn,
		plan.ChangeAlterColumn,
		plan.ChangeRenameColumn,
		plan.ChangeAddFK,
		plan.ChangeDropFK,
		plan.ChangeAddCheck,
		plan.ChangeDropCheck,
		plan.ChangeValidateConstraint,
		plan.ChangeAttachPartition,
		plan.ChangeDetachPartition,
		plan.ChangeEnableRLS,
		plan.ChangeDisableRLS,
		plan.ChangeForceRLS,
		plan.ChangeNoForceRLS,
		plan.ChangeCreatePolicy,
		plan.ChangeAlterPolicy,
		plan.ChangeDropPolicy,
		plan.ChangeCreateTrigger,
		plan.ChangeDropTrigger,
		plan.ChangeCommentTable,
		plan.ChangeCommentColumn,
		plan.ChangeDropIndex:
		return true
	case plan.ChangeCreateIndex:
		return !s.NonTx
	default:
		return false
	}
}

// CollectTablesNeedingStrongLocks inspects steps and returns a deduplicated slice of
// SchemaTable identifiers for all existing tables that will be locked during DDL execution.
func CollectTablesNeedingStrongLocks(primarySchema string, steps []plan.Step) []postgres.SchemaTable {
	seen := make(map[postgres.SchemaTable]bool)
	var result []postgres.SchemaTable
	for _, s := range steps {
		if !StepNeedsStrongLock(s) {
			continue
		}
		schema := cmp.Or(s.Schema, primarySchema, "public")
		st := postgres.SchemaTable{Schema: schema, Table: s.Table}
		if !seen[st] {
			seen[st] = true
			result = append(result, st)
		}
		if s.ParentTable != "" {
			parentST := postgres.SchemaTable{Schema: schema, Table: s.ParentTable}
			if !seen[parentST] {
				seen[parentST] = true
				result = append(result, parentST)
			}
		}
	}
	return result
}
