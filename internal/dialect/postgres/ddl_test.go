package postgres_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/dialect/postgres"
	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
)

func TestGenerateAlterPolicySQL(t *testing.T) {
	tests := []struct {
		name     string
		schema   string
		table    string
		policy   *schema.Policy
		expected string
	}{
		{
			name:   "using expression update",
			schema: "public",
			table:  "docs",
			policy: &schema.Policy{
				Name:  "docs_select",
				Using: "owner_id = 2",
			},
			expected: `ALTER POLICY "docs_select" ON "public"."docs" USING (owner_id = 2);`,
		},
		{
			name:   "roles and with check update",
			schema: "tenant_1",
			table:  "invoices",
			policy: &schema.Policy{
				Name:      "invoices_insert",
				Roles:     []string{"editor", "admin"},
				WithCheck: "amount > 0",
			},
			expected: `ALTER POLICY "invoices_insert" ON "tenant_1"."invoices" TO "editor", "admin" WITH CHECK (amount > 0);`,
		},
		{
			name:   "roles, using, and with check update",
			schema: "public",
			table:  "items",
			policy: &schema.Policy{
				Name:      "items_rw",
				Roles:     []string{"app_user"},
				Using:     "tenant_id = 1",
				WithCheck: "tenant_id = 1 AND active = true",
			},
			expected: `ALTER POLICY "items_rw" ON "public"."items" TO "app_user" USING (tenant_id = 1) WITH CHECK (tenant_id = 1 AND active = true);`,
		},
		{
			name:     "nil policy returns empty",
			schema:   "public",
			table:    "docs",
			policy:   nil,
			expected: "",
		},
		{
			name:     "empty policy name returns empty",
			schema:   "public",
			table:    "docs",
			policy:   &schema.Policy{},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := postgres.GenerateAlterPolicySQL(tt.schema, tt.table, tt.policy)
			if got != tt.expected {
				t.Errorf("GenerateAlterPolicySQL() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestRenderChange_RenameEnumValue(t *testing.T) {
	c := diff.Change{
		Type:         plan.ChangeRenameEnumValue,
		Schema:       "public",
		Table:        "status",
		OldEnumValue: "pending",
		EnumValue:    "in_progress",
	}

	step := postgres.RenderChange("public", c)
	expectedSQL := `ALTER TYPE "status" RENAME VALUE 'pending' TO 'in_progress';`
	if step.SQL != expectedSQL {
		t.Errorf("step.SQL = %q, want %q", step.SQL, expectedSQL)
	}
	if step.OldEnumValue != "pending" || step.EnumValue != "in_progress" {
		t.Errorf("step OldEnumValue/EnumValue mismatch: old=%q new=%q", step.OldEnumValue, step.EnumValue)
	}
}
