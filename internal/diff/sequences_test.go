package diff_test

import (
	"math"
	"testing"

	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

func TestDiff_Sequences_CreateAlterDrop(t *testing.T) {
	live := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		Sequences: map[string]*schema.Sequence{
			"stable_seq": {
				Schema:    "public",
				Name:      "stable_seq",
				DataType:  "bigint",
				Start:     1,
				Increment: 1,
				MinValue:  new(int64(1)),
				MaxValue:  new(int64(math.MaxInt64)),
				Cache:     1,
				Cycle:     false,
			},
			"param_drift_seq": {
				Schema:    "public",
				Name:      "param_drift_seq",
				DataType:  "bigint",
				Start:     1,
				Increment: 1,
				MinValue:  new(int64(1)),
				MaxValue:  new(int64(math.MaxInt64)),
				Cache:     1,
				Cycle:     false,
			},
			"ownership_drift_seq": {
				Schema:        "public",
				Name:          "ownership_drift_seq",
				DataType:      "bigint",
				Start:         1,
				Increment:     1,
				MinValue:      new(int64(1)),
				MaxValue:      new(int64(math.MaxInt64)),
				Cache:         1,
				Cycle:         false,
				OwnedByTable:  "orders",
				OwnedByColumn: "legacy_id",
			},
			"dropped_seq": {
				Schema:    "public",
				Name:      "dropped_seq",
				DataType:  "bigint",
				Start:     1,
				Increment: 1,
				MinValue:  new(int64(1)),
				MaxValue:  new(int64(math.MaxInt64)),
				Cache:     1,
				Cycle:     false,
			},
			"unmanaged_seq": {
				Schema:    "public",
				Name:      "unmanaged_seq",
				DataType:  "bigint",
				Start:     1,
				Increment: 1,
				MinValue:  new(int64(1)),
				MaxValue:  new(int64(math.MaxInt64)),
				Cache:     1,
				Cycle:     false,
			},
		},
		Unmanaged: map[string]*schema.UnmanagedObject{
			"sequence:unmanaged_seq": {
				Name: "unmanaged_seq",
				Kind: schema.UnmanagedSequence,
			},
		},
	}

	desired := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		Sequences: map[string]*schema.Sequence{
			"new_standalone_seq": {
				Schema:    "public",
				Name:      "new_standalone_seq",
				DataType:  "integer",
				Increment: 5,
				Cache:     10,
				Cycle:     true,
			},
			"new_owned_seq": {
				Schema:        "public",
				Name:          "new_owned_seq",
				DataType:      "bigint",
				OwnedByTable:  "users",
				OwnedByColumn: "id",
			},
			"stable_seq": {
				// Implicit defaults omitted in desired schema
				Schema: "public",
				Name:   "stable_seq",
			},
			"param_drift_seq": {
				Schema:    "public",
				Name:      "param_drift_seq",
				Increment: 2,
				Cache:     5,
			},
			"ownership_drift_seq": {
				Schema:        "public",
				Name:          "ownership_drift_seq",
				OwnedByTable:  "orders",
				OwnedByColumn: "new_id",
			},
		},
	}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	var foundCreateStandalone, foundCreateOwned, foundAlterOwnedCreation bool
	var foundAlterParamDrift, foundAlterOwnershipDrift, foundDropSeq, foundDropUnmanaged bool

	for _, c := range changes {
		switch c.Type {
		case plan.ChangeCreateSequence:
			if c.Table == "new_standalone_seq" {
				foundCreateStandalone = true
				if c.Destructive {
					t.Errorf("ChangeCreateSequence must not be destructive")
				}
			}
			if c.Table == "new_owned_seq" {
				foundCreateOwned = true
			}
		case plan.ChangeAlterSequence:
			if c.Table == "new_owned_seq" {
				foundAlterOwnedCreation = true
			}
			if c.Table == "param_drift_seq" {
				foundAlterParamDrift = true
			}
			if c.Table == "ownership_drift_seq" {
				foundAlterOwnershipDrift = true
			}
		case plan.ChangeDropSequence:
			if c.Table == "dropped_seq" {
				foundDropSeq = true
				if !c.Destructive {
					t.Errorf("ChangeDropSequence must be destructive")
				}
			}
			if c.Table == "unmanaged_seq" {
				foundDropUnmanaged = true
			}
		}
	}

	if !foundCreateStandalone {
		t.Errorf("expected ChangeCreateSequence for new_standalone_seq")
	}
	if !foundCreateOwned {
		t.Errorf("expected ChangeCreateSequence for new_owned_seq")
	}
	if !foundAlterOwnedCreation {
		t.Errorf("expected ChangeAlterSequence (OWNED BY) for new_owned_seq")
	}
	if !foundAlterParamDrift {
		t.Errorf("expected ChangeAlterSequence for param_drift_seq")
	}
	if !foundAlterOwnershipDrift {
		t.Errorf("expected ChangeAlterSequence for ownership_drift_seq")
	}
	if !foundDropSeq {
		t.Errorf("expected ChangeDropSequence for dropped_seq")
	}
	if foundDropUnmanaged {
		t.Errorf("unmanaged_seq must NEVER be dropped (protected invariant violation)")
	}
}

func TestDiff_Sequences_ImplicitSerialSkipped(t *testing.T) {
	desired := &schema.Schema{
		Name: "public",
		Tables: map[string]*schema.Table{
			"users": {
				Name: "users",
				Columns: map[string]*schema.Column{
					"id": {
						Name:     "id",
						DataType: "serial",
					},
				},
			},
		},
		Sequences: map[string]*schema.Sequence{
			"users_id_seq": {
				Schema:        "public",
				Name:          "users_id_seq",
				DataType:      "integer",
				OwnedByTable:  "users",
				OwnedByColumn: "id",
			},
		},
	}

	changes, err := diff.Diff(nil, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	for _, c := range changes {
		if c.Type == plan.ChangeCreateSequence && c.Table == "users_id_seq" {
			t.Errorf("implicit serial sequence users_id_seq should not have ChangeCreateSequence (handled by CREATE TABLE)")
		}
	}
}

func TestDiff_Sequences_DefaultNormalization_NoFalsePositives(t *testing.T) {
	live := &schema.Schema{
		Name: "public",
		Sequences: map[string]*schema.Sequence{
			"orders_seq": {
				Schema:    "public",
				Name:      "orders_seq",
				DataType:  "bigint",
				Start:     1,
				Increment: 1,
				MinValue:  new(int64(1)),
				MaxValue:  new(int64(math.MaxInt64)),
				Cache:     1,
				Cycle:     false,
			},
		},
	}

	desired := &schema.Schema{
		Name: "public",
		Sequences: map[string]*schema.Sequence{
			"orders_seq": {
				// User only declared name; defaults must normalize to match live
				Schema: "public",
				Name:   "orders_seq",
			},
		},
	}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	for _, c := range changes {
		if c.Table == "orders_seq" {
			t.Errorf("unexpected change for orders_seq with default normalization: %+v", c)
		}
	}
}

func TestPlan_Sequence_SafetyAndToposort(t *testing.T) {
	steps := []plan.Step{
		{Type: plan.ChangeDropTable, Table: "old_table", Destructive: true},
		{Type: plan.ChangeDropSequence, Table: "old_seq", Destructive: true},
		{Type: plan.ChangeCreateTable, Table: "new_table"},
		{Type: plan.ChangeCreateSequence, Table: "new_seq"},
		{Type: plan.ChangeAlterSequence, Table: "new_seq"},
	}

	// 1. Hazard verification
	h := (&plan.Plan{Steps: steps}).Hazards()
	var foundDropHazard bool
	for _, hazard := range h {
		if hazard.Code == plan.HazardDropSequence {
			foundDropHazard = true
			if hazard.Level != plan.HazardLevelCritical {
				t.Errorf("HazardDropSequence must be CRITICAL, got %v", hazard.Level)
			}
		}
	}
	if !foundDropHazard {
		t.Errorf("expected HazardDropSequence for ChangeDropSequence")
	}

	// 2. Drop policy verification
	p := plan.DropPolicy{AllowDropSequence: false}
	if p.IsAllowed(steps[1]) {
		t.Errorf("ChangeDropSequence should be blocked when AllowDropSequence is false")
	}
	p.AllowDropSequence = true
	if !p.IsAllowed(steps[1]) {
		t.Errorf("ChangeDropSequence should be allowed when AllowDropSequence is true")
	}

	// 3. Topological ordering verification
	plan.SortSteps(steps)

	indices := make(map[plan.ChangeType]int)
	for i, s := range steps {
		indices[s.Type] = i
	}

	// Sequences created before tables:
	if indices[plan.ChangeCreateSequence] >= indices[plan.ChangeCreateTable] {
		t.Errorf("ChangeCreateSequence (%d) must precede ChangeCreateTable (%d)",
			indices[plan.ChangeCreateSequence], indices[plan.ChangeCreateTable])
	}
	// Sequence ownership altered after tables exist:
	if indices[plan.ChangeAlterSequence] <= indices[plan.ChangeCreateTable] {
		t.Errorf("ChangeAlterSequence (%d) must follow ChangeCreateTable (%d)",
			indices[plan.ChangeAlterSequence], indices[plan.ChangeCreateTable])
	}
	// Sequences dropped after tables are dropped:
	if indices[plan.ChangeDropSequence] <= indices[plan.ChangeDropTable] {
		t.Errorf("ChangeDropSequence (%d) must follow ChangeDropTable (%d)",
			indices[plan.ChangeDropSequence], indices[plan.ChangeDropTable])
	}
}
