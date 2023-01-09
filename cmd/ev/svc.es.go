package main

import (
	"context"

	"github.com/sour-is/ev"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/env"
	"github.com/sour-is/ev/pkg/es"
	diskstore "github.com/sour-is/ev/pkg/es/driver/disk-store"
	memstore "github.com/sour-is/ev/pkg/es/driver/mem-store"
	"github.com/sour-is/ev/pkg/es/driver/projecter"
	resolvelinks "github.com/sour-is/ev/pkg/es/driver/resolve-links"
	"github.com/sour-is/ev/pkg/es/driver/streamer"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/service"
	"go.uber.org/multierr"
)

var _ = apps.Register(10, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	// setup eventstore
	err := multierr.Combine(
		ev.Init(ctx),
		event.Init(ctx),
		diskstore.Init(ctx),
		memstore.Init(ctx),
	)
	if err != nil {
		span.RecordError(err)
		return err
	}

	eventstore, err := ev.Open(
		ctx,
		env.Default("EV_DATA", "mem:"),
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
	svc.Add(eventstore, &es.EventStore{EventStore: eventstore})

	return nil
})
