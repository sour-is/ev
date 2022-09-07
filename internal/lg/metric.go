package lg

import (
	"context"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/sdk/metric/aggregator/histogram"
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
func NewHTTP(ctx context.Context) *httpHandle {
	t := fromContext[contextKey, *prometheus.Exporter](ctx, promHTTPKey)
	return &httpHandle{t}
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

	config := prometheus.Config{
		DefaultHistogramBoundaries: []float64{
			2 << 6, 2 << 8, 2 << 10, 2 << 12, 2 << 14, 2 << 16, 2 << 18, 2 << 20, 2 << 22, 2 << 24, 2 << 26, 2 << 28,
		},
	}
	cont := controller.New(
		processor.NewFactory(
			selector.NewWithHistogramDistribution(
				histogram.WithExplicitBoundaries(config.DefaultHistogramBoundaries),
			),
			aggregation.CumulativeTemporalitySelector(),
			processor.WithMemory(true),
		),
		controller.WithResource(
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

type httpHandle struct {
	exp *prometheus.Exporter
}

func (h *httpHandle) RegisterHTTP(mux *http.ServeMux) {
	if h.exp == nil {
		return
	}
	mux.Handle("/metrics", h.exp)
}
