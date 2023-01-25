package webfinger

import (
	"crypto/ed25519"
	"encoding/base64"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/oklog/ulid/v2"
)

var (
	defaultExpire   = 30 * time.Minute
	defaultIssuer   = "sour.is/webfinger"
	defaultAudience = "sour.is/webfinger"
)

func NewSignedRequest(jrd *JRD, key ed25519.PrivateKey) (string, error) {
	type claims struct {
		PubKey string `json:"pub"`
		*JRD
		jwt.RegisteredClaims
	}
	pub := []byte(key.Public().(ed25519.PublicKey))

	j := claims{
		PubKey: enc(pub),
		JRD:    jrd.CloneValues(),
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        ulid.Make().String(),
			Subject:   jrd.Subject,
			Audience:  jwt.ClaimStrings{defaultAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(defaultExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    defaultIssuer,
		},
	}
	j.JRD.Subject = "" // move subject into registered claims.

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, &j)
	return token.SignedString(key)
}

func enc(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
