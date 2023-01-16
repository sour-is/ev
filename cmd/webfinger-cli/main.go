package main

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/docopt/docopt-go"
	"github.com/golang-jwt/jwt"
	"gopkg.in/yaml.v3"

	"github.com/sour-is/ev/app/webfinger"
	"github.com/sour-is/ev/cmd/webfinger-cli/xdg"
)

var usage = `Webfinger CLI.
usage:
  webfinger-cli gen [--key KEY] [--force]
  webfinger-cli get [--host HOST] <subject> [<rel>...]
  webfinger-cli put [--host HOST] [--key KEY] <filename>
  webfinger-cli rm  [--host HOST] [--key KEY] <subject>

Options:
  --key <key>      From key [default: ` + xdg.Get(xdg.EnvConfigHome, "webfinger/$USER.key") + `]
  --host <host>    Hostname to use [default: https://ev.sour.is]
  --force, -f      Force recreate key for gen
`

type opts struct {
	Gen    bool `docopt:"gen"`
	Get    bool `docopt:"get"`
	Put    bool `docopt:"put"`
	Remove bool `docopt:"rm"`

	Key     string   `docopt:"--key"`
	Host    string   `docopt:"--host"`
	File    string   `docopt:"<filename>"`
	Subject string   `docopt:"<subject>"`
	Rel     []string `docopt:"<rel>"`

	Force bool `docopt:"--force"`
}

func main() {
	o, err := docopt.ParseDoc(usage)
	if err != nil {
		fmt.Println(err)
		os.Exit(2)
	}

	var opts opts
	o.Bind(&opts)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, os.Kill)
	go func() {
		<-ctx.Done()
		defer cancel() // restore interrupt function
	}()

	if err := run(opts); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func run(opts opts) error {
	// fmt.Fprintf(os.Stderr, "%#v\n", opts)

	switch {
	case opts.Gen:
		err := mkKeyfile(opts.Key, opts.Force)
		if err != nil {
			return err
		}
		fmt.Println("wrote keyfile to", opts.Key)
	case opts.Get:
		url, err := url.Parse(opts.Host)
		if err != nil {
			return err
		}

		url.Path = "/.well-known/webfinger"
		query := url.Query()
		query.Set("resource", opts.Subject)
		for _, rel := range opts.Rel {
			query.Add("rel", rel)
		}
		url.RawQuery = query.Encode()

		req, err := http.NewRequest(http.MethodGet, url.String(), nil)
		if err != nil {
			return err
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		s, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}

		fmt.Println(string(s))
	case opts.Remove:
		url, err := url.Parse(opts.Host)
		if err != nil {
			return err
		}

		url.Path = "/.well-known/webfinger"

		key, err := readKeyfile(opts.Key)
		if err != nil {
			return err
		}
		bkey := []byte(key.Public().(ed25519.PublicKey))

		token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, jwt.MapClaims{
			"sub":     opts.Subject,
			"subject": opts.Subject,
			"pub":     enc(bkey),
			"exp":     time.Now().Add(90 * time.Minute).Unix(),
			"iat":     time.Now().Unix(),
			"aud":     "webfinger",
			"iss":     "sour.is-webfingerCLI",
		})
		aToken, err := token.SignedString(key)
		if err != nil {
			return err
		}

		body := strings.NewReader(aToken)
		req, err := http.NewRequest(http.MethodDelete, url.String(), body)
		if err != nil {
			return err
		}

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()

		s, err := io.ReadAll(res.Body)
		if err != nil {
			return err
		}

		fmt.Println(res.Status, string(s))
	case opts.Put:
		url, err := url.Parse(opts.Host)
		if err != nil {
			return err
		}

		url.Path = "/.well-known/webfinger"

		key, err := readKeyfile(opts.Key)
		if err != nil {
			return err
		}
		bkey := []byte(key.Public().(ed25519.PublicKey))

		fmt.Fprintln(os.Stderr, opts.File)
		fp, err := os.Open(opts.File)
		if err != nil {
			return err
		}
		y := yaml.NewDecoder(fp)

		type claims struct {
			Subject string `json:"sub"`
			PubKey  string `json:"pub"`
			*webfinger.JRD
			jwt.StandardClaims
		}

		for err == nil {
			j := claims{
				PubKey: enc(bkey),
				JRD:    &webfinger.JRD{},
				StandardClaims: jwt.StandardClaims{
					Audience:  "sour.is-webfinger",
					ExpiresAt: time.Now().Add(30 * time.Minute).Unix(),
					IssuedAt:  time.Now().Unix(),
					Issuer:    "sour.is-webfingerCLI",
				},
			}

			err = y.Decode(j.JRD)
			if err != nil {
				break
			}

			j.Subject = j.JRD.Subject
			j.StandardClaims.Subject = j.JRD.Subject

			token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, &j)
			aToken, err := token.SignedString(key)
			if err != nil {
				return err
			}

			body := strings.NewReader(aToken)

			req, err := http.NewRequest(http.MethodPut, url.String(), body)
			if err != nil {
				return err
			}

			res, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer res.Body.Close()

			s, err := io.ReadAll(res.Body)
			if err != nil {
				return err
			}

			fmt.Println(res.Status, string(s))

		}
		if err != nil && !errors.Is(err, io.EOF) {
			return err
		}
	}

	return nil
}

func enc(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}
func dec(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	return base64.RawURLEncoding.DecodeString(s)
}

func mkKeyfile(keyfile string, force bool) error {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return err
	}

	err = os.MkdirAll(filepath.Dir(keyfile), 0700)
	if err != nil {
		return err
	}

	_, err = os.Stat(keyfile)
	if !os.IsNotExist(err) {
		if force {
			fmt.Println("removing keyfile", keyfile)
			err = os.Remove(keyfile)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("the keyfile %s exists. use --force", keyfile)
		}
	}

	fp, err := os.OpenFile(keyfile, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	fmt.Fprint(fp, "# pub: ", enc(pub), "\n", enc(priv))

	return fp.Close()
}

func readKeyfile(keyfile string) (ed25519.PrivateKey, error) {
	fd, err := os.Stat(keyfile)
	if err != nil {
		return nil, err
	}

	if fd.Mode()&0066 != 0 {
		return nil, fmt.Errorf("permissions are too weak")
	}

	f, err := os.Open(keyfile)
	scan := bufio.NewScanner(f)

	var key ed25519.PrivateKey
	for scan.Scan() {
		txt := scan.Text()
		if strings.HasPrefix(txt, "#") {
			continue
		}
		if strings.TrimSpace(txt) == "" {
			continue
		}

		txt = strings.TrimPrefix(txt, "# priv: ")
		b, err := dec(txt)
		if err != nil {
			return nil, err
		}
		key = b
	}

	return key, err
}
