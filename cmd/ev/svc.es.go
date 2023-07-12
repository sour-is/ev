package main

import (
	"context"

	"go.sour.is/pkg/env"
	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/service"
	"go.uber.org/multierr"

	"go.sour.is/ev"
	diskstore "go.sour.is/ev/pkg/driver/disk-store"
	memstore "go.sour.is/ev/pkg/driver/mem-store"
	"go.sour.is/ev/pkg/driver/projecter"
	resolvelinks "go.sour.is/ev/pkg/driver/resolve-links"
	"go.sour.is/ev/pkg/driver/streamer"
	"go.sour.is/ev/pkg/es"
	"go.sour.is/ev/pkg/event"
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
