package exec_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/dialect/postgres"
	"github.com/muandane/grizzle/internal/exec"
	"github.com/muandane/grizzle/internal/plan"
)

func TestStepNeedsStrongLock(t *testing.T) {
	tests := []struct {
		name string
		step plan.Step
		want bool
	}{
		{
			name: "CreateTable does not need strong lock on existing table",
			step: plan.Step{Type: plan.ChangeCreateTable, Table: "users"},
			want: false,
		},
		{
			name: "Step without Table does not need lock",
			step: plan.Step{Type: plan.ChangeCreateExtension, SQL: "CREATE EXTENSION pgcrypto;"},
			want: false,
		},
		{
			name: "AddColumn requires strong lock",
			step: plan.Step{Type: plan.ChangeAddColumn, Table: "users", Column: "bio"},
			want: true,
		},
		{
			name: "DropColumn requires strong lock",
			step: plan.Step{Type: plan.ChangeDropColumn, Table: "users", Column: "bio"},
			want: true,
		},
		{
			name: "AlterColumn requires strong lock",
			step: plan.Step{Type: plan.ChangeAlterColumn, Table: "users", Column: "bio"},
			want: true,
		},
		{
			name: "DropTable requires strong lock",
			step: plan.Step{Type: plan.ChangeDropTable, Table: "users"},
			want: true,
		},
		{
			name: "AddFK requires strong lock",
			step: plan.Step{Type: plan.ChangeAddFK, Table: "orders"},
			want: true,
		},
		{
			name: "DropFK requires strong lock",
			step: plan.Step{Type: plan.ChangeDropFK, Table: "orders"},
			want: true,
		},
		{
			name: "EnableRLS requires strong lock",
			step: plan.Step{Type: plan.ChangeEnableRLS, Table: "docs"},
			want: true,
		},
		{
			name: "DropIndex requires strong lock",
			step: plan.Step{Type: plan.ChangeDropIndex, Table: "users"},
			want: true,
		},
		{
			name: "Non-concurrent CreateIndex requires strong lock",
			step: plan.Step{Type: plan.ChangeCreateIndex, Table: "users", NonTx: false},
			want: true,
		},
		{
			name: "Concurrent CreateIndex (NonTx) does not require strong lock",
			step: plan.Step{Type: plan.ChangeCreateIndex, Table: "users", NonTx: true},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := exec.StepNeedsStrongLock(tc.step)
			if got != tc.want {
				t.Errorf("StepNeedsStrongLock(%+v) = %v, want %v", tc.step, got, tc.want)
			}
		})
	}
}

func TestCollectTablesNeedingStrongLocks(t *testing.T) {
	steps := []plan.Step{
		{Type: plan.ChangeCreateTable, Table: "new_table"},
		{Type: plan.ChangeAddColumn, Table: "users", Column: "age"},
		{Type: plan.ChangeDropColumn, Table: "users", Column: "legacy"},
		{Type: plan.ChangeAddFK, Table: "orders", Schema: "billing"},
		{Type: plan.ChangeAttachPartition, Table: "measurements_y2026", ParentTable: "measurements"},
	}

	tables := exec.CollectTablesNeedingStrongLocks("public", steps)

	want := []postgres.SchemaTable{
		{Schema: "public", Table: "users"},
		{Schema: "billing", Table: "orders"},
		{Schema: "public", Table: "measurements_y2026"},
		{Schema: "public", Table: "measurements"},
	}

	if len(tables) != len(want) {
		t.Fatalf("expected %d tables, got %d: %+v", len(want), len(tables), tables)
	}

	for i, st := range tables {
		if st != want[i] {
			t.Errorf("tables[%d] = %+v, want %+v", i, st, want[i])
		}
	}
}
