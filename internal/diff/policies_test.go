package diff_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

func newTable(name string) *schema.Table {
	return &schema.Table{
		Schema: "public",
		Name:   name,
		Columns: map[string]*schema.Column{
			"id": {Name: "id", DataType: "bigint", IsNullable: false, Position: 1},
		},
	}
}

func TestDiff_RLSAndPolicies_GreenfieldExistingTable(t *testing.T) {
	live := &schema.Schema{Name: "public", Tables: map[string]*schema.Table{
		"docs": newTable("docs"),
	}}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.RLSForced = true
	desiredTable.Policies = map[string]*schema.Policy{
		"docs_select_own": {
			Name:       "docs_select_own",
			Cmd:        "SELECT",
			Roles:      []string{"app"},
			Using:      "owner_id = current_user",
			Permissive: true,
		},
	}
	desired := &schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	types := map[plan.ChangeType]int{}
	for _, c := range changes {
		types[c.Type]++
	}
	if types[plan.ChangeEnableRLS] != 1 || types[plan.ChangeForceRLS] != 1 || types[plan.ChangeCreatePolicy] != 1 {
		t.Fatalf("expected ENABLE_RLS+FORCE_RLS+CREATE_POLICY, got %v", types)
	}
	for _, c := range changes {
		if c.Destructive {
			t.Fatalf("greenfield RLS sync must not be destructive: %+v", c)
		}
	}
}

func TestDiff_PolicyExpressionEditProducesAlterPolicy(t *testing.T) {
	pol := func(using string) *schema.Policy {
		return &schema.Policy{
			Name:       "docs_select_own",
			Cmd:        "SELECT",
			Roles:      []string{"app"},
			Using:      using,
			Permissive: true,
		}
	}
	liveTable := newTable("docs")
	liveTable.RLSEnabled = true
	liveTable.Policies = map[string]*schema.Policy{"docs_select_own": pol("owner_id = 1")}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.Policies = map[string]*schema.Policy{"docs_select_own": pol("owner_id = 2")}

	changes, err := diff.Diff(
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}},
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}},
		"public", "", scope.Filters{},
	)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected exactly 1 change, got %d (%+v)", len(changes), changes)
	}
	c := changes[0]
	if c.Type != plan.ChangeAlterPolicy {
		t.Errorf("expected ChangeAlterPolicy, got %s", c.Type)
	}
	if c.Destructive {
		t.Errorf("ALTER_POLICY must not be destructive")
	}
}

func TestDiff_PolicyCmdOrPermissiveChangeProducesDropAndCreate(t *testing.T) {
	liveTable := newTable("docs")
	liveTable.RLSEnabled = true
	liveTable.Policies = map[string]*schema.Policy{"docs_select_own": {
		Name:       "docs_select_own",
		Cmd:        "SELECT",
		Roles:      []string{"app"},
		Using:      "owner_id = 1",
		Permissive: true,
	}}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.Policies = map[string]*schema.Policy{"docs_select_own": {
		Name:       "docs_select_own",
		Cmd:        "ALL", // command changed -> must DROP + CREATE
		Roles:      []string{"app"},
		Using:      "owner_id = 1",
		Permissive: true,
	}}

	changes, err := diff.Diff(
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}},
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}},
		"public", "", scope.Filters{},
	)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	var drops, creates int
	for _, c := range changes {
		switch c.Type {
		case plan.ChangeDropPolicy:
			drops++
			if !c.Destructive {
				t.Errorf("DROP_POLICY must be destructive")
			}
		case plan.ChangeCreatePolicy:
			creates++
		}
	}
	if drops != 1 || creates != 1 {
		t.Fatalf("expected 1 DROP_POLICY + 1 CREATE_POLICY, got %d/%d (%+v)", drops, creates, changes)
	}
}

func TestDiff_PolicySecondSyncNoOp(t *testing.T) {
	mk := func() *schema.Table {
		tbl := newTable("docs")
		tbl.RLSEnabled = true
		tbl.Policies = map[string]*schema.Policy{
			"docs_all": {Name: "docs_all", Cmd: "ALL", Roles: []string{"public"}, Using: "true", WithCheck: "true"},
		}
		return tbl
	}
	s := &schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": mk()}}
	changes, err := diff.Diff(s, &schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": mk()}}, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("second sync must be a no-op, got %+v", changes)
	}
}

func TestDiff_PolicyRoleOrderAndExprWhitespaceStable(t *testing.T) {
	liveTable := newTable("docs")
	liveTable.RLSEnabled = true
	liveTable.Policies = map[string]*schema.Policy{
		"docs_all": {Name: "docs_all", Cmd: "ALL", Roles: []string{"b_role", "a_role"}, Using: "owner_id = current_user"},
	}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.Policies = map[string]*schema.Policy{
		"docs_all": {Name: "docs_all", Cmd: "ALL", Roles: []string{"a_role", "b_role"}, Using: "owner_id = current_user"},
	}
	changes, err := diff.Diff(
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}},
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}},
		"public", "", scope.Filters{},
	)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("role order must not produce churn, got %+v", changes)
	}
}

func TestDiff_PoliciesNotTouchedOnOutOfScopeTables(t *testing.T) {
	liveTable := newTable("docs")
	liveTable.Policies = map[string]*schema.Policy{
		"legacy": {Name: "legacy", Cmd: "ALL"},
	}
	s := &schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}}
	filters := scope.Filters{Excludes: []string{"docs"}}
	changes, err := diff.Diff(s, &schema.Schema{Name: "public", Tables: map[string]*schema.Table{}}, "public", "", filters)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("out-of-scope table policies must not produce changes, got %+v", changes)
	}
}

func TestDiff_PolicyRoleChangeProducesAlterPolicy(t *testing.T) {
	// Role alteration from explicit role to default public emits ALTER_POLICY
	liveTable := newTable("docs")
	liveTable.RLSEnabled = true
	liveTable.Policies = map[string]*schema.Policy{
		"docs_read": {Name: "docs_read", Cmd: "SELECT", Roles: []string{"app_user"}, Using: "true"},
	}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.Policies = map[string]*schema.Policy{
		"docs_read": {Name: "docs_read", Cmd: "SELECT", Roles: nil, Using: "true"}, // reset to public
	}

	changes, err := diff.Diff(
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}},
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}},
		"public", "", scope.Filters{},
	)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("expected 1 ChangeAlterPolicy, got %d (%+v)", len(changes), changes)
	}
	if changes[0].Type != plan.ChangeAlterPolicy {
		t.Errorf("expected ChangeAlterPolicy, got %s", changes[0].Type)
	}
	if changes[0].Destructive {
		t.Errorf("ALTER_POLICY for role reset must not be destructive")
	}
	if changes[0].OldPolicy == nil || len(changes[0].OldPolicy.Roles) == 0 {
		t.Errorf("expected OldPolicy to be populated with previous roles")
	}
}

func TestDiff_PolicyExpressionParensAndCastsNoFalseDiff(t *testing.T) {
	liveTable := newTable("docs")
	liveTable.RLSEnabled = true
	liveTable.Policies = map[string]*schema.Policy{
		"docs_select": {
			Name:       "docs_select",
			Cmd:        "SELECT",
			Roles:      []string{"public"},
			Using:      "((user_id = auth.uid()))",
			WithCheck:  "((status = 'active'::text))",
			Permissive: true,
		},
	}
	desiredTable := newTable("docs")
	desiredTable.RLSEnabled = true
	desiredTable.Policies = map[string]*schema.Policy{
		"docs_select": {
			Name:       "docs_select",
			Cmd:        "SELECT",
			Roles:      nil, // equivalent to public
			Using:      "user_id = auth.uid()",
			WithCheck:  "status = 'active'",
			Permissive: true,
		},
	}

	changes, err := diff.Diff(
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": liveTable}},
		&schema.Schema{Name: "public", Tables: map[string]*schema.Table{"docs": desiredTable}},
		"public", "", scope.Filters{},
	)
	if err != nil {
		t.Fatalf("diff: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("expected no changes for semantically identical policy expressions, got %d (%+v)", len(changes), changes)
	}
}
