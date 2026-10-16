# Observability in Grizzle

Grizzle provides first-class observability for production schema migrations via OpenTelemetry tracing, metrics, and zero-side-effect readiness probes.

---

## 1. Architectural Boundary

To keep Grizzle lightweight and embeddable, the core library (`github.com/muandane/grizzle`) has **zero OpenTelemetry dependencies** (standard library only). All OpenTelemetry adapters live in an isolated subpackage module:

```
github.com/muandane/grizzle/otelgrizzle
```

Install it alongside the core module:

```bash
go get github.com/muandane/grizzle/otelgrizzle@v0.2.0
```

Applications that do not use OpenTelemetry never download, compile, or link any OpenTelemetry packages.

```
┌────────────────────────────────────────────────────────┐
│  Application Code                                      │
└───────────────┬────────────────────────┬───────────────┘
                │                        │
                ▼                        ▼
     ┌──────────────────────┐  ┌───────────────────┐
     │  otelgrizzle module  │  │  grizzle (Core)   │
     │  (OpenTelemetry SDK) │  │  (Zero OTel deps) │
     └──────────┬───────────┘  └─────────▲─────────┘
                │                        │
                └────── implements ──────┘
                   grizzle.Tracer &
                   grizzle.Metrics
```

---

## 2. Distributed Tracing (`otelgrizzle.WithTracer`)

Grizzle emits structured OpenTelemetry spans during the migration lifecycle.

### Spans Emitted

| Span Name | Description | Attributes / Details |
| :--- | :--- | :--- |
| `grizzle.diff_plan` | Schema compilation and diff calculation against live state. | Measures shadow schema compilation and diffing. |
| `grizzle.acquire_lock` | Advisory lock acquisition window. | Duration waiting for session-level advisory locks; records errors if acquisition times out. |
| `grizzle.exec_step` | Execution of an individual DDL step. | `step_index`, `type` (e.g. `ADD_COLUMN`), `table`, `sql`, `is_non_tx`. |

### Tracing Example

```go
package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/otelgrizzle"
	"go.opentelemetry.io/otel"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/dbname?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Obtain Tracer from your application's TracerProvider
	tracer := otel.GetTracerProvider().Tracer("db-migrations")

	opts := grizzle.Options{
		Dialect:      grizzle.DialectPostgres,
		SchemaSQL:    "CREATE TABLE customers (id BIGINT PRIMARY KEY, name TEXT NOT NULL);",
	}

	// Attach OpenTelemetry tracer
	otelgrizzle.WithTracer(tracer)(&opts)

	if err := grizzle.Sync(ctx, db, opts); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}
```

---

## 3. Metrics (`otelgrizzle.WithMetrics`)

Grizzle exposes golden migration metrics using OpenTelemetry instruments.

### Metric Catalog

| Metric Name | Instrument | Unit | Description | Attributes |
| :--- | :--- | :--- | :--- | :--- |
| `migration_duration_seconds` | `Float64Histogram` | `s` | End-to-end execution duration of schema migrations. | `dialect` ("postgres", "sqlite"), `status` ("applied", "failed", "noop") |
| `drift_detected` | `Int64Counter` | `{events}` | Count of schema drift and plan hash mismatch events. | `dialect` ("postgres", "sqlite"), `schema` |
| `lock_wait_seconds` | `Float64Histogram` | `s` | Duration spent waiting for or acquiring database locks. | `dialect` ("postgres", "sqlite") |

### Metrics Example

```go
package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/otelgrizzle"
	"go.opentelemetry.io/otel"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	ctx := context.Background()
	db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/dbname?sslmode=disable")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Obtain Meter from your application's MeterProvider
	meter := otel.GetMeterProvider().Meter("db-migrations")

	metrics, err := otelgrizzle.NewMetrics(meter)
	if err != nil {
		log.Fatalf("failed to create otel metrics: %v", err)
	}

	opts := grizzle.Options{
		Dialect:   grizzle.DialectPostgres,
		SchemaSQL: "CREATE TABLE orders (id BIGINT PRIMARY KEY, amount NUMERIC(10, 2));",
	}

	// Attach OpenTelemetry metrics
	otelgrizzle.WithMetrics(metrics)(&opts)

	if err := grizzle.Sync(ctx, db, opts); err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}
```

---

## 4. Zero-Lock Readiness Probes (`grizzle.IsReady`)

For containerized environments (Kubernetes readiness probes, ECS health checks, systemd health probes), Grizzle provides `grizzle.IsReady`.

### Readiness Invariant

`grizzle.IsReady(ctx, db, opts...)`:
- **Zero locks**: Does not acquire PostgreSQL advisory locks or table locks.
- **Zero side effects**: Does not run DDL and never creates `grizzle_history`.
- **Pure query**: Runs a single `SELECT ... FROM grizzle_history ORDER BY id DESC LIMIT 1`.

### Evaluation Rules

| Database History State | `IsReady` Result | Meaning |
| :--- | :--- | :--- |
| `grizzle_history` table does not exist | `(false, nil)` | Brand new database, migrations have not run yet. |
| `grizzle_history` table exists but is empty | `(false, nil)` | Database unmigrated. |
| Latest record has `status = "applied"` | `(true, nil)` | Database is ready for traffic. |
| Latest record has `status = "baseline"` | `(true, nil)` | Database baseline recorded, ready for traffic. |
| Latest record has `status = "failed"` or `"partial"` | `(false, nil)` | Prior migration failed; pod should NOT receive traffic. |
| Connection dropped / context canceled | `(false, err)` | Database unreachable. |

### Kubernetes Readiness Probe Example

```go
package main

import (
	"context"
	"database/sql"
	"net/http"

	"github.com/muandane/grizzle"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func readinessHandler(db *sql.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		// Checks migration status without acquiring locks or executing DDL
		ready, err := grizzle.IsReady(ctx, db)
		if err != nil {
			http.Error(w, "database unreachable: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		if !ready {
			http.Error(w, "database migrations pending or failed", http.StatusServiceUnavailable)
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ready\n"))
	}
}

func main() {
	db, err := sql.Open("pgx", "postgres://user:pass@localhost:5432/dbname?sslmode=disable")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	http.HandleFunc("/readyz", readinessHandler(db))
	_ = http.ListenAndServe(":8080", nil)
}
```

---

## 5. Combined Setup (Tracing + Metrics + Readiness)

```go
package main

import (
	"context"
	"database/sql"
	"log"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/otelgrizzle"
	"go.opentelemetry.io/otel"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func runMigrations(ctx context.Context, db *sql.DB, schemaSQL string) error {
	tracer := otel.GetTracerProvider().Tracer("grizzle")
	meter := otel.GetMeterProvider().Meter("grizzle")

	metrics, err := otelgrizzle.NewMetrics(meter)
	if err != nil {
		return err
	}

	opts := grizzle.Options{
		Dialect:   grizzle.DialectPostgres,
		SchemaSQL: schemaSQL,
	}
	otelgrizzle.WithTracer(tracer)(&opts)
	otelgrizzle.WithMetrics(metrics)(&opts)

	return grizzle.Sync(ctx, db, opts)
}
```
