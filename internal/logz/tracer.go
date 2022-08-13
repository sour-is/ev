package logz

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

type contextKey struct {
	name string
}

var tracerKey = contextKey{"tracer"}

func Tracer(ctx context.Context) trace.Tracer {
	if t := fromContext[contextKey, trace.Tracer](ctx, tracerKey); t != nil {
		return t
	}
	return otel.Tracer("")
}

func Span(ctx context.Context, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	var attrs []attribute.KeyValue
	var name string
	if pc, file, line, ok := runtime.Caller(1); ok {
		if fn := runtime.FuncForPC(pc); fn != nil {
			name = fn.Name()
		}
		attrs = append(attrs,
			attribute.String("pc", fmt.Sprintf("%v", pc)),
			attribute.String("file", file),
			attribute.Int("line", line),
			attribute.String("name", name),
		)
	}
	ctx, span := Tracer(ctx).Start(ctx, name, opts...)
	span.SetAttributes(attrs...)

	return ctx, span
}

func initTracing(ctx context.Context, name string) (context.Context, func() error) {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(name),
		),
	)
	if err != nil {
		log.Println(wrap(err, "failed to create trace resource"))
		return ctx, nil
	}

	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("localhost:4318"),
	)
	if err != nil {
		log.Println(wrap(err, "failed to create trace exporter"))
		return ctx, nil
	}
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)

	var sample sdktrace.TracerProviderOption
	sampleRate := env("LOGZIO_TRACE_SAMPLE", "always")
	switch sampleRate {
	case "always":
		sample = sdktrace.WithSampler(sdktrace.AlwaysSample())
	case "never":
		sample = sdktrace.WithSampler(sdktrace.NeverSample())
	default:
		if v, err := strconv.Atoi(sampleRate); err != nil {
			sample = sdktrace.WithSampler(sdktrace.AlwaysSample())
		} else {
			sample = sdktrace.WithSampler(sdktrace.TraceIDRatioBased(float64(v) * 0.01))
		}
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sample,
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	ctx = toContext(ctx, tracerKey, otel.Tracer(name))

	return ctx, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		defer log.Println("tracer stopped")
		return wrap(tracerProvider.Shutdown(ctx), "failed to shutdown TracerProvider")
	}
}

func wrap(err error, s string) error {
	if err != nil {
		return fmt.Errorf(s, err)
	}
	return nil
}
func reverse[T any](s []T) {
	first, last := 0, len(s)-1
	for first < last {
		s[first], s[last] = s[last], s[first]
		first++
		last--
	}
}
func env(name, defaultValue string) string {
	if v := os.Getenv(name); v != "" {
		log.Println("# ", name, " = ", v)
		return v
	}
	return defaultValue
}

func Htrace(h http.Handler, name string) http.Handler {
	return otelhttp.NewHandler(h, name)
}

func toContext[K comparable, V any](ctx context.Context, key K, value V) context.Context {
	return context.WithValue(ctx, key, value)
}
func fromContext[K comparable, V any](ctx context.Context, key K) V {
	var empty V
	if v, ok := ctx.Value(key).(V); ok {
		return v
	}
	return empty
}
