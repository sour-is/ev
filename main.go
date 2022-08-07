package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/sour-is/ev/api/gql_ev"
	"github.com/sour-is/ev/internal/graph"
	"github.com/sour-is/ev/internal/graph/generated"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/service"
	"github.com/sour-is/ev/pkg/playground"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel()
	}()

	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
func run(ctx context.Context) error {
	diskstore.Init(ctx)
	memstore.Init(ctx)

	es, err := es.Open(ctx, env("EV_DATA", "file:data"))
	if err != nil {
		return err
	}

	svc, err := service.New(ctx, es)
	if err != nil {
		return err
	}

	res := graph.New(gql_ev.New(es))
	gql := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: res}))

	s := http.Server{
		Addr: env("EV_HTTP", ":8080"),
	}
	http.Handle("/", playground.Handler("GraphQL playground", "/gql"))
	http.Handle("/gql", res.ChainMiddlewares(gql))
	http.Handle("/event/", http.StripPrefix("/event/", svc))

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
