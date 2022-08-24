package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"time"

	"github.com/rs/cors"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/app/gql"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/peerfinder"
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

		if strings.HasPrefix(s.Addr, ":") {
			s.Addr = "[::]" + s.Addr
		}

		enable := set(strings.Fields(env("EV_ENABLE", "salty msgbus gql peers"))...)
		var svcs []interface{ RegisterHTTP(*http.ServeMux) }

		if enable.Has("salty") {
			span.AddEvent("Enable Salty")
			salty, err := salty.New(ctx, es, path.Join(env("EV_BASE_URL", "http://"+s.Addr), "inbox"))
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, salty)
		}

		if enable.Has("msgbus") {
			span.AddEvent("Enable Msgbus")
			msgbus, err := msgbus.New(ctx, es)
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, msgbus)
		}

		if enable.Has("peers") {
			span.AddEvent("Enable Peers")
			peers, err := peerfinder.New(ctx, es)
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, peers)
		}

		if enable.Has("gql") {
			span.AddEvent("Enable GraphQL")
			gql, err := gql.New(ctx, svcs...)
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, gql)
		}
		svcs = append(svcs, logz.NewHTTP(ctx))

		s.Handler = httpMux(svcs...)

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
	name = strings.TrimSpace(name)
	defaultValue = strings.TrimSpace(defaultValue)
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		log.Println("#", name, "=", v)
		return v
	}
	log.Println("#", name, "=", defaultValue, "(default)")
	return defaultValue
}
func httpMux(fns ...interface{ RegisterHTTP(*http.ServeMux) }) http.Handler {
	mux := http.NewServeMux()
	for _, fn := range fns {
		fn.RegisterHTTP(mux)
	}
	return cors.AllowAll().Handler(mux)
}

type Set[T comparable] map[T]struct{}

func set[T comparable](items ...T) Set[T] {
	s := make(map[T]struct{}, len(items))
	for i := range items {
		s[items[i]] = struct{}{}
	}
	return s
}
func (s Set[T]) Has(v T) bool {
	_, ok := (s)[v]
	return ok
}
func (s Set[T]) String() string {
	if s == nil {
		return "set(<nil>)"
	}
	lis := make([]string, 0, len(s))
	for k := range s {
		lis = append(lis, fmt.Sprint(k))
	}

	var b bytes.Buffer
	b.WriteString("set(")
	b.WriteString(strings.Join(lis, ","))
	b.WriteString(")")
	return b.String()
}
