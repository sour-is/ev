package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.sour.is/ev"
	"go.sour.is/ev/app/salty"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/env"
	"go.sour.is/ev/pkg/service"
	"go.sour.is/ev/pkg/slice"
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

	var opts []salty.Option

	base, err := url.JoinPath(env.Default("SALTY_BASE_URL", "http://"+addr), "inbox")
	if err != nil {
		span.RecordError(err)
		return err
	}
	opts = append(opts, salty.WithBaseURL(base))

	if p := env.Default("SALTY_BLOB_DIR", ""); p != "" {
		if strings.HasPrefix(p, "~/") {
			home, _ := os.UserHomeDir()
			p = filepath.Join(home, strings.TrimPrefix(p, "~/"))
		}
		opts = append(opts, salty.WithBlobStore(p))
	}

	salty, err := salty.New(
		ctx,
		eventstore,
		opts...,
	)
	if err != nil {
		span.RecordError(err)
		return err
	}
	svc.Add(salty)

	return nil
})
