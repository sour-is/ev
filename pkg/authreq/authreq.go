package authreq

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"hash/fnv"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v4"
)

var SignatureLifetime = 30 * time.Minute
var AuthHeader = "Authorization"

func Sign(req *http.Request, key ed25519.PrivateKey) (*http.Request, error) {
	pub := enc([]byte(key.Public().(ed25519.PublicKey)))

	h := fnv.New128a()
	fmt.Fprint(h, req.Method, req.URL.String())

	if req.Body != nil {
		b := &bytes.Buffer{}
		w := io.MultiWriter(h, b)
		_, err := io.Copy(w, req.Body)
		if err != nil {
			return req, err
		}
		req.Body = io.NopCloser(b)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.RegisteredClaims{
		Subject:   enc(h.Sum(nil)),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(SignatureLifetime)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		Issuer:    pub,
	})

	sig, err := token.SignedString(key)
	if err != nil {
		return req, err
	}

	req.Header.Set(AuthHeader, sig)

	return req, nil
}

func Authorization(hdlr http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		auth := req.Header.Get(AuthHeader)
		if auth == "" {
			rw.WriteHeader(http.StatusUnauthorized)
			return
		}

		h := fnv.New128a()
		fmt.Fprint(h, req.Method, req.URL.String())

		if req.Body != nil {
			b := &bytes.Buffer{}
			w := io.MultiWriter(h, b)
			_, err := io.Copy(w, req.Body)
			if err != nil {
				rw.WriteHeader(http.StatusBadRequest)
			}
		}

		subject := enc(h.Sum(nil))
		token, err := jwt.ParseWithClaims(
			string(auth),
			&jwt.RegisteredClaims{},
			func(tok *jwt.Token) (any, error) {
				c, ok := tok.Claims.(*jwt.RegisteredClaims)
				if !ok {
					return nil, fmt.Errorf("wrong type of claim")
				}

				pub, err := dec(c.Issuer)
				return ed25519.PublicKey(pub), err
			},
			jwt.WithValidMethods([]string{jwt.SigningMethodEdDSA.Alg()}),
			jwt.WithJSONNumber(),
		)
		if err != nil {
			rw.WriteHeader(http.StatusBadRequest)
			return
		}

		c, ok := token.Claims.(*jwt.RegisteredClaims)
		if !ok {
			rw.WriteHeader(http.StatusUnprocessableEntity)

			return
		}

		req = req.WithContext(context.WithValue(req.Context(), contextKey, c))

		if c.Subject != subject {
			rw.WriteHeader(http.StatusForbidden)
			return
		}

		hdlr.ServeHTTP(rw, req)
	})
}

func enc(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
func dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

var contextKey = struct{ name string }{"jwtClaim"}

func FromContext(ctx context.Context) *jwt.RegisteredClaims {
	if v := ctx.Value(contextKey); v != nil {
		if c, ok := v.(*jwt.RegisteredClaims); ok {
			return c
		}
	}

	return nil
}
