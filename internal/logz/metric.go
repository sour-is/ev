package logz

import (
	"context"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	// metricsExporter "github.com/logzio/go-metrics-sdk"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric/aggregator/histogram"
	"go.opentelemetry.io/otel/sdk/metric/controller/basic"
	controller "go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/metric/export/aggregation"
	processor "go.opentelemetry.io/otel/sdk/metric/processor/basic"
	selector "go.opentelemetry.io/otel/sdk/metric/selector/simple"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
)

var meterKey = contextKey{"meter"}
var promHTTPKey = contextKey{"promHTTP"}

func Meter(ctx context.Context) metric.Meter {
	if t := fromContext[contextKey, metric.Meter](ctx, tracerKey); t != nil {
		return t
	}
	return global.Meter("")
}
func PromHTTP(ctx context.Context) http.Handler {
	if t := fromContext[contextKey, *prometheus.Exporter](ctx, promHTTPKey); t != nil {
		return t
	}
	return http.NotFoundHandler()
}

func initMetrics(ctx context.Context, name string) (context.Context, func() error) {
	goversion := ""
	pkg := ""
	host := ""
	if info, ok := debug.ReadBuildInfo(); ok {
		goversion = info.GoVersion
		pkg = info.Path
	}
	if h, err := os.Hostname(); err == nil {
		host = h
	}

	config := prometheus.Config{}
	cont := controller.New(
		processor.NewFactory(
			selector.NewWithHistogramDistribution(
				histogram.WithExplicitBoundaries(config.DefaultHistogramBoundaries),
			),
			aggregation.CumulativeTemporalitySelector(),
			processor.WithMemory(true),
		),
		basic.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				attribute.String("app", name),
				attribute.String("host", host),
				attribute.String("go_version", goversion),
				attribute.String("pkg", pkg),
			),
		),
	)
	ex, err := prometheus.New(config, cont)
	if err != nil {
		return ctx, nil
	}

	ctx = toContext(ctx, promHTTPKey, ex)

	global.SetMeterProvider(cont)
	m := cont.Meter(name)
	ctx = toContext(ctx, meterKey, m)
	runtime.Start()

	return ctx, func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		defer log.Println("metrics stopped")
		return cont.Stop(ctx)
	}
}
