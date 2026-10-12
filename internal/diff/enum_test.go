package diff_test

import (
	"slices"
	"testing"

	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

func TestDiff_Enum_RenameAndDrop(t *testing.T) {
	live := &schema.Schema{
		Tables: make(map[string]*schema.Table),
		Enums: map[string]*schema.Enum{
			"status": {
				Name:   "status",
				Values: []string{"active", "pending", "legacy_val", "archived"},
			},
			"user_role": {
				Name:   "user_role",
				Values: []string{"admin", "member"},
			},
		},
	}

	desired := &schema.Schema{
		Tables: make(map[string]*schema.Table),
		Enums: map[string]*schema.Enum{
			"status": {
				Name: "status",
				// "pending" renamed to "in_progress"
				// "legacy_val" dropped
				// "new_val" added
				Values: []string{"active", "in_progress", "new_val", "archived"},
			},
			"user_role": {
				Name: "user_role",
				// "member" renamed to "viewer" via enum.user_role.member
				Values: []string{"admin", "viewer"},
			},
		},
	}

	filters := scope.Filters{
		Renames: map[string]string{
			"status.pending":        "in_progress",
			"enum.user_role.member": "viewer",
		},
	}

	changes, err := diff.Diff(live, desired, "public", "", filters)
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	var foundRenameStatus, foundRenameRole, foundDropStatus, foundAlterStatus bool
	for _, c := range changes {
		switch c.Type {
		case plan.ChangeRenameEnumValue:
			if c.Table == "status" && c.OldEnumValue == "pending" && c.EnumValue == "in_progress" {
				foundRenameStatus = true
				if c.Destructive {
					t.Errorf("rename enum value must not be marked destructive")
				}
			}
			if c.Table == "user_role" && c.OldEnumValue == "member" && c.EnumValue == "viewer" {
				foundRenameRole = true
			}
		case plan.ChangeDropEnumValue:
			if c.Table == "status" && c.OldEnumValue == "legacy_val" {
				foundDropStatus = true
				if !c.Destructive {
					t.Errorf("drop enum value must be marked destructive")
				}
			}
		case plan.ChangeAlterEnum:
			if c.Table == "status" && c.EnumValue == "new_val" {
				foundAlterStatus = true
			}
			if c.EnumValue == "in_progress" || c.EnumValue == "viewer" {
				t.Errorf("renamed enum values must not trigger ChangeAlterEnum: %s", c.EnumValue)
			}
		}
	}

	if !foundRenameStatus {
		t.Errorf("expected ChangeRenameEnumValue for status.pending -> in_progress")
	}
	if !foundRenameRole {
		t.Errorf("expected ChangeRenameEnumValue for user_role.member -> viewer")
	}
	if !foundDropStatus {
		t.Errorf("expected ChangeDropEnumValue for status.legacy_val")
	}
	if !foundAlterStatus {
		t.Errorf("expected ChangeAlterEnum for status.new_val")
	}

	// Test plan hazard generation for removed enum label
	dropStep := plan.Step{
		Type:         plan.ChangeDropEnumValue,
		Table:        "status",
		OldEnumValue: "legacy_val",
		Destructive:  true,
	}
	p := &plan.Plan{TargetSchema: "public", Steps: []plan.Step{dropStep}}
	hazards := p.Hazards()

	if len(hazards) != 1 {
		t.Fatalf("expected 1 hazard, got: %+v", hazards)
	}
	h := hazards[0]
	if h.Code != plan.HazardDropEnumValue {
		t.Errorf("hazard code = %s, want %s", h.Code, plan.HazardDropEnumValue)
	}
	if h.Level != plan.HazardLevelCritical {
		t.Errorf("hazard level = %s, want %s", h.Level, plan.HazardLevelCritical)
	}
	expectedDesc := `Enum type "status" label "legacy_val" will be removed; PostgreSQL does not support in-place enum label deletion without recreating the type and dependent columns`
	if h.Description != expectedDesc {
		t.Errorf("unexpected hazard description:\ngot:  %s\nwant: %s", h.Description, expectedDesc)
	}

	// Validate hazards requires explicit acceptance
	if err := p.ValidateHazards(nil); err == nil {
		t.Errorf("expected ValidateHazards to fail without HazardDropEnumValue acceptance")
	}
	if err := p.ValidateHazards([]plan.HazardCode{plan.HazardDropEnumValue}); err != nil {
		t.Errorf("ValidateHazards failed despite accepting HazardDropEnumValue: %v", err)
	}

	// Sorting priority: Rename enum value (34) must sort before Alter enum (35)
	steps := []plan.Step{
		{Type: plan.ChangeAlterEnum, Table: "status", EnumValue: "new_val"},
		{Type: plan.ChangeRenameEnumValue, Table: "status", OldEnumValue: "pending", EnumValue: "in_progress"},
	}
	plan.SortSteps(steps)
	renameIdx := slices.IndexFunc(steps, func(s plan.Step) bool { return s.Type == plan.ChangeRenameEnumValue })
	alterIdx := slices.IndexFunc(steps, func(s plan.Step) bool { return s.Type == plan.ChangeAlterEnum })
	if renameIdx >= 0 && alterIdx >= 0 && renameIdx > alterIdx {
		t.Errorf("expected ChangeRenameEnumValue (%d) to sort before ChangeAlterEnum (%d)", renameIdx, alterIdx)
	}
}
