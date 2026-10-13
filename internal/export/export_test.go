package export_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/muandane/grizzle/internal/export"
	"github.com/muandane/grizzle/internal/plan"
)

func TestExport_SQL_Format(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "users",
				SQL:   "CREATE TABLE users (id integer PRIMARY KEY, name text NOT NULL);",
			},
			{
				Type:  plan.ChangeCreateIndex,
				Table: "users",
				SQL:   "CREATE INDEX CONCURRENTLY idx_users_name ON users (name);",
				NonTx: true,
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatSQL, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export sql failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	art := artifacts[0]
	goldenPath := filepath.Join("..", "..", "testdata", "export", "sql.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenPath), 0750)
	expected, err := os.ReadFile(goldenPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenPath, []byte(art.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expected = []byte(art.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if art.Content != string(expected) {
		t.Errorf("exported SQL content mismatch:\ngot:\n%s\nwant:\n%s", art.Content, string(expected))
	}
}

func TestExport_Goose_Reversible(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "accounts",
				SQL:   "CREATE TABLE accounts (id integer PRIMARY KEY);",
			},
			{
				Type:   plan.ChangeAddColumn,
				Table:  "accounts",
				Column: "balance",
				SQL:    "ALTER TABLE accounts ADD COLUMN balance numeric NOT NULL DEFAULT 0;",
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatGoose, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export goose failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	art := artifacts[0]
	goldenPath := filepath.Join("..", "..", "testdata", "export", "goose_reversible.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenPath), 0750)
	expected, err := os.ReadFile(goldenPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenPath, []byte(art.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expected = []byte(art.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if art.Content != string(expected) {
		t.Errorf("exported Goose reversible mismatch:\ngot:\n%s\nwant:\n%s", art.Content, string(expected))
	}
}

func TestExport_Goose_Irreversible(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:        plan.ChangeDropTable,
				Table:       "legacy_users",
				SQL:         "DROP TABLE legacy_users CASCADE;",
				Destructive: true,
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatGoose, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export goose failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	art := artifacts[0]
	goldenPath := filepath.Join("..", "..", "testdata", "export", "goose_irreversible.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenPath), 0750)
	expected, err := os.ReadFile(goldenPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenPath, []byte(art.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expected = []byte(art.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if art.Content != string(expected) {
		t.Errorf("exported Goose irreversible mismatch:\ngot:\n%s\nwant:\n%s", art.Content, string(expected))
	}
}

func TestExport_Atlas_Format(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "orders",
				SQL:   "CREATE TABLE orders (id integer PRIMARY KEY);",
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatAtlas, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export atlas failed: %v", err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(artifacts))
	}

	art := artifacts[0]
	goldenPath := filepath.Join("..", "..", "testdata", "export", "atlas.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenPath), 0750)
	expected, err := os.ReadFile(goldenPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenPath, []byte(art.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expected = []byte(art.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if art.Content != string(expected) {
		t.Errorf("exported Atlas mismatch:\ngot:\n%s\nwant:\n%s", art.Content, string(expected))
	}
}

func TestExport_Migrate_Reversible(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "accounts",
				SQL:   "CREATE TABLE accounts (id integer PRIMARY KEY);",
			},
			{
				Type:   plan.ChangeAddColumn,
				Table:  "accounts",
				Column: "balance",
				SQL:    "ALTER TABLE accounts ADD COLUMN balance numeric NOT NULL DEFAULT 0;",
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatMigrate, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export migrate failed: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}

	upArt := artifacts[0]
	if upArt.Filename != "20260101120000_migration.up.sql" {
		t.Errorf("expected up filename '20260101120000_migration.up.sql', got %q", upArt.Filename)
	}
	downArt := artifacts[1]
	if downArt.Filename != "20260101120000_migration.down.sql" {
		t.Errorf("expected down filename '20260101120000_migration.down.sql', got %q", downArt.Filename)
	}

	goldenUpPath := filepath.Join("..", "..", "testdata", "export", "migrate_up.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenUpPath), 0750)
	expectedUp, err := os.ReadFile(goldenUpPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenUpPath, []byte(upArt.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expectedUp = []byte(upArt.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if upArt.Content != string(expectedUp) {
		t.Errorf("exported Migrate up mismatch:\ngot:\n%s\nwant:\n%s", upArt.Content, string(expectedUp))
	}

	goldenDownPath := filepath.Join("..", "..", "testdata", "export", "migrate_down_reversible.golden.sql")
	expectedDown, err := os.ReadFile(goldenDownPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenDownPath, []byte(downArt.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expectedDown = []byte(downArt.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if downArt.Content != string(expectedDown) {
		t.Errorf("exported Migrate down mismatch:\ngot:\n%s\nwant:\n%s", downArt.Content, string(expectedDown))
	}
}

func TestExport_Migrate_Irreversible(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:        plan.ChangeDropTable,
				Table:       "legacy_users",
				SQL:         "DROP TABLE legacy_users CASCADE;",
				Destructive: true,
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	artifacts, err := export.Export(p, export.FormatMigrate, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export migrate failed: %v", err)
	}
	if len(artifacts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(artifacts))
	}

	downArt := artifacts[1]
	goldenDownPath := filepath.Join("..", "..", "testdata", "export", "migrate_down_irreversible.golden.sql")
	_ = os.MkdirAll(filepath.Dir(goldenDownPath), 0750)
	expectedDown, err := os.ReadFile(goldenDownPath) //nolint:gosec // G304: test reads static testdata golden file
	if os.IsNotExist(err) {
		if err := os.WriteFile(goldenDownPath, []byte(downArt.Content), 0600); err != nil {
			t.Fatalf("writing golden file: %v", err)
		}
		expectedDown = []byte(downArt.Content)
	} else if err != nil {
		t.Fatalf("reading golden file: %v", err)
	}

	if downArt.Content != string(expectedDown) {
		t.Errorf("exported Migrate down irreversible mismatch:\ngot:\n%s\nwant:\n%s", downArt.Content, string(expectedDown))
	}
}

func TestExport_Migrate_Alias(t *testing.T) {
	t.Parallel()

	p := &plan.Plan{
		TargetSchema: "public",
		Steps: []plan.Step{
			{
				Type:  plan.ChangeCreateTable,
				Table: "items",
				SQL:   "CREATE TABLE items (id integer PRIMARY KEY);",
			},
		},
	}

	fixedTime := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	arts1, err := export.Export(p, export.FormatMigrate, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export migrate failed: %v", err)
	}
	arts2, err := export.Export(p, export.FormatGolangMigrate, "v0.1.0", fixedTime)
	if err != nil {
		t.Fatalf("export golang-migrate failed: %v", err)
	}

	if len(arts1) != len(arts2) {
		t.Fatalf("expected same number of artifacts, got %d vs %d", len(arts1), len(arts2))
	}
	for i := range arts1 {
		if arts1[i].Filename != arts2[i].Filename || arts1[i].Content != arts2[i].Content {
			t.Errorf("artifact %d mismatch between migrate and golang-migrate", i)
		}
	}
}
