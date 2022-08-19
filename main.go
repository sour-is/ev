package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/rs/cors"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/app/gql"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
)

const AppName string = "sour.is-ev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel()
	}()

	ctx, stop := logz.Init(ctx, AppName)
	defer stop()

	if err := run(ctx); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	{
		ctx, span := logz.Span(ctx)

		diskstore.Init(ctx)
		memstore.Init(ctx)

		es, err := es.Open(ctx, env("EV_DATA", "file:data"), streamer.New(ctx))
		if err != nil {
			span.RecordError(err)
			return err
		}

		s := http.Server{
			Addr: env("EV_HTTP", ":8080"),
		}

		salty, err := salty.New(ctx, es, path.Join(env("EV_BASE_URL", "http://localhost"+s.Addr), "inbox"))
		if err != nil {
			span.RecordError(err)
			return err
		}

		msgbus, err := msgbus.New(ctx, es)
		if err != nil {
			span.RecordError(err)
			return err
		}

		gql, err := gql.New(ctx, msgbus, salty)
		if err != nil {
			span.RecordError(err)
			return err
		}

		s.Handler = httpMux(logz.NewHTTP(ctx), msgbus, salty, gql)

		log.Print("Listen on ", s.Addr)
		span.AddEvent("begin listen and serve")

		Mup, err := logz.Meter(ctx).SyncInt64().UpDownCounter("up")
		if err != nil {
			return err
		}
		Mup.Add(ctx, 1)

		g.Go(s.ListenAndServe)

		g.Go(func() error {
			<-ctx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return s.Shutdown(ctx)
		})

		span.End()
	}

	if err := g.Wait(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}
func env(name, defaultValue string) string {
	if v := os.Getenv(name); v != "" {
		log.Println("# ", name, " = ", v)
		return v
	}
	return defaultValue
}
func httpMux(fns ...interface{ RegisterHTTP(*http.ServeMux) }) http.Handler {
	mux := http.NewServeMux()
	for _, fn := range fns {
		fn.RegisterHTTP(mux)
	}
	return cors.AllowAll().Handler(mux)
}
