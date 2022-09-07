package main

import (
	"log"
	"net/http"

	"github.com/rs/cors"
)

type mux struct {
	*http.ServeMux
	api *http.ServeMux
}

func httpMux(fns ...interface{ RegisterHTTP(*http.ServeMux) }) http.Handler {
	mux := newMux()
	for _, fn := range fns {
		fn.RegisterHTTP(mux.ServeMux)

		if fn, ok := fn.(interface{ RegisterAPIv1(*http.ServeMux) }); ok {
			log.Printf("register api %T", fn)
			fn.RegisterAPIv1(mux.api)
		}
	}

	return cors.AllowAll().Handler(mux)
}
func newMux() *mux {
	mux := &mux{
		api:      http.NewServeMux(),
		ServeMux: http.NewServeMux(),
	}
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", mux.api))

	return mux
}
