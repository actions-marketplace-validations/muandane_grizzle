// Package otelgrizzle provides OpenTelemetry tracing and metrics instrumentation adapters for Grizzle.
//
// By isolating these adapters in a separate subpackage module, applications not using OpenTelemetry
// avoid downloading or compiling any OpenTelemetry dependencies.
//
// # Tracing Usage
//
// Pass WithTracer with an OpenTelemetry trace.Tracer to grizzle.Sync or grizzle.PlanDiff:
//
//	tp := otel.GetTracerProvider()
//	tracer := tp.Tracer("my-service")
//
//	err := grizzle.Sync(ctx, db, grizzle.Options{
//	    SchemaSQL: schemaSQL,
//	}, otelgrizzle.WithTracer(tracer))
//
// # Metrics Usage
//
// Pass WithMetrics with an adapter created from an OpenTelemetry metric.Meter:
//
//	mp := otel.GetMeterProvider()
//	meter := mp.Meter("my-service")
//	metrics, err := otelgrizzle.NewMetrics(meter)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	err = grizzle.Sync(ctx, db, grizzle.Options{
//	    SchemaSQL: schemaSQL,
//	}, otelgrizzle.WithMetrics(metrics))
package otelgrizzle
