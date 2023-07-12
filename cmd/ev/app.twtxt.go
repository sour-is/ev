package main

import (
	"context"

	"go.sour.is/pkg/lg"
	"go.sour.is/pkg/service"
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	_, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable Twtxt")

	return nil
})
