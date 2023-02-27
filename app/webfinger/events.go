package webfinger

import (
	"encoding/json"

	"go.sour.is/ev/pkg/es/event"
)

type SubjectSet struct {
	Subject    string             `json:"subject"`
	Aliases    []string           `json:"aliases,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`

	eventMeta event.Meta
}

func (e *SubjectSet) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *SubjectSet) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *SubjectSet) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *SubjectSet) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

var _ event.Event = (*SubjectSet)(nil)

type SubjectDeleted struct {
	Subject string `json:"subject"`

	eventMeta event.Meta
}

func (e *SubjectDeleted) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *SubjectDeleted) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *SubjectDeleted) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *SubjectDeleted) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

var _ event.Event = (*SubjectDeleted)(nil)

type LinkSet struct {
	Rel        string             `json:"rel"`
	Type       string             `json:"type,omitempty"`
	HRef       string             `json:"href,omitempty"`
	Titles     map[string]string  `json:"titles,omitempty"`
	Properties map[string]*string `json:"properties,omitempty"`

	eventMeta event.Meta
}

func (e *LinkSet) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *LinkSet) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *LinkSet) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *LinkSet) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

var _ event.Event = (*LinkSet)(nil)

type LinkDeleted struct {
	Rel string `json:"rel"`

	eventMeta event.Meta
}

func (e *LinkDeleted) EventMeta() event.Meta {
	if e == nil {
		return event.Meta{}
	}
	return e.eventMeta
}
func (e *LinkDeleted) SetEventMeta(m event.Meta) {
	if e != nil {
		e.eventMeta = m
	}
}
func (e *LinkDeleted) MarshalBinary() (text []byte, err error) {
	return json.Marshal(e)
}
func (e *LinkDeleted) UnmarshalBinary(b []byte) error {
	return json.Unmarshal(b, e)
}

var _ event.Event = (*LinkDeleted)(nil)
