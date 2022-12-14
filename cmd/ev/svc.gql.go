package main

import (
	"context"
	"net/http"

	"github.com/sour-is/ev/app/gql"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/gql/resolver"
	"github.com/sour-is/ev/pkg/mux"
	"github.com/sour-is/ev/pkg/service"
	"github.com/sour-is/ev/pkg/slice"
)

var _ = apps.Register(90, func(ctx context.Context, svc *service.Harness) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent("Enable GraphQL")
	gql, err := resolver.New(ctx, &gql.Resolver{}, slice.FilterType[resolver.IsResolver](svc.Services...)...)
	if err != nil {
		span.RecordError(err)
		return err
	}
	gql.CheckOrigin = func(r *http.Request) bool {
		switch r.Header.Get("Origin") {
		case "https://ev.sour.is", "https://www.graphqlbin.com", "http://localhost:8080":
			return true
		default:
			return false
		}
	}

	svc.Add(gql)
	svc.Add(mux.RegisterHTTP(func(mux *http.ServeMux) {
		mux.Handle("/", http.RedirectHandler("/playground", http.StatusTemporaryRedirect))
	}))

	return nil
})
