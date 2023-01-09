package service

import (
	"context"
	"log"
	"net/http"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/cron"
	"go.opentelemetry.io/otel/attribute"
	"go.uber.org/multierr"
	"golang.org/x/sync/errgroup"
)

type crontab interface {
	NewCron(expr string, task func(context.Context, time.Time) error)
	RunOnce(ctx context.Context, once func(context.Context, time.Time) error)
}
type Harness struct {
	crontab

	Services []any

	onStart []func(context.Context) error
	onStop  []func(context.Context) error
}

func (s *Harness) Setup(ctx context.Context, apps ...application) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	// setup crontab
	c := cron.New(cron.DefaultGranularity)
	s.OnStart(c.Run)
	s.crontab = c

	var err error
	for _, app := range apps {
		err = multierr.Append(err, app(ctx, s))
	}

	span.RecordError(err)
	return err
}
func (s *Harness) OnStart(fn func(context.Context) error) {
	s.onStart = append(s.onStart, fn)
}
func (s *Harness) OnStop(fn func(context.Context) error) {
	s.onStop = append(s.onStop, fn)
}
func (s *Harness) Add(svcs ...any) {
	s.Services = append(s.Services, svcs...)
}
func (s *Harness) stop(ctx context.Context) error {
	g, _ := errgroup.WithContext(ctx)
	for i := range s.onStop {
		fn := s.onStop[i]
		g.Go(func() error {
			if err := fn(ctx); err != nil && err != http.ErrServerClosed {
				return err
			}
			return nil
		})
	}
	return g.Wait()
}
func (s *Harness) Run(ctx context.Context, appName, version string) error {
	{
		ctx, span := lg.Span(ctx)

		log.Println(appName, version)
		span.SetAttributes(
			attribute.String("app", appName),
			attribute.String("version", version),
		)

		Mup, err := lg.Meter(ctx).SyncInt64().UpDownCounter("up")
		if err != nil {
			return err
		}
		Mup.Add(ctx, 1)

		span.End()
	}

	g, _ := errgroup.WithContext(ctx)
	g.Go(func() error {
		<-ctx.Done()
		// shutdown jobs
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		return s.stop(ctx)
	})

	for i := range s.onStart {
		fn := s.onStart[i]
		g.Go(func() error { return fn(ctx) })
	}

	return g.Wait()
}

type application func(context.Context, *Harness) error // Len is the number of elements in the collection.

type appscore struct {
	score int
	application
}
type Apps []appscore

func (a *Apps) Apps() []application {
	sort.Sort(a)
	lis := make([]application, len(*a))
	for i, app := range *a {
		lis[i] = app.application
	}
	return lis
}

// Len is the number of elements in the collection.
func (a *Apps) Len() int {
	if a == nil {
		return 0
	}
	return len(*a)
}

// Less reports whether the element with index i
func (a *Apps) Less(i int, j int) bool {
	if a == nil {
		return false
	}

	return (*a)[i].score < (*a)[j].score
}

// Swap swaps the elements with indexes i and j.
func (a *Apps) Swap(i int, j int) {
	if a == nil {
		return
	}

	(*a)[i], (*a)[j] = (*a)[j], (*a)[i]
}

func (a *Apps) Register(score int, app application) (none struct{}) {
	if a == nil {
		return
	}

	*a = append(*a, appscore{score, app})
	return
}

func AppName() (string, string) {
	if info, ok := debug.ReadBuildInfo(); ok {
		_, name, _ := strings.Cut(info.Main.Path, "/")
		name = strings.Replace(name, "-", ".", -1)
		name = strings.Replace(name, "/", "-", -1)
		return name, info.Main.Version
	}

	return "sour.is-app", "(devel)"
}
