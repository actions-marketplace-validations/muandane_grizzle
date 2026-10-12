# Grizzle

[![CI](https://github.com/muandane/grizzle/actions/workflows/ci.yml/badge.svg)](https://github.com/muandane/grizzle/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/muandane/grizzle.svg)](https://pkg.go.dev/github.com/muandane/grizzle)
[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

Declarative schema migrations for Go.

Point Grizzle at your plain `schema.sql`, and it diffs and migrates your database on boot. No numbered migration files (`001_init.sql`), no external migration binaries inside your production containers.

- **Safe by default:** Destructive drops are blocked unless explicitly allowed.
- **Production-safe DDL:** Builds PostgreSQL indexes `CONCURRENTLY` and creates foreign keys as `NOT VALID` before validating.
- **Cluster-safe:** Uses PostgreSQL session advisory locks so multi-pod deployments won't collide.
- **Engine support:** PostgreSQL (14–18) and SQLite (3.35+, pure-Go or CGo).
- **Works with sqlc:** Share one `schema.sql` between sqlc and Grizzle.

---

## Quickstart

Embed your desired schema and run `Sync` on application startup:

```sql
-- schema.sql
CREATE TABLE users (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    email      VARCHAR(255) NOT NULL UNIQUE,
    full_name  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_created_at ON users (created_at);
```

> **Note:** Grizzle supports both `SERIAL` and standard-compliant identity columns, but recommends `GENERATED ALWAYS AS IDENTITY` for new tables (see [SAFETY.md](docs/SAFETY.md)).


```go
package main

import (
	"context"
	"database/sql"
	_ "embed"
	"log"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/muandane/grizzle"
)

//go:embed schema.sql
var schemaSQL string

func main() {
	db, err := sql.Open("pgx", "postgres://postgres:secret@localhost:5432/myapp?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Bring the database in line with schema.sql
	err = grizzle.Sync(context.Background(), db, grizzle.Options{
		SchemaSQL:        schemaSQL,
		AllowDrop:        false, // Fails if the diff would drop columns or tables
		LockTimeout:      5 * time.Second,
		StatementTimeout: 2 * time.Minute,
	})
	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}
```

> **Need approval gates?** Instead of auto-syncing on boot, use `grizzle.PlanDiff(...)` to generate a migration plan, review the SQL, then `grizzle.Apply(..., grizzle.ApplyOpts{ExpectedHash: plan.Hash()})` so apply aborts if the live database drifted.

---

## CLI

Use the CLI for CI drift checks, dry runs, or exporting plans to standard migration tools:

```bash
# Preview changes and generate a migration plan
grizzle plan --dsn "$DATABASE_URL" --schema schema.sql --out plan.json

# Apply a generated plan safely
grizzle apply --dsn "$DATABASE_URL" --plan plan.json

# Fail CI if live DB has drifted from schema.sql (exits with code 4)
grizzle check --dsn "$DATABASE_URL" --schema schema.sql

# Convert a plan to Goose migrations if you need an exit hatch
grizzle export --plan plan.json --format goose --out ./migrations
```

### Exit Codes

| Code | Meaning |
| --- | --- |
| `0` | Success / database is in sync |
| `1` | Connection or execution error |
| `2` | Blocked by an unaccepted hazard (e.g., unexpected DROP) |
| `3` | Plan hash mismatch (live database changed after plan was generated) |
| `4` | Schema drift detected (`grizzle check`) |

---

## Supported Features

### PostgreSQL (14–18)

| Object Category | Managed Constructs (Declarative Lifecycle) | Protected-Only / Invariant |
| :--- | :--- | :--- |
| **Tables & Columns** | `CREATE`/`DROP TABLE`, `ADD`/`DROP`/`RENAME COLUMN`, type conversions (`USING`), nullability, defaults, generated columns (`STORED`), partitioned tables (declarative attach/detach). | Identity/serial sequences (`deptype = 'i'`) preserved; full validation scans flagged; unmanaged tables outside scope untouched. |
| **Indexes & Constraints** | `CREATE INDEX CONCURRENTLY`, partial/unique/expression/BRIN/GIN/GiST indexes; Foreign keys (`NOT VALID` + `VALIDATE`), `CHECK` constraints (staged validation, normalized expressions). | Primary key / unique constraint backing indexes are managed via table/constraint definition. |
| **Data Types** | Enums (`CREATE`/`ADD VALUE`/`RENAME VALUE`), Domains (`CHECK` constraint alteration), Custom Range Types (`CREATE`/`DROP`), Composite Types (`CREATE`/`ALTER ATTRIBUTE`/`DROP`). | System types; extension-provided types (`deptype = 'e'`); dropped enum labels emit `DROP_ENUM_VALUE` critical hazard. |
| **Sequences** | Standalone `CREATE SEQUENCE` declared in schema SQL, parameters (`INCREMENT`, `MIN/MAXVALUE`, `CACHE`, `CYCLE`), ownership (`OWNED BY table.col` vs `NONE`), `DROP SEQUENCE IF EXISTS`. | Implicit identity/serial sequences (`deptype = 'i'`); external unmanaged sequences are never auto-dropped. |
| **Routines & Triggers** | Functions & Procedures (`CREATE OR REPLACE`, signature drift drop/recreate), Triggers (per-table trigger defs), User-defined aggregates (`CREATE AGGREGATE`). | Built-in routines; extension-owned functions (`deptype = 'e'`); window functions (`prokind = 'w'`); ordered-set / hypothetical aggregates (`aggkind != 'd'`). |
| **Views** | Views (`CREATE OR REPLACE` for append-only columns, drop/create for structural alterations), Materialized Views (drop/recreate + refresh). | System catalogs / unmanaged views. |
| **Security & Access** | Row-Level Security (`ENABLE`/`FORCE`), RLS Policies (in-place `ALTER POLICY` for roles/USING/WITH CHECK, drop/create for command), Roles & Grants (via `RolesSQL` / `SchemaSQL`). | Unmanaged roles; grants to `PUBLIC` or unmanaged roles are never revoked; marker comment `grizzle-managed` required to drop roles. |
| **Cluster & Replication** | Extensions (`CREATE EXTENSION`), Logical Publications, Subscriptions, Logical Replication Slots, DDL Event Triggers (via `CatalogSQL` / `SchemaSQL`). | Physical replication slots; unmanaged cluster triggers/publications without `grizzle-managed` marker comment. |

> [!NOTE]
> **API & Hash Stability:** `Options.Renames`, `Options.ExpandContract`, and `Options.Backfill` API shapes and `Plan.Hash()` inputs are frozen across the `rc` line (stable, no longer experimental). Plan hashes remain strictly deterministic across versions. Full catalog queries, hazard taxonomy, and engine details are documented in [docs/SPEC.md](docs/SPEC.md#3-supported-schema-constructs).

### SQLite (3.35+)

* Tables, columns, foreign keys, CHECK constraints, and generated columns.
* Automatic 12-step table rebuilds for schema changes SQLite cannot execute in-place via `ALTER TABLE`.
* Supports both `modernc.org/sqlite` (pure Go) and `mattn/go-sqlite3` (CGo).
* SQLite does not support `COMMENT ON` or Row-Level Security (`ENABLE ROW LEVEL SECURITY`, `CREATE POLICY`); comments are ignored (no catalog metadata) and RLS is PostgreSQL-only.

---

## Important Operational Notes

* **PgBouncer / Connection Poolers:** Grizzle relies on session-level advisory locks to prevent concurrent migrations. Connect directly to Postgres or use **Session Pooling**. Do not run migrations through **Transaction Pooling**.
* **Destructive Changes:** Dropping tables/columns requires `AllowDrop: true` (or `--allow-drop` in the CLI). See [docs/SAFETY.md](docs/SAFETY.md) for full hazard handling.

---

## Examples

* [postgres-stdlib](examples/postgres-stdlib) — `database/sql` + pgx boot sync
* [sqlite-embedded](examples/sqlite-embedded) — pure-Go SQLite setup
* [sqlc-workflow](examples/sqlc-workflow) — single source of truth: share `schema.sql` between sqlc and Grizzle
* [kubernetes-blueprint](examples/kubernetes-blueprint) — multi-replica startup coordination
* [expand-contract](examples/expand-contract) — expand / backfill / contract column rename
* [roles-grants](examples/roles-grants) / [catalog-sync](examples/catalog-sync) — roles and publications via optional SQL files
* [full-catalog](examples/full-catalog) — extensions, RLS, functions, triggers, and views in one `schema.sql`

## Documentation

* [SPEC](docs/SPEC.md) — Supported SQL constructs and engine details
* [SAFETY](docs/SAFETY.md) — Hazards, data safety rules, and drop policies
* [ARCHITECTURE](docs/ARCHITECTURE.md) — Internal planner and execution flow
* [CONTRIBUTING](CONTRIBUTING.md) — devenv, hooks, PR checklist
* [CHANGELOG](CHANGELOG.md)

Current tag: **v0.1.0-rc2**.

## License

Apache 2.0. See [LICENSE](LICENSE).
