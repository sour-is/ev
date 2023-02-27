package salty

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"go.opentelemetry.io/otel/metric/instrument"
	"go.opentelemetry.io/otel/metric/instrument/syncint64"
	"go.sour.is/ev/internal/lg"
	"go.sour.is/ev/pkg/authreq"
	"go.uber.org/multierr"
)

var (
	ErrAddressExists = errors.New("error: address already exists")
	ErrBlobNotFound  = errors.New("error: blob not found")
)

func WithBlobStore(path string) *withBlobStore {
	return &withBlobStore{path: path}
}

type withBlobStore struct {
	path string

	m_get_blob    syncint64.Counter
	m_put_blob    syncint64.Counter
	m_delete_blob syncint64.Counter
}

func (o *withBlobStore) ApplySalty(s *service) {}

func (o *withBlobStore) Setup(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var err, errs error

	err = os.MkdirAll(o.path, 0700)
	if err != nil {
		return err
	}

	m := lg.Meter(ctx)
	o.m_get_blob, err = m.SyncInt64().Counter("salty_get_blob",
		instrument.WithDescription("salty get blob called"),
	)
	errs = multierr.Append(errs, err)

	o.m_put_blob, err = m.SyncInt64().Counter("salty_put_blob",
		instrument.WithDescription("salty put blob called"),
	)
	errs = multierr.Append(errs, err)

	o.m_delete_blob, err = m.SyncInt64().Counter("salty_delete_blob",
		instrument.WithDescription("salty delete blob called"),
	)
	errs = multierr.Append(errs, err)

	return errs
}

func (o *withBlobStore) RegisterAPIv1(mux *http.ServeMux) {
	mux.Handle("/blob/", authreq.Authorization(o))
}

func (o *withBlobStore) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := lg.Span(r.Context())
	defer span.End()

	claims := authreq.FromContext(ctx)
	if claims == nil {
		httpError(w, http.StatusUnauthorized)
		return
	}
	signer := claims.Issuer

	key := strings.TrimPrefix(r.URL.Path, "/blob/")

	switch r.Method {
	case http.MethodDelete:
		if err := deleteBlob(o.path, key, signer); err != nil {
			if errors.Is(err, ErrBlobNotFound) {
				httpError(w, http.StatusNotFound)
				return
			}

			span.RecordError(fmt.Errorf("%w: getting blob %s for %s", err, key, signer))
			httpError(w, http.StatusInternalServerError)
			return
		}

		http.Error(w, "Blob Deleted", http.StatusOK)
	case http.MethodGet, http.MethodHead:
		blob, err := getBlob(o.path, key, signer)
		if err != nil {
			if errors.Is(err, ErrBlobNotFound) {
				httpError(w, http.StatusNotFound)
				return
			}

			span.RecordError(fmt.Errorf("%w: getting blob %s for %s", err, key, signer))
			httpError(w, http.StatusInternalServerError)
			return
		}
		defer blob.Close()

		blob.SetHeaders(r)

		if r.Method == http.MethodGet {
			_, _ = io.Copy(w, blob)
		}
	case http.MethodPut:
		data, err := io.ReadAll(r.Body)
		if err != nil {
			httpError(w, http.StatusInternalServerError)
			return
		}
		defer r.Body.Close()

		if err := putBlob(o.path, key, data, signer); err != nil {
			span.RecordError(fmt.Errorf("%w: putting blob %s for %s", err, key, signer))

			httpError(w, http.StatusInternalServerError)
			return
		}

		http.Error(w, "Blob Created", http.StatusCreated)
	default:
		http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
	}
}

func putBlob(path string, key string, data []byte, signer string) error {
	p := filepath.Join(path, signer, key)
	if err := os.MkdirAll(p, 0700); err != nil {
		return fmt.Errorf("error creating blobs paths %s: %w", p, err)
	}
	fn := filepath.Join(p, "content")

	if err := os.WriteFile(fn, data, os.FileMode(0600)); err != nil {
		return fmt.Errorf("error writing blob %s: %w", fn, err)
	}

	return nil
}

func getBlob(path string, key string, signer string) (*Blob, error) {
	p := filepath.Join(path, signer, key)

	if err := os.MkdirAll(p, 0755); err != nil {
		return nil, fmt.Errorf("error creating blobs paths %s: %w", p, err)
	}

	fn := filepath.Join(p, "content")

	if !FileExists(fn) {
		return nil, ErrBlobNotFound
	}

	return OpenBlob(fn)
}

func deleteBlob(path string, key string, signer string) error {

	p := filepath.Join(path, signer, key)

	if !FileExists(p) {
		return ErrBlobNotFound
	}

	return os.RemoveAll(p)
}

// FileExists returns true if the given file exists
func FileExists(name string) bool {
	if _, err := os.Stat(name); err != nil {
		if os.IsNotExist(err) {
			return false
		}
	}
	return true
}

func httpError(w http.ResponseWriter, code int) {
	http.Error(w, http.StatusText(code), code)
}

// Blob defines the type, filename and whether or not a blob is publicly accessible or not.
// A Blob also holds zero or more properties as a map of key/value pairs of string interpreted
// by the client.
type Blob struct {
	r io.ReadSeekCloser `json:"-"`

	Type       string            `json:"type"`
	Public     bool              `json:"public"`
	Filename   string            `json:"-"`
	Properties map[string]string `json:"props"`
}

// Close closes the blob and the underlying io.ReadSeekCloser
func (b *Blob) Close() error { return b.r.Close() }

// Read reads data from the blob from the underlying io.ReadSeekCloser
func (b *Blob) Read(p []byte) (n int, err error) { return b.r.Read(p) }

// SetHeaders sets HTTP headers on the net/http.Request object based on the blob's type, filename
// and various other properties (if any).
func (b *Blob) SetHeaders(r *http.Request) {
	// TODO: Implement this...
}

// OpenBlob opens a blob at the given path and returns a Blob object
func OpenBlob(fn string) (*Blob, error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, fmt.Errorf("%w: opening blob %s", err, fn)
	}
	b := &Blob{r: f, Filename: fn}

	props := filepath.Join(filepath.Dir(fn), "props.json")

	if FileExists(filepath.Dir(props)) {
		pf, err := os.Open(props)
		if err != nil {
			return b, fmt.Errorf("%w: opening blob props %s", err, props)
		}
		err = json.NewDecoder(pf).Decode(b)
		if err != nil {
			return b, fmt.Errorf("%w: opening blob props %s", err, props)
		}
	}

	return b, nil
}
