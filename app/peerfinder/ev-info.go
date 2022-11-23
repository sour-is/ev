package peerfinder

import (
	"bytes"
	"encoding/json"

	"github.com/tj/go-semver"

	"github.com/sour-is/ev/pkg/es/event"
)

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
func (a *Info) OnUpsert() error {
	if a.StreamVersion() == 0 {
		event.Raise(a, &VersionChanged{ScriptVersion: initVersion})
	}
	current, _ := semver.Parse(initVersion)
	previous, _ := semver.Parse(a.ScriptVersion)

	if current.Compare(previous) > 0 {
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
