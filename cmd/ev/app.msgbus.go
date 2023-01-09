package main

import (
	"context"
	"fmt"

	"github.com/sour-is/ev"
	"github.com/sour-is/ev/app/msgbus"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/service"
	"github.com/sour-is/ev/pkg/slice"
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable Msgbus")
	eventstore, ok := slice.Find[*ev.EventStore](svc.Services...)
	if !ok {
		return fmt.Errorf("*es.EventStore not found in services")
	}

	msgbus, err := msgbus.New(ctx, eventstore)
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.Add(msgbus)

	return nil
})
