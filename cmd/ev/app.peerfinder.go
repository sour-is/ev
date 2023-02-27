package main

import (
	"context"
	"fmt"

	"go.sour.is/ev"
	"go.sour.is/ev/app/peerfinder"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/env"
	"go.sour.is/ev/pkg/es/driver/projecter"
	"go.sour.is/ev/pkg/service"
	"go.sour.is/ev/pkg/slice"
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable Peers")

	eventstore, ok := slice.Find[*ev.EventStore](svc.Services...)
	if !ok {
		return fmt.Errorf("*es.EventStore not found in services")
	}
	eventstore.Option(projecter.New(ctx, peerfinder.Projector))

	peers, err := peerfinder.New(ctx, eventstore, env.Secret("PEER_STATUS", "").Secret())
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.RunOnce(ctx, peers.RefreshJob)
	svc.NewCron("0,15,30,45", peers.RefreshJob)
	svc.RunOnce(ctx, peers.CleanJob)
	svc.NewCron("0 1", peers.CleanJob)
	svc.OnStart(peers.Run)
	svc.OnStop(peers.Stop)

	svc.Add(peers)

	return nil
})
