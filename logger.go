package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	metricsExporter "github.com/logzio/go-metrics-sdk"
	"github.com/logzio/logzio-go"
	"go.uber.org/multierr"

	"github.com/go-logr/stdr"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric/global"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric/controller/basic"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
	"go.opentelemetry.io/otel/trace"
)

var tracer trace.Tracer

func Init(ctx context.Context) func() error {
	stop := []func() error{
		initLogger(),
		initMetrics(),
		initTracing(ctx),
	}

	tracer = otel.Tracer(app_name)
	reverse(stop)

	return func() error {
		log.Println("flushing logs...")
		errs := make([]error, len(stop))
		for i, fn := range stop {
			errs[i] = fn()
		}
		log.Println("all stopped.")
		return multierr.Combine(errs...)
	}
}

type logzwriter struct {
	pkg       string
	goversion string
	hostname  string

	w io.Writer
}

func (l *logzwriter) Write(b []byte) (int, error) {
	i := 0
	for _, sp := range bytes.Split(b, []byte("\n")) {
		msg := struct {
			Message   string `json:"message"`
			Host      string `json:"host"`
			GoVersion string `json:"go_version"`
			Package   string `json:"pkg"`
			App       string `json:"app"`
		}{
			Message:   strings.TrimSpace(string(sp)),
			Host:      l.hostname,
			GoVersion: l.goversion,
			Package:   l.pkg,
			App:       app_name,
		}

		if msg.Message == "" || strings.HasPrefix(msg.Message, "#") {
			continue
		}

		b, err := json.Marshal(msg)
		if err != nil {
			return 0, err
		}

		j, err := l.w.Write(b)

		i += j
		if err != nil {
			return i, err
		}
	}
	return i, nil
}

func initLogger() func() error {
	log.SetPrefix("[" + app_name + "] ")
	log.SetFlags(log.LstdFlags&^(log.Ldate|log.Ltime) | log.Lshortfile)

	token := env("LOGZIO_LOG_TOKEN", "")
	if token == "" {
		return nil
	}

	l, err := logzio.New(
		token,
		// logzio.SetDebug(os.Stderr),
		logzio.SetUrl(env("LOGZIO_LOG_URL", "https://listener.logz.io:8071")),
		logzio.SetDrainDuration(time.Second*5),
		logzio.SetTempDirectory(env("LOGZIO_DIR", os.TempDir())),
		logzio.SetCheckDiskSpace(true),
		logzio.SetDrainDiskThreshold(70),
	)
	if err != nil {
		return nil
	}

	w := io.MultiWriter(os.Stderr, lzw(l))
	log.SetOutput(w)
	otel.SetLogger(stdr.New(log.Default()))

	return func() error {
		defer log.Println("logger stopped")
		log.SetOutput(os.Stderr)
		l.Stop()
		return nil
	}
}
func lzw(l io.Writer) io.Writer {
	lz := &logzwriter{
		w: l,
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		lz.goversion = info.GoVersion
		lz.pkg = info.Path
	}
	if hostname, err := os.Hostname(); err == nil {
		lz.hostname = hostname
	}

	return lz
}

func initMetrics() func() error {
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

	config := metricsExporter.Config{
		LogzioMetricsListener: env("LOGZIO_METRIC_URL", "https://listener.logz.io:8053"),
		LogzioMetricsToken:    env("LOGZIO_METRIC_TOKEN", ""),
		RemoteTimeout:         30 * time.Second,
		PushInterval:          5 * time.Second,
	}
	if config.LogzioMetricsToken == "" {
		return nil
	}

	// Use the `config` instance from last step.

	cont, err := metricsExporter.InstallNewPipeline(
		config,
		basic.WithCollectPeriod(30*time.Second),
		basic.WithResource(
			resource.NewWithAttributes(
				semconv.SchemaURL,
				attribute.String("app", app_name),
				attribute.String("host", host),
				attribute.String("go_version", goversion),
				attribute.String("pkg", pkg),
			),
		),
	)
	if err != nil {
		return nil
	}

	global.SetMeterProvider(cont)
	runtime.Start()

	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		defer log.Println("metrics stopped")
		return cont.Stop(ctx)
	}
}

func initTracing(ctx context.Context) func() error {
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String("sour.is-ev"),
		),
	)
	if err != nil {
		log.Println(wrap(err, "failed to create trace resource"))
		return nil
	}

	traceExporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithInsecure(),
		otlptracehttp.WithEndpoint("localhost:4318"),
	)
	if err != nil {
		log.Println(wrap(err, "failed to create trace exporter"))
		return nil
	}
	bsp := sdktrace.NewBatchSpanProcessor(traceExporter)
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithResource(res),
		sdktrace.WithSpanProcessor(bsp),
	)
	otel.SetTracerProvider(tracerProvider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return func() error {
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

func htrace(h http.Handler, name string) http.Handler {
	return otelhttp.NewHandler(h, name)
}
