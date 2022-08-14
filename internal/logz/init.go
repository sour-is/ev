package logz

import (
	"context"
	"log"

	"go.uber.org/multierr"
)

func Init(ctx context.Context, name string) (context.Context, func() error) {
	ctx, span := Span(ctx)
	defer span.End()

	stop := [3]func() error{
		initLogger(name),
	}
	ctx, stop[1] = initMetrics(ctx, name)
	ctx, stop[2] = initTracing(ctx, name)

	reverse(stop[:])

	return ctx, func() error {
		log.Println("flushing logs...")
		errs := make([]error, len(stop))
		for i, fn := range stop {
			if fn != nil {
				errs[i] = fn()
			}
		}
		log.Println("all stopped.")
		return multierr.Combine(errs...)
	}
}
