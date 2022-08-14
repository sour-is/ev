package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/ravilushqa/otelgqlgen"
	"github.com/rs/cors"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/api/gql_ev"
	"github.com/sour-is/ev/internal/graph"
	"github.com/sour-is/ev/internal/graph/generated"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/domain"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
	"github.com/sour-is/ev/pkg/msgbus"
	"github.com/sour-is/ev/pkg/playground"
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
	diskstore.Init(ctx)
	memstore.Init(ctx)
	if err := domain.Init(ctx); err != nil {
		return err
	}

	es, err := es.Open(ctx, env("EV_DATA", "file:data"), streamer.New(ctx))
	if err != nil {
		return err
	}

	svc, err := msgbus.New(ctx, es)
	if err != nil {
		return err
	}

	r, err := gql_ev.New(es)
	if err != nil {
		return err
	}
	res := graph.New(r)
	gql := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: res}))
	gql.Use(otelgqlgen.Middleware())

	s := http.Server{
		Addr: env("EV_HTTP", ":8080"),
	}
	mux := http.NewServeMux()

	mux.Handle("/", playground.Handler("GraphQL playground", "/gql"))
	mux.Handle("/gql", logz.Htrace(res.ChainMiddlewares(gql), "gql"))
	mux.Handle("/inbox/", logz.Htrace(http.StripPrefix("/inbox/", svc), "inbox"))
	mux.Handle("/metrics", logz.PromHTTP(ctx))

	wk := http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintln(w, `{
  "endpoint": "https://ev.sour.is/inbox/01GA4Q3NDX4TPAZ2EZ8E92CQE6",
  "key": "kex1pqwqatj6sge7qaqrsvk4u4yhue4x3vej8znetkwj6a5k0xds2fmqqe3plh"
}`)
		},
	)
	mux.Handle("/.well-known/salty/0ce550020ce36a9932b286b141edd515d33c2b0f51c715445de89ae106345993.json", wk)

	s.Handler = cors.AllowAll().Handler(mux)

	log.Print("Listen on ", s.Addr)
	g, ctx := errgroup.WithContext(ctx)

	g.Go(s.ListenAndServe)

	g.Go(func() error {
		<-ctx.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return s.Shutdown(ctx)
	})

	return g.Wait()
}
func env(name, defaultValue string) string {
	if v := os.Getenv(name); v != "" {
		log.Println("# ", name, " = ", v)
		return v
	}
	return defaultValue
}
