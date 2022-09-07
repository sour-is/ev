package salty

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/keys-pub/keys"
	"github.com/sour-is/ev/internal/lg"
)

// Config represents a Salty Config for a User which at a minimum is required
// to have an Endpoint and Key (Public Key)
type Config struct {
	Endpoint string `json:"endpoint"`
	Key      string `json:"key"`
}

type Capabilities struct {
	AcceptEncoding string
}

func (c Capabilities) String() string {
	if c.AcceptEncoding == "" {
		return "<nil>"
	}
	return fmt.Sprint("accept-encoding: ", c.AcceptEncoding)
}

type Addr struct {
	User   string
	Domain string

	capabilities     Capabilities
	discoveredDomain string
	dns              DNSResolver
	endpoint         *url.URL
	key              *keys.EdX25519PublicKey
}

// ParseAddr parsers a Salty Address for a user into it's user and domain
// parts and returns an Addr object with the User and Domain and a method
// for returning the expected User's Well-Known URI
func (s *service) ParseAddr(addr string) (*Addr, error) {
	parts := strings.Split(strings.ToLower(addr), "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("expected nick@domain found %q", addr)
	}

	return &Addr{User: parts[0], Domain: parts[1], dns: s.dns}, nil
}

func (a *Addr) String() string {
	return fmt.Sprintf("%s@%s", a.User, a.Domain)
}

// Hash returns the Hex(SHA256Sum()) of the Address
func (a *Addr) Hash() string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.ToLower(a.String()))))
}

// URI returns the Well-Known URI for this Addr
func (a *Addr) URI() string {
	return fmt.Sprintf("https://%s/.well-known/salty/%s.json", a.DiscoveredDomain(), a.User)
}

// HashURI returns the Well-Known HashURI for this Addr
func (a *Addr) HashURI() string {
	return fmt.Sprintf("https://%s/.well-known/salty/%s.json", a.DiscoveredDomain(), a.Hash())
}

// DiscoveredDomain returns the discovered domain (if any) of fallbacks to the Domain
func (a *Addr) DiscoveredDomain() string {
	if a.discoveredDomain != "" {
		return a.discoveredDomain
	}
	return a.Domain
}

func (a *Addr) Refresh(ctx context.Context) error {
	ctx, span := lg.Span(ctx)
	defer span.End()

	span.AddEvent(fmt.Sprintf("Looking up SRV record for _salty._tcp.%s", a.Domain))
	if _, srv, err := a.dns.LookupSRV(ctx, "salty", "tcp", a.Domain); err == nil {
		if len(srv) > 0 {
			a.discoveredDomain = strings.TrimSuffix(srv[0].Target, ".")
		}
		span.AddEvent(fmt.Sprintf("Discovered salty services %s", a.discoveredDomain))
	} else if err != nil {
		span.RecordError(fmt.Errorf("error looking up SRV record for _salty._tcp.%s : %s", a.Domain, err))
	}

	config, cap, err := fetchConfig(ctx, a.HashURI())
	if err != nil {
		// Fallback to plain user nick
		span.RecordError(err)

		config, cap, err = fetchConfig(ctx, a.URI())
	}
	if err != nil {
		err = fmt.Errorf("error looking up user %s: %w", a, err)
		span.RecordError(err)
		return err
	}
	key, err := keys.NewEdX25519PublicKeyFromID(keys.ID(config.Key))
	if err != nil {
		err = fmt.Errorf("error parsing public key %s: %w", config.Key, err)
		span.RecordError(err)
		return err
	}
	a.key = key

	u, err := url.Parse(config.Endpoint)
	if err != nil {
		err = fmt.Errorf("error parsing endpoint %s: %w", config.Endpoint, err)
		span.RecordError(err)
		return err
	}
	a.endpoint = u
	a.capabilities = cap

	span.AddEvent(fmt.Sprintf("Discovered endpoint: %v", a.endpoint))
	span.AddEvent(fmt.Sprintf("Discovered capability: %v", a.capabilities))

	return nil
}

func fetchConfig(ctx context.Context, addr string) (config Config, cap Capabilities, err error) {
	ctx, span := lg.Span(ctx)
	defer span.End()

	var req *http.Request
	req, err = http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
	if err != nil {
		return
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}

	if err = json.NewDecoder(res.Body).Decode(&config); err != nil {
		return
	}

	cap.AcceptEncoding = res.Header.Get("Accept-Encoding")

	return
}
