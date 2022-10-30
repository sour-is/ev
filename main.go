package main

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"time"

	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/app/gql"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/peerfinder"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/driver/projecter"
	resolvelinks "github.com/sour-is/ev/pkg/es/driver/resolve-links"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/set"
)

const AppName string = "sour.is-ev"

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel()
	}()

	ctx, stop := lg.Init(ctx, AppName)
	defer stop()

	if err := run(ctx); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
func run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	{
		ctx, span := lg.Span(ctx)

		err := multierr.Combine(
			es.Init(ctx),
			event.Init(ctx),
			diskstore.Init(ctx),
			memstore.Init(ctx),
		)
		if err != nil {
			span.RecordError(err)
			return err
		}

		es, err := es.Open(
			ctx,
			env("EV_DATA", "mem:"),
			streamer.New(ctx),
			projecter.New(
				ctx,
				projecter.DefaultProjection,
				peerfinder.Projector,
			),
			resolvelinks.New(),
		)
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

		enable := set.New(strings.Fields(env("EV_ENABLE", "salty msgbus gql peers"))...)
		var svcs []interface{ RegisterHTTP(*http.ServeMux) }

		svcs = append(svcs, es)

		if enable.Has("salty") {
			span.AddEvent("Enable Salty")
			base, err := url.JoinPath(env("EV_BASE_URL", "http://"+s.Addr), "inbox")
			if err != nil {
				span.RecordError(err)
				return err
			}

			salty, err := salty.New(ctx, es, base)
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
		svcs = append(svcs, lg.NewHTTP(ctx), RegisterHTTP(func(mux *http.ServeMux) {
			mux.Handle("/", http.RedirectHandler("/playground", http.StatusTemporaryRedirect))
		}))

		s.Handler = httpMux(svcs...)

		log.Print("Listen on ", s.Addr)
		span.AddEvent("begin listen and serve on " + s.Addr)

		Mup, err := lg.Meter(ctx).SyncInt64().UpDownCounter("up")
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
