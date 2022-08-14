package domain

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"strings"

	"github.com/keys-pub/keys"
	"github.com/oklog/ulid/v2"
	"github.com/sour-is/ev/pkg/es/event"
)

func Init(ctx context.Context) error {
	return event.Register(ctx, &UserRegistered{})
}

type SaltyUser struct {
	Name   string
	Pubkey *keys.EdX25519PublicKey
	Inbox  ulid.ULID

	event.AggregateRoot
}

var _ event.Aggregate = (*SaltyUser)(nil)

// ApplyEvent  applies the event to the aggrigate state
func (a *SaltyUser) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *UserRegistered:
			a.Name = e.Name
			a.Pubkey = e.Pubkey
			a.Inbox = e.EventMeta().EventID
			a.SetStreamID(a.streamID())
		default:
			log.Printf("unknown event %T", e)
		}
	}
}

func (a *SaltyUser) streamID() string {
	return fmt.Sprintf("saltyuser-%x", sha256.Sum256([]byte(strings.ToLower(a.Name))))
}

func (a *SaltyUser) OnUserRegister(name string, pubkey *keys.EdX25519PublicKey) error {
	event.Raise(a, &UserRegistered{Name: name, Pubkey: pubkey})
	return nil
}

type UserRegistered struct {
	Name     string
	Pubkey   *keys.EdX25519PublicKey
	Endpoint ulid.ULID

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
