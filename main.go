package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"

	"github.com/sour-is/ev/app/gql"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/app/peerfinder"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/cron"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/driver/projecter"
	resolvelinks "github.com/sour-is/ev/pkg/es/driver/resolve-links"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/gql/resolver"
	"github.com/sour-is/ev/pkg/set"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel() // restore interrupt function
	}()

	// Initialize logger
	ctx, stop := lg.Init(ctx, appName)
	defer stop()

	// Run application
	if err := run(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}
func run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	stop := &stopFns{}

	cron := cron.New(cron.DefaultGranularity)

	{
		ctx, span := lg.Span(ctx)

		log.Println(appName, version)
		span.SetAttributes(
			attribute.String("app", appName),
			attribute.String("version", version),
		)

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
			resolvelinks.New(),
			streamer.New(ctx),
			projecter.New(
				ctx,
				projecter.DefaultProjection,
			),
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
		var res []resolver.IsResolver

		res = append(res, es)

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
			res = append(res, salty)
		}

		if enable.Has("msgbus") {
			span.AddEvent("Enable Msgbus")
			msgbus, err := msgbus.New(ctx, es)
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, msgbus)
			res = append(res, msgbus)
		}

		if enable.Has("peers") {
			span.AddEvent("Enable Peers")
			es.Option(projecter.New(ctx, peerfinder.Projector))

			peers, err := peerfinder.New(ctx, es, env("PEER_STATUS", ""))
			if err != nil {
				span.RecordError(err)
				return err
			}
			svcs = append(svcs, peers)
			cron.Once(ctx, peers.RefreshJob)
			cron.NewJob("0,15,30,45", peers.RefreshJob)
			cron.Once(ctx, peers.CleanJob)
			cron.NewJob("0 1", peers.CleanJob)
			g.Go(func() error {
				return peers.Run(ctx)
			})
			stop.add(peers.Stop)
		}

		if enable.Has("gql") {
			span.AddEvent("Enable GraphQL")
			gql, err := resolver.New(ctx, &gql.Resolver{}, res...)
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
		stop.add(s.Shutdown)

		span.End()
	}

	g.Go(func() error {
		<-ctx.Done()
		// shutdown jobs

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return stop.stop(ctx)
	})
	g.Go(func() error {
		return cron.Run(ctx)
	})

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

var appName, version = func() (string, string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		_, name, _ := strings.Cut(info.Main.Path, "/")
		name = strings.Replace(name, "-", ".", -1)
		name = strings.Replace(name, "/", "-", -1)
		return name, info.Main.Version
	}

	return "sour.is-ev", "(devel)"
}()

type application interface {
	Setup(context.Context) error
}

type stopFns struct {
	fns []func(context.Context) error
}

func (s *stopFns) add(fn func(context.Context) error) {
	s.fns = append(s.fns, fn)
}
func (s *stopFns) stop(ctx context.Context) error {
	g, _ := errgroup.WithContext(ctx)
	for i := range s.fns {
		fn := s.fns[i]
		g.Go(func() error {
			return fn(ctx)
		})
	}
	return g.Wait()
}
