package mux

import (
	"net/http"
)

type mux struct {
	*http.ServeMux
	api       *http.ServeMux
	wellknown *http.ServeMux
}

func (mux *mux) Add(fns ...interface{ RegisterHTTP(*http.ServeMux) }) {
	for _, fn := range fns {
		// log.Printf("HTTP: %T", fn)
		fn.RegisterHTTP(mux.ServeMux)

		if fn, ok := fn.(interface{ RegisterAPIv1(*http.ServeMux) }); ok {
			// log.Printf("APIv1: %T", fn)
			fn.RegisterAPIv1(mux.api)
		}

		if fn, ok := fn.(interface{ RegisterWellKnown(*http.ServeMux) }); ok {
			// log.Printf("WellKnown: %T", fn)
			fn.RegisterWellKnown(mux.wellknown)
		}
	}
}
func New() *mux {
	mux := &mux{
		api:       http.NewServeMux(),
		wellknown: http.NewServeMux(),
		ServeMux:  http.NewServeMux(),
	}
	mux.Handle("/api/v1/", http.StripPrefix("/api/v1", mux.api))
	mux.Handle("/.well-known/", http.StripPrefix("/.well-known", mux.wellknown))

	return mux
}

type RegisterHTTP func(*http.ServeMux)

func (fn RegisterHTTP) RegisterHTTP(mux *http.ServeMux) { fn(mux) }
