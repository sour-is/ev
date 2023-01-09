package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/sour-is/ev"
	"github.com/sour-is/ev/app/salty"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/env"
	"github.com/sour-is/ev/pkg/service"
	"github.com/sour-is/ev/pkg/slice"
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable Salty")
	eventstore, ok := slice.Find[*ev.EventStore](svc.Services...)
	if !ok {
		return fmt.Errorf("*es.EventStore not found in services")
	}

	addr := "localhost"
	if ht, ok := slice.Find[*http.Server](svc.Services...); ok {
		addr = ht.Addr
	}

	base, err := url.JoinPath(env.Default("SALTY_BASE_URL", "http://"+addr), "inbox")
	if err != nil {
		span.RecordError(err)
		return err
	}

	salty, err := salty.New(ctx, eventstore, base)
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.Add(salty)

	return nil
})
