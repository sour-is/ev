package main

import (
	"context"
	"fmt"

	"go.sour.is/ev"
	"go.sour.is/ev/app/msgbus"
	"go.sour.is/ev/pkg/driver/projecter"
	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/service"
	"go.sour.is/pkg/slice"
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable Msgbus")
	eventstore, ok := slice.Find[*ev.EventStore](svc.Services...)
	if !ok {
		return fmt.Errorf("*es.EventStore not found in services")
	}
	eventstore.Option(projecter.New(ctx, msgbus.Projector))

	msgbus, err := msgbus.New(ctx, eventstore)
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.Add(msgbus)

	return nil
})
