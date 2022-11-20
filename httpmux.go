package main

import (
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

type RegisterHTTP func(*http.ServeMux)

func (fn RegisterHTTP) RegisterHTTP(mux *http.ServeMux) {
	fn(mux)
}
