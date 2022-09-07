package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"
)

type mockHTTP struct {
	onServeHTTP func()
}

func (m *mockHTTP) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	m.onServeHTTP()
}

func (h *mockHTTP) RegisterHTTP(mux *http.ServeMux) {
	mux.Handle("/", h)
}
func (h *mockHTTP) RegisterAPIv1(mux *http.ServeMux) {
	mux.Handle("/ping", h)
}

func TestHttpMux(t *testing.T) {
	is := is.New(t)

	called := false

	mux := httpMux(&mockHTTP{func() { called = true }})

	is.True(mux != nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	mux.ServeHTTP(w, r)

	is.True(called)
}
