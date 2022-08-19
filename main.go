package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/ravilushqa/otelgqlgen"
	"github.com/rs/cors"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/api/gql_ev"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/playground"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/graph/generated"
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

	Mup, err := logz.Meter(ctx).SyncInt64().UpDownCounter("up")
	if err != nil {
		log.Fatal(err)
	}
	Mup.Add(ctx, 1)

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

		saltySVC, err := salty.New(ctx, es, path.Join(env("EV_BASE_URL", "http://localhost" + s.Addr), "inbox"))
		if err != nil {
			span.RecordError(err)
			return err
		}

		msgbusSVC, err := msgbus.New(ctx, es)
		if err != nil {
			span.RecordError(err)
			return err
		}

		res, err := gql_ev.New(ctx, msgbusSVC, saltySVC)
		if err != nil {
			span.RecordError(err)
			return err
		}
		gql := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: res}))
		gql.Use(otelgqlgen.Middleware())


		mux := http.NewServeMux()

		mux.Handle("/", playground.Handler("GraphQL playground", "/gql"))
		mux.Handle("/gql", logz.Htrace(res.ChainMiddlewares(gql), "gql"))
		mux.Handle("/metrics", logz.PromHTTP(ctx))

		mux.Handle("/inbox/", logz.Htrace(http.StripPrefix("/inbox/", msgbusSVC), "inbox"))
		mux.Handle("/.well-known/salty/", logz.Htrace(saltySVC, "lookup"))

		s.Handler = cors.AllowAll().Handler(mux)

		log.Print("Listen on ", s.Addr)
		span.AddEvent("begin listen and serve")

		g.Go(s.ListenAndServe)

		g.Go(func() error {
			<-ctx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return s.Shutdown(ctx)
		})

		span.End()
	}
	return g.Wait()
}
func env(name, defaultValue string) string {
	if v := os.Getenv(name); v != "" {
		log.Println("# ", name, " = ", v)
		return v
	}
	return defaultValue
}
