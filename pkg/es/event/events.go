// package event implements functionality for working with an eventstore.
package event

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	ulid "github.com/oklog/ulid/v2"
)

var pool = sync.Pool{
	New: func() interface{} { return ulid.Monotonic(rand.Reader, 0) },
}

func getULID() ulid.ULID {
	var entropy io.Reader = rand.Reader
	if e, ok := pool.Get().(io.Reader); ok {
		entropy = e
		defer pool.Put(e)
	}
	return ulid.MustNew(ulid.Now(), entropy)
}

// Event implements functionality of an individual event used with the event store. It should implement the getter/setter for EventMeta and BinaryMarshaler/BinaryUnmarshaler.
type Event interface {
	EventMeta() Meta
	SetEventMeta(Meta)
}

// Events is a list of events
type Events []Event

func NewEvents(lis ...Event) Events {
	for i, e := range lis {
		meta := e.EventMeta()
		meta.Position = uint64(i)
		if meta.ActualPosition == 0 {
			meta.ActualPosition = uint64(i)
		}
		meta.EventID = getULID()
		e.SetEventMeta(meta)
	}
	return lis
}

func (lis Events) StreamID() string {
	if len(lis) == 0 {
		return ""
	}
	return lis.First().EventMeta().StreamID
}
func (lis Events) SetStreamID(streamID string) {
	SetStreamID(streamID, lis...)
}
func (lis Events) Count() int64 {
	return int64(len(lis))
}
func (lis Events) First() Event {
	if len(lis) == 0 {
		return NilEvent
	}
	return lis[0]
}
func (lis Events) Rest() Events {
	if len(lis) == 0 {
		return nil
	}
	return lis[1:]
}
func (lis Events) Last() Event {
	if len(lis) == 0 {
		return NilEvent
	}
	return lis[len(lis)-1]
}

func TypeOf(e any) string {
	if ie, ok := e.(interface{ UnwrapEvent() Event }); ok {
		e = ie.UnwrapEvent()
	}
	if e, ok := e.(interface{ EventType() string }); ok {
		return e.EventType()
	}

	// Default to printed representation for unnamed types
	return strings.Trim(fmt.Sprintf("%T", e), "*")
}

type streamID string

func (s streamID) StreamID() string {
	return string(s)
}

func StreamID(e Event) streamID {
	return streamID(e.EventMeta().StreamID)
}
func SetStreamID(id string, lis ...Event) {
	for _, e := range lis {
		meta := e.EventMeta()
		meta.StreamID = id
		if meta.ActualStreamID == "" {
			meta.ActualStreamID = id
		}
		e.SetEventMeta(meta)
	}
}

func EventID(e Event) ulid.ULID {
	return e.EventMeta().EventID
}
func SetEventID(e Event, id ulid.ULID) {
	meta := e.EventMeta()
	meta.EventID = id
	e.SetEventMeta(meta)
}
func SetPosition(e Event, i uint64) {
	meta := e.EventMeta()
	meta.Position = i
	meta.ActualPosition = i
	e.SetEventMeta(meta)
}

type Meta struct {
	EventID        ulid.ULID
	StreamID       string
	Position       uint64
	ActualStreamID string
	ActualPosition uint64
}

func (m Meta) Created() time.Time {
	return ulid.Time(m.EventID.Time())
}
func (m Meta) GetEventID() string { return m.EventID.String() }

func Init(ctx context.Context) error {
	if err := Register(ctx, NilEvent, &EventPtr{}); err != nil {
		return err
	}
	if err := RegisterName(ctx, "event.eventPtr", &EventPtr{}); err != nil {
		return err
	}
	return nil
}

type nilEvent struct {
	IsEvent
}

var NilEvent = &nilEvent{}

func (e *nilEvent) MarshalBinary() ([]byte, error) {
	return nil, nil
}
func (e *nilEvent) UnmarshalBinary(b []byte) error {
	return nil
}

type EventPtr struct {
	StreamID string `json:"stream_id"`
	Pos      uint64 `json:"pos"`

	IsEvent
}

var _ Event = (*EventPtr)(nil)

func NewPtr(streamID string, pos uint64) *EventPtr {
	return &EventPtr{StreamID: streamID, Pos: pos}
}

// MarshalBinary implements Event
func (e *EventPtr) MarshalBinary() (data []byte, err error) {
	return []byte(fmt.Sprintf("%s@%d", e.StreamID, e.Pos)), nil
}

// UnmarshalBinary implements Event
func (e *EventPtr) UnmarshalBinary(data []byte) error {
	s := string(data)
	idx := strings.LastIndex(s, "@")
	if idx == -1 {
		return fmt.Errorf("missing @ in: %s", s)
	}
	e.StreamID = s[:idx]
	var err error
	e.Pos, err = strconv.ParseUint(s[idx+1:], 10, 64)

	return err
}

func (e *EventPtr) Values() any {
	return struct {
		StreamID string `json:"stream_id"`
		Pos      uint64 `json:"pos"`
	}{
		e.StreamID,
		e.Pos,
	}
}

type FeedTruncated struct {
	IsEvent
}

func (e *FeedTruncated) Values() any {
	return struct {
	}{}
}

type property[T any] struct {
	v T
}

type IsEvent = property[Meta]

func (p *property[T]) EventMeta() T {
	if p == nil {
		var t T
		return t
	}
	return p.v
}

func (p *property[T]) SetEventMeta(x T) {
	if p != nil {
		p.v = x
	}
}
