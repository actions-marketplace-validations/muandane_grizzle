package plan_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/muandane/grizzle/internal/plan"
)

func TestPlan_FormatInteractiveSummary_Empty(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps:        nil,
	}

	var buf bytes.Buffer
	if err := p.FormatInteractiveSummary(&buf); err != nil {
		t.Fatalf("FormatInteractiveSummary failed: %v", err)
	}

	expected := "Planned changes:\n  No changes. Database schema is already in sync.\n"
	if buf.String() != expected {
		t.Fatalf("expected:\n%q\ngot:\n%q", expected, buf.String())
	}
}

func TestPlan_FormatInteractiveSummary_SpecExample(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "accounts",
				SQL:   "CREATE TABLE accounts (id, balance)",
			},
			{
				Type:        plan.ChangeDropColumn,
				Table:       "users",
				Column:      "legacy_role",
				SQL:         `ALTER TABLE "users" DROP COLUMN "legacy_role";`,
				Destructive: true,
			},
		},
		Policy: plan.DropPolicy{
			AllowColumn: true,
		},
	}

	var buf bytes.Buffer
	if err := p.FormatInteractiveSummary(&buf); err != nil {
		t.Fatalf("FormatInteractiveSummary failed: %v", err)
	}

	expected := "Planned changes:\n" +
		"  + CREATE TABLE accounts (id, balance)\n" +
		"  ! DROP COLUMN users.legacy_role [DROP_COLUMN: CRITICAL]\n\n" +
		"[DROP_COLUMN] Column on table \"users\" will be dropped with all existing row values\n" +
		"    remediation: allow via DropPolicy AllowDropColumn and accept DROP_COLUMN; the column and its data are permanently lost\n"

	if buf.String() != expected {
		t.Fatalf("expected:\n%s\ngot:\n%s", expected, buf.String())
	}
}

func TestPlan_FormatInteractiveSummary_OperationsAndHazards(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:   plan.ChangeAddColumn,
				Table:  "users",
				Column: "email",
				SQL:    `ALTER TABLE "users" ADD COLUMN "email" text;`,
			},
			{
				Type:         plan.ChangeAlterColumn,
				Table:        "users",
				Column:       "bio",
				SQL:          `ALTER TABLE "users" ALTER COLUMN "bio" TYPE varchar(50);`,
				TypeNarrowed: true,
				Destructive:  true,
			},
			{
				Type:  plan.ChangeCreateIndex,
				Table: "users",
				SQL:   `CREATE INDEX "idx_users_email" ON "users" ("email");`,
			},
			{
				Type:        plan.ChangeAttachPartition,
				Table:       "orders_2026",
				ParentTable: "orders",
				SQL:         `ALTER TABLE orders ATTACH PARTITION orders_2026 FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');`,
			},
		},
		Policy: plan.DropPolicy{
			AllowColumn: false, // bio alter is destructive and not allowed
		},
	}

	var buf bytes.Buffer
	if err := p.FormatInteractiveSummary(&buf); err != nil {
		t.Fatalf("FormatInteractiveSummary failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "  + ADD COLUMN users.email\n") {
		t.Errorf("missing add column line, got:\n%s", out)
	}
	if !strings.Contains(out, "  ! ALTER COLUMN users.bio [TYPE_NARROW: CRITICAL] [BLOCKED by policy]\n") {
		t.Errorf("missing alter column line with critical badge and blocked badge, got:\n%s", out)
	}
	if !strings.Contains(out, "  + CREATE INDEX idx_users_email ON users [INDEX_BUILD: NOTICE]\n") {
		t.Errorf("missing create index line, got:\n%s", out)
	}
	if !strings.Contains(out, "  ! ATTACH PARTITION orders_2026 TO orders [PARTITION_ATTACH_SCAN: WARNING]\n") {
		t.Errorf("missing attach partition line, got:\n%s", out)
	}
	if !strings.Contains(out, "[TYPE_NARROW] Column on table \"users\" has a destructive type change that may cause data loss or truncation\n") {
		t.Errorf("missing critical hazard description, got:\n%s", out)
	}
}

func TestPlan_FormatInteractiveSummary_UnmanagedRemediation(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:          plan.ChangeDropColumn,
				Table:         "users",
				Column:        "email",
				SQL:           `ALTER TABLE "users" DROP COLUMN "email";`,
				Destructive:   true,
				UnmanagedDeps: []string{"VIEW:v_user_emails (column users.email)"},
			},
		},
		Policy: plan.DropPolicy{AllowColumn: true},
	}

	var buf bytes.Buffer
	if err := p.FormatInteractiveSummary(&buf); err != nil {
		t.Fatalf("FormatInteractiveSummary failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "[UNMANAGED_DEPENDENCY: CRITICAL]") {
		t.Errorf("expected UNMANAGED_DEPENDENCY badge, got:\n%s", out)
	}
	if !strings.Contains(out, "remediation: accept via AcceptHazards: UNMANAGED_DEPENDENCY") {
		t.Errorf("expected remediation hint under critical hazard, got:\n%s", out)
	}
}

func TestPlan_PartitionHazards_Descriptions(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:        plan.ChangeAttachPartition,
				Table:       "orders_2026",
				ParentTable: "orders",
				SQL:         `ALTER TABLE orders ATTACH PARTITION orders_2026 FOR VALUES FROM ('2026-01-01') TO ('2027-01-01');`,
			},
			{
				Type:            plan.ChangeDetachPartition,
				Table:           "orders_2025",
				ParentTable:     "orders",
				IsPendingDetach: true,
				SQL:             `ALTER TABLE orders DETACH PARTITION orders_2025 FINALIZE;`,
			},
		},
	}

	hazards := p.Hazards()
	if len(hazards) != 2 {
		t.Fatalf("expected 2 hazards, got %d", len(hazards))
	}

	expectedAttach := `Attaching existing table "orders_2026" to parent "orders" requires a full table validation scan under ACCESS EXCLUSIVE lock. To avoid prolonged locks on large tables, add and validate an equivalent CHECK constraint before attaching.`
	if hazards[0].Description != expectedAttach {
		t.Errorf("unexpected attach hazard description:\ngot:  %s\nwant: %s", hazards[0].Description, expectedAttach)
	}

	expectedDetach := `Partition "orders_2025" is in an interrupted pending-detach state; planning ALTER TABLE ... DETACH PARTITION ... FINALIZE to complete detachment.`
	if hazards[1].Description != expectedDetach {
		t.Errorf("unexpected detach hazard description:\ngot:  %s\nwant: %s", hazards[1].Description, expectedDetach)
	}
}

func TestPlan_FormatInteractiveSummary_EnumChanges(t *testing.T) {
	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:         plan.ChangeRenameEnumValue,
				Table:        "status",
				OldEnumValue: "pending",
				EnumValue:    "in_progress",
				SQL:          `ALTER TYPE "status" RENAME VALUE 'pending' TO 'in_progress';`,
			},
			{
				Type:         plan.ChangeDropEnumValue,
				Table:        "status",
				OldEnumValue: "archived",
				Destructive:  true,
			},
		},
	}

	var buf bytes.Buffer
	if err := p.FormatInteractiveSummary(&buf); err != nil {
		t.Fatalf("FormatInteractiveSummary failed: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "  ~ RENAME ENUM VALUE status.pending TO in_progress\n") {
		t.Errorf("missing rename enum value line, got:\n%s", out)
	}
	if !strings.Contains(out, "  ! DROP ENUM VALUE status.archived [DROP_ENUM_VALUE: CRITICAL] [BLOCKED by policy]\n") {
		t.Errorf("missing drop enum value line, got:\n%s", out)
	}
	if !strings.Contains(out, "[DROP_ENUM_VALUE] Enum type \"status\" label \"archived\" will be removed; PostgreSQL does not support in-place enum label deletion without recreating the type and dependent columns\n") {
		t.Errorf("missing drop enum value hazard description, got:\n%s", out)
	}
	if !strings.Contains(out, "remediation: PostgreSQL does not support dropping enum values directly; recreate the enum type or map the value via Options.Renames\n") {
		t.Errorf("missing drop enum value remediation hint, got:\n%s", out)
	}
}
