package otelgrizzle_test

import (
	"context"
	"database/sql"
	"log"

	"github.com/muandane/grizzle"
	"github.com/muandane/grizzle/otelgrizzle"
	"go.opentelemetry.io/otel"
	_ "modernc.org/sqlite"
)

// Example demonstrates configuring Grizzle with both OpenTelemetry tracing and metrics.
func Example() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	// Retrieve OpenTelemetry tracer and meter from global or custom providers
	tracer := otel.GetTracerProvider().Tracer("grizzle-app")
	meter := otel.GetMeterProvider().Meter("grizzle-app")

	metrics, err := otelgrizzle.NewMetrics(meter)
	if err != nil {
		log.Fatal(err)
	}

	opts := grizzle.Options{
		Dialect:   grizzle.DialectSQLite,
		SchemaSQL: "CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL);",
	}

	// Apply both OpenTelemetry tracing and metrics options
	otelgrizzle.WithTracer(tracer)(&opts)
	otelgrizzle.WithMetrics(metrics)(&opts)

	if err := grizzle.Sync(ctx, db, opts); err != nil {
		log.Fatal(err)
	}
}

// ExampleWithTracer demonstrates configuring Grizzle with an OpenTelemetry trace.Tracer.
func ExampleWithTracer() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	tracer := otel.GetTracerProvider().Tracer("my-service")

	opts := grizzle.Options{
		Dialect:   grizzle.DialectSQLite,
		SchemaSQL: "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);",
	}
	otelgrizzle.WithTracer(tracer)(&opts)

	_ = grizzle.Sync(ctx, db, opts)
}

// ExampleWithMetrics demonstrates configuring Grizzle with OpenTelemetry metrics.
func ExampleWithMetrics() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = db.Close() }()

	meter := otel.GetMeterProvider().Meter("my-service")
	metrics, err := otelgrizzle.NewMetrics(meter)
	if err != nil {
		log.Fatal(err)
	}

	opts := grizzle.Options{
		Dialect:   grizzle.DialectSQLite,
		SchemaSQL: "CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT);",
	}
	otelgrizzle.WithMetrics(metrics)(&opts)

	_ = grizzle.Sync(ctx, db, opts)
}
