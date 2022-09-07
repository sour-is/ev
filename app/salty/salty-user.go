package salty

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/url"
	"strings"


	"github.com/keys-pub/keys"
	"github.com/oklog/ulid/v2"
	"github.com/sour-is/ev/pkg/es/event"
	"github.com/sour-is/ev/pkg/gql"
)

type SaltyUser struct {
	name   string
	pubkey *keys.EdX25519PublicKey
	inbox  ulid.ULID

	event.AggregateRoot
}

var _ event.Aggregate = (*SaltyUser)(nil)

// ApplyEvent  applies the event to the aggrigate state
func (a *SaltyUser) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *UserRegistered:
			a.name = e.Name
			a.pubkey = e.Pubkey
			a.inbox = e.EventMeta().EventID
			a.SetStreamID(a.streamID())
		default:
			log.Printf("unknown event %T", e)
		}
	}
}

func (a *SaltyUser) streamID() string {
	return fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(a.name))))
}

func (a *SaltyUser) OnUserRegister(name string, pubkey *keys.EdX25519PublicKey) error {
	event.Raise(a, &UserRegistered{Name: name, Pubkey: pubkey})
	return nil
}

func (a *SaltyUser) Nick() string   { return a.name }
func (a *SaltyUser) Inbox() string  { return a.inbox.String() }
func (a *SaltyUser) Pubkey() string { return a.pubkey.String() }
func (s *SaltyUser) Endpoint(ctx context.Context) (string, error) {
	svc := gql.FromContext[contextKey, *service](ctx, saltyKey)
	return url.JoinPath(svc.BaseURL(), s.inbox.String())
}

type UserRegistered struct {
	Name   string
	Pubkey *keys.EdX25519PublicKey

	eventMeta event.Meta
}

var _ event.Event = (*UserRegistered)(nil)

func (e *UserRegistered) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *UserRegistered) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *UserRegistered) MarshalBinary() (text []byte, err error) {
	var b bytes.Buffer
	b.WriteString(e.Name)
	b.WriteRune('\t')
	b.WriteString(e.Pubkey.String())

	return b.Bytes(), nil
}
func (e *UserRegistered) UnmarshalBinary(b []byte) error {
	name, pub, ok := bytes.Cut(b, []byte{'\t'})
	if !ok {
		return fmt.Errorf("parse error")
	}

	var err error
	e.Name = string(name)
	e.Pubkey, err = keys.NewEdX25519PublicKeyFromID(keys.ID(pub))

	return err
}
