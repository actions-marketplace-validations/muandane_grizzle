package lint_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/lint"
	"github.com/muandane/grizzle/internal/schema"
)

func TestTriggerWithoutFunction_L010(t *testing.T) {
	rule := lint.TriggerWithoutFunction{}
	if rule.ID() != "L010" {
		t.Fatalf("expected ID L010, got %s", rule.ID())
	}
	if rule.Description() == "" {
		t.Fatal("expected non-empty description")
	}

	tests := []struct {
		name        string
		schema      *schema.Schema
		wantDiags   int
		wantTable   string
		wantCol     string
		wantMessage string
	}{
		{
			name: "clean trigger with matching routine passes",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: "CREATE TRIGGER trg_guard BEFORE INSERT ON public.docs FOR EACH ROW EXECUTE FUNCTION trg_noop()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"trg_noop()": {Name: "trg_noop"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "trigger with missing function flags warning",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: "CREATE TRIGGER trg_guard BEFORE INSERT ON public.docs FOR EACH ROW EXECUTE FUNCTION trg_noop()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{},
			},
			wantDiags:   1,
			wantTable:   "docs",
			wantCol:     "trg_guard",
			wantMessage: `trigger executes function "trg_noop" which is not defined in the desired schema`,
		},
		{
			name: "case-insensitive matching matches",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: "CREATE TRIGGER trg_guard BEFORE INSERT ON public.docs FOR EACH ROW EXECUTE FUNCTION TRG_NOOP()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"trg_noop()": {Name: "trg_noop"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "schema-qualified trigger function matches unqualified routine",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: `CREATE TRIGGER trg_guard BEFORE INSERT ON public.docs FOR EACH ROW EXECUTE FUNCTION "public"."trg_noop"()`,
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"trg_noop()": {Name: "trg_noop"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "unqualified trigger matches qualified routine",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: "CREATE TRIGGER trg_guard BEFORE INSERT ON public.docs FOR EACH ROW EXECUTE FUNCTION trg_noop()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"public.trg_noop()": {Name: "public.trg_noop"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "quoted identifier in trigger definition matches",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"docs": {
						Name: "docs",
						Triggers: map[string]*schema.Trigger{
							"trg_guard": {
								Name:       "trg_guard",
								Definition: `CREATE TRIGGER trg_guard BEFORE INSERT ON docs FOR EACH ROW EXECUTE FUNCTION "custom_audit"()`,
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"custom_audit()": {Name: "custom_audit"},
				},
			},
			wantDiags: 0,
		},
		{
			name: "execute procedure syntax is supported",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"orders": {
						Name: "orders",
						Triggers: map[string]*schema.Trigger{
							"proc_trg": {
								Name:       "proc_trg",
								Definition: "CREATE TRIGGER proc_trg BEFORE UPDATE ON orders FOR EACH ROW EXECUTE PROCEDURE check_orders()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{},
			},
			wantDiags:   1,
			wantTable:   "orders",
			wantCol:     "proc_trg",
			wantMessage: `trigger executes function "check_orders" which is not defined in the desired schema`,
		},
		{
			name: "trigger with arguments extracts function name",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"orders": {
						Name: "orders",
						Triggers: map[string]*schema.Trigger{
							"args_trg": {
								Name:       "args_trg",
								Definition: "CREATE TRIGGER args_trg AFTER INSERT ON orders FOR EACH ROW EXECUTE FUNCTION log_action('orders', 1)",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{},
			},
			wantDiags:   1,
			wantTable:   "orders",
			wantCol:     "args_trg",
			wantMessage: `trigger executes function "log_action" which is not defined in the desired schema`,
		},
		{
			name: "unparseable trigger definition is skipped",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"orders": {
						Name: "orders",
						Triggers: map[string]*schema.Trigger{
							"bad_trg": {
								Name:       "bad_trg",
								Definition: "NOT A VALID TRIGGER DEFINITION",
							},
						},
					},
				},
			},
			wantDiags: 0,
		},
		{
			name: "multiple triggers with one missing and one present",
			schema: &schema.Schema{
				Name: "public",
				Tables: map[string]*schema.Table{
					"orders": {
						Name: "orders",
						Triggers: map[string]*schema.Trigger{
							"good_trg": {
								Name:       "good_trg",
								Definition: "CREATE TRIGGER good_trg BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION ok_fn()",
							},
							"bad_trg": {
								Name:       "bad_trg",
								Definition: "CREATE TRIGGER bad_trg BEFORE INSERT ON orders FOR EACH ROW EXECUTE FUNCTION missing_fn()",
							},
						},
					},
				},
				Routines: map[string]*schema.Routine{
					"ok_fn()": {Name: "ok_fn"},
				},
			},
			wantDiags:   1,
			wantTable:   "orders",
			wantCol:     "bad_trg",
			wantMessage: `trigger executes function "missing_fn" which is not defined in the desired schema`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diags := lint.Lint(tt.schema, lint.TriggerWithoutFunction{})
			if len(diags) != tt.wantDiags {
				t.Fatalf("expected %d diagnostics, got %d: %+v", tt.wantDiags, len(diags), diags)
			}
			if tt.wantDiags > 0 {
				d := diags[0]
				if d.RuleID != "L010" {
					t.Errorf("expected RuleID L010, got %s", d.RuleID)
				}
				if d.Severity != lint.SeverityWarning {
					t.Errorf("expected SeverityWarning, got %s", d.Severity)
				}
				if d.Table != tt.wantTable {
					t.Errorf("expected Table %q, got %q", tt.wantTable, d.Table)
				}
				if d.Column != tt.wantCol {
					t.Errorf("expected Column %q, got %q", tt.wantCol, d.Column)
				}
				if d.Message != tt.wantMessage {
					t.Errorf("expected Message %q, got %q", tt.wantMessage, d.Message)
				}
			}
		})
	}
}
