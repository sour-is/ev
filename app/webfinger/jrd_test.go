package webfinger_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/matryer/is"
	"go.sour.is/ev"
	"go.uber.org/multierr"

	"go.sour.is/ev/app/webfinger"
	memstore "go.sour.is/ev/pkg/driver/mem-store"
	"go.sour.is/ev/pkg/driver/projecter"
	"go.sour.is/ev/pkg/driver/streamer"
	"go.sour.is/ev/pkg/event"
)

func TestParseJRD(t *testing.T) {

	// Adapted from spec http://tools.ietf.org/html/rfc6415#appendix-A
	blob := `
        {
              "subject":"http://blog.example.com/article/id/314",
              "aliases":[
                "http://blog.example.com/cool_new_thing",
                "http://blog.example.com/steve/article/7"],
              "properties":{
                "http://blgx.example.net/ns/version":"1.3",
                "http://blgx.example.net/ns/ext":null
              },
              "links":[
                {
                  "rel":"author",
                  "type":"text/html",
                  "href":"http://blog.example.com/author/steve",
                  "titles":{
                    "default":"About the Author",
                    "en-us":"Author Information"
                  },
                  "properties":{
                    "http://example.com/role":"editor"
                  }
                },
                {
                  "rel":"author",
                  "href":"http://example.com/author/john",
                  "titles":{
                    "default":"The other author"
                  }
                },
                {
                  "rel":"copyright"
                }
              ]
            }
        `
	obj, err := webfinger.ParseJRD([]byte(blob))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := obj.Subject, "http://blog.example.com/article/id/314"; got != want {
		t.Errorf("JRD.Subject is %q, want %q", got, want)
	}
	if got, want := obj.GetProperty("http://blgx.example.net/ns/version"), "1.3"; got != want {
		t.Errorf("obj.GetProperty('http://blgx.example.net/ns/version') returned %q, want %q", got, want)
	}
	if got, want := obj.GetProperty("http://blgx.example.net/ns/ext"), ""; got != want {
		t.Errorf("obj.GetProperty('http://blgx.example.net/ns/ext') returned %q, want %q", got, want)
	}
	if obj.GetLinkByRel("copyright") == nil {
		t.Error("obj.GetLinkByRel('copyright') returned nil, want non-nil value")
	}
	if got, want := obj.GetLinkByRel("author").Titles["default"], "About the Author"; got != want {
		t.Errorf("obj.GetLinkByRel('author').Titles['default'] returned %q, want %q", got, want)
	}
	if got, want := obj.GetLinkByRel("author").GetProperty("http://example.com/role"), "editor"; got != want {
		t.Errorf("obj.GetLinkByRel('author').GetProperty('http://example.com/role') returned %q, want %q", got, want)
	}
}

func TestEncodeJRD(t *testing.T) {
	s, err := json.Marshal(&webfinger.JRD{
		Subject: "test",
		Properties: map[string]*string{
			"https://sour.is/ns/prop1": nil,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(s) != `{"subject":"test","properties":{"https://sour.is/ns/prop1":null}}` {
		t.Fatal("output does not match")
	}
}

func TestApplyEvents(t *testing.T) {
	is := is.New(t)

	events := event.NewEvents(
		&webfinger.SubjectSet{
			Subject: "acct:me@sour.is",
			Properties: map[string]*string{
				"https://sour.is/ns/pubkey": ptr("kex1d330ama4vnu3vll5dgwjv3k0pcxsccc5k2xy3j8khndggkszsmsq3hl4ru"),
			},
		},
		&webfinger.LinkSet{
			Index: 0,
			Rel:   "salty:public",
			Type:  "application/json+salty",
		},
		&webfinger.LinkSet{
			Index: 1,
			Rel:   "salty:private",
			Type:  "application/json+salty",
		},
		&webfinger.LinkSet{
			Index: 0,
			Rel:   "salty:public",
			Type:  "application/json+salty",
			HRef:  "https://ev.sour.is/inbox/01GAEMKXYJ4857JQP1MJGD61Z5",
			Properties: map[string]*string{
				"pub": ptr("kex1r8zshlvkc787pxvauaq7hd6awa9kmheddxjj9k80qmenyxk6284s50uvpw"),
			},
		},
		&webfinger.LinkDeleted{
			Index: 1,
			Rel:   "salty:private",
		},
	)
	event.SetStreamID(webfinger.StreamID("acct:me@sour.is"), events...)

	jrd := &webfinger.JRD{}
	jrd.ApplyEvent(events...)

	s, err := json.Marshal(jrd)
	if err != nil {
		t.Fatal(err)
	}

	is.Equal(string(s), `{"subject":"acct:me@sour.is","properties":{"https://sour.is/ns/pubkey":"kex1d330ama4vnu3vll5dgwjv3k0pcxsccc5k2xy3j8khndggkszsmsq3hl4ru"},"links":[{"rel":"salty:public","type":"application/json+salty","href":"https://ev.sour.is/inbox/01GAEMKXYJ4857JQP1MJGD61Z5","properties":{"pub":"kex1r8zshlvkc787pxvauaq7hd6awa9kmheddxjj9k80qmenyxk6284s50uvpw"}}]}`)

	events = event.NewEvents(
		&webfinger.SubjectDeleted{},
	)
	event.SetStreamID(webfinger.StreamID("acct:me@sour.is"), events...)

	jrd.ApplyEvent(events...)

	s, err = json.Marshal(jrd)
	if err != nil {
		t.Fatal(err)
	}

	t.Log(string(s))
	if string(s) != `{}` {
		t.Fatal("output does not match")
	}
}

func TestCommands(t *testing.T) {
	is := is.New(t)
	ctx := context.Background()

	pub, priv, err := ed25519.GenerateKey(nil)
	is.NoErr(err)

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
		"sub":     "acct:me@sour.is",
		"pub":     enc(pub),
		"aliases": []string{"acct:xuu@sour.is"},
		"properties": map[string]*string{
			"https://example.com/ns/asdf": nil,
			webfinger.NSpubkey:            ptr(enc(pub)),
		},
		"links": []map[string]any{{
			"rel":    "salty:public",
			"type":   "application/json+salty",
			"href":   "https://ev.sour.is",
			"titles": map[string]string{"default": "Jon Lundy"},
			"properties": map[string]*string{
				"pub": ptr("kex140fwaena9t0mrgnjeare5zuknmmvl0vc7agqy5yr938vusxfh9ys34vd2p"),
			},
		}},
		"exp": time.Now().Add(30 * time.Second).Unix(),
	})
	aToken, err := token.SignedString(priv)
	is.NoErr(err)

	es, err := ev.Open(ctx, "mem:", streamer.New(ctx), projecter.New(ctx))
	is.NoErr(err)

	type claims struct {
		Subject string `json:"sub"`
		PubKey  string `json:"pub"`
		*webfinger.JRD
		jwt.StandardClaims
	}

	token, err = jwt.ParseWithClaims(
		aToken,
		&claims{},
		func(tok *jwt.Token) (any, error) {
			c, ok := tok.Claims.(*claims)
			if !ok {
				return nil, fmt.Errorf("wrong type of claim")
			}

			c.JRD.Subject = c.Subject
			c.StandardClaims.Subject = c.Subject

			c.SetProperty(webfinger.NSpubkey, &c.PubKey)

			pub, err := dec(c.PubKey)
			return ed25519.PublicKey(pub), err
		},
		jwt.WithValidMethods([]string{"EdDSA"}),
		jwt.WithJSONNumber(),
	)
	is.NoErr(err)

	c, ok := token.Claims.(*claims)
	is.True(ok)

	t.Logf("%#v", c)
	a, err := ev.Upsert(ctx, es, webfinger.StreamID(c.Subject), func(ctx context.Context, a *webfinger.JRD) error {
		var auth *webfinger.JRD

		// does the target have a pubkey for self auth?
		if _, ok := a.Properties[webfinger.NSpubkey]; ok {
			auth = a
		}

		// Check current version for auth.
		if authID, ok := a.Properties[webfinger.NSauth]; ok && authID != nil && auth == nil {
			auth = &webfinger.JRD{}
			auth.SetStreamID(webfinger.StreamID(*authID))
			err := es.Load(ctx, auth)
			if err != nil {
				return err
			}
		}
		if a.Version() == 0 || a.IsDeleted() {
			// else does the new object claim auth from another object?
			if authID, ok := c.Properties[webfinger.NSauth]; ok && authID != nil && auth == nil {
				auth = &webfinger.JRD{}
				auth.SetStreamID(webfinger.StreamID(*authID))
				err := es.Load(ctx, auth)
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

		return a.OnClaims(c.JRD)
	})
	is.NoErr(err)

	for _, e := range a.Events(false) {
		t.Log(e)
	}
}

func ptr[T any](v T) *T {
	return &v
}
func enc(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
func dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

func TestMain(m *testing.M) {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()

	err := multierr.Combine(
		ev.Init(ctx),
		event.Init(ctx),
		memstore.Init(ctx),
	)
	if err != nil {
		fmt.Println(err)
		return
	}

	m.Run()
}
