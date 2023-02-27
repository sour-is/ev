package authreq_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/matryer/is"
	"go.sour.is/ev/pkg/authreq"
)

func TestGETRequest(t *testing.T) {
	is := is.New(t)

	pub, priv, err := ed25519.GenerateKey(nil)
	is.NoErr(err)

	req, err := http.NewRequest(http.MethodGet, "http://example.com/"+enc(pub)+"/test?q=test", nil)
	is.NoErr(err)

	req, err = authreq.Sign(req, priv)
	is.NoErr(err)

	t.Log(enc(pub))
	t.Log(req.Header.Get(authreq.AuthHeader))

	var hdlr http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := authreq.FromContext(r.Context())
		if c == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if !strings.Contains(req.URL.Path, c.Issuer) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	})

	hdlr = authreq.Authorization(hdlr)

	rw := httptest.NewRecorder()

	hdlr.ServeHTTP(rw, req)

	is.Equal(rw.Code, http.StatusOK)
}

func TestPOSTRequest(t *testing.T) {
	is := is.New(t)

	content := "this is post!"

	pub, priv, err := ed25519.GenerateKey(nil)
	is.NoErr(err)

	req, err := http.NewRequest(http.MethodPost, "http://example.com/"+enc(pub)+"/test?q=test", strings.NewReader(content))
	is.NoErr(err)

	req, err = authreq.Sign(req, priv)
	is.NoErr(err)

	t.Log(enc(pub))
	t.Log(req.Header.Get(authreq.AuthHeader))

	var hdlr http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := authreq.FromContext(r.Context())
		if c == nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		contentCheck, err := io.ReadAll(r.Body)
		r.Body.Close()

		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		t.Log(string(contentCheck))
		if !strings.Contains(req.URL.Path, c.Issuer) {
			w.WriteHeader(http.StatusForbidden)
			return
		}
	})

	hdlr = authreq.Authorization(hdlr)

	rw := httptest.NewRecorder()

	hdlr.ServeHTTP(rw, req)

	is.Equal(rw.Code, http.StatusOK)

}

func enc(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
