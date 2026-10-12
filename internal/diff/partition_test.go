package diff_test

import (
	"testing"

	"github.com/muandane/grizzle/internal/diff"
	"github.com/muandane/grizzle/internal/plan"
	"github.com/muandane/grizzle/internal/schema"
	"github.com/muandane/grizzle/internal/scope"
)

func TestDiff_Partition_AttachAndPendingDetach(t *testing.T) {
	live := &schema.Schema{
		Tables: map[string]*schema.Table{
			"orders": {
				Name: "orders",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
				PartitionKey: &schema.PartitionKey{
					Strategy: schema.PartitionStrategyRange,
					Def:      "orders (order_date)",
				},
			},
			"orders_2025": {
				Name: "orders_2025",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
				PartitionOf: &schema.PartitionOf{
					Parent:          "orders",
					Bounds:          "FROM ('2025-01-01') TO ('2026-01-01')",
					IsDetachPending: true,
				},
			},
			"orders_2026": {
				Name: "orders_2026",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
			},
		},
		Enums: make(map[string]*schema.Enum),
	}

	desired := &schema.Schema{
		Tables: map[string]*schema.Table{
			"orders": {
				Name: "orders",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
				PartitionKey: &schema.PartitionKey{
					Strategy: schema.PartitionStrategyRange,
					Def:      "orders (order_date)",
				},
			},
			"orders_2025": {
				Name: "orders_2025",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
			},
			"orders_2026": {
				Name: "orders_2026",
				Columns: map[string]*schema.Column{
					"id":         {Name: "id", DataType: "bigint", IsNullable: false},
					"order_date": {Name: "order_date", DataType: "date", IsNullable: false},
				},
				PartitionOf: &schema.PartitionOf{
					Parent: "orders",
					Bounds: "FROM ('2026-01-01') TO ('2027-01-01')",
				},
			},
		},
		Enums: make(map[string]*schema.Enum),
	}

	changes, err := diff.Diff(live, desired, "public", "", scope.Filters{})
	if err != nil {
		t.Fatalf("diff failed: %v", err)
	}

	var foundAttach, foundDetach bool
	for _, c := range changes {
		if c.Type == plan.ChangeAttachPartition && c.Table == "orders_2026" && c.ParentTable == "orders" {
			foundAttach = true
			if c.Destructive {
				t.Errorf("attach partition must not be marked destructive")
			}
		}
		if c.Type == plan.ChangeDetachPartition && c.Table == "orders_2025" && c.ParentTable == "orders" {
			foundDetach = true
			if !c.IsPendingDetach {
				t.Errorf("expected IsPendingDetach to be true on detach change")
			}
		}
	}

	if !foundAttach {
		t.Errorf("expected ChangeAttachPartition in changes: %+v", changes)
	}
	if !foundDetach {
		t.Errorf("expected ChangeDetachPartition in changes: %+v", changes)
	}
}
