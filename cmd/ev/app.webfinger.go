package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/patrickmn/go-cache"
	"go.sour.is/ev"
	"go.sour.is/ev/app/webfinger"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/env"
	"go.sour.is/ev/pkg/service"
	"go.sour.is/ev/pkg/slice"
)

var (
	defaultExpire   = 3 * time.Minute
	cleanupInterval = 10 * time.Minute
)

var _ = apps.Register(50, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable WebFinger")
	eventstore, ok := slice.Find[*ev.EventStore](svc.Services...)
	if !ok {
		return fmt.Errorf("*es.EventStore not found in services")
	}

	cache := cache.New(defaultExpire, cleanupInterval)
	var withCache webfinger.WithCache = (func(s string) bool {
		if _, ok := cache.Get(s); ok {
			return true
		}
		cache.SetDefault(s, true)
		return false
	})
	var withHostnames webfinger.WithHostnames = strings.Fields(env.Default(" ", "sour.is"))

	wf, err := webfinger.New(ctx, eventstore, withCache, withHostnames)
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.Add(wf)

	return nil
})
