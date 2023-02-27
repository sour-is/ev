package mux_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/matryer/is"
	"go.sour.is/ev/pkg/mux"
)

type mockHTTP struct {
	onServeHTTP      func()
	onServeAPIv1     func()
	onServeWellKnown func()
}

func (*mockHTTP) ServeFn(fn func()) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) { fn() }
}
func (h *mockHTTP) RegisterHTTP(mux *http.ServeMux) {
	mux.HandleFunc("/", h.ServeFn(h.onServeHTTP))
}
func (h *mockHTTP) RegisterAPIv1(mux *http.ServeMux) {
	mux.HandleFunc("/ping", h.ServeFn(h.onServeAPIv1))
}
func (h *mockHTTP) RegisterWellKnown(mux *http.ServeMux) {
	mux.HandleFunc("/echo", h.ServeFn(h.onServeWellKnown))
}

func TestHttp(t *testing.T) {
	is := is.New(t)

	called := false
	calledAPIv1 := false
	calledWellKnown := false

	mux := mux.New()
	mux.Add(&mockHTTP{
		func() { called = true },
		func() { calledAPIv1 = true },
		func() { calledWellKnown = true },
	})

	is.True(mux != nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	mux.ServeHTTP(w, r)

	is.True(called)
	is.True(!calledAPIv1)
	is.True(!calledWellKnown)
}

func TestHttpAPIv1(t *testing.T) {
	is := is.New(t)

	called := false
	calledAPIv1 := false
	calledWellKnown := false

	mux := mux.New()
	mux.Add(&mockHTTP{
		func() { called = true },
		func() { calledAPIv1 = true },
		func() { calledWellKnown = true },
	})

	is.True(mux != nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/ping", nil)
	mux.ServeHTTP(w, r)

	is.True(!called)
	is.True(calledAPIv1)
	is.True(!calledWellKnown)
}

func TestHttpWellKnown(t *testing.T) {
	is := is.New(t)

	called := false
	calledAPIv1 := false
	calledWellKnown := false

	mux := mux.New()
	mux.Add(&mockHTTP{
		func() { called = true },
		func() { calledAPIv1 = true },
		func() { calledWellKnown = true },
	})

	is.True(mux != nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/.well-known/echo", nil)
	mux.ServeHTTP(w, r)

	is.True(!called)
	is.True(!calledAPIv1)
	is.True(calledWellKnown)
}
