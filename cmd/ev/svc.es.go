package main

import (
	"context"

	"go.sour.is/ev"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/env"
	"go.sour.is/ev/pkg/es"
	diskstore "go.sour.is/ev/pkg/es/driver/disk-store"
	memstore "go.sour.is/ev/pkg/es/driver/mem-store"
	"go.sour.is/ev/pkg/es/driver/projecter"
	resolvelinks "go.sour.is/ev/pkg/es/driver/resolve-links"
	"go.sour.is/ev/pkg/es/driver/streamer"
	"go.sour.is/ev/pkg/es/event"
	"go.sour.is/ev/pkg/service"
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
