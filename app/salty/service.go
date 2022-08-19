package salty

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"

	"github.com/keys-pub/keys"
	"github.com/sour-is/ev/internal/logz"
	"github.com/sour-is/ev/pkg/es"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/gql"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.uber.org/multierr"
)

type service struct {
	baseURL string
	es      *es.EventStore

	Mresolver_create_salty_user syncint64.Counter
	Mresolver_salty_user        syncint64.Counter
}
type contextKey struct {
	name string
}

var saltyKey = contextKey{"salty"}

type SaltyResolver interface {
	CreateSaltyUser(ctx context.Context, nick string, pub string) (*SaltyUser, error)
	SaltyUser(ctx context.Context, nick string) (*SaltyUser, error)
}

func New(ctx context.Context, es *es.EventStore, baseURL string) (*service, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	if err := event.Register(ctx, &UserRegistered{}); err != nil {
		return nil, err
	}
	if err := event.RegisterName(ctx, "domain.UserRegistered", &UserRegistered{}); err != nil {
		return nil, err
	}

	m := logz.Meter(ctx)

	svc := &service{baseURL: baseURL, es: es}

	var err, errs error
	svc.Mresolver_create_salty_user, err = m.SyncInt64().Counter("resolver_create_salty_user")
	errs = multierr.Append(errs, err)

	svc.Mresolver_salty_user, err = m.SyncInt64().Counter("resolver_salty_user")
	errs = multierr.Append(errs, err)
	span.RecordError(err)

	return svc, errs
}

func (s *service) BaseURL() string {
	if s == nil {
		return "http://missing.context/"
	}
	return s.baseURL
}
func (s *service) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	ctx, span := logz.Span(ctx)
	defer span.End()

	addr := "saltyuser-" + strings.TrimPrefix(r.URL.Path, "/.well-known/salty/")
	addr = strings.TrimSuffix(addr, ".json")

	span.AddEvent(fmt.Sprint("find ", addr))
	a, err := es.Update(ctx, s.es, addr, func(ctx context.Context, agg *SaltyUser) error { return nil })
	switch {
	case errors.Is(err, event.ErrShouldExist):
		span.RecordError(err)

		w.WriteHeader(http.StatusNotFound)
		return
	case err != nil:
		span.RecordError(err)

		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	err = json.NewEncoder(w).Encode(
		struct {
			Endpoint string `json:"endpoint"`
			Key      string `json:"key"`
		}{
			Endpoint: path.Join(s.baseURL, a.inbox.String()),
			Key:      a.pubkey.ID().String(),
		})
	if err != nil {
		span.RecordError(err)
	}
}
func (s *service) CreateSaltyUser(ctx context.Context, nick string, pub string) (*SaltyUser, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	s.Mresolver_create_salty_user.Add(ctx, 1)

	streamID := fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(nick))))
	span.AddEvent(streamID)

	key, err := keys.NewEdX25519PublicKeyFromID(keys.ID(pub))
	if err != nil {
		span.RecordError(err)
		return nil, err
	}

	a, err := es.Create(ctx, s.es, streamID, func(ctx context.Context, agg *SaltyUser) error {
		return agg.OnUserRegister(nick, key)
	})
	switch {
	case errors.Is(err, es.ErrShouldNotExist):
		span.RecordError(err)
		return nil, fmt.Errorf("user exists")

	case err != nil:
		span.RecordError(err)
		return nil, fmt.Errorf("internal error")
	}

	return a, nil
}
func (s *service) SaltyUser(ctx context.Context, nick string) (*SaltyUser, error) {
	ctx, span := logz.Span(ctx)
	defer span.End()

	s.Mresolver_salty_user.Add(ctx, 1)

	streamID := fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(nick))))
	span.AddEvent(streamID)

	a, err := es.Update(ctx, s.es, streamID, func(ctx context.Context, agg *SaltyUser) error { return nil })
	switch {
	case errors.Is(err, es.ErrShouldExist):
		span.RecordError(err)
		return nil, fmt.Errorf("user not found")

	case err != nil:
		span.RecordError(err)
		return nil, fmt.Errorf("%w internal error", err)
	}

	return a, err
}
func (s *service) GetMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r = r.WithContext(gql.ToContext(r.Context(), saltyKey, s))
			next.ServeHTTP(w, r)
		})
	}
}
