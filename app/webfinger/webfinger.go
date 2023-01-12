package webfinger

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/sour-is/ev"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es/event"
)

type service struct {
	es *ev.EventStore
}

func New(ctx context.Context, es *ev.EventStore) (*service, error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	if err := event.Register(
		ctx,
		&SubjectSet{},
		&SubjectDeleted{},
		&LinkSet{},
		&LinkDeleted{},
	); err != nil {
		return nil, err
	}
	svc := &service{es: es}

	return svc, nil
}

func (s *service) RegisterHTTP(mux *http.ServeMux) {}
func (s *service) RegisterWellKnown(mux *http.ServeMux) {
	mux.Handle("/webfinger", lg.Htrace(s, "webfinger"))
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := lg.Span(ctx)
	defer span.End()

	if r.URL.Path != "/webfinger" {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, http.StatusText(http.StatusNotFound))
		return

	}

	switch r.Method {
	case http.MethodPut, http.MethodDelete:
		if r.ContentLength > 4096 {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			fmt.Fprint(w, http.StatusText(http.StatusRequestEntityTooLarge))
			span.AddEvent("request too large")

			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, http.StatusText(http.StatusInternalServerError))
			span.RecordError(err)

			return
		}
		r.Body.Close()

		type claims struct {
			Subject string `json:"sub"`
			PubKey  string `json:"pub"`
			*JRD
			jwt.StandardClaims
		}

		token, err := jwt.ParseWithClaims(
			string(body),
			&claims{},
			func(tok *jwt.Token) (any, error) {
				c, ok := tok.Claims.(*claims)
				if !ok {
					return nil, fmt.Errorf("wrong type of claim")
				}

				c.JRD.Subject = c.Subject
				c.StandardClaims.Subject = c.Subject

				pub, err := dec(c.PubKey)
				return ed25519.PublicKey(pub), err
			},
			jwt.WithValidMethods([]string{"EdDSA"}),
			jwt.WithJSONNumber(),
		)
		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity), ": ", err.Error())
			span.RecordError(err)

			return
		}

		c, ok := token.Claims.(*claims)
		if !ok {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity))
			span.AddEvent("not a claim")

			return
		}

		a, err := ev.Upsert(ctx, s.es, StreamID(c.Subject), func(ctx context.Context, a *JRD) error {
			if r.Method == http.MethodDelete {
				return a.OnDelete(c.PubKey, c.JRD)
			}
			return a.OnClaims(c.PubKey, c.JRD)
		})

		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity), ": ", err.Error())
			span.RecordError(err)

			return
		}

		w.Header().Set("Content-Type", "application/jrd+json")
		w.WriteHeader(http.StatusCreated)

		j := json.NewEncoder(w)
		j.SetIndent("", "  ")
		err = j.Encode(a)
		span.RecordError(err)

	case http.MethodGet:
		resource := r.URL.Query().Get("resource")

		a := &JRD{}
		a.SetStreamID(StreamID(resource))
		err := s.es.Load(ctx, a)
		if err != nil {
			span.RecordError(err)

			if errors.Is(err, ev.ErrNotFound) {
				w.WriteHeader(http.StatusNotFound)
				fmt.Fprint(w, http.StatusText(http.StatusNotFound))
				return
			}

			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, http.StatusText(http.StatusInternalServerError))
			return
		}

		if a.IsDeleted() {
			w.WriteHeader(http.StatusGone)
			fmt.Fprint(w, http.StatusText(http.StatusGone))
			span.AddEvent("is deleted")

			return
		}

		w.Header().Set("Content-Type", "application/jrd+json")
		w.WriteHeader(http.StatusOK)

		j := json.NewEncoder(w)
		j.SetIndent("", "  ")
		err = j.Encode(a)
		span.RecordError(err)

	default:
		w.Header().Set("Allow", "GET, PUT, DELETE, OPTIONS")
		w.WriteHeader(http.StatusMethodNotAllowed)
		fmt.Fprint(w, http.StatusText(http.StatusMethodNotAllowed))
		span.AddEvent("method not allow: " + r.Method)
	}
}

func dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	return base64.RawURLEncoding.DecodeString(s)
}
