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
	"net/url"
	"strings"

	"github.com/golang-jwt/jwt/v4"
	"github.com/sour-is/ev"
	"github.com/sour-is/ev/internal/lg"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/set"
)

type service struct {
	es   *ev.EventStore
	self set.Set[string]
}

type Option interface {
	ApplyWebfinger(s *service)
}

type WithHostnames []string

func (o WithHostnames) ApplyWebfinger(s *service) {
	s.self = set.New(o...)
}

func New(ctx context.Context, es *ev.EventStore, opts ...Option) (*service, error) {
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

	for _, o := range opts {
		o.ApplyWebfinger(svc)
	}

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

				c.SetProperty(NSpubkey, &c.PubKey)

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
			var auth *JRD

			// does the target have a pubkey for self auth?
			if _, ok := a.Properties[NSpubkey]; ok {
				auth = a
			}

			// Check current version for auth.
			if authID, ok := a.Properties[NSauth]; ok && authID != nil && auth == nil {
				auth = &JRD{}
				auth.SetStreamID(StreamID(*authID))
				err := s.es.Load(ctx, auth)
				if err != nil {
					return err
				}
			}
			if a.Version() == 0 || a.IsDeleted() {
				// else does the new object claim auth from another object?
				if authID, ok := c.Properties[NSauth]; ok && authID != nil && auth == nil {
					auth = &JRD{}
					auth.SetStreamID(StreamID(*authID))
					err := s.es.Load(ctx, auth)
					if err != nil {
						return err
					}
				}

				// fall back to use auth from submitted claims
				if auth == nil {
					auth = c.JRD
				}
			}

			if auth == nil {
				return fmt.Errorf("auth not found")
			}

			err = a.OnAuth(c.JRD, auth)
			if err != nil {
				return err
			}

			if r.Method == http.MethodDelete {
				return a.OnDelete(c.JRD)
			}
			return a.OnClaims(c.JRD)
		})

		if err != nil {
			w.WriteHeader(http.StatusUnprocessableEntity)
			fmt.Fprint(w, http.StatusText(http.StatusUnprocessableEntity), ": ", err.Error())
			span.RecordError(err)

			return
		}

		if version := a.Version(); r.Method == http.MethodDelete && version > 0 {
			err = s.es.Truncate(ctx, a.StreamID(), int64(version))
			span.RecordError(err)
		}

		w.Header().Set("Content-Type", "application/jrd+json")
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusCreated)
		}
		j := json.NewEncoder(w)
		j.SetIndent("", "  ")
		err = j.Encode(a)
		span.RecordError(err)

	case http.MethodGet:
		resource := r.URL.Query().Get("resource")
		rels := r.URL.Query()["rel"]

		if u := Parse(resource); u != nil && !s.self.Has(u.URL.Hostname()) {
			redirect := &url.URL{}
			redirect.Scheme = "https"
			redirect.Host = u.URL.Host
			redirect.RawQuery = r.URL.RawQuery
			redirect.Path = "/.well-known/webfinger"
			w.Header().Set("location", redirect.String())
			w.WriteHeader(http.StatusSeeOther)
			return
		}

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

		if len(rels) > 0 {
			a.Links = a.GetLinksByRel(rels...)
		}

		if a.Properties != nil {
			if redirect, ok := a.Properties[NSredirect]; ok && redirect != nil {
				w.Header().Set("location", *redirect)
				w.WriteHeader(http.StatusSeeOther)
				return
			}
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
