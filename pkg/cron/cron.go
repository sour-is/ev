package cron

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/locker"
	"github.com/sour-is/ev/pkg/set"
	"go.opentelemetry.io/otel/attribute"
	"golang.org/x/sync/errgroup"
)

type task func(context.Context, time.Time) error
type job struct {
	Month, Weekday, Day,
	Hour, Minute, Second *set.BoundSet[int8]
	Task task
}

var DefaultGranularity = time.Minute

type state struct {
	queue []task
}
type cron struct {
	jobs        []job
	state       *locker.Locked[state]
	granularity time.Duration
}

func New(granularity time.Duration) *cron {
	return &cron{granularity: granularity, state: locker.New(&state{})}
}

func parseInto(c string, s *set.BoundSet[int8]) *set.BoundSet[int8] {
	if c == "*" || c == "" {
		s.AddRange(0, 100)
	}
	for _, split := range strings.Split(c, ",") {
		minmax := strings.SplitN(split, "-", 2)
		switch len(minmax) {
		case 2:
			min, _ := strconv.ParseInt(minmax[0], 10, 8)
			max, _ := strconv.ParseInt(minmax[1], 10, 8)
			s.AddRange(int8(min), int8(max))
		default:
			min, _ := strconv.ParseInt(minmax[0], 10, 8)
			s.Add(int8(min))
		}
	}
	return s
}

// This function creates a new job that occurs at the given day and the given
// 24hour time. Any of the values may be -1 as an "any" match, so passing in
// a day of -1, the event occurs every day; passing in a second value of -1, the
// event will fire every second that the other parameters match.
func (c *cron) NewCron(expr string, task func(context.Context, time.Time) error) {
	sp := append(strings.Fields(expr), make([]string, 5)...)[:5]

	job := job{
		Month:   parseInto(sp[4], set.NewBoundSet[int8](1, 12)),
		Weekday: parseInto(sp[3], set.NewBoundSet[int8](0, 6)),
		Day:     parseInto(sp[2], set.NewBoundSet[int8](1, 31)),
		Hour:    parseInto(sp[1], set.NewBoundSet[int8](0, 23)),
		Minute:  parseInto(sp[0], set.NewBoundSet[int8](0, 59)),
		Task:    task,
	}
	c.jobs = append(c.jobs, job)
}
func (c *cron) RunOnce(ctx context.Context, once func(context.Context, time.Time) error) {
	c.state.Modify(ctx, func(ctx context.Context, state *state) error {
		state.queue = append(state.queue, once)
		return nil
	})
}

func (cj job) Matches(t time.Time) (ok bool) {
	return cj.Month.Has(int8(t.Month())) &&
		cj.Day.Has(int8(t.Day())) &&
		cj.Weekday.Has(int8(t.Weekday()%7)) &&
		cj.Hour.Has(int8(t.Hour())) &&
		cj.Minute.Has(int8(t.Minute()))
}

func (cj job) String() string {
	return fmt.Sprintf("job[\n m:%s\n h:%s\n d:%s\n w:%s\n M:%s\n]", cj.Minute, cj.Hour, cj.Day, cj.Weekday, cj.Month)
}

func (c *cron) Run(ctx context.Context) error {
	tick := time.NewTicker(c.granularity)
	defer tick.Stop()

	go c.run(ctx, time.Now())

	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-tick.C:
			// fmt.Println(now.Second(), now.Hour(), now.Day(), int8(now.Weekday()), uint8(now.Month()))
			go c.run(ctx, now)
		}
	}
}

func (c *cron) run(ctx context.Context, now time.Time) {
	var run []task
	ctx, span := lg.Span(ctx)
	defer span.End()

	// Add Jitter
	timer := time.NewTimer(time.Duration(rand.Intn(300)) * time.Millisecond)
	select {
	case <-ctx.Done():
		timer.Stop()
		return
	case <-timer.C:
	}

	span.AddEvent("Cron Run: " + now.Format(time.RFC822))
	// fmt.Println("Cron Run: ", now.Format(time.RFC822))

	c.state.Modify(ctx, func(ctx context.Context, state *state) error {
		run = append(run, state.queue...)
		state.queue = state.queue[:0]

		return nil
	})

	for _, j := range c.jobs {
		if j.Matches(now) {
			span.AddEvent(j.String())
			run = append(run, j.Task)
		}
	}

	if len(run) == 0 {
		return
	}

	wg, _ := errgroup.WithContext(ctx)

	for i := range run {
		fn := run[i]
		wg.Go(func() error { return fn(ctx, now) })
	}
	span.SetAttributes(
		attribute.String("tick", now.String()),
		attribute.Int("count", len(run)),
	)

	err := wg.Wait()
	span.RecordError(err)
}
