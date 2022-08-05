package event

import (
	"bytes"
	"crypto/rand"
	"fmt"
	"io"
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
func (lis Events) First() Event {
	if len(lis) == 0 {
		return nilEvent
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
		return nilEvent
	}
	return lis[len(lis)-1]
}
func (lis Events) MarshalText() ([]byte, error) {
	b := &bytes.Buffer{}
	for i := range lis {
		txt, err := MarshalText(lis[i])
		if err != nil {
			return nil, err
		}
		b.Write(txt)
	}
	return b.Bytes(), nil
}

func TypeOf(e Event) string {
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
	e.SetEventMeta(meta)
}

type Meta struct {
	EventID  ulid.ULID
	StreamID string
	Position uint64
}

func (m Meta) Time() time.Time {
	return ulid.Time(m.EventID.Time())
}

type _nilEvent struct{}

func (_nilEvent) EventMeta() Meta {
	return Meta{}
}
func (_nilEvent) SetEventMeta(eventMeta Meta) {}

var nilEvent _nilEvent
