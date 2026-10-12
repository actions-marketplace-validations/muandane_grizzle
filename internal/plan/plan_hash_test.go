package plan_test

import (
	"strings"
	"testing"

	"github.com/muandane/grizzle/internal/plan"
)

// TestPlan_Hash_StableAcrossMapOrder verifies Hash() is invariant to the
// insertion order of map-backed fields (Renames) and unsorted slices
// (IncludeTables/ExcludeTables/TargetSchemas). A hash that varies across Go runs would
// falsely trip the ExpectedHash drift check for byte-identical plans.
func TestPlan_Hash_StableAcrossMapOrder(t *testing.T) {
	newPlan := func(renames map[string]string, includes, excludes []string) *plan.Plan {
		return &plan.Plan{
			TargetSchema:  "public",
			Steps:         []plan.Step{{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN name TEXT;`}},
			IncludeTables: includes,
			ExcludeTables: excludes,
			Renames:       renames,
		}
	}

	renamesForward := map[string]string{"users_accounts": "users", "user_profiles": "profiles"}
	renamesReverse := map[string]string{"user_profiles": "profiles", "users_accounts": "users"}

	// Deterministic anchor: same content, different map insertion order.
	for i := range 50 {
		p1 := newPlan(renamesForward, []string{"users", "posts"}, []string{"internal_log"})
		p2 := newPlan(renamesReverse, []string{"posts", "users"}, []string{"internal_log"})

		h1, h2 := p1.Hash(), p2.Hash()
		if h1 != h2 {
			t.Fatalf("iteration %d: hash depends on map/slice order: %s != %s", i, h1, h2)
		}

		// Content change must change the hash (drift detection stays meaningful).
		p3 := newPlan(renamesReverse, []string{"posts", "users"}, []string{"internal_log", "audit"})
		if p3.Hash() == h1 {
			t.Fatalf("iteration %d: hash unchanged after exclude list change", i)
		}
	}
}

// TestPlan_Hash_Golden pins the hash of a fixed plan so any format change is
// a deliberate, reviewed event (records in grizzle_history key drift checks
// on this digest).
func TestPlan_Hash_Golden(t *testing.T) {
	p := &plan.Plan{
		TargetSchema:  "public",
		TargetSchemas: []string{"public"},
		Steps:         []plan.Step{{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN name TEXT;`}},
		IncludeTables: []string{"users", "posts"},
		Renames:       map[string]string{"users_accounts": "users"},
	}

	const want = "5535ef580cf9685c596b1ec84856498d732d77974a5e056e296f752849e0b825"
	if got := p.Hash(); got != want {
		t.Errorf("golden hash mismatch:\n got  %s\n want %s\nIf this change is intentional (hash format edit), update the golden value.", got, want)
	}
}

// TestPlan_Hash_ApprovalSensitiveFields verifies that mutating any
// approval-sensitive field changes the plan hash (integrity boundary).
func TestPlan_Hash_ApprovalSensitiveFields(t *testing.T) {
	base := &plan.Plan{
		TargetSchema:  "public",
		TargetSchemas: []string{"public"},
		Policy:        plan.DropPolicy{AllowIndex: true},
		IncludeTables: []string{"users"},
		ExcludeTables: []string{"audit"},
		Renames:       map[string]string{"a": "b"},
		SchemaSQL:     "CREATE TABLE users (id INT);",
		RolesSQL:      "CREATE ROLE app_read;",
		CatalogSQL:    "CREATE PUBLICATION docs FOR TABLE users;",
		Steps: []plan.Step{
			{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN name TEXT;`},
		},
	}
	baseHash := base.Hash()

	cases := []struct {
		name   string
		mutate func(p *plan.Plan)
	}{
		{"policy", func(p *plan.Plan) { p.Policy.AllowTable = true }},
		{"target_schema_without_target_schemas", func(p *plan.Plan) {
			p.TargetSchemas = nil
			p.TargetSchema = "billing"
		}},
		{"target_schemas", func(p *plan.Plan) { p.TargetSchemas = []string{"billing", "public"} }},
		{"include_tables", func(p *plan.Plan) { p.IncludeTables = []string{"users", "posts"} }},
		{"exclude_tables", func(p *plan.Plan) { p.ExcludeTables = []string{"audit", "log"} }},
		{"renames", func(p *plan.Plan) { p.Renames = map[string]string{"a": "c"} }},
		{"expand_contract", func(p *plan.Plan) { p.ExpandContract = true }},
		{"non_concurrent", func(p *plan.Plan) { p.NonConcurrentIndexes = true }},
		{"schema_sql", func(p *plan.Plan) { p.SchemaSQL = "CREATE TABLE users (id BIGINT);" }},
		{"roles_sql", func(p *plan.Plan) { p.RolesSQL = "CREATE ROLE app_write;" }},
		{"catalog_sql", func(p *plan.Plan) { p.CatalogSQL = "CREATE PUBLICATION docs FOR ALL TABLES;" }},
		{"step_sql", func(p *plan.Plan) { p.Steps[0].SQL = `ALTER TABLE "public"."users" ADD COLUMN age INT;` }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clone := *base
			clone.Policy = base.Policy
			clone.IncludeTables = append([]string(nil), base.IncludeTables...)
			clone.ExcludeTables = append([]string(nil), base.ExcludeTables...)
			clone.TargetSchemas = append([]string(nil), base.TargetSchemas...)
			clone.Renames = map[string]string{"a": "b"}
			clone.Steps = append([]plan.Step(nil), base.Steps...)
			tc.mutate(&clone)
			if clone.Hash() == baseHash {
				t.Fatalf("mutating %s did not change hash", tc.name)
			}
		})
	}
}

func TestPlan_Hash_TargetSchemasOneElementIsIncluded(t *testing.T) {
	base := &plan.Plan{TargetSchema: "public"}
	withOneTarget := &plan.Plan{TargetSchema: "public", TargetSchemas: []string{"billing"}}
	if base.Hash() == withOneTarget.Hash() {
		t.Fatal("one-element TargetSchemas must participate in the plan hash")
	}
}

// TestPlan_Document_OmitsLockAndShadow verifies the serialized artifact
// never carries lock identity or shadow schema (generated at apply time).
func TestPlan_Document_OmitsLockAndShadow(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps:        []plan.Step{{Type: plan.ChangeAddColumn, Table: "users", SQL: "ALTER TABLE users ADD COLUMN x INT;"}},
		SchemaSQL:    "CREATE TABLE users (id INT, x INT);",
		RolesSQL:     "CREATE ROLE app_read;",
		CatalogSQL:   "CREATE PUBLICATION docs FOR TABLE users;",
	}
	data, err := p.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	s := string(data)
	for _, forbidden := range []string{`"lock_id"`, `"lock_namespace"`, `"shadow_schema"`, `"options_digest"`} {
		if strings.Contains(s, forbidden) {
			t.Errorf("document must not contain %s", forbidden)
		}
	}
	parsed, hash, err := plan.ParsePlanJSON(data)
	if err != nil {
		t.Fatalf("ParsePlanJSON: %v", err)
	}
	if hash != p.Hash() || parsed.Hash() != p.Hash() {
		t.Fatalf("round-trip hash mismatch: envelope=%s parsed=%s want=%s", hash, parsed.Hash(), p.Hash())
	}
	if parsed.RolesSQL != p.RolesSQL || parsed.CatalogSQL != p.CatalogSQL {
		t.Fatalf("side-channel SQL did not round-trip: roles=%q/%q catalog=%q/%q",
			parsed.RolesSQL, p.RolesSQL, parsed.CatalogSQL, p.CatalogSQL)
	}
}

// TestPlan_Document_PasswordRoundTripHashStable verifies Hash uses the same
// password redaction as Document storage so apply --plan does not see drift.
func TestPlan_Document_PasswordRoundTripHashStable(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		RolesSQL:     `CREATE ROLE app_login LOGIN PASSWORD 's3cret-live';`,
		Steps: []plan.Step{{
			Type:  plan.ChangeAlterRole,
			Table: "app_login",
			SQL:   `ALTER ROLE "app_login" PASSWORD '********';`,
		}},
	}
	want := p.Hash()
	data, err := p.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON: %v", err)
	}
	if strings.Contains(string(data), "s3cret-live") {
		t.Fatal("document must not embed plaintext password")
	}
	parsed, envelopeHash, err := plan.ParsePlanJSON(data)
	if err != nil {
		t.Fatalf("ParsePlanJSON: %v", err)
	}
	if envelopeHash != want {
		t.Fatalf("document envelope hash %q != original %q", envelopeHash, want)
	}
	if got := parsed.Hash(); got != want {
		t.Fatalf("reloaded plan hash %q != original %q (Document/Hash redaction mismatch)", got, want)
	}
}

// TestPlan_Hash_StagedEvolutionGolden pins golden plan hashes for Renames,
// staged ExpandContract expand phase, and contract phase plans to guarantee
// hash inputs remain frozen and immutable across versions.
func TestPlan_Hash_StagedEvolutionGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan *plan.Plan
		want string
	}{
		{
			name: "bare_rename",
			plan: &plan.Plan{
				TargetSchema: "public",
				Renames:      map[string]string{"old_col": "new_col"},
				Steps: []plan.Step{
					{Type: plan.ChangeRenameColumn, Table: "users", OldColumn: "old_col", SQL: `ALTER TABLE "public"."users" RENAME COLUMN "old_col" TO "new_col";`},
				},
			},
			want: "72ebef41177705bb88e1aaa795e717fddc46ae8f7f3a2448b23036868fd7a9b2",
		},
		{
			name: "table_qualified_rename",
			plan: &plan.Plan{
				TargetSchema: "public",
				Renames:      map[string]string{"users.old_col": "new_col"},
				Steps: []plan.Step{
					{Type: plan.ChangeRenameColumn, Table: "users", OldColumn: "old_col", SQL: `ALTER TABLE "public"."users" RENAME COLUMN "old_col" TO "new_col";`},
				},
			},
			want: "e2314bdd43fba85ecf05037cb485e07577b8dda2177f37beed25dab6fffe6b41",
		},
		{
			name: "staged_expand_phase",
			plan: &plan.Plan{
				TargetSchema:   "public",
				ExpandContract: true,
				Renames:        map[string]string{"users.full_name": "display_name"},
				Steps: []plan.Step{
					{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN "display_name" TEXT;`},
				},
			},
			want: "d4438ca0ff73e84154711f80fa4d1c6404dda8f4cc9a7af102b2fea6111e66f5",
		},
		{
			name: "staged_contract_phase",
			plan: &plan.Plan{
				TargetSchema: "public",
				Policy:       plan.DropPolicy{AllowColumn: true},
				Renames:      map[string]string{"users.full_name": "display_name"},
				Steps: []plan.Step{
					{Type: plan.ChangeDropColumn, Table: "users", OldColumn: "full_name", Destructive: true, SQL: `ALTER TABLE "public"."users" DROP COLUMN "full_name";`},
				},
			},
			want: "c5b8836d6490c949ceadf2d29aa22479a9dfc55f970642e1e61c6b8f2fdd4dea",
		},
		{
			name: "multi_table_expand_renames",
			plan: &plan.Plan{
				TargetSchema:   "public",
				TargetSchemas:  []string{"public"},
				ExpandContract: true,
				Renames: map[string]string{
					"users.first_name": "given_name",
					"users.last_name":  "family_name",
					"orders.cost":      "amount",
				},
				Steps: []plan.Step{
					{Type: plan.ChangeAddColumn, Table: "orders", SQL: `ALTER TABLE "public"."orders" ADD COLUMN "amount" INTEGER;`},
					{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN "family_name" TEXT;`},
					{Type: plan.ChangeAddColumn, Table: "users", SQL: `ALTER TABLE "public"."users" ADD COLUMN "given_name" TEXT;`},
				},
			},
			want: "073ab04579dc2eb5f41e78711f4ab59e9d526502f9d479229705b58de9b0170b",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.plan.Hash()
			if tc.want != "" && got != tc.want {
				t.Fatalf("golden hash mismatch for %s:\ngot:  %s\nwant: %s\nIf hash inputs were intentionally modified, review carefully.", tc.name, got, tc.want)
			}

			// Verify JSON round-trip stability
			data, err := tc.plan.ToJSON()
			if err != nil {
				t.Fatalf("ToJSON failed: %v", err)
			}
			parsed, envelopeHash, err := plan.ParsePlanJSON(data)
			if err != nil {
				t.Fatalf("ParsePlanJSON failed: %v", err)
			}
			if envelopeHash != got {
				t.Fatalf("envelope hash %q != computed hash %q", envelopeHash, got)
			}
			if parsed.Hash() != got {
				t.Fatalf("parsed plan hash %q != original hash %q", parsed.Hash(), got)
			}
		})
	}
}
