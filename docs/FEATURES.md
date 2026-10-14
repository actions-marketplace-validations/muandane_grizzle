# Supported features

This document lists the schema objects, DDL capabilities, and safety guards supported by Grizzle for PostgreSQL and SQLite.

For catalog query definitions, hazard taxonomy, and engine internals, see [docs/SPEC.md](SPEC.md). For data safety rules and drop controls, see [docs/SAFETY.md](SAFETY.md).

## PostgreSQL (14 to 18)

| Category | Supported capabilities | Safety and scope guards |
| :--- | :--- | :--- |
| **Tables & Columns** | Table creation and drop, column add/drop/rename, type alters (`USING`), nullability, defaults, generated columns, partitioned tables (declarative attach and detach). | Identity and serial sequences preserved; unmanaged tables outside target schemas untouched. |
| **Indexes & Constraints** | `CREATE INDEX CONCURRENTLY`, partial, unique, expression, BRIN, GIN, and GiST indexes; foreign keys (`NOT VALID` plus `VALIDATE CONSTRAINT`), `CHECK` constraints (staged validation, normalized expressions). | Backing indexes for primary keys and unique constraints managed via constraint definition. |
| **Data Types** | Enums (`ADD VALUE`, `RENAME VALUE`), domains (`CHECK` constraints), custom range types, composite types (`ADD`/`DROP ATTRIBUTE`). | System and extension types preserved; dropped enum values flagged as critical hazards. |
| **Sequences** | Standalone sequences, parameters (`INCREMENT`, `MIN`/`MAXVALUE`, `CACHE`, `CYCLE`), ownership (`OWNED BY table.col`), `DROP SEQUENCE IF EXISTS`. | Implicit identity/serial sequences and unmanaged external sequences are preserved. |
| **Routines & Triggers** | Functions and procedures (`CREATE OR REPLACE`, signature drift drop/recreate), per-table triggers, user-defined aggregates. | Built-in and extension functions untouched; window functions and hypothetical aggregates preserved. |
| **Views** | Standard views (`CREATE OR REPLACE` for append-only, drop/recreate for structural alters), materialized views (drop/recreate plus refresh). | System catalogs and unmanaged views untouched. |
| **Security & Access** | Row-Level Security (`ENABLE`/`FORCE`), RLS policies (`CREATE`/`ALTER`/`DROP POLICY`), roles and grants (via `RolesSQL` / `SchemaSQL`). | Unmanaged roles and `PUBLIC` grants preserved; marker comment `grizzle-managed` required to drop roles. |
| **Cluster & Replication** | Extensions (`CREATE EXTENSION`), logical publications, subscriptions, replication slots, DDL event triggers. | Physical slots and unmanaged publications/triggers without `grizzle-managed` marker are untouched. |

## SQLite (3.35+)

- **Schema constructs:** Tables, columns, foreign keys, `CHECK` constraints, indexes, and generated columns (`STORED` and `VIRTUAL`).
- **Table rebuild engine:** Schema changes that SQLite cannot execute in place with `ALTER TABLE` run through an atomic 12-step table rebuild (temporary table creation, batch data copying, constraint verification via `PRAGMA foreign_key_check`, index and trigger restoration).
- **Driver compatibility:** Works with both `modernc.org/sqlite` (pure Go, zero CGo) and `github.com/mattn/go-sqlite3` (CGo).
- **Unsupported features:** SQLite does not support `COMMENT ON` or Row-Level Security.
