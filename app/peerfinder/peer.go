package peerfinder

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"time"

	"github.com/sour-is/ev/pkg/es/event"
)

type Request struct{
	event.AggregateRoot

	RequestIP string `json:"req_ip"`
	Hidden    bool   `json:"hide,omitempty"`

	Responses []Response `json:"responses"`
}

var _ event.Aggregate = (*Request)(nil)
func (a *Request) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e:=e.(type) {
		case *RequestSubmitted:
			a.RequestIP = e.RequestIP
			a.Hidden = e.Hidden
		case *ResultSubmitted:
				a.Responses = append(a.Responses, Response{
					PeerID: e.PeerID,
					PeerVersion: e.PeerVersion,
					Latency: e.Latency,
				})
		}
	}
}

type Response struct {
	PeerID      string  `json:"peer_id"`
	PeerVersion string  `json:"peer_version"`
	Latency     float64 `json:"latency,omitempty"`
}

type RequestSubmitted struct {
	eventMeta event.Meta

	RequestIP string `json:"req_ip"`
	Hidden    bool   `json:"hide,omitempty"`
}

func (r *RequestSubmitted) StreamID() string {
	return r.EventMeta().GetEventID()
}
func (r *RequestSubmitted) RequestID() string {
	return r.EventMeta().GetEventID()
}
func (r *RequestSubmitted) Created() time.Time {
	return r.EventMeta().Created()
}
func (r *RequestSubmitted) CreatedString() string {
	return r.Created().Format("2006-01-02 15:04:05")
}
func (r *RequestSubmitted) Family() int {
	if r == nil {
		return 0
	}

	ip, err := netip.ParseAddr(r.RequestIP)
	switch {
	case err != nil:
		return 0
	case ip.Is4():
		return 1
	default:
		return 2
	}
}

var _ event.Event = (*RequestSubmitted)(nil)

func (e *RequestSubmitted) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *RequestSubmitted) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *RequestSubmitted) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *RequestSubmitted) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}
func (e *RequestSubmitted) MarshalEnviron() ([]byte, error) {
	if e == nil {
		return nil, nil
	}

	var b bytes.Buffer
	b.WriteString("REQ_ID=")
	b.WriteString(e.RequestID())
	b.WriteRune('\n')

	b.WriteString("REQ_IP=")
	b.WriteString(e.RequestIP)
	b.WriteRune('\n')

	b.WriteString("REQ_FAMILY=")
	if family := e.Family(); family > 0 {
		b.WriteString(strconv.Itoa(family))
	}
	b.WriteRune('\n')

	b.WriteString("REQ_CREATED=")
	b.WriteString(e.CreatedString())
	b.WriteRune('\n')

	return b.Bytes(), nil
}

type ResultSubmitted struct {
	eventMeta event.Meta

	RequestID   string  `json:"req_id"`
	PeerID      string  `json:"peer_id"`
	PeerVersion string  `json:"peer_version"`
	Latency     float64 `json:"latency,omitempty"`
}

func (r *ResultSubmitted) Created() time.Time {
	return r.eventMeta.Created()
}

var _ event.Event = (*ResultSubmitted)(nil)

func (e *ResultSubmitted) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *ResultSubmitted) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *ResultSubmitted) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *ResultSubmitted) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}
func (e *ResultSubmitted) String() string {
	return fmt.Sprintf("id: %s\npeer: %s\nversion: %s\nlatency: %0.4f", e.RequestID, e.PeerID, e.PeerVersion, e.Latency)
}

type Info struct {
	ScriptVersion string `json:"script_version"`

	event.AggregateRoot
}

var _ event.Aggregate = (*Info)(nil)

func (a *Info) ApplyEvent(lis ...event.Event) {
	for _, e := range lis {
		switch e := e.(type) {
		case *VersionChanged:
			a.ScriptVersion = e.ScriptVersion
		}
	}
}
func (a *Info) MarshalEnviron() ([]byte, error) {
	var b bytes.Buffer

	b.WriteString("SCRIPT_VERSION=")
	b.WriteString(a.ScriptVersion)
	b.WriteRune('\n')

	return b.Bytes(), nil
}
func (a *Info) OnCreate() error {
	if a.StreamVersion() == 0 {
		event.Raise(a, &VersionChanged{ScriptVersion: initVersion})
	}
	return nil
}

type VersionChanged struct {
	ScriptVersion string `json:"script_version"`

	eventMeta event.Meta
}

var _ event.Event = (*VersionChanged)(nil)

func (e *VersionChanged) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *VersionChanged) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *VersionChanged) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *VersionChanged) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}
