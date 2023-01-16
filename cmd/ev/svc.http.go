package main

import (
	"context"
	"log"
	"net/http"
	"strings"

	"github.com/rs/cors"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/env"
	"github.com/sour-is/ev/pkg/mux"
	"github.com/sour-is/ev/pkg/service"
	"github.com/sour-is/ev/pkg/slice"
)

var _ = apps.Register(20, func(ctx context.Context, svc *service.Harness) error {
	s := &http.Server{
		
	}
	svc.Add(s)

	mux := mux.New()
	s.Handler = cors.AllowAll().Handler(mux)

	// s.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	// 	log.Println(r.URL.Path)
	// 	mux.ServeHTTP(w, r)
	// })

	s.Addr = env.Default("EV_HTTP", ":8080")
	if strings.HasPrefix(s.Addr, ":") {
		s.Addr = "[::]" + s.Addr
	}
	svc.OnStart(func(ctx context.Context) error {
		_, span := lg.Span(ctx)
		defer span.End()

		log.Print("Listen on ", s.Addr)
		span.AddEvent("begin listen and serve on " + s.Addr)

		mux.Add(slice.FilterType[interface{ RegisterHTTP(*http.ServeMux) }](svc.Services...)...)
		return s.ListenAndServe()
	})
	svc.OnStop(s.Shutdown)

	return nil
})
