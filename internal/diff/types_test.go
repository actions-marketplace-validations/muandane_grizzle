package diff_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

func TestDiff_RangeTypes_CreateDropAndSubtypeDrift(t *testing.T) {
	live := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		RangeTypes: map[string]*schema.RangeType{
			"stale_range": {
				Schema:  "public",
				Name:    "stale_range",
				Subtype: "integer",
			},
			"evolving_range": {
				Schema:  "public",
				Name:    "evolving_range",
				Subtype: "integer",
			},
			"stable_range": {
				Schema:  "public",
				Name:    "stable_range",
				Subtype: "numeric",
			},
		},
	}

	desired := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		RangeTypes: map[string]*schema.RangeType{
			"new_range": {
				Schema:         "public",
				Name:           "new_range",
				Subtype:        "timestamptz",
				SubtypeOpclass: "timestamptz_ops",
			},
			"evolving_range": {
				Schema:  "public",
				Name:    "evolving_range",
				Subtype: "bigint", // changed from integer
			},
			"stable_range": {
				Schema:  "public",
				Name:    "stable_range",
				Subtype: "numeric",
			},
		},
	}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	var foundCreateNew, foundDropStale, foundDropEvolving, foundCreateEvolving bool
	for _, c := range changes {
		switch c.Type {
		case plan.ChangeCreateRangeType:
			if c.Table == "new_range" {
				foundCreateNew = true
				if c.Destructive {
					t.Errorf("ChangeCreateRangeType must not be destructive")
				}
				if c.RangeType == nil || c.RangeType.Subtype != "timestamptz" {
					t.Errorf("expected RangeType with subtype timestamptz, got %v", c.RangeType)
				}
			}
			if c.Table == "evolving_range" {
				foundCreateEvolving = true
				if c.RangeType == nil || c.RangeType.Subtype != "bigint" {
					t.Errorf("expected replacement RangeType with subtype bigint, got %v", c.RangeType)
				}
			}
		case plan.ChangeDropRangeType, plan.ChangeDropRangeRetype:
			if c.Table == "stale_range" {
				foundDropStale = true
				if c.Type != plan.ChangeDropRangeType {
					t.Errorf("expected ChangeDropRangeType for stale_range, got %v", c.Type)
				}
				if !c.Destructive {
					t.Errorf("ChangeDropRangeType must be destructive")
				}
			}
			if c.Table == "evolving_range" {
				foundDropEvolving = true
				if c.Type != plan.ChangeDropRangeRetype {
					t.Errorf("expected ChangeDropRangeRetype for evolving_range, got %v", c.Type)
				}
				if !c.Destructive {
					t.Errorf("ChangeDropRangeRetype for evolving subtype must be destructive")
				}
			}
		}
	}

	if !foundCreateNew {
		t.Errorf("expected ChangeCreateRangeType for new_range")
	}
	if !foundDropStale {
		t.Errorf("expected ChangeDropRangeType for stale_range")
	}
	if !foundDropEvolving || !foundCreateEvolving {
		t.Errorf("expected drop + recreate for evolving_range (drop=%t, create=%t)", foundDropEvolving, foundCreateEvolving)
	}
}

func TestDiff_CompositeTypes_CreateDropAndAttributes(t *testing.T) {
	live := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		CompositeTypes: map[string]*schema.CompositeType{
			"legacy_composite": {
				Schema: "public",
				Name:   "legacy_composite",
				Attributes: []*schema.CompositeAttribute{
					{Name: "old_val", DataType: "text"},
				},
			},
			"address": {
				Schema: "public",
				Name:   "address",
				Attributes: []*schema.CompositeAttribute{
					{Name: "street", DataType: "text"},
					{Name: "city", DataType: "text"},
					{Name: "deprecated_field", DataType: "text"},
					{Name: "code", DataType: "integer"},
				},
			},
		},
	}

	desired := &schema.Schema{
		Name:   "public",
		Tables: make(map[string]*schema.Table),
		CompositeTypes: map[string]*schema.CompositeType{
			"new_composite": {
				Schema: "public",
				Name:   "new_composite",
				Attributes: []*schema.CompositeAttribute{
					{Name: "id", DataType: "uuid"},
					{Name: "data", DataType: "jsonb"},
				},
			},
			"address": {
				Schema: "public",
				Name:   "address",
				Attributes: []*schema.CompositeAttribute{
					{Name: "street", DataType: "text"},
					{Name: "city", DataType: "text"},
					// deprecated_field dropped
					{Name: "country", DataType: "varchar(50)"}, // added
					{Name: "code", DataType: "varchar(20)"},    // altered from integer
				},
			},
		},
	}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	var foundCreateNew, foundDropLegacy bool
	var foundAddCountry, foundDropDeprecated, foundAlterCode bool

	for _, c := range changes {
		switch c.Type {
		case plan.ChangeCreateCompositeType:
			if c.Table == "new_composite" {
				foundCreateNew = true
				if c.Destructive {
					t.Errorf("ChangeCreateCompositeType must not be destructive")
				}
				if len(c.CompositeType.Attributes) != 2 {
					t.Errorf("expected 2 attributes on new_composite, got %d", len(c.CompositeType.Attributes))
				}
			}
		case plan.ChangeDropCompositeType:
			if c.Table == "legacy_composite" {
				foundDropLegacy = true
				if !c.Destructive {
					t.Errorf("ChangeDropCompositeType must be destructive")
				}
			}
		case plan.ChangeAlterCompositeType:
			if c.Table == "address" && c.CompositeAttr != nil {
				switch c.CompositeAttr.Name {
				case "country":
					foundAddCountry = true
					if c.Destructive {
						t.Errorf("adding attribute must not be destructive")
					}
					if c.CompositeAttr.DataType != "varchar(50)" {
						t.Errorf("expected country data type varchar(50), got %s", c.CompositeAttr.DataType)
					}
				case "deprecated_field":
					foundDropDeprecated = true
					if !c.Destructive {
						t.Errorf("dropping attribute must be destructive")
					}
				case "code":
					foundAlterCode = true
					if !c.TypeChanged {
						t.Errorf("altering attribute type must mark TypeChanged=true")
					}
					if c.CompositeAttr.DataType != "varchar(20)" {
						t.Errorf("expected code data type varchar(20), got %s", c.CompositeAttr.DataType)
					}
				}
			}
		}
	}

	if !foundCreateNew {
		t.Errorf("expected ChangeCreateCompositeType for new_composite")
	}
	if !foundDropLegacy {
		t.Errorf("expected ChangeDropCompositeType for legacy_composite")
	}
	if !foundAddCountry {
		t.Errorf("expected ChangeAlterCompositeType (ADD) for country attribute")
	}
	if !foundDropDeprecated {
		t.Errorf("expected ChangeAlterCompositeType (DROP) for deprecated_field attribute")
	}
	if !foundAlterCode {
		t.Errorf("expected ChangeAlterCompositeType (ALTER TYPE) for code attribute")
	}
}

func TestHazardsAndPolicy_RangeAndCompositeTypes(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:        plan.ChangeDropRangeType,
				Table:       "price_range",
				SQL:         `DROP TYPE "public"."price_range";`,
				Destructive: true,
			},
			{
				Type:        plan.ChangeDropCompositeType,
				Table:       "address",
				SQL:         `DROP TYPE "public"."address";`,
				Destructive: true,
			},
			{
				Type:          plan.ChangeAlterCompositeType,
				Table:         "address",
				CompositeAttr: "old_attr",
				SQL:           `ALTER TYPE "public"."address" DROP ATTRIBUTE "old_attr";`,
				Destructive:   true,
			},
			{
				Type:          plan.ChangeAlterCompositeType,
				Table:         "address",
				CompositeAttr: "new_attr",
				SQL:           `ALTER TYPE "public"."address" ADD ATTRIBUTE "new_attr" text;`,
				Destructive:   false,
			},
		},
	}

	// 1. Check Hazards
	hazards := p.Hazards()
	if len(hazards) < 2 {
		t.Fatalf("expected at least 2 hazards, got %d", len(hazards))
	}
	var foundRangeHazard, foundCompHazard bool
	for _, h := range hazards {
		if h.Code == plan.HazardDropRangeType {
			foundRangeHazard = true
			if h.Level != plan.HazardLevelCritical {
				t.Errorf("expected CRITICAL level for HazardDropRangeType, got %s", h.Level)
			}
		}
		if h.Code == plan.HazardDropCompositeType {
			foundCompHazard = true
			if h.Level != plan.HazardLevelCritical {
				t.Errorf("expected CRITICAL level for HazardDropCompositeType, got %s", h.Level)
			}
		}
	}
	if !foundRangeHazard || !foundCompHazard {
		t.Errorf("missing hazards: range=%t, composite=%t", foundRangeHazard, foundCompHazard)
	}

	// 2. Default DropPolicy blocks drops
	defaultPolicy := plan.DropPolicy{}
	for _, s := range p.Steps {
		if s.Destructive {
			if defaultPolicy.IsAllowed(s) {
				t.Errorf("default policy should block destructive step %s on %s", s.Type, s.Table)
			}
		} else {
			if !defaultPolicy.IsAllowed(s) {
				t.Errorf("default policy should allow non-destructive step %s on %s", s.Type, s.Table)
			}
		}
	}

	// 3. Fine-grained AllowRangeType allows only range drop
	rangePolicy := plan.DropPolicy{AllowRangeType: true}
	if !rangePolicy.IsAllowed(p.Steps[0]) {
		t.Errorf("AllowRangeType should permit ChangeDropRangeType")
	}
	if rangePolicy.IsAllowed(p.Steps[1]) {
		t.Errorf("AllowRangeType should NOT permit ChangeDropCompositeType")
	}

	// 4. Fine-grained AllowCompositeType allows composite and composite attr drop
	compPolicy := plan.DropPolicy{AllowCompositeType: true}
	if compPolicy.IsAllowed(p.Steps[0]) {
		t.Errorf("AllowCompositeType should NOT permit ChangeDropRangeType")
	}
	if !compPolicy.IsAllowed(p.Steps[1]) {
		t.Errorf("AllowCompositeType should permit ChangeDropCompositeType")
	}
	if !compPolicy.IsAllowed(p.Steps[2]) {
		t.Errorf("AllowCompositeType should permit destructive ChangeAlterCompositeType")
	}

	// 5. Blanket AllowType permits both
	typePolicy := plan.DropPolicy{AllowType: true}
	if !typePolicy.IsAllowed(p.Steps[0]) || !typePolicy.IsAllowed(p.Steps[1]) || !typePolicy.IsAllowed(p.Steps[2]) {
		t.Errorf("AllowType should permit all range and composite drops")
	}
}

func TestPlan_SortSteps_RangeAndCompositeOrdering(t *testing.T) {
	steps := []plan.Step{
		{Type: plan.ChangeCreateTable, Table: "orders"},
		{Type: plan.ChangeCreateRangeType, Table: "price_range"},
		{Type: plan.ChangeCreateCompositeType, Table: "address"},
		{Type: plan.ChangeAlterCompositeType, Table: "address"},
		{Type: plan.ChangeDropTable, Table: "old_orders", Destructive: true},
		{Type: plan.ChangeDropCompositeType, Table: "old_address", Destructive: true},
		{Type: plan.ChangeDropRangeType, Table: "old_range", Destructive: true},
		{Type: plan.ChangeDropCompositeRetype, Table: "retype_address", Destructive: true},
		{Type: plan.ChangeDropRangeRetype, Table: "retype_range", Destructive: true},
	}

	plan.SortSteps(steps)

	// Expected priorities:
	// ChangeDropCompositeRetype: 28
	// ChangeDropRangeRetype: 28
	// ChangeCreateCompositeType: 31
	// ChangeCreateRangeType: 32
	// ChangeAlterCompositeType: 33
	// ChangeCreateTable: 40
	// ChangeDropTable: 100
	// ChangeDropRangeType: 101
	// ChangeDropCompositeType: 101

	indices := make(map[plan.ChangeType]int)
	for i, s := range steps {
		if _, exists := indices[s.Type]; !exists {
			indices[s.Type] = i
		}
	}

	order := []plan.ChangeType{
		plan.ChangeCreateCompositeType,
		plan.ChangeCreateRangeType,
		plan.ChangeAlterCompositeType,
		plan.ChangeCreateTable,
		plan.ChangeDropTable,
	}

	for i := 0; i < len(order)-1; i++ {
		first := order[i]
		second := order[i+1]
		if indices[first] >= indices[second] {
			t.Errorf("expected %s (idx %d) before %s (idx %d)", first, indices[first], second, indices[second])
		}
	}

	// Check retype drops happen early (28 < 40)
	if indices[plan.ChangeDropCompositeRetype] >= indices[plan.ChangeCreateTable] {
		t.Errorf("expected ChangeDropCompositeRetype before ChangeCreateTable")
	}
	if indices[plan.ChangeDropRangeRetype] >= indices[plan.ChangeCreateTable] {
		t.Errorf("expected ChangeDropRangeRetype before ChangeCreateTable")
	}

	// Check live-only drops happen after drop table (101 > 100)
	if indices[plan.ChangeDropCompositeType] <= indices[plan.ChangeDropTable] {
		t.Errorf("expected ChangeDropCompositeType after ChangeDropTable")
	}
	if indices[plan.ChangeDropRangeType] <= indices[plan.ChangeDropTable] {
		t.Errorf("expected ChangeDropRangeType after ChangeDropTable")
	}
}
